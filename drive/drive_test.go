package drive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mandos/client"
)

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "mandos-drive-test")
	os.Setenv("MANDOS_CONFIG", filepath.Join(dir, "mandos.yml"))
	os.Unsetenv("MANDOS_TEAM_CONFIG")
	os.Unsetenv("MANDOS_CLOUD_BASE")
	os.Unsetenv("MANDOS_CLOUD_SUBDIR")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestClientCloudDirPrecedence(t *testing.T) {
	t.Setenv("MANDOS_CLOUD_BASE", "/drive/root")

	// 1. cloud_dir is used verbatim (absolute, machine-specific).
	if d, err := ClientCloudDir(&client.Client{Name: "a", CloudDir: "/abs/backup", CloudFolder: "ignored"}); err != nil || d != "/abs/backup" {
		t.Errorf("cloud_dir precedence: %q, %v", d, err)
	}
	// 2. cloud_folder under cloud_base.
	if d, err := ClientCloudDir(&client.Client{Name: "a", CloudFolder: "acme.de", Domain: "ignored.com"}); err != nil || d != "/drive/root/acme.de/100_Backup" {
		t.Errorf("cloud_folder precedence: %q, %v", d, err)
	}
	// 3. domain fallback.
	if d, err := ClientCloudDir(&client.Client{Name: "a", Domain: "acme.com"}); err != nil || d != "/drive/root/acme.com/100_Backup" {
		t.Errorf("domain fallback: %q, %v", d, err)
	}
	// None set → error.
	if _, err := ClientCloudDir(&client.Client{Name: "a"}); err == nil {
		t.Error("no cloud_dir/cloud_folder/domain should error")
	}
	// cloud_base unset but a folder is given → error.
	t.Setenv("MANDOS_CLOUD_BASE", "")
	if _, err := ClientCloudDir(&client.Client{Name: "a", CloudFolder: "x"}); err == nil {
		t.Error("missing cloud_base should error when a folder needs it")
	}
}

func TestClientCloudDirSubdirOverride(t *testing.T) {
	t.Setenv("MANDOS_CLOUD_BASE", "/drive/root")
	t.Setenv("MANDOS_CLOUD_SUBDIR", "Backups")
	if d, err := ClientCloudDir(&client.Client{Name: "a", CloudFolder: "x"}); err != nil || d != "/drive/root/x/Backups" {
		t.Errorf("subdir override: %q, %v", d, err)
	}
}

func TestAvailable(t *testing.T) {
	base := t.TempDir()
	t.Setenv("MANDOS_CLOUD_BASE", base)
	c := &client.Client{Name: "a", CloudFolder: "acme.de"}

	// The project (domain) folder must pre-exist — absent → unavailable.
	if Available(c) {
		t.Error("Available should be false before the project folder exists")
	}
	if err := os.MkdirAll(filepath.Join(base, "acme.de"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !Available(c) {
		t.Error("Available should be true once the project folder exists")
	}

	// cloud_base unset → unavailable (never errors).
	t.Setenv("MANDOS_CLOUD_BASE", "")
	if Available(&client.Client{Name: "a", CloudFolder: "x"}) {
		t.Error("Available should be false when cloud_base is unset")
	}
}

func TestCopyInAtomic(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "db.sql"), []byte("SELECT 1"), 0o644)
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "sub", "x.txt"), []byte("hi"), 0o644)

	dest := filepath.Join(t.TempDir(), "proj", "100_Backup", "20260101_120000")
	if err := CopyIn(src, dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "db.sql")); string(b) != "SELECT 1" {
		t.Errorf("db.sql not copied: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "sub", "x.txt")); string(b) != "hi" {
		t.Errorf("nested file not copied: %q", b)
	}
	// No leftover .tmp sibling (the atomic stage dir was renamed into place).
	entries, _ := os.ReadDir(filepath.Dir(dest))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("leftover temp staging dir: %s", e.Name())
		}
	}
}
