// Package remote runs SSH to client servers with connection multiplexing, and
// installs the current user's SSH key on a server (onboarding).
//
// It shells out to the system `ssh`/`ssh-copy-id` rather than using a Go SSH library
// on purpose: that reuses the user's ~/.ssh/config, known_hosts, agent and any custom
// port, and mirrors wpsite's ControlMaster setup exactly.
package remote

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// controlDir holds the ControlMaster sockets. Kept under /tmp (not $TMPDIR) because
// the socket path has a hard 104-byte limit on macOS and $TMPDIR is too long once the
// %C hash is appended. Namespaced by PID so concurrent runs don't collide.
func controlDir() string {
	return fmt.Sprintf("/tmp/mandos-ssh.%d", os.Getpid())
}

func muxArgs() []string {
	return []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + controlDir() + "/%C",
		"-o", "ControlPersist=120",
	}
}

// SetupMux ensures the control-socket directory exists (0700).
func SetupMux() error {
	d := controlDir()
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	return os.Chmod(d, 0o700)
}

// CloseMux closes any open master connections and removes the control dir.
func CloseMux() {
	d := controlDir()
	entries, err := os.ReadDir(d)
	if err != nil {
		return
	}
	for _, e := range entries {
		sock := filepath.Join(d, e.Name())
		_ = exec.Command("ssh", "-o", "ControlPath="+sock, "-O", "exit", "_").Run()
	}
	_ = os.RemoveAll(d)
}

