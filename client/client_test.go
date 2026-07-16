package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Isolate from the real ~/.config/mandos + ambient MANDOS_* env.
func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "mandos-client-test")
	os.Setenv("MANDOS_CONFIG", filepath.Join(dir, "mandos.yml"))
	os.Unsetenv("MANDOS_TEAM_CONFIG")
	os.Unsetenv("MANDOS_CLOUD_BASE")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// teamFile writes a registry file, points MANDOS_TEAM_CONFIG at it, and isolates the
// local config. Returns the team file path.
func teamFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "team.yml")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MANDOS_CONFIG", filepath.Join(dir, "local.yml"))
	t.Setenv("MANDOS_TEAM_CONFIG", f)
	return f
}

func TestValidName(t *testing.T) {
	for _, n := range []string{"acme", "acme-dev", "a1", "x", "buy-my-site", "site123"} {
		if !ValidName(n) {
			t.Errorf("ValidName(%q) = false, want true", n)
		}
	}
	for _, n := range []string{"", "-lead", "trail-", "Bad_Name", "UPPER", "a b", "under_score", "dot.name"} {
		if ValidName(n) {
			t.Errorf("ValidName(%q) = true, want false", n)
		}
	}
}

func TestSetGetRoundTrip(t *testing.T) {
	teamFile(t, "clients:\n  acme:\n    ssh: u@a\n    wp_root: /var/www/a\n")

	if err := Set("acme", "local_host", "acme.test"); err != nil {
		t.Fatal(err)
	}
	if v, err := GetField("acme", "local_host"); err != nil || v != "acme.test" {
		t.Errorf("GetField(local_host) = %q, %v", v, err)
	}
	if v, _ := GetField("acme", "ssh"); v != "u@a" {
		t.Errorf("GetField(ssh) = %q", v)
	}
	// Missing field → empty, no error.
	if v, err := GetField("acme", "nope"); err != nil || v != "" {
		t.Errorf("GetField(missing) = %q, %v", v, err)
	}
	// Missing client → error.
	if _, err := GetField("ghost", "ssh"); err == nil {
		t.Error("GetField on unknown client should error")
	}
}

func TestSetCreatesClientAndListPreservesOrder(t *testing.T) {
	teamFile(t, "clients:\n  acme:\n    ssh: u@a\n    wp_root: /v\n")

	if err := Set("newco", "ssh", "u@new"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := Has("newco"); !ok {
		t.Error("Has(newco) = false after Set")
	}
	names, err := List()
	if err != nil {
		t.Fatal(err)
	}
	// Existing entries keep file order; a new client is appended.
	if len(names) != 2 || names[0] != "acme" || names[1] != "newco" {
		t.Errorf("List() = %v, want [acme newco]", names)
	}
}

func TestCommentPreservation(t *testing.T) {
	content := "# HANDS OFF — managed by mandos\n" +
		"clients:\n" +
		"  acme:\n" +
		"    ssh: u@a   # the prod box\n" +
		"    wp_root: /v\n"
	f := teamFile(t, content)

	if err := Set("acme", "wp_root", "/var/www/new"); err != nil { // edit existing field
		t.Fatal(err)
	}
	if err := Set("baker", "ssh", "u@b"); err != nil { // append a new client
		t.Fatal(err)
	}
	out, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"# HANDS OFF", "# the prod box", "/var/www/new", "u@b"} {
		if !strings.Contains(s, want) {
			t.Errorf("edited file missing %q:\n%s", want, s)
		}
	}
}

func TestUnsetAndRemove(t *testing.T) {
	teamFile(t, "clients:\n  acme:\n    ssh: u@a\n    wp_root: /v\n    cloud_dir: /x\n")

	if err := Unset("acme", "cloud_dir"); err != nil {
		t.Fatal(err)
	}
	if v, _ := GetField("acme", "cloud_dir"); v != "" {
		t.Errorf("cloud_dir after Unset = %q, want empty", v)
	}
	// Unset of a missing key is a no-op (no error).
	if err := Unset("acme", "not-there"); err != nil {
		t.Errorf("Unset missing key: %v", err)
	}
	if err := Remove("acme"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := Has("acme"); ok {
		t.Error("acme still present after Remove")
	}
	// Remove of a missing client errors (mirrors the CLI).
	if err := Remove("ghost"); err == nil {
		t.Error("Remove(ghost) should error")
	}
}

func TestGetTypedAndExtra(t *testing.T) {
	teamFile(t, "clients:\n"+
		"  acme:\n"+
		"    ssh: u@a\n"+
		"    wp_root: /v\n"+
		"    cloud_folder: acme.de\n"+
		"    deactivate_plugins:\n"+
		"      - some-plugin\n")

	c, err := Get("acme")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "acme" || c.SSH != "u@a" || c.WPRoot != "/v" || c.CloudFolder != "acme.de" {
		t.Errorf("Get() = %+v", c)
	}
	// Unknown (tool-specific) fields survive in Extra.
	if _, ok := c.Extra["deactivate_plugins"]; !ok {
		t.Errorf("deactivate_plugins missing from Extra: %+v", c.Extra)
	}
	if _, err := Get("ghost"); err == nil {
		t.Error("Get(ghost) should error")
	}
}

func TestGetMap(t *testing.T) {
	teamFile(t, "clients:\n  acme:\n    ssh: u@a\n    wp_root: /v\n")
	m, err := GetMap("acme")
	if err != nil {
		t.Fatal(err)
	}
	if m["ssh"] != "u@a" || m["wp_root"] != "/v" {
		t.Errorf("GetMap() = %v", m)
	}
}

func TestWriteRefusesWhenTeamMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MANDOS_CONFIG", filepath.Join(dir, "local.yml"))
	t.Setenv("MANDOS_TEAM_CONFIG", filepath.Join(dir, "gone.yml")) // configured but absent

	if err := Set("acme", "ssh", "u@a"); err == nil {
		t.Error("Set should refuse when the team file is missing (Drive unmounted)")
	}
	if _, err := List(); err == nil {
		t.Error("List should error when the team file is missing")
	}
}

func TestSoloFallbackToLocalFile(t *testing.T) {
	// No team configured → clients live in (and are read from) the local file.
	dir := t.TempDir()
	local := filepath.Join(dir, "local.yml")
	os.WriteFile(local, []byte("clients:\n  soloco:\n    ssh: u@solo\n    wp_root: /v\n"), 0o644)
	t.Setenv("MANDOS_CONFIG", local)
	t.Setenv("MANDOS_TEAM_CONFIG", "")

	names, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "soloco" {
		t.Errorf("List() = %v, want [soloco]", names)
	}
	if v, _ := GetField("soloco", "ssh"); v != "u@solo" {
		t.Errorf("solo GetField(ssh) = %q", v)
	}
}
