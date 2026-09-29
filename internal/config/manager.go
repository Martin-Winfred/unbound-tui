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
	"github.com/Martin-Winfred/unbound-tui/internal/validate"
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

// MainConfPath returns the Unbound main config path the manager was built for.
func (m *Manager) MainConfPath() string { return m.mainConfPath }

// SetFragmentPath overrides the fragment location (development / hosts
// without /etc write access).
func (m *Manager) SetFragmentPath(p string) { m.fragmentPath = p }

// ReadFragment parses the fragment file from disk and returns the generic
// section/entry model. A missing file yields an empty Fragment, not an error.
// The caller owns the returned model.
func (m *Manager) ReadFragment() (domain.Fragment, error) {
	return ParseFragment(m.fragmentPath)
}

// WriteFragment is the single write path for the generic fragment model. It
// validates the fragment first, then installs it atomically (creating the
// parent directory), so an invalid model never reaches the file.
func (m *Manager) WriteFragment(f domain.Fragment) error {
	if err := validate.ValidateFragment(f); err != nil {
		return fmt.Errorf("write fragment: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.fragmentPath), 0755); err != nil {
		return fmt.Errorf("create fragment dir: %w", err)
	}
	return atomicWriteFile(m.fragmentPath, SerializeFragment(f), 0644)
}

// CheckInclude verifies (read-only) that the main config includes our
// fragment, either as a literal path or via an include glob that matches it.
// Comment lines are ignored. It never modifies the main config.
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
		if includeMatches(includeTarget(line), m.fragmentPath) {
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

// includeTarget strips the include/include-toplevel prefix, surrounding quotes
// and any trailing comment.
func includeTarget(line string) string {
	rest := line
	for _, p := range []string{"include-toplevel:", "include:"} {
		if strings.HasPrefix(rest, p) {
			rest = strings.TrimSpace(strings.TrimPrefix(rest, p))
			break
		}
	}
	rest = strings.Trim(rest, `"`)
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		rest = strings.TrimSpace(rest[:i])
	}
	return rest
}

// includeMatches reports whether an include target covers fragment: either the
// target literally names it, or it is a glob that expands to it.
func includeMatches(target, fragment string) bool {
	if target == "" {
		return false
	}
	if strings.Contains(target, fragment) {
		return true
	}
	if !strings.ContainsAny(target, "*?[") {
		return false
	}
	// Syntactic match works even before the fragment file exists.
	if ok, err := filepath.Match(target, fragment); err == nil && ok {
		return true
	}
	matches, err := filepath.Glob(target)
	if err != nil {
		return false
	}
	want, err := filepath.Abs(fragment)
	if err != nil {
		want = fragment
	}
	for _, mt := range matches {
		got, err := filepath.Abs(mt)
		if err != nil {
			got = mt
		}
		if got == want {
			return true
		}
	}
	return false
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