// Run executes `ssh <target> <args...>` with multiplexing, wired to the current
// process's stdin/stdout/stderr. Returns the ssh exit error (an *exec.ExitError,
// whose ExitCode() the caller can propagate).
func Run(target string, args ...string) error {
	if err := SetupMux(); err != nil {
		return err
	}
	a := append(muxArgs(), target)
	a = append(a, args...)
	cmd := exec.Command("ssh", a...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Output runs a remote command and returns its stdout (stderr passes through).
func Output(target string, args ...string) ([]byte, error) {
	if err := SetupMux(); err != nil {
		return nil, err
	}
	a := append(muxArgs(), target)
	a = append(a, args...)
	cmd := exec.Command("ssh", a...)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

// SetupKey ensures key-based SSH auth to target works. It probes first (BatchMode,
// accept-new host key) and returns nil if the key already works; otherwise it installs
// the key via ssh-copy-id, or — on macOS, which ships no ssh-copy-id — appends the
// public key to the remote authorized_keys manually. identity is optional (a private
// key path); empty means "use the default key".
func SetupKey(target, identity string) error {
	// With no explicit identity, make sure the user actually has a default key to
	// install (a fresh Mac has none) — generate one before probing.
	if identity == "" {
		if err := ensureLocalKey(); err != nil {
			return err
		}
	}

	probe := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "StrictHostKeyChecking=accept-new"}
	if identity != "" {
		probe = append(probe, "-i", identity)
	}
	probe = append(probe, target, "true")
	if exec.Command("ssh", probe...).Run() == nil {
		return nil // key-based auth already works
	}

	// A server-password prompt may follow. Set up the askpass bridge so it can be
	// answered by a native dialog when there's no terminal (GUI); harmless on a terminal.
	apEnv, apCleanup := askpassSetup()
	defer apCleanup()

	if _, err := exec.LookPath("ssh-copy-id"); err == nil {
		a := []string{"-o", "StrictHostKeyChecking=accept-new"}
		if identity != "" {
			a = append(a, "-i", identity)
		}
		a = append(a, target)
		c := exec.Command("ssh-copy-id", a...)
		c.Env = append(os.Environ(), apEnv...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return c.Run()
	}

	// macOS fallback: append the public key over a normal ssh session.
	pub, err := FindPubKey(identity)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(pub)
	if err != nil {
		return err
	}
	c := exec.Command("ssh", "-o", "StrictHostKeyChecking=accept-new", target,
		"umask 077; mkdir -p ~/.ssh && cat >> ~/.ssh/authorized_keys")
	c.Env = append(os.Environ(), apEnv...)
	c.Stdin = strings.NewReader(string(data))
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

// askpassSetup writes a tiny wrapper script that execs `mandos askpass` and returns the
// environment additions (SSH_ASKPASS + a placeholder DISPLAY) that make OpenSSH use it.
//
// Effect: when a command runs WITHOUT a controlling terminal (e.g. launched from a GUI),
// ssh/ssh-copy-id read the password from that helper — which pops a native macOS dialog
// (`mandos askpass`) — instead of the unreachable /dev/tty. When run FROM a terminal, ssh
// still prompts on the terminal: SSH_ASKPASS is only consulted when no tty is present, and
// we deliberately do NOT set SSH_ASKPASS_REQUIRE=force. So plain CLI use is unchanged.
//
// The dialog logic lives in this same binary, so nothing extra ships — the wrapper is a
// 2-line temp file. cleanup removes it; call it after the ssh command finishes. On any
// failure it returns nil env + a no-op cleanup, so callers degrade to the old tty-only
// behaviour rather than breaking.
func askpassSetup() (env []string, cleanup func()) {
	cleanup = func() {}
	self, err := os.Executable()
	if err != nil {
		return nil, cleanup
	}
	f, err := os.CreateTemp("", "mandos-askpass-*.sh")
	if err != nil {
		return nil, cleanup
	}
	// ssh invokes the askpass program with the prompt string as its single argument.
	script := "#!/bin/sh\nexec " + shQuote(self) + " askpass \"$@\"\n"
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, cleanup
	}
	f.Close()
	if err := os.Chmod(f.Name(), 0o700); err != nil {
		os.Remove(f.Name())
		return nil, cleanup
	}
	name := f.Name()
	cleanup = func() { os.Remove(name) }
	env = []string{"SSH_ASKPASS=" + name}
	// ssh only consults SSH_ASKPASS (when it has no tty) if DISPLAY is also set. The value
	// is irrelevant to our osascript helper — set a placeholder only if there is none.
	if os.Getenv("DISPLAY") == "" {
		env = append(env, "DISPLAY=:0")
	}
	return env, cleanup
}

// shQuote single-quotes s for safe interpolation into a /bin/sh script.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ensureLocalKey makes sure a default SSH key exists, generating an ed25519 keypair at
// ~/.ssh/id_ed25519 if the user has none (a fresh Mac). ssh-keygen runs interactively
// (prompts for a passphrase — Enter twice for none). No-op when a key already exists.
func ensureLocalKey() error {
	if _, err := FindPubKey(""); err == nil {
		return nil
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		return fmt.Errorf("no SSH key found and ssh-keygen is unavailable — create one with: ssh-keygen -t ed25519")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return err
	}
	comment := "mandos"
	if u, err := user.Current(); err == nil {
		comment = "mandos-" + u.Username
	}
	if h, err := os.Hostname(); err == nil {
		comment += "@" + h
	}
	fmt.Fprintln(os.Stderr, "No SSH key found — creating one at ~/.ssh/id_ed25519 (press Enter / leave empty for no passphrase).")
	apEnv, apCleanup := askpassSetup()
	defer apCleanup()
	c := exec.Command("ssh-keygen", "-t", "ed25519", "-f", filepath.Join(sshDir, "id_ed25519"), "-C", comment)
	c.Env = append(os.Environ(), apEnv...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// FindPubKey locates a public key: identity (+ ".pub" if needed) when given, else the
// first of the usual keys in ~/.ssh.
func FindPubKey(identity string) (string, error) {
	if identity != "" {
		if strings.HasSuffix(identity, ".pub") {
			return identity, nil
		}
		return identity + ".pub", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, n := range []string{"id_ed25519.pub", "id_rsa.pub", "id_ecdsa.pub"} {
		p := filepath.Join(home, ".ssh", n)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no SSH public key found in ~/.ssh (id_ed25519/id_rsa/id_ecdsa) — generate one with ssh-keygen")
}
