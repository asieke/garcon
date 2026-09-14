// Package claude runs Claude Code through Garcon without giving up Remote Control.
//
// Claude Code refuses Remote Control (the session showing in the claude.ai app) when
// ANTHROPIC_BASE_URL names any host but api.anthropic.com, which Garcon's proxy route does. Its
// unix-socket transport (ANTHROPIC_UNIX_SOCKET) is exempt from that check, so this subcommand serves a
// private socket for one session, relays it to Garcon's ordinary /claude/<account> route, and hands the
// session its stored claude.ai login. Usage is recorded exactly as with the environment snippet.
package claude

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"garcon/internal/onboarding"
)

// Main runs `garcon claude` and exits with Claude Code's own status.
func Main(args []string) {
	code, err := run(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "garcon claude:", err)
	}
	os.Exit(code)
}

func run(args []string) (int, error) {
	fs := flag.NewFlagSet("garcon claude", flag.ContinueOnError)
	account := fs.String("account", "", "the account label Garcon records this session under, usually the login email")
	dir := fs.String("config-dir", "", "Claude Code configuration directory (default $CLAUDE_CONFIG_DIR or ~/.claude)")
	garcon := fs.String("url", onboarding.DefaultURL, "the running garcon")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: garcon claude --account EMAIL [--config-dir DIR] [--url URL] [-- claude arguments]\n\nRuns claude through Garcon with Remote Control available. Put -- before any claude flag.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	if *account == "" || strings.ContainsAny(*account, "/ \t\r\n") {
		fs.Usage()
		return 2, nil
	}
	if *dir == "" {
		*dir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if *dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 1, err
		}
		*dir = filepath.Join(home, ".claude")
	}
	base, err := onboarding.LocalURL(*garcon)
	if err != nil {
		return 1, err
	}
	exe, err := exec.LookPath("claude")
	if err != nil {
		return 1, errors.New("claude is not on PATH; install Claude Code first")
	}
	s := session{claude: exe, account: *account, dir: *dir, base: base}
	creds, err := Load(s.dir)
	if err != nil {
		return 1, fmt.Errorf("%w; run claude once without garcon to log in, then retry", err)
	}
	if _, err := onboarding.Health(base); err != nil {
		return 1, fmt.Errorf("%w; run garcon service status", err)
	}
	return s.serve(creds, fs.Args())
}

// serve binds the socket, starts Claude Code with it, keeps the login fresh and cleans up after.
func (s session) serve(creds Credentials, claudeArgs []string) (int, error) {
	ln, sock, err := listen()
	if err != nil {
		return 1, err
	}
	target, _ := url.Parse(s.base)
	srv := &http.Server{
		Handler:           handler(s.account, target, func() string { c, _ := Load(s.dir); return c.AccessToken }),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go srv.Serve(ln)
	stop := func() {
		srv.Close()
		ln.Close()
		os.Remove(sock)
	}

	cmd := exec.Command(s.claude, claudeArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(append(os.Environ(),
		"ANTHROPIC_UNIX_SOCKET="+sock,
		"ANTHROPIC_BASE_URL="+s.route(),
		"_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1",
		"CLAUDE_CONFIG_DIR="+s.dir),
		creds.Env()...)
	if err := cmd.Start(); err != nil {
		stop()
		return 1, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	go s.renewLoop(ctx)
	// Ctrl-C reaches the child from the terminal already; only forward what would otherwise stop at us.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range signals {
			cmd.Process.Signal(sig)
		}
	}()

	err = cmd.Wait()
	signal.Stop(signals)
	close(signals)
	cancel()
	stop()

	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if code = exit.ExitCode(); code < 0 {
			code = 1
		}
		err = nil
	}
	return code, err
}
