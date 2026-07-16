package remote

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// NOTE: Run/Output/SetupKey shell out to `ssh`/`ssh-copy-id` and need a real server,
// so they aren't unit-tested here. FindPubKey and the control-socket dir lifecycle are.

func TestFindPubKeyIdentity(t *testing.T) {
	// An identity without a .pub suffix → append .pub.
	if p, err := FindPubKey("/keys/id"); err != nil || p != "/keys/id.pub" {
		t.Errorf("FindPubKey(/keys/id) = %q, %v", p, err)
	}
	// Already a .pub path → verbatim.
	if p, err := FindPubKey("/keys/id.pub"); err != nil || p != "/keys/id.pub" {
		t.Errorf("FindPubKey(/keys/id.pub) = %q, %v", p, err)
	}
}

func TestFindPubKeyDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No key present → error.
	if _, err := FindPubKey(""); err == nil {
		t.Error("FindPubKey with no key should error")
	}
	// Create the default ed25519 pubkey → found.
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	pub := filepath.Join(home, ".ssh", "id_ed25519.pub")
	os.WriteFile(pub, []byte("ssh-ed25519 AAAA test"), 0o644)
	if p, err := FindPubKey(""); err != nil || p != pub {
		t.Errorf("FindPubKey(default) = %q, %v", p, err)
	}
}

func TestShQuote(t *testing.T) {
	cases := map[string]string{
		"/usr/local/bin/mandos": `'/usr/local/bin/mandos'`,
		"a b":                   `'a b'`,
		"a'b":                   `'a'\''b'`,
		"":                      `''`,
	}
	for in, want := range cases {
		if got := shQuote(in); got != want {
			t.Errorf("shQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAskpassSetup(t *testing.T) {
	// With no DISPLAY, the helper should add a placeholder one.
	t.Setenv("DISPLAY", "")

	env, cleanup := askpassSetup()
	if env == nil {
		t.Fatal("askpassSetup returned nil env")
	}

	// Extract SSH_ASKPASS and assert DISPLAY was added.
	var askpath, display string
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "SSH_ASKPASS="); ok {
			askpath = v
		}
		if v, ok := strings.CutPrefix(e, "DISPLAY="); ok {
			display = v
		}
	}
	if askpath == "" {
		t.Fatalf("SSH_ASKPASS not set in env %v", env)
	}
	if display == "" {
		t.Errorf("DISPLAY placeholder not added (env %v)", env)
	}

	// The wrapper must exist, be executable, and exec `mandos askpass`.
	fi, err := os.Stat(askpath)
	if err != nil {
		t.Fatalf("wrapper not written: %v", err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("wrapper not executable: %o", fi.Mode().Perm())
	}
	body, err := os.ReadFile(askpath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "askpass \"$@\"") {
		t.Errorf("wrapper does not exec askpass:\n%s", body)
	}

	// cleanup removes the wrapper.
	cleanup()
	if _, err := os.Stat(askpath); !os.IsNotExist(err) {
		t.Errorf("cleanup did not remove wrapper (err=%v)", err)
	}
}

func TestMuxDirLifecycle(t *testing.T) {
	// Mirrors remote.controlDir()'s path formula (it's unexported).
	dir := fmt.Sprintf("/tmp/mandos-ssh.%d", os.Getpid())
	os.RemoveAll(dir)

	if err := SetupMux(); err != nil {
		t.Fatalf("SetupMux: %v", err)
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("control dir not created at %s: %v", dir, err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Errorf("control dir perms = %o, want 700", perm)
	}

	// No sockets present, so CloseMux just removes the (empty) dir — no ssh calls.
	CloseMux()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("control dir not removed by CloseMux (err=%v)", err)
	}
}
