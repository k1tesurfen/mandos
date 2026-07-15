// Package drive resolves each client's backup folder on a mounted Google Drive and
// copies data there atomically. It owns only PATHS + availability + the atomic copy
// primitive; tool-specific policy (retention, manifests, what counts as a backup)
// stays in the consuming tool.
//
// The cloud is treated as the source of truth. Mandos never creates a site's project
// (domain) folder — that's the site's shared working folder, created by humans; it
// only writes into the fixed backup subfolder within it.
package drive

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"mandos/client"
	"mandos/config"
)

// backupSubdir is the fixed folder within a project folder that backups live in.
// Overridable via MANDOS_CLOUD_SUBDIR.
func backupSubdir() string {
	if s := os.Getenv("MANDOS_CLOUD_SUBDIR"); s != "" {
		return s
	}
	return "100_Backup"
}

// ClientCloudDir resolves the client's backup directory with precedence:
//
//	cloud_dir (absolute, verbatim — machine-specific, discouraged in the team file)
//	  → <cloud_base>/<cloud_folder>/<subdir>
//	  → <cloud_base>/<domain>/<subdir>
//
// It errors when none of cloud_dir/cloud_folder/domain is set, or when cloud_base is
// needed but unset.
func ClientCloudDir(c *client.Client) (string, error) {
	if c.CloudDir != "" {
		return c.CloudDir, nil
	}
	folder := c.CloudFolder
	if folder == "" {
		folder = c.Domain
	}
	if folder == "" {
		return "", fmt.Errorf("client %q has no cloud_dir, cloud_folder or domain set", c.Name)
	}
	base, err := config.CloudBase()
	if err != nil {
		return "", err
	}
	if base == "" {
		return "", fmt.Errorf("cloud_base is not set in the local config")
	}
	return filepath.Join(base, folder, backupSubdir()), nil
}

// Available reports whether the client's cloud target is usable right now: the backup
// dir resolves and its PARENT (the project/domain folder) already exists. A missing
// parent (project folder not created, or Drive unmounted) yields false, never an error
// — callers skip cloud and carry on.
func Available(c *client.Client) bool {
	dir, err := ClientCloudDir(c)
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Dir(dir))
	return err == nil && info.IsDir()
}

// CopyIn copies srcDir into destDir atomically: it stages into a temp sibling of
// destDir, then renames into place, so an interrupted copy never looks complete.
func CopyIn(srcDir, destDir string) error {
	if err := os.MkdirAll(filepath.Dir(destDir), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", destDir, os.Getpid())
	_ = os.RemoveAll(tmp)
	if err := copyTree(srcDir, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	_ = os.RemoveAll(destDir)
	if err := os.Rename(tmp, destDir); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return nil
}

// copyTree copies the contents of src into dst, preferring rsync (preserves
// attributes) and falling back to cp -a.
func copyTree(src, dst string) error {
	if _, err := exec.LookPath("rsync"); err == nil {
		return exec.Command("rsync", "-a", src+string(os.PathSeparator), dst+string(os.PathSeparator)).Run()
	}
	return exec.Command("cp", "-a", src, dst).Run()
}
