package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Isolate every test in this package from the real ~/.config/mandos and any ambient
// MANDOS_* env; individual tests override via t.Setenv as needed.
func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "mandos-config-test")
	os.Setenv("MANDOS_CONFIG", filepath.Join(dir, "mandos.yml"))
	os.Unsetenv("MANDOS_TEAM_CONFIG")
	os.Unsetenv("MANDOS_CLOUD_BASE")
	os.Unsetenv("MANDOS_CLOUD_SUBDIR")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestLocalPathEnvOverride(t *testing.T) {
	t.Setenv("MANDOS_CONFIG", "/tmp/x/mandos.yml")
	if got := LocalPath(); got != "/tmp/x/mandos.yml" {
		t.Errorf("LocalPath() = %q, want /tmp/x/mandos.yml", got)
	}
}

func TestLoadLocalMissingIsEmpty(t *testing.T) {
	t.Setenv("MANDOS_CONFIG", filepath.Join(t.TempDir(), "nope.yml"))
	l, err := LoadLocal()
	if err != nil {
		t.Fatalf("LoadLocal on missing file: %v", err)
	}
	if l.TeamConfig != "" || l.CloudBase != "" {
		t.Errorf("expected empty config, got %+v", l)
	}
}

func TestWriteLoadLocalRoundTrip(t *testing.T) {
	t.Setenv("MANDOS_CONFIG", filepath.Join(t.TempDir(), "mandos.yml"))
	want := &Local{TeamConfig: "/drive/team.yml", CloudBase: "/drive/LIVE_WEB"}
	if err := WriteLocal(want); err != nil {
		t.Fatalf("WriteLocal: %v", err)
	}
	got, err := LoadLocal()
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if *got != *want {
		t.Errorf("round-trip: got %+v, want %+v", got, want)
	}
}

func TestExpandTilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		"~/x":   filepath.Join(home, "x"),
		"~":     home,
		"/abs":  "/abs",
		"rel/x": "rel/x",
		"~x/y":  "~x/y", // not a ~/ prefix — left unchanged
	}
	for in, want := range cases {
		if got := ExpandTilde(in); got != want {
			t.Errorf("ExpandTilde(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTeamPath(t *testing.T) {
	// Env override wins.
	t.Setenv("MANDOS_TEAM_CONFIG", "/env/team.yml")
	if p, _ := TeamPath(); p != "/env/team.yml" {
		t.Errorf("env override: got %q", p)
	}

	// No env override → read from the local config's team_config (tilde-expanded).
	home, _ := os.UserHomeDir()
	local := filepath.Join(t.TempDir(), "mandos.yml")
	os.WriteFile(local, []byte("team_config: ~/drive/team.yml\n"), 0o644)
	t.Setenv("MANDOS_TEAM_CONFIG", "")
	t.Setenv("MANDOS_CONFIG", local)
	if p, _ := TeamPath(); p != filepath.Join(home, "drive/team.yml") {
		t.Errorf("from local config: got %q", p)
	}

	// No team configured anywhere → empty.
	empty := filepath.Join(t.TempDir(), "mandos.yml")
	os.WriteFile(empty, []byte("cloud_base: /x\n"), 0o644)
	t.Setenv("MANDOS_CONFIG", empty)
	if p, _ := TeamPath(); p != "" {
		t.Errorf("no team configured: got %q, want empty", p)
	}
}

func TestClientFileResolution(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "local.yml")
	team := filepath.Join(dir, "team.yml")
	if err := os.WriteFile(team, []byte("clients: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MANDOS_CONFIG", local)

	// Team configured + present → both read and write resolve to the team file.
	t.Setenv("MANDOS_TEAM_CONFIG", team)
	if p, err := ClientFileRead(); err != nil || p != team {
		t.Errorf("read (team present): %q, %v", p, err)
	}
	if p, err := ClientFileWrite(); err != nil || p != team {
		t.Errorf("write (team present): %q, %v", p, err)
	}

	// Team configured but MISSING (Drive unmounted) → both error.
	t.Setenv("MANDOS_TEAM_CONFIG", filepath.Join(dir, "gone.yml"))
	if _, err := ClientFileRead(); err == nil {
		t.Error("ClientFileRead should error when the team file is missing")
	}
	if _, err := ClientFileWrite(); err == nil {
		t.Error("ClientFileWrite should refuse when the team file is missing")
	}

	// No team configured → both fall back to the local file.
	t.Setenv("MANDOS_TEAM_CONFIG", "")
	if p, err := ClientFileRead(); err != nil || p != local {
		t.Errorf("read (solo): %q, %v", p, err)
	}
	if p, err := ClientFileWrite(); err != nil || p != local {
		t.Errorf("write (solo): %q, %v", p, err)
	}
}

func TestCloudBase(t *testing.T) {
	home, _ := os.UserHomeDir()

	// Env override, tilde-expanded.
	t.Setenv("MANDOS_CLOUD_BASE", "~/drive")
	if b, _ := CloudBase(); b != filepath.Join(home, "drive") {
		t.Errorf("env override: got %q", b)
	}

	// From the local config.
	local := filepath.Join(t.TempDir(), "mandos.yml")
	os.WriteFile(local, []byte("cloud_base: /drive/root\n"), 0o644)
	t.Setenv("MANDOS_CLOUD_BASE", "")
	t.Setenv("MANDOS_CONFIG", local)
	if b, _ := CloudBase(); b != "/drive/root" {
		t.Errorf("from local config: got %q", b)
	}
}
