package delegate

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

// RunCLI reads one bounded prompt from stdin. It writes exactly one normalized
// result on success; failures leave stdout empty and return a diagnostic error.
func RunCLI(ctx context.Context, args []string, in io.Reader, out, diagnostics io.Writer) error {
	fs := flag.NewFlagSet("garcon delegate", flag.ContinueOnError)
	fs.SetOutput(diagnostics)
	allow := fs.Bool("allow-inference", false, "explicitly authorize a CLI invocation after checking identity and billing")
	exe := fs.String("executable", "claude", "path to the unmodified Claude Code executable")
	directory := fs.String("cwd", "", "trusted working directory (default current directory)")
	timeout := fs.Duration("timeout", 2*time.Minute, "total deadline, including reading stdin (maximum 10m)")
	fs.Usage = func() {
		fmt.Fprintln(diagnostics, "usage: garcon delegate --allow-inference [--executable PATH] [--cwd DIR] [--timeout 2m] < prompt.txt\nExternal CLI task only; does not replace Codex's model backend.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("provide the prompt on stdin, not as command arguments")
	}
	if !*allow {
		return errors.New("inference disabled; review auth and billing, then explicitly pass --allow-inference")
	}
	if *timeout <= 0 || *timeout > 10*time.Minute {
		return errors.New("timeout must be greater than zero and at most 10m")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	type input struct {
		data []byte
		err  error
	}
	read := make(chan input, 1)
	// Closing the command process also releases a blocked stdin reader. The timeout
	// must not wait for EOF from an interactive pipe before it can take effect.
	go func() { data, err := io.ReadAll(io.LimitReader(in, MaxPromptBytes+1)); read <- input{data, err} }()
	var prompt input
	select {
	case prompt = <-read:
	case <-ctx.Done():
		return ctx.Err()
	}
	if prompt.err != nil {
		return fmt.Errorf("read prompt: %w", prompt.err)
	}
	result, err := Run(ctx, string(prompt.data), Options{*exe, *directory, *allow, *timeout})
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
