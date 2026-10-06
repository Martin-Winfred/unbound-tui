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
	// Resolve a symlinked fragment to its target before writing: POSIX rename
	// replaces the directory entry it is handed, so writing through the link
	// would swap the symlink for a regular file and leave the real, included
	// file stale — the reload would load old config while apply reported
	// success. The include gate resolves both spellings already, so it accepts
	// this pairing; the write must follow suit.
	dest := writeDestination(m.fragmentPath)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("create fragment dir: %w", err)
	}
	return atomicWriteFile(dest, SerializeFragment(f), 0644)
}

// writeDestination returns the file an atomic write must ultimately replace.
// A symlink is resolved to its target so the rename swaps the target rather
// than the link: EvalSymlinks when the target exists, otherwise the raw link
// target (a relative one anchored to the link's directory). Every other path
// is returned untouched, keeping regular-file writes byte-for-byte identical.
func writeDestination(path string) string {
	li, err := os.Lstat(path)
	if err != nil || li.Mode()&os.ModeSymlink == 0 {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	target, err := os.Readlink(path)
	if err != nil {
		return path
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target)
}

// CheckInclude verifies (read-only) that the main config includes our
// fragment, either as a literal path or via an include glob that matches it.
// Comment lines are ignored. It never modifies the main config.
func (m *Manager) CheckInclude() error {
	data, err := os.ReadFile(m.mainConfPath)
	if err != nil {
		return fmt.Errorf("cannot read main config: %w", err)
	}
	// Relative include targets resolve against the main config's directory.
	// Anchor that base to an absolute path once, so targets never resolve
	// against the process working directory.
	mainDir := filepath.Dir(m.mainConfPath)
	if abs, err := filepath.Abs(mainDir); err == nil {
		mainDir = abs
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "include:") && !strings.HasPrefix(line, "include-toplevel:") {
			continue
		}
		if includeMatches(includeTarget(line), m.fragmentPath, mainDir) {
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
// target literally names it, or it is a glob that expands to it. Relative
// targets resolve against mainDir (the main config's directory), never the
// process working directory; both sides are compared after ResolvePath, which
// resolves symlinks (falling back to the cleaned absolute path for a file that
// does not exist yet) so a fragment included by its real path matches the same
// file passed as a symlink, and vice versa.
func includeMatches(target, fragment, mainDir string) bool {
	if target == "" {
		return false
	}
	fragResolved := ResolvePath(fragment)
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(mainDir, resolved)
	}
	resolved = filepath.Clean(resolved)

	if !strings.ContainsAny(target, "*?[") {
		return ResolvePath(resolved) == fragResolved
	}
	// Syntactic match works even before the fragment file exists: ResolvePath
	// falls back to the absolute path when EvalSymlinks cannot resolve a glob.
	if ok, err := filepath.Match(ResolvePath(resolved), fragResolved); err == nil && ok {
		return true
	}
	matches, err := filepath.Glob(resolved)
	if err != nil {
		return false
	}
	for _, mt := range matches {
		if ResolvePath(mt) == fragResolved {
			return true
		}
	}
	return false
}

// atomicWriteFile writes crash-safely: temp file + fsync + rename + parent
// fsync. When path already exists its permission bits are preserved, so a
// rewrite (which a root-run apply performs) never changes the fragment's mode;
// perm is used only when creating a new file. Only a missing file falls back
// to perm: any other stat error is returned rather than swallowed, which would
// silently reset the mode to perm.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat fragment: %w", err)
	}
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
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename temp: %w", err)
	}
	// Fsync the parent directory so the rename itself is durable: a crash
	// after the rename but before the directory entry reaches disk would lose
	// the new fragment. Return the error rather than best-effort it, so the
	// crash-safe claim stays honest.
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open fragment dir: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync fragment dir: %w", err)
	}
	return nil
}
