package delegate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test binary plays the external CLI. It never loads an account or calls a model.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--descendant" {
		signal.Ignore(syscall.SIGTERM)
		os.WriteFile("child.pid", []byte(strconv.Itoa(os.Getpid())), 0600)
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "--safe-mode" {
		want := []string{"--safe-mode", "--print", "--output-format", "json", "--tools", "", "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--no-session-persistence"}
		if !reflect.DeepEqual(os.Args[1:], want) {
			os.Exit(98)
		}
		p, _ := io.ReadAll(os.Stdin)
		switch string(p) {
		case "exit":
			fmt.Fprint(os.Stderr, "sensitive diagnostic")
			os.Exit(7)
		case "error":
			fmt.Print(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"private failure"}`)
		case "malformed":
			fmt.Print("not JSON")
		case "incomplete":
			fmt.Print(`{"type":"result","subtype":"success"}`)
		case "trailing":
			fmt.Print(`{"type":"result","subtype":"success","is_error":false,"result":"ok"} junk`)
		case "stdout-limit":
			fmt.Print(strings.Repeat("x", maxOutputBytes+1))
		case "stderr-limit":
			fmt.Fprint(os.Stderr, strings.Repeat("x", maxDiagnosticBytes+1))
		case "tree", "orphan":
			child := exec.Command(os.Args[0], "--descendant")
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if child.Start() != nil {
				os.Exit(97)
			}
			if string(p) == "orphan" {
				os.Exit(0)
			}
			signal.Ignore(syscall.SIGTERM)
			time.Sleep(time.Minute)
		case "hang":
			signal.Ignore(syscall.SIGTERM)
			time.Sleep(time.Minute)
		default:
			json.NewEncoder(os.Stdout).Encode(Result{"result", "success", false, string(p)})
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func options(t *testing.T) Options {
	t.Helper()
	for _, key := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_UNIX_SOCKET", "_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL"} {
		t.Setenv(key, "")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Options{exe, t.TempDir(), true, 5 * time.Second}
}

func TestLiteralPrompt(t *testing.T) {
	o := options(t)
	prompt := "--dangerously-skip-permissions\n$(touch injected); 'quotes' & Unicode café"
	result, err := Run(context.Background(), prompt, o)
	if err != nil || result.Result != prompt {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(o.Directory, "injected")); !os.IsNotExist(err) {
		t.Fatal("prompt executed")
	}
}

func TestFailures(t *testing.T) {
	for _, tc := range []struct{ prompt, message string }{
		{"exit", "exit 7"}, {"error", "failed result"}, {"malformed", "invalid result JSON"},
		{"incomplete", "invalid result JSON"}, {"trailing", "invalid result JSON"},
		{"stdout-limit", "output limit exceeded"}, {"stderr-limit", "output limit exceeded"},
	} {
		t.Run(tc.prompt, func(t *testing.T) {
			_, err := Run(context.Background(), tc.prompt, options(t))
			if err == nil || !strings.Contains(err.Error(), tc.message) || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "private failure") {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestPreflight(t *testing.T) {
	o := options(t)
	o.Executable = filepath.Join(o.Directory, "missing")
	o.AllowInference = false
	if _, err := Run(context.Background(), "ok", o); err == nil || !strings.Contains(err.Error(), "inference disabled") {
		t.Fatal(err)
	}
	o.AllowInference = true
	for _, p := range []string{"", "  ", strings.Repeat("x", MaxPromptBytes+1)} {
		if _, err := Run(context.Background(), p, o); err == nil || !strings.Contains(err.Error(), "prompt must") {
			t.Fatal(err)
		}
	}
	if _, err := Run(context.Background(), "ok", o); err == nil || !strings.Contains(err.Error(), "start external CLI") {
		t.Fatal(err)
	}
	o.Timeout = 0
	if _, err := Run(context.Background(), "ok", o); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatal(err)
	}
}

func TestEnvironment(t *testing.T) {
	for _, key := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_UNIX_SOCKET", "_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL"} {
		if err := directEnvironment([]string{key + "=secret-value"}); err == nil || strings.Contains(err.Error(), "secret-value") {
			t.Fatal(err)
		}
	}
	if err := directEnvironment([]string{"CLAUDE_CONFIG_DIR=/profile", "ANTHROPIC_API_KEY=existing-key"}); err != nil {
		t.Fatal(err)
	}
}

func TestCancellation(t *testing.T) {
	for _, prompt := range []string{"hang", "tree"} {
		t.Run(prompt, func(t *testing.T) {
			o := options(t)
			o.Timeout = 500 * time.Millisecond
			start := time.Now()
			_, err := Run(context.Background(), prompt, o)
			if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
				t.Fatalf("%v, %s", err, time.Since(start))
			}
			if prompt == "tree" {
				assertChildStopped(t, o.Directory)
			}
		})
	}
	o := options(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, "ok", o); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	o = options(t)
	ctx, cancel = context.WithCancel(context.Background())
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	defer timer.Stop()
	if _, err := Run(ctx, "hang", o); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestOrphanPipe(t *testing.T) {
	o := options(t)
	_, err := Run(context.Background(), "orphan", o)
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatal(err)
	}
	assertChildStopped(t, o.Directory)
}

func assertChildStopped(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		// Linux may retain a killed orphan as a zombie until init reaps it.
		if stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil && strings.Contains(string(stat), ") Z ") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("descendant %d still alive", pid)
}

func TestCLI(t *testing.T) {
	o := options(t)
	var out, diagnostics strings.Builder
	if err := RunCLI(context.Background(), nil, strings.NewReader("ok"), &out, &diagnostics); err == nil || out.Len() != 0 {
		t.Fatal(err)
	}
	if err := RunCLI(context.Background(), []string{"--help"}, strings.NewReader(""), &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	args := []string{"--allow-inference", "--executable", o.Executable, "--cwd", o.Directory}
	if err := RunCLI(context.Background(), args, strings.NewReader("literal"), &out, &diagnostics); err != nil || !strings.Contains(out.String(), `"result":"literal"`) {
		t.Fatalf("%v %s", err, out.String())
	}
	out.Reset()
	if err := RunCLI(context.Background(), args, strings.NewReader("error"), &out, &diagnostics); err == nil || out.Len() != 0 {
		t.Fatalf("%v %s", err, out.String())
	}
	in, writer := io.Pipe()
	defer in.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RunCLI(ctx, args, in, &out, &diagnostics); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
