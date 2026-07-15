// Package config resolves Mandos's two-layer configuration:
//
//   - LOCAL  (~/.config/mandos/mandos.yml, per-user): points at the team file
//     (team_config) and holds this machine's cloud_base (the Google Drive root).
//   - TEAM   (a YAML file on a mounted shared Drive): holds the shared `clients:`
//     map — the single source of truth every colleague works from.
//
// Team is authoritative. When a team file is configured, clients are read from and
// written to it; the local file is only a fallback for a solo/pre-split setup. A
// configured-but-missing team file (Drive unmounted) is a soft error on reads and a
// hard error on writes, so a client is never silently written where the team can't
// see it.
//
// Every path can be overridden by an env var (used by other tools and by tests):
//
//	MANDOS_CONFIG        local config file path
//	MANDOS_TEAM_CONFIG   team (clients) file path
//	MANDOS_CLOUD_BASE    Google Drive projects root
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Local is the per-user config file.
type Local struct {
	TeamConfig string `yaml:"team_config,omitempty"`
	CloudBase  string `yaml:"cloud_base,omitempty"`
}

// LocalPath returns the per-user local config path (MANDOS_CONFIG override, else
// ~/.config/mandos/mandos.yml).
func LocalPath() string {
	if p := os.Getenv("MANDOS_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mandos", "mandos.yml")
}

// LoadLocal reads the local config. A missing file yields an empty (zero) config
// rather than an error, so a fresh machine still works.
func LoadLocal() (*Local, error) {
	var l Local
	b, err := os.ReadFile(LocalPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &l, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", LocalPath(), err)
	}
	return &l, nil
}

// WriteLocal writes the local config, creating its directory.
func WriteLocal(l *Local) error {
	p := LocalPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(l)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// ExpandTilde expands a leading ~ / ~/ to the user's home directory. No shell/eval.
func ExpandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

// TeamPath resolves the shared team config path (MANDOS_TEAM_CONFIG override, else
// local team_config). Returns "" when no team file is configured.
func TeamPath() (string, error) {
	if p := os.Getenv("MANDOS_TEAM_CONFIG"); p != "" {
		return ExpandTilde(p), nil
	}
	l, err := LoadLocal()
	if err != nil {
		return "", err
	}
	if l.TeamConfig == "" {
		return "", nil
	}
	return ExpandTilde(l.TeamConfig), nil
}

// ClientFileRead returns the file that holds the `clients:` map for READS: the team
// file when configured and present, else the local file (fallback). A configured but
// missing team file is an error (Drive unmounted) — callers may warn and degrade.
func ClientFileRead() (string, error) {
	t, err := TeamPath()
	if err != nil {
		return "", err
	}
	if t == "" {
		return LocalPath(), nil
	}
	if _, err := os.Stat(t); err != nil {
		return "", fmt.Errorf("team config not found at %s — is your Google Drive mounted?", t)
	}
	return t, nil
}

// ClientFileWrite is like ClientFileRead but a configured-but-missing team file is
// fatal: client changes must never be written into the local file where the team
// can't see them.
func ClientFileWrite() (string, error) {
	t, err := TeamPath()
	if err != nil {
		return "", err
	}
	if t == "" {
		return LocalPath(), nil
	}
	if _, err := os.Stat(t); err != nil {
		return "", fmt.Errorf("team config not found at %s — client changes must write to the shared team config (is Drive mounted?)", t)
	}
	return t, nil
}

// CloudBase returns the Google Drive projects root (MANDOS_CLOUD_BASE override, else
// local cloud_base), tilde-expanded. May be "" when cloud sync isn't set up.
func CloudBase() (string, error) {
	if p := os.Getenv("MANDOS_CLOUD_BASE"); p != "" {
		return ExpandTilde(p), nil
	}
	l, err := LoadLocal()
	if err != nil {
		return "", err
	}
	return ExpandTilde(l.CloudBase), nil
}
