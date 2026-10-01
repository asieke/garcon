// Package delegate runs a one-shot external CLI task. It is not a model provider.
package delegate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const MaxPromptBytes = 1 << 20
const maxOutputBytes = 1 << 20
const maxDiagnosticBytes = 64 << 10
const stopGrace = 300 * time.Millisecond

// Options intentionally has no token, endpoint, extra-argument or session fields.
// The unmodified CLI owns authentication, quota enforcement and model selection.
type Options struct {
	Executable     string
	Directory      string
	AllowInference bool
	Timeout        time.Duration
}

type Result struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// Run sends only the supplied prompt to the CLI, with no tool access or automatic
// project context. Diagnostics are bounded but never echoed: they may contain secrets.
func Run(ctx context.Context, prompt string, o Options) (Result, error) {
	if !o.AllowInference {
		return Result{}, errors.New("inference disabled; review auth and billing, then explicitly pass --allow-inference")
	}
	if strings.TrimSpace(prompt) == "" || len(prompt) > MaxPromptBytes {
		return Result{}, errors.New("prompt must contain 1 to 1048576 bytes of text")
	}
	if o.Timeout <= 0 || o.Timeout > 10*time.Minute {
		return Result{}, errors.New("timeout must be greater than zero and at most 10m")
	}
	if o.Executable == "" {
		o.Executable = "claude"
	}
	if err := directEnvironment(os.Environ()); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	cmd := exec.Command(o.Executable, "--safe-mode", "--print", "--output-format", "json",
		"--tools", "", "--permission-mode", "dontAsk", "--strict-mcp-config",
		"--mcp-config", `{"mcpServers":{}}`, "--no-session-persistence")
	cmd.Dir = o.Directory
	cmd.Stdin = strings.NewReader(prompt)
	// Preserve the existing CLI identity; do not read or rewrite credential storage.
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	stdout := &boundedBuffer{limit: maxOutputBytes, cancel: cancel}
	stderr := &boundedBuffer{limit: maxDiagnosticBytes, cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start external CLI: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		timer := time.NewTimer(stopGrace)
		select {
		case err = <-done:
		case <-timer.C:
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			err = <-done
		}
		timer.Stop()
	}
	// Also remove descendants left holding pipes or ignoring TERM after their parent exits.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if stdout.exceeded || stderr.exceeded {
		return Result{}, errors.New("external CLI output limit exceeded")
	}
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("external CLI stopped: %w", ctx.Err())
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return Result{}, fmt.Errorf("external CLI failed (exit %d); check CLI authentication, quota and supported flags separately", exit.ExitCode())
		}
		return Result{}, fmt.Errorf("external CLI did not finish cleanly: %w", err)
	}
	var result struct {
		Type    string  `json:"type"`
		Subtype string  `json:"subtype"`
		IsError *bool   `json:"is_error"`
		Result  *string `json:"result"`
	}
	if json.Unmarshal(stdout.data.Bytes(), &result) != nil || result.Type != "result" || result.IsError == nil {
		return Result{}, errors.New("external CLI returned invalid result JSON")
	}
	if *result.IsError || result.Subtype != "success" {
		return Result{}, errors.New("external CLI reported a failed result; check authentication and quota separately")
	}
	if result.Result == nil {
		return Result{}, errors.New("external CLI returned invalid result JSON")
	}
	return Result{result.Type, result.Subtype, *result.IsError, *result.Result}, nil
}

// Reject inherited relay/identity-disguise settings instead of silently changing
// them. This command never joins Garcon's credential pools or HTTP relay.
func directEnvironment(env []string) error {
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if value == "" {
			continue
		}
		switch key {
		case "ANTHROPIC_BASE_URL", "ANTHROPIC_UNIX_SOCKET", "_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL":
			return fmt.Errorf("%s is set; launch the bridge from a direct CLI environment", key)
		}
	}
	return nil
}

type boundedBuffer struct {
	data     bytes.Buffer
	mu       sync.Mutex
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := b.limit - b.data.Len()
	if n > remaining {
		b.data.Write(p[:remaining])
		b.exceeded = true
		b.cancel()
	} else {
		b.data.Write(p)
	}
	return n, nil
}
