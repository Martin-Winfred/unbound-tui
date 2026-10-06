package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

func testManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	conf := filepath.Join(dir, "unbound.conf")
	if err := os.WriteFile(conf, []byte("include: "+filepath.Join(dir, "frag.conf")+"\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	frag := filepath.Join(dir, "frag.conf")
	m.SetFragmentPath(frag)
	return m, frag
}

// frag parses src into a Fragment. The helper lives here after merge_test.go
// was removed; serialize_test.go uses it too.
func frag(t *testing.T, src string) domain.Fragment {
	t.Helper()
	f, err := parseFragment([]byte(src))
	if err != nil {
		t.Fatalf("parseFragment: %v", err)
	}
	return f
}

func TestManagerReadWriteFragment(t *testing.T) {
	m, fragPath := testManager(t)

	if f, err := m.ReadFragment(); err != nil || len(f.Sections) != 0 {
		t.Fatalf("ReadFragment on missing fragment = %+v, %v; want empty, nil", f, err)
	}

	want := domain.Fragment{Sections: []domain.Section{{
		Kind: "server",
		Entries: []domain.Entry{
			{Key: "local-zone", Value: `"example.com." transparent`},
			{Key: "local-data", Value: `"example.com. 300 IN A 192.0.2.1"`},
		},
	}}}
	if err := m.WriteFragment(want); err != nil {
		t.Fatalf("WriteFragment: %v", err)
	}
	if _, err := os.Stat(fragPath); err != nil {
		t.Fatalf("fragment not created: %v", err)
	}
	got, err := m.ReadFragment()
	if err != nil {
		t.Fatalf("ReadFragment: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadFragment = %+v, want %+v", got, want)
	}

	zones, err := ZonesFromFragment(got)
	if err != nil {
		t.Fatalf("ZonesFromFragment: %v", err)
	}
	if len(zones) != 1 || zones[0].Name != "example.com." {
		t.Errorf("projected zones = %+v, want example.com.", zones)
	}
}

func TestZonesFromFragmentMalformed(t *testing.T) {
	m, fragPath := testManager(t)
	if err := os.WriteFile(fragPath, []byte("server:\nlocal-data: \"broken\n"), 0644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	// Reading is total; projection is what rejects the malformed entry.
	f, err := m.ReadFragment()
	if err != nil {
		t.Fatalf("ReadFragment = %v, want nil (parsing is total)", err)
	}
	zones, err := ZonesFromFragment(f)
	if err == nil {
		t.Fatalf("ZonesFromFragment = %+v, nil error; want projection error", zones)
	}
	if !strings.Contains(err.Error(), "local-data") {
		t.Errorf("projection error %q does not name the malformed local-data entry", err)
	}
}

func TestCheckInclude(t *testing.T) {
	m, _ := testManager(t)
	if err := m.CheckInclude(); err != nil {
		t.Fatalf("CheckInclude (included) = %v, want nil", err)
	}

	dir := t.TempDir()
	conf := filepath.Join(dir, "unbound.conf")
	if err := os.WriteFile(conf, []byte("server:\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	m2, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	err = m2.CheckInclude()
	var ce *ConfigError
	if !errors.As(err, &ce) || ce.Type != ErrMissingInclude {
		t.Errorf("CheckInclude (missing) = %v, want ConfigError %s", err, ErrMissingInclude)
	}
}

func TestCheckIncludeIgnoresComments(t *testing.T) {
	dir := t.TempDir()
	frag := filepath.Join(dir, "frag.conf")
	// The fragment path appears only in a comment: this is NOT an include.
	conf := writeTemp(t, "# include: "+frag+"\nserver:\n")
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetFragmentPath(frag)
	var ce *ConfigError
	if err := m.CheckInclude(); !errors.As(err, &ce) {
		t.Errorf("CheckInclude with only a commented include = %v, want ConfigError", err)
	}
}

func TestCheckIncludeGlob(t *testing.T) {
	dir := t.TempDir()
	confD := filepath.Join(dir, "unbound.conf.d")
	if err := os.MkdirAll(confD, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	conf := writeTemp(t, "include-toplevel: \""+confD+"/*.conf\"\n")
	frag := filepath.Join(confD, "unbound-tui.conf") // not created yet
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetFragmentPath(frag)
	if err := m.CheckInclude(); err != nil {
		t.Errorf("CheckInclude with a matching glob = %v, want nil", err)
	}
}

// TestCheckIncludeSubstringTrap pins that a literal include naming a sibling
// file is not mistaken for our fragment just because the fragment path is a
// substring of it.
func TestCheckIncludeSubstringTrap(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "unbound.conf")
	if err := os.WriteFile(conf, []byte("include: /etc/unbound/unbound-tui.conf.d/other.conf\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetFragmentPath("/etc/unbound/unbound-tui.conf")
	var ce *ConfigError
	if err := m.CheckInclude(); !errors.As(err, &ce) || ce.Type != ErrMissingInclude {
		t.Errorf("CheckInclude substring trap = %v, want ConfigError %s", err, ErrMissingInclude)
	}
}

// TestCheckIncludeLiteralAbsolute pins that an absolute literal include of the
// fragment still matches.
func TestCheckIncludeLiteralAbsolute(t *testing.T) {
	dir := t.TempDir()
	frag := filepath.Join(dir, "unbound-tui.conf")
	conf := filepath.Join(dir, "unbound.conf")
	if err := os.WriteFile(conf, []byte("include: "+frag+"\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetFragmentPath(frag)
	if err := m.CheckInclude(); err != nil {
		t.Errorf("CheckInclude absolute literal = %v, want nil", err)
	}
}

// TestCheckIncludeRelativeLiteral pins that a relative include target resolves
// against the main config's directory, not the process working directory.
func TestCheckIncludeRelativeLiteral(t *testing.T) {
	dir := t.TempDir()
	confD := filepath.Join(dir, "conf.d")
	if err := os.MkdirAll(confD, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	conf := filepath.Join(dir, "unbound.conf")
	if err := os.WriteFile(conf, []byte("include: conf.d/unbound-tui.conf\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	frag := filepath.Join(confD, "unbound-tui.conf")
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetFragmentPath(frag)
	if err := m.CheckInclude(); err != nil {
		t.Errorf("CheckInclude relative literal = %v, want nil", err)
	}
}

// TestCheckIncludeRelativeGlob pins that a relative glob include resolves
// against the main config's directory, not the process working directory.
func TestCheckIncludeRelativeGlob(t *testing.T) {
	dir := t.TempDir()
	confD := filepath.Join(dir, "conf.d")
	if err := os.MkdirAll(confD, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	conf := filepath.Join(dir, "unbound.conf")
	if err := os.WriteFile(conf, []byte("include-toplevel: \"conf.d/*.conf\"\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	frag := filepath.Join(confD, "unbound-tui.conf") // not created yet
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetFragmentPath(frag)
	if err := m.CheckInclude(); err != nil {
		t.Errorf("CheckInclude relative glob = %v, want nil", err)
	}
}

// TestCheckIncludeUnrelatedAbsoluteGlob pins that an absolute glob pointing
// elsewhere does not match the fragment.
func TestCheckIncludeUnrelatedAbsoluteGlob(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "unbound.conf")
	if err := os.WriteFile(conf, []byte("include-toplevel: \"/etc/other.conf.d/*.conf\"\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	frag := filepath.Join(dir, "unbound-tui.conf")
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetFragmentPath(frag)
	var ce *ConfigError
	if err := m.CheckInclude(); !errors.As(err, &ce) || ce.Type != ErrMissingInclude {
		t.Errorf("CheckInclude unrelated glob = %v, want ConfigError %s", err, ErrMissingInclude)
	}
}

// TestWriteFragmentValidatesBeforeDisk pins the gate ordering: an invalid
// fragment must be rejected before MkdirAll runs, so it neither creates the
// parent directory nor the file. The fragment path lives in a not-yet-existing
// subdirectory so an accidentally-created directory is observable.
func TestWriteFragmentValidatesBeforeDisk(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "unbound.conf")
	if err := os.WriteFile(conf, []byte("server:\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	sub := filepath.Join(dir, "nested")
	frag := filepath.Join(sub, "frag.conf")
	m.SetFragmentPath(frag)

	invalid := domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "foo bar", Value: "x"}}},
	}}
	err = m.WriteFragment(invalid)
	if err == nil {
		t.Fatalf("WriteFragment(invalid) = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "write fragment") || !strings.Contains(err.Error(), "foo bar") {
		t.Errorf("WriteFragment(invalid) = %v, want wrapped validation error naming the key", err)
	}
	if _, statErr := os.Stat(frag); !os.IsNotExist(statErr) {
		t.Errorf("invalid fragment must not create the file (stat err = %v)", statErr)
	}
	if _, statErr := os.Stat(sub); !os.IsNotExist(statErr) {
		t.Errorf("invalid fragment must not create the parent dir (stat err = %v)", statErr)
	}

	// Positive control: a valid fragment does create the directory and file.
	valid := domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "local-zone", Value: `"example.com" static`}}},
	}}
	if err := m.WriteFragment(valid); err != nil {
		t.Fatalf("WriteFragment(valid) = %v", err)
	}
	if _, statErr := os.Stat(sub); statErr != nil {
		t.Errorf("valid fragment must create the parent dir: %v", statErr)
	}
	if _, statErr := os.Stat(frag); statErr != nil {
		t.Errorf("valid fragment must create the file: %v", statErr)
	}
}

// TestWriteFragmentPreservesMode pins the atomic write's permission contract:
// a new fragment gets the default 0644, but a rewrite keeps the existing
// file's bits. Without this, a root-run apply would silently reset an
// operator's tightened fragment (e.g. 0600) back to 0644.
func TestWriteFragmentPreservesMode(t *testing.T) {
	m, fragPath := testManager(t)
	valid := domain.Fragment{Sections: []domain.Section{{
		Kind:    "server",
		Entries: []domain.Entry{{Key: "local-zone", Value: `"example.com" static`}},
	}}}

	if err := m.WriteFragment(valid); err != nil {
		t.Fatalf("WriteFragment: %v", err)
	}
	info, err := os.Stat(fragPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Errorf("new fragment mode = %o, want 0644", got)
	}

	if err := os.Chmod(fragPath, 0600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := m.WriteFragment(valid); err != nil {
		t.Fatalf("WriteFragment rewrite: %v", err)
	}
	info, err = os.Stat(fragPath)
	if err != nil {
		t.Fatalf("Stat after rewrite: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("rewritten fragment mode = %o, want 0600 (preserved)", got)
	}
}

// TestWriteFragmentThroughSymlink pins that an atomic write to a symlinked
// fragment path replaces the link's target, not the link itself. POSIX rename
// swaps the directory entry it is given, so without resolving the link first
// the write would replace the symlink with a fresh regular file while the real,
// included file kept its old content — apply would reload stale config and
// still report success.
func TestWriteFragmentThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.conf")
	if err := os.WriteFile(real, []byte("server:\n"), 0600); err != nil {
		t.Fatalf("write real fragment: %v", err)
	}
	link := filepath.Join(dir, "link.conf")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	// The main config includes the real path while -fragment names the symlink:
	// the pairing the include gate deliberately blesses.
	conf := writeTemp(t, "include: "+real+"\n")
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetFragmentPath(link)
	if err := m.CheckInclude(); err != nil {
		t.Fatalf("CheckInclude = %v, want nil (gate must accept the pairing)", err)
	}

	valid := domain.Fragment{Sections: []domain.Section{{
		Kind:    "server",
		Entries: []domain.Entry{{Key: "local-zone", Value: `"example.com" static`}},
	}}}
	if err := m.WriteFragment(valid); err != nil {
		t.Fatalf("WriteFragment: %v", err)
	}

	// The fragment path is still a symlink pointing at the real file.
	li, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("Lstat(link): %v", err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("fragment path is no longer a symlink (mode %v); the rename replaced it", li.Mode())
	}
	if target, err := os.Readlink(link); err != nil || target != real {
		t.Errorf("Readlink(link) = %q, %v; want %q", target, err, real)
	}

	// The real target holds the new content and kept its original mode.
	got, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("ReadFile(real): %v", err)
	}
	if want := string(SerializeFragment(valid)); string(got) != want {
		t.Errorf("real target content = %q, want %q", got, want)
	}
	ri, err := os.Stat(real)
	if err != nil {
		t.Fatalf("Stat(real): %v", err)
	}
	if ri.Mode().Perm() != 0600 {
		t.Errorf("real target mode = %o, want 0600 (preserved)", ri.Mode().Perm())
	}
}

// TestWriteFragmentThroughDanglingSymlink pins that a symlink to a
// not-yet-existing fragment is followed too: the write creates the target and
// the link stays a symlink aimed at it. Both an absolute and a relative link
// target are covered.
func TestWriteFragmentThroughDanglingSymlink(t *testing.T) {
	valid := domain.Fragment{Sections: []domain.Section{{
		Kind:    "server",
		Entries: []domain.Entry{{Key: "local-zone", Value: `"example.com" static`}},
	}}}
	for _, tc := range []struct {
		name     string
		relative bool // whether the link target is spelled relative to the link's dir
	}{
		{"absolute target", false},
		{"relative target", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			real := filepath.Join(dir, "real.conf") // does not exist yet
			link := filepath.Join(dir, "link.conf")
			target := real
			if tc.relative {
				target = filepath.Base(real)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
			conf := writeTemp(t, "include: "+real+"\n")
			m, err := NewManager(conf)
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			m.SetFragmentPath(link)

			if err := m.WriteFragment(valid); err != nil {
				t.Fatalf("WriteFragment: %v", err)
			}
			if _, err := os.Stat(real); err != nil {
				t.Errorf("dangling symlink target was not created: %v", err)
			}
			li, err := os.Lstat(link)
			if err != nil {
				t.Fatalf("Lstat(link): %v", err)
			}
			if li.Mode()&os.ModeSymlink == 0 {
				t.Errorf("link is no longer a symlink (mode %v)", li.Mode())
			}
			got, err := os.ReadFile(real)
			if err != nil {
				t.Fatalf("ReadFile(real): %v", err)
			}
			if want := string(SerializeFragment(valid)); string(got) != want {
				t.Errorf("real target content = %q, want %q", got, want)
			}
		})
	}
}

// TestAtomicWriteFileStatError pins the mode-preservation contract's error
// branch: only a missing file falls back to the passed perm. Any other stat
// error — here ENOTDIR because the path sits under a regular file — must be
// returned (wrapped) rather than swallowed, which would silently reset the
// mode to the passed perm.
func TestAtomicWriteFileStatError(t *testing.T) {
	dir := t.TempDir()
	notDir := filepath.Join(dir, "notadir")
	if err := os.WriteFile(notDir, []byte("x"), 0644); err != nil {
		t.Fatalf("write notadir: %v", err)
	}
	err := atomicWriteFile(filepath.Join(notDir, "frag.conf"), []byte("server:\n"), 0644)
	if err == nil {
		t.Fatalf("atomicWriteFile under a regular file = nil, want a stat error")
	}
	if !strings.Contains(err.Error(), "stat fragment") {
		t.Errorf("atomicWriteFile error = %v, want a wrapped stat error", err)
	}
}

// TestIncludeMatches pins the include-glob predicate behind CheckInclude:
// an exact literal path, a syntactically matching glob (which works before the
// fragment exists), a glob that only matches once paths are resolved, and the
// non-matches (empty target, plain mismatch, invalid pattern).
func TestIncludeMatches(t *testing.T) {
	dir := t.TempDir()
	frag := filepath.Join(dir, "unbound-tui.conf")
	if err := os.WriteFile(frag, []byte("server:\n"), 0644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	// An unresolved path for the same file: filepath.Match cannot cross the
	// extra separator, so only the Abs-normalized glob comparison can match it.
	dotted := dir + "/../" + filepath.Base(dir) + "/unbound-tui.conf"

	cases := []struct {
		name     string
		target   string
		fragment string
		want     bool
	}{
		{"literal path", frag, frag, true},
		{"glob matches the fragment", filepath.Join(dir, "*.conf"), frag, true},
		{"glob matches a fragment that does not exist yet", filepath.Join(dir, "*.conf"), filepath.Join(dir, "later.conf"), true},
		{"resolved path resolves to the fragment", filepath.Join(dir, "*.conf"), dotted, true},
		{"plain path that differs", filepath.Join(dir, "other.conf"), frag, false},
		{"empty target", "", frag, false},
		{"invalid glob pattern", "[", frag, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := includeMatches(tc.target, tc.fragment, dir); got != tc.want {
				t.Errorf("includeMatches(%q, %q, %q) = %v, want %v", tc.target, tc.fragment, dir, got, tc.want)
			}
		})
	}
}

// TestIncludeMatchesRelativeTargets pins that relative targets resolve against
// the supplied main config directory rather than the process working directory.
func TestIncludeMatchesRelativeTargets(t *testing.T) {
	dir := t.TempDir()
	confD := filepath.Join(dir, "conf.d")
	if err := os.MkdirAll(confD, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	frag := filepath.Join(confD, "unbound-tui.conf")

	cases := []struct {
		name   string
		target string
		want   bool
	}{
		{"relative literal", "conf.d/unbound-tui.conf", true},
		{"relative glob", "conf.d/*.conf", true},
		{"relative literal other file", "conf.d/other.conf", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := includeMatches(tc.target, frag, dir); got != tc.want {
				t.Errorf("includeMatches(%q, %q, %q) = %v, want %v", tc.target, frag, dir, got, tc.want)
			}
		})
	}
}

// TestIncludeMatchesSymlinkedFragment pins that symlinked and real spellings of
// the same fragment compare equal. The matcher must resolve symlinks the same
// way the rest of the package does (ResolvePath); otherwise the apply gate
// permanently blocks a fragment included by its real path but passed as a
// symlink, or vice versa.
func TestIncludeMatchesSymlinkedFragment(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real", "unbound-tui.conf")
	if err := os.MkdirAll(filepath.Dir(real), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(real, []byte("server:\n"), 0644); err != nil {
		t.Fatalf("write real fragment: %v", err)
	}
	link := filepath.Join(dir, "link.conf")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	cases := []struct {
		name     string
		target   string
		fragment string
		want     bool
	}{
		{"target real, fragment symlink", real, link, true},
		{"target symlink, fragment real", link, real, true},
		{"target real glob, fragment symlink", filepath.Join(filepath.Dir(real), "*.conf"), link, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := includeMatches(tc.target, tc.fragment, dir); got != tc.want {
				t.Errorf("includeMatches(%q, %q, %q) = %v, want %v", tc.target, tc.fragment, dir, got, tc.want)
			}
		})
	}
}

// TestCheckIncludeSymlinkedFragment pins the apply gate end to end: a main
// config that includes the fragment's real path accepts a fragment path given
// as a symlink.
func TestCheckIncludeSymlinkedFragment(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "unbound-tui.conf")
	if err := os.WriteFile(real, []byte("server:\n"), 0644); err != nil {
		t.Fatalf("write real fragment: %v", err)
	}
	link := filepath.Join(dir, "link.conf")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	conf := writeTemp(t, "include: "+real+"\n")
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetFragmentPath(link)
	if err := m.CheckInclude(); err != nil {
		t.Errorf("CheckInclude with a symlinked fragment path = %v, want nil", err)
	}
}
