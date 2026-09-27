// Package claude launches Claude Code with the selected profile. Ordinary launches
// relay a private per-session socket to Garcon's /claude route. Current Claude Code
// versions reject Remote Control with either a custom gateway or this socket.
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
	"syscall"
	"time"

	"garcon/internal/connections"
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
	remote := len(args) > 0 && args[0] == "rc"
	if remote {
		args = args[1:]
	}
	fs := flag.NewFlagSet("garcon claude", flag.ContinueOnError)
	dir := fs.String("config-dir", "", "Claude profile (default $CLAUDE_CONFIG_DIR or ~/.claude)")
	garcon := fs.String("url", onboarding.DefaultURL, "the running garcon")
	register := fs.String("register-launcher", "", "register an already configured profile launcher for dashboard status; does not start Claude")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: garcon claude [rc] [--config-dir DIR] [--url URL] [-- claude arguments]\n\nRuns Claude through Garcon. Use rc to explicitly start one Remote Control session. Put -- before any Claude flag.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	profile, err := profileDir(*dir)
	if err != nil {
		return 1, err
	}
	*dir = profile
	base, err := onboarding.LocalURL(*garcon)
	if err != nil {
		return 1, err
	}
	if *register != "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 1, err
		}
		if err := connections.RegisterClaudeLauncher(home, *register, *dir, base); err != nil {
			return 1, err
		}
		fmt.Println("Claude profile launcher registered. This confirms configuration, not a live Remote Control connection.")
		return 0, nil
	}
	exe, err := exec.LookPath("claude")
	if err != nil {
		return 1, errors.New("claude is not on PATH; install Claude Code first")
	}
	s := session{claude: exe, dir: *dir, base: base}
	creds, err := Load(s.dir)
	if err != nil {
		return 1, fmt.Errorf("%w (profile %s); log in with CLAUDE_CONFIG_DIR=%q claude auth login, then retry", err, s.dir, s.dir)
	}
	if _, err := onboarding.Health(base); err != nil {
		return 1, fmt.Errorf("%w; run garcon service status", err)
	}
	return s.serve(creds, launchArgs(remote, fs.Args()))
}

func profileDir(explicit string) (string, error) {
	if explicit == "" {
		explicit = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if explicit == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		explicit = filepath.Join(home, ".claude")
	}
	return filepath.Abs(explicit)
}

func launchArgs(remote bool, args []string) []string {
	if remote {
		return append([]string{"--remote-control"}, args...)
	}
	return args
}

// serve binds the socket, starts Claude Code with it, keeps the login fresh and cleans up after.
func (s session) serve(creds Credentials, claudeArgs []string) (int, error) {
	ln, sock, err := listen()
	if err != nil {
		return 1, err
	}
	target, _ := url.Parse(s.base)
	srv := &http.Server{
		Handler:           handler(target, func() string { c, _ := Load(s.dir); return c.AccessToken }),
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
