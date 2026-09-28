// Package config owns the Unbound config fragment: it parses the fragment
// file into zones and records, serializes the in-memory model back to that
// file, and writes it atomically. The fragment file is the single source of
// truth; no database is involved.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// Manager owns the fragment file lifecycle and knows the main config path
// used for the read-only include check.
type Manager struct {
	fragmentPath string
	mainConfPath string
}

// NewManager returns a Manager for the Unbound main config at confPath.
func NewManager(confPath string) (*Manager, error) {
	if _, err := os.Stat(confPath); err != nil {
		return nil, fmt.Errorf("unbound config not found: %w", err)
	}
	return &Manager{fragmentPath: DefaultFragmentPath, mainConfPath: confPath}, nil
}

// FragmentPath returns the effective fragment path.
func (m *Manager) FragmentPath() string { return m.fragmentPath }

// SetFragmentPath overrides the fragment location (development / hosts
// without /etc write access).
func (m *Manager) SetFragmentPath(p string) { m.fragmentPath = p }

// Read parses the fragment file into the zone model. A missing file yields
// an empty model, not an error.
func (m *Manager) Read() ([]domain.Zone, error) {
	return ParseFragment(m.fragmentPath)
}

// Write serializes zones and installs them atomically, creating the parent
// directory if needed.
func (m *Manager) Write(zones []domain.Zone) error {
	if err := os.MkdirAll(filepath.Dir(m.fragmentPath), 0755); err != nil {
		return fmt.Errorf("create fragment dir: %w", err)
	}
	return atomicWriteFile(m.fragmentPath, SerializeFragment(zones), 0644)
}

// CheckInclude verifies (read-only) that the main config includes our
// fragment as an include directive, returning an actionable ConfigError when
// it does not. Comment lines are ignored, so a path mentioned in a comment
// does not count. It never modifies the main config.
func (m *Manager) CheckInclude() error {
	data, err := os.ReadFile(m.mainConfPath)
	if err != nil {
		return fmt.Errorf("cannot read main config: %w", err)
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "include:") && !strings.HasPrefix(line, "include-toplevel:") {
			continue
		}
		if strings.Contains(line, m.fragmentPath) {
			return nil
		}
	}
	return &ConfigError{
		Type:    ErrMissingInclude,
		Message: "main config does not include our fragment",
		Fix: fmt.Sprintf("Add this line to %s:\n  include: %s",
			m.mainConfPath, m.fragmentPath),
	}
}

// atomicWriteFile writes crash-safely: temp file + fsync + rename.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".unbound-tui-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	return os.Rename(tmp.Name(), path)
}
