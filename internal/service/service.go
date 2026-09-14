// `garcon service <install|uninstall|restart|status>`: run the proxy in the
// background and at every login, as a systemd user unit on Linux or a
// LaunchAgent on macOS. Install/restart refresh a stable private executable.
package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const unitName = "garcon"

// Main runs `garcon service <cmd>`.
func Main(args []string) {
	cmd := "status"
	if len(args) > 0 {
		cmd = args[0]
	}
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "usage: garcon service install|uninstall|restart|status")
		os.Exit(2)
	}
	var err error
	switch cmd {
	case "install":
		err = Install()
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

func Installed() bool { _, err := os.Stat(unitPath()); return err == nil }

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

// Install refreshes a private service binary so npm cache eviction and Node
// version changes cannot remove the executable used at login.
func Install() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return errors.New("services require macOS or Linux; run garcon in the foreground")
	}
	if os.Geteuid() == 0 {
		return errors.New("run garcon setup as your normal user, without sudo")
	}
	if runtime.GOOS == "linux" && !quiet("systemctl", "--user", "show-environment") {
		return errors.New("systemd user session unavailable; run garcon in a terminal, then garcon setup --no-service")
	}
	destination := filepath.Join(home(), ".local/share/garcon/bin/garcon")
	if err := copyExecutable(exe, destination); err != nil {
		return err
	}
	exe = destination
	path := unitPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "linux":
		if err := os.WriteFile(path, []byte(systemdUnit(exe)), 0o644); err != nil {
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
	<key>ProgramArguments</key><array><string>` + xmlText(exe) + `</string></array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>` + xmlText(logs) + `/garcon.log</string>
	<key>StandardErrorPath</key><string>` + xmlText(logs) + `/garcon.log</string>
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

// Restart refreshes the service executable after an npm or source update.
func serviceRestart() error {
	if _, err := os.Stat(unitPath()); err != nil {
		fmt.Println("no service installed; nothing to restart")
		return nil
	}
	return Install()
}

func serviceStatus() error {
	if _, err := os.Stat(unitPath()); err != nil {
		fmt.Println("not installed as a service (garcon service install)")
		return nil
	}
	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command("systemctl", "--user", "is-active", unitName).Output()
		fmt.Printf("systemd user unit %s: %s\n", unitName, strings.TrimSpace(string(out)))
		if err != nil {
			return errors.New("service is not active; run garcon setup or inspect journalctl --user -u garcon")
		}
	case "darwin":
		if quiet("launchctl", "print", fmt.Sprintf("gui/%d/dev.garcon", os.Getuid())) {
			fmt.Println("LaunchAgent dev.garcon: loaded")
		} else {
			return errors.New("LaunchAgent dev.garcon is not loaded; run garcon setup")
		}
	}
	return nil
}

func copyExecutable(src, dst string) error {
	if src == dst {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), ".garcon-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	_, copyErr := io.Copy(out, in)
	modeErr := out.Chmod(0o755)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if modeErr != nil {
		return modeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(out.Name(), dst)
}

func systemdQuote(s string) string { return strconv.Quote(strings.ReplaceAll(s, "%", "%%")) }
func xmlText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;").Replace(s)
}

// systemdUnit is the user unit. Its hardening block is all seccomp and rlimit
// based, so it never needs unprivileged user namespaces, which hardened kernels
// and Ubuntu's AppArmor restriction can deny: the unit cannot fail to start for
// it. ProtectSystem, ProtectHome, PrivateTmp and ProtectKernel* are left out for
// exactly that reason, and CapabilityBoundingSet= because dropping capabilities
// needs CAP_SETPCAP, which a user manager lacks (status 218/CAPABILITIES). A
// syscall outside @system-service fails with EPERM instead of killing the process.
func systemdUnit(exe string) string {
	return "[Unit]\nDescription=Garcon local LLM usage proxy\n\n[Service]\nExecStart=" + systemdQuote(exe) + `
Restart=on-failure
RestartSec=2
NoNewPrivileges=yes
UMask=0077
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM

[Install]
WantedBy=default.target
`
}
