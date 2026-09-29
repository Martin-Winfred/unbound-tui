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

// TestIncludeMatches pins the include-glob predicate behind CheckInclude:
// literal containment, a syntactically matching glob (which works before the
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
			if got := includeMatches(tc.target, tc.fragment); got != tc.want {
				t.Errorf("includeMatches(%q, %q) = %v, want %v", tc.target, tc.fragment, got, tc.want)
			}
		})
	}
}
