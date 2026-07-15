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
	probe := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "StrictHostKeyChecking=accept-new"}
	if identity != "" {
		probe = append(probe, "-i", identity)
	}
	probe = append(probe, target, "true")
	if exec.Command("ssh", probe...).Run() == nil {
		return nil // key-based auth already works
	}

	if _, err := exec.LookPath("ssh-copy-id"); err == nil {
		a := []string{"-o", "StrictHostKeyChecking=accept-new"}
		if identity != "" {
			a = append(a, "-i", identity)
		}
		a = append(a, target)
		c := exec.Command("ssh-copy-id", a...)
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
	c.Stdin = strings.NewReader(string(data))
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
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
