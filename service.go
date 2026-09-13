// `garcon service <install|uninstall|restart|status>`: run the proxy in the
// background and at every login, as a systemd user unit on Linux or a
// LaunchAgent on macOS. The unit points at this executable, so an upgrade that
// replaces the file in place (npm, install.sh) only needs a restart.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const unitName = "garcon"

func serviceMain(args []string) {
	cmd := "status"
	if len(args) > 0 {
		cmd = args[0]
	}
	var err error
	switch cmd {
	case "install":
		err = serviceInstall()
	case "uninstall":
		err = serviceUninstall()
	case "restart":
		err = serviceRestart()
	case "status":
		err = serviceStatus()
	default:
		err = fmt.Errorf("usage: garcon service install|uninstall|restart|status")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func home() string { h, _ := os.UserHomeDir(); return h }

func unitPath() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home(), "Library/LaunchAgents/dev.garcon.plist")
	}
	return filepath.Join(home(), ".config/systemd/user", unitName+".service")
}

func run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func quiet(name string, args ...string) bool { return exec.Command(name, args...).Run() == nil }

func serviceInstall() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	path := unitPath()
	os.MkdirAll(filepath.Dir(path), 0o755)
	switch runtime.GOOS {
	case "linux":
		unit := "[Unit]\nDescription=Garcon local LLM usage proxy\n\n[Service]\nExecStart=" + exe + "\nRestart=always\n\n[Install]\nWantedBy=default.target\n"
		if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
			return err
		}
		for _, a := range [][]string{{"daemon-reload"}, {"enable", "--now", unitName}, {"restart", unitName}} {
			if err := run("systemctl", append([]string{"--user"}, a...)...); err != nil {
				return err
			}
		}
		fmt.Printf("systemd user unit %q runs %s (systemctl --user status %s)\n", unitName, exe, unitName)
	case "darwin":
		logs := filepath.Join(home(), "Library/Logs")
		os.MkdirAll(logs, 0o755)
		plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
	<key>Label</key><string>dev.garcon</string>
	<key>ProgramArguments</key><array><string>` + exe + `</string></array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>` + logs + `/garcon.log</string>
	<key>StandardErrorPath</key><string>` + logs + `/garcon.log</string>
</dict></plist>
`
		if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
			return err
		}
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		quiet("launchctl", "bootout", domain, path)
		if err := run("launchctl", "bootstrap", domain, path); err != nil {
			return err
		}
		fmt.Printf("LaunchAgent dev.garcon runs %s (log: %s/garcon.log)\n", exe, logs)
	default:
		return errors.New("no service support on " + runtime.GOOS + "; run garcon yourself")
	}
	return nil
}

func serviceUninstall() error {
	path := unitPath()
	switch runtime.GOOS {
	case "linux":
		quiet("systemctl", "--user", "disable", "--now", unitName)
		os.Remove(path)
		quiet("systemctl", "--user", "daemon-reload")
	case "darwin":
		quiet("launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), path)
		os.Remove(path)
	}
	fmt.Println("service removed; usage log and settings kept")
	return nil
}

// serviceRestart is a no-op when no service is installed, so package
// post-install hooks can call it unconditionally.
func serviceRestart() error {
	if _, err := os.Stat(unitPath()); err != nil {
		fmt.Println("no service installed; nothing to restart")
		return nil
	}
	switch runtime.GOOS {
	case "linux":
		return run("systemctl", "--user", "restart", unitName)
	case "darwin":
		return run("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/dev.garcon", os.Getuid()))
	}
	return nil
}

func serviceStatus() error {
	if _, err := os.Stat(unitPath()); err != nil {
		fmt.Println("not installed as a service (garcon service install)")
		return nil
	}
	switch runtime.GOOS {
	case "linux":
		out, _ := exec.Command("systemctl", "--user", "is-active", unitName).Output()
		fmt.Printf("systemd user unit %s: %s\n", unitName, strings.TrimSpace(string(out)))
	case "darwin":
		if quiet("launchctl", "print", fmt.Sprintf("gui/%d/dev.garcon", os.Getuid())) {
			fmt.Println("LaunchAgent dev.garcon: loaded")
		} else {
			fmt.Println("LaunchAgent dev.garcon: not loaded")
		}
	}
	return nil
}
