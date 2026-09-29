package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// fixturePath resolves a path under the package testdata tree to an absolute,
// symlink-resolved path. Tests compare against the same spelling ReadEffective
// reports (Sources and Files are symlink-resolved), so the repo may live behind
// a symlinked prefix.
func fixturePath(t *testing.T, parts ...string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join(append([]string{"testdata"}, parts...)...))
	if err != nil {
		t.Fatalf("abs fixture path: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

func effEntry(key, value string) domain.Entry {
	return domain.Entry{Key: key, Value: value}
}

func effSec(kind, src string, entries ...domain.Entry) EffectiveSection {
	return EffectiveSection{Section: domain.Section{Kind: kind, Entries: entries}, Source: src}
}

// TestReadEffectiveInterleave pins positional inlining: an include between an
// open section and later entries must NOT collect the whole including file
// first. The open section is closed before recursing, and the trailing entry
// reopens a new section of the same kind AFTER the included file.
func TestReadEffectiveInterleave(t *testing.T) {
	main := fixturePath(t, "interleave", "main.conf")
	one := fixturePath(t, "interleave", "sub", "one.conf")

	got, err := ReadEffective(main)
	if err != nil {
		t.Fatalf("ReadEffective(%s): %v", main, err)
	}
	want := Effective{
		Sections: []EffectiveSection{
			effSec("server", main, effEntry("local-zone", `"a" static`)),
			effSec("server", one, effEntry("local-zone", `"b" static`)),
			effSec("server", main, effEntry("local-zone", `"c" static`)),
		},
		Files: []string{main, one},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadEffective() =\n%+v\nwant\n%+v", got, want)
	}
}

// TestReadEffectiveGlob checks sorted glob expansion and that a glob with zero
// matches is skipped silently.
func TestReadEffectiveGlob(t *testing.T) {
	main := fixturePath(t, "glob", "main.conf")
	a := fixturePath(t, "glob", "glob-a.conf")
	b := fixturePath(t, "glob", "glob-b.conf")

	got, err := ReadEffective(main)
	if err != nil {
		t.Fatalf("ReadEffective(%s): %v", main, err)
	}
	want := Effective{
		Sections: []EffectiveSection{
			effSec("server", main, effEntry("local-zone", `"main" static`)),
			effSec("server", a, effEntry("local-zone", `"a" static`)),
			effSec("server", b, effEntry("local-zone", `"b" static`)),
		},
		Files: []string{main, a, b},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadEffective() =\n%+v\nwant\n%+v", got, want)
	}
}

// TestReadEffectiveRelative verifies an include value is resolved against the
// including file's directory, not the process working directory: the decoy
// testdata/one.conf must not be picked up.
func TestReadEffectiveRelative(t *testing.T) {
	main := fixturePath(t, "relative", "main.conf")
	one := fixturePath(t, "relative", "one.conf")

	got, err := ReadEffective(main)
	if err != nil {
		t.Fatalf("ReadEffective(%s): %v", main, err)
	}
	want := Effective{
		Sections: []EffectiveSection{
			effSec("server", main, effEntry("local-zone", `"main" static`)),
			effSec("forward-zone", one, effEntry("name", `"relative.example"`)),
		},
		Files: []string{main, one},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadEffective() =\n%+v\nwant\n%+v", got, want)
	}
}

// TestReadEffectiveCycle ensures a cyclic include graph terminates and that
// each file's sections appear exactly once.
func TestReadEffectiveCycle(t *testing.T) {
	a := fixturePath(t, "cycle", "cyclic-a.conf")
	b := fixturePath(t, "cycle", "cyclic-b.conf")

	got, err := ReadEffective(a)
	if err != nil {
		t.Fatalf("ReadEffective(%s): %v", a, err)
	}
	want := Effective{
		Sections: []EffectiveSection{
			effSec("server", a, effEntry("local-zone", `"a" static`)),
			effSec("server", b, effEntry("local-zone", `"b" static`)),
		},
		Files: []string{a, b},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadEffective() =\n%+v\nwant\n%+v", got, want)
	}
}

// TestReadEffectiveFilesDeduped pins the visited-file list: first-visit order,
// deduped when the same file is reached twice.
func TestReadEffectiveFilesDeduped(t *testing.T) {
	main := fixturePath(t, "files", "main.conf")
	one := fixturePath(t, "files", "sub", "one.conf")
	two := fixturePath(t, "files", "sub", "two.conf")
	nested := fixturePath(t, "files", "nested.conf")

	got, err := ReadEffective(main)
	if err != nil {
		t.Fatalf("ReadEffective(%s): %v", main, err)
	}
	want := Effective{
		Sections: []EffectiveSection{
			effSec("server", main, effEntry("local-zone", `"main" static`)),
			effSec("server", one, effEntry("local-zone", `"one" static`)),
			effSec("server", nested, effEntry("local-zone", `"nested" static`)),
			effSec("server", two, effEntry("local-zone", `"two" static`)),
		},
		Files: []string{main, one, nested, two},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadEffective() =\n%+v\nwant\n%+v", got, want)
	}
	if len(got.Files) != 4 {
		t.Fatalf("visited %d files, want 4 (nested.conf must be deduped)", len(got.Files))
	}
}

// TestReadEffectiveDisabledPassthrough checks that a disabled block inside an
// included file is preserved honestly: disabled entries (and a disabled
// section header) survive with Disabled set, and nothing is dropped.
func TestReadEffectiveDisabledPassthrough(t *testing.T) {
	main := fixturePath(t, "disabled", "main.conf")
	sub := fixturePath(t, "disabled", "sub", "withdisabled.conf")

	got, err := ReadEffective(main)
	if err != nil {
		t.Fatalf("ReadEffective(%s): %v", main, err)
	}
	want := Effective{
		Sections: []EffectiveSection{
			effSec("server", main, effEntry("local-zone", `"main" static`)),
			effSec("server", sub, effEntry("local-zone", `"active" static`)),
			effSec("forward-zone", sub, domain.Entry{Key: "name", Value: `"off.example"`, Disabled: true}),
		},
		Files: []string{main, sub},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadEffective() =\n%+v\nwant\n%+v", got, want)
	}
}

// TestReadEffectiveMissingLiteral checks that a literal include that does not
// exist is an error naming the resolved absolute target and wrapping the OS
// error.
func TestReadEffectiveMissingLiteral(t *testing.T) {
	main := fixturePath(t, "missing", "main.conf")
	target := filepath.Join(filepath.Dir(main), "nope.conf")

	_, err := ReadEffective(main)
	if err == nil {
		t.Fatal("ReadEffective() = nil error, want missing-include error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error %v is not fs.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), target) {
		t.Errorf("error %q does not name resolved path %q", err, target)
	}
}

// TestReadEffectiveMissingMain checks the top-level contract: unlike
// ParseFragment, a missing main config is an error naming the path.
func TestReadEffectiveMissingMain(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.conf")

	_, err := ReadEffective(absent)
	if err == nil {
		t.Fatal("ReadEffective(missing main) = nil error, want error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error %v is not fs.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), absent) {
		t.Errorf("error %q does not name path %q", err, absent)
	}
}

// resolvedPath returns the absolute, symlink-resolved spelling ReadEffective
// reports for a runtime-created fixture path.
func resolvedPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs %s: %v", path, err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// TestReadEffectiveAbsoluteInclude pins that an absolute include target is used
// verbatim instead of being joined onto the declaring file's directory: both an
// absolute literal include and an absolute include-toplevel glob must be read,
// with the glob expanded in sorted order.
func TestReadEffectiveAbsoluteInclude(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.conf")
	literal := filepath.Join(dir, "literal.conf")
	g1 := filepath.Join(dir, "abs-01.conf")
	g2 := filepath.Join(dir, "abs-02.conf")

	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	write(literal, "forward-zone:\n  name: \"literal.\"\n  forward-addr: 192.0.2.1\n")
	write(g1, "forward-zone:\n  name: \"g1.\"\n")
	write(g2, "forward-zone:\n  name: \"g2.\"\n")
	write(main, "include: "+literal+"\ninclude-toplevel: \""+filepath.Join(dir, "abs-*.conf")+"\"\n")

	got, err := ReadEffective(main)
	if err != nil {
		t.Fatalf("ReadEffective(%s): %v", main, err)
	}
	want := Effective{
		Sections: []EffectiveSection{
			effSec("forward-zone", resolvedPath(t, literal), effEntry("name", `"literal."`), effEntry("forward-addr", "192.0.2.1")),
			effSec("forward-zone", resolvedPath(t, g1), effEntry("name", `"g1."`)),
			effSec("forward-zone", resolvedPath(t, g2), effEntry("name", `"g2."`)),
		},
		Files: []string{resolvedPath(t, main), resolvedPath(t, literal), resolvedPath(t, g1), resolvedPath(t, g2)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadEffective() =\n%+v\nwant\n%+v", got, want)
	}
}

// TestReadEffectiveAbsoluteMissing checks that a missing absolute literal
// include is an error naming that exact absolute path, not a path mangled by
// joining it onto the declaring file's directory.
func TestReadEffectiveAbsoluteMissing(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.conf")
	missing := filepath.Join(dir, "nope", "absent.conf")
	if err := os.WriteFile(main, []byte("include: "+missing+"\n"), 0644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	_, err := ReadEffective(main)
	if err == nil {
		t.Fatal("ReadEffective() = nil error, want missing-include error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error %v is not fs.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), `"`+missing+`"`) {
		t.Errorf("error %q does not name the absolute path %q", err, missing)
	}
}

// TestReadEffectiveMalformedInclude checks the cutToken failure path: the
// error names the offending file and line, and wraps the tokenizer error.
func TestReadEffectiveMalformedInclude(t *testing.T) {
	main := fixturePath(t, "malformed", "main.conf")

	_, err := ReadEffective(main)
	if err == nil {
		t.Fatal("ReadEffective() = nil error, want malformed-include error")
	}
	if !strings.Contains(err.Error(), main+":3") {
		t.Errorf("error %q does not name %s:3", err, main)
	}
	if !strings.Contains(err.Error(), "unterminated quote") {
		t.Errorf("error %q does not mention the tokenizer failure", err)
	}
}

// mustReadEffective reads the include graph for a fixture or fails the test.
func mustReadEffective(t *testing.T, path string) Effective {
	t.Helper()
	eff, err := ReadEffective(path)
	if err != nil {
		t.Fatalf("ReadEffective(%s): %v", path, err)
	}
	return eff
}

// mustParseFragment parses a fixture fragment or fails the test.
func mustParseFragment(t *testing.T, path string) domain.Fragment {
	t.Helper()
	f, err := ParseFragment(path)
	if err != nil {
		t.Fatalf("ParseFragment(%s): %v", path, err)
	}
	return f
}

// TestSectionKeyName pins the identity normalization: the first name entry,
// whitespace trimmed, exactly one leading and one trailing quote stripped; a
// nameless forward-zone is the root ".", while a nameless stub/view has no
// identity and non-identity kinds never carry one.
func TestSectionKeyName(t *testing.T) {
	sec := func(kind string, entries ...domain.Entry) domain.Section {
		return domain.Section{Kind: kind, Entries: entries}
	}
	tests := []struct {
		name string
		sec  domain.Section
		want string
	}{
		{"quoted name", sec("forward-zone", effEntry("name", `"example.com"`)), "example.com"},
		{"bare name", sec("forward-zone", effEntry("name", `example.com`)), "example.com"},
		{"one quote stripped per side", sec("forward-zone", effEntry("name", `""example"`)), `"example`},
		{"whitespace trimmed", sec("forward-zone", effEntry("name", `  "example.com"  `)), "example.com"},
		{"first name entry wins", sec("forward-zone", effEntry("name", `"first"`), effEntry("name", `"second"`)), "first"},
		{"disabled name still identifies", sec("forward-zone", domain.Entry{Key: "name", Value: `"x"`, Disabled: true}, effEntry("forward-addr", "192.0.2.1")), "x"},
		{"nameless forward-zone is root", sec("forward-zone", effEntry("forward-addr", "192.0.2.1")), "."},
		{"nameless stub-zone has no identity", sec("stub-zone", effEntry("stub-addr", "192.0.2.1")), ""},
		{"nameless view has no identity", sec("view", effEntry("view-first", "yes")), ""},
		{"non-identity kind ignores name", sec("server", effEntry("name", `"x"`)), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SectionKeyName(tt.sec); got != tt.want {
				t.Errorf("SectionKeyName(%+v) = %q, want %q", tt.sec, got, tt.want)
			}
		})
	}
}

// TestFindConflicts exercises conflict detection over the include graph: a
// forward-zone/stub-zone of ours collides with an active foreign section of
// the same kind and identity name, sourced from a file other than our own
// fragment. A section with no active entry never participates, on either side.
func TestFindConflicts(t *testing.T) {
	t.Run("same kind and name names the foreign file", func(t *testing.T) {
		ours := fixturePath(t, "conflict", "same", "ours.conf")
		foreign := fixturePath(t, "conflict", "same", "foreign.conf")
		got := FindConflicts(mustParseFragment(t, ours), mustReadEffective(t, fixturePath(t, "conflict", "same", "main.conf")), ours)
		want := []Conflict{{Kind: "forward-zone", Name: "conflict.example", Source: foreign}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FindConflicts() = %+v, want %+v", got, want)
		}
	})

	// The caller may spell our fragment through a symlink while ReadEffective
	// reports the symlink-resolved path. Both sides must normalize identically,
	// or our own included sections would look foreign and self-conflict.
	t.Run("symlinked ownPath does not self-conflict", func(t *testing.T) {
		ours := fixturePath(t, "conflict", "same", "ours.conf")
		foreign := fixturePath(t, "conflict", "same", "foreign.conf")
		link := filepath.Join(t.TempDir(), "own-via-symlink.conf")
		if err := os.Symlink(ours, link); err != nil {
			t.Fatalf("symlink %s -> %s: %v", link, ours, err)
		}
		got := FindConflicts(mustParseFragment(t, ours), mustReadEffective(t, fixturePath(t, "conflict", "same", "main.conf")), link)
		want := []Conflict{{Kind: "forward-zone", Name: "conflict.example", Source: foreign}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FindConflicts(ownPath=%s) = %+v, want only the foreign conflict %+v", link, got, want)
		}
	})

	t.Run("fully disabled foreign section is not a conflict", func(t *testing.T) {
		ours := fixturePath(t, "conflict", "dead", "ours.conf")
		got := FindConflicts(mustParseFragment(t, ours), mustReadEffective(t, fixturePath(t, "conflict", "dead", "main.conf")), ours)
		if len(got) != 0 {
			t.Errorf("FindConflicts() = %+v, want none (all foreign entries disabled)", got)
		}
	})

	// A disabled name entry does not kill the section: the active-entry count
	// decides participation, and the name is still read from that entry.
	t.Run("disabled name with another active entry still matches", func(t *testing.T) {
		ours := fixturePath(t, "conflict", "partial", "ours.conf")
		foreign := fixturePath(t, "conflict", "partial", "foreign.conf")
		got := FindConflicts(mustParseFragment(t, ours), mustReadEffective(t, fixturePath(t, "conflict", "partial", "main.conf")), ours)
		want := []Conflict{{Kind: "forward-zone", Name: "partial.example", Source: foreign}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FindConflicts() = %+v, want %+v", got, want)
		}
	})

	t.Run("our fully disabled section does not conflict", func(t *testing.T) {
		ours := fixturePath(t, "conflict", "ourdisabled", "ours.conf")
		got := FindConflicts(mustParseFragment(t, ours), mustReadEffective(t, fixturePath(t, "conflict", "ourdisabled", "main.conf")), ours)
		if len(got) != 0 {
			t.Errorf("FindConflicts() = %+v, want none (our section has no active entry)", got)
		}
	})

	t.Run("nameless forward-zone matches root", func(t *testing.T) {
		ours := fixturePath(t, "conflict", "nameless-fwd", "ours.conf")
		foreign := fixturePath(t, "conflict", "nameless-fwd", "foreign.conf")
		got := FindConflicts(mustParseFragment(t, ours), mustReadEffective(t, fixturePath(t, "conflict", "nameless-fwd", "main.conf")), ours)
		want := []Conflict{{Kind: "forward-zone", Name: ".", Source: foreign}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FindConflicts() = %+v, want %+v", got, want)
		}
	})

	t.Run("nameless stub-zone has no identity", func(t *testing.T) {
		ours := fixturePath(t, "conflict", "nameless-stub", "ours.conf")
		got := FindConflicts(mustParseFragment(t, ours), mustReadEffective(t, fixturePath(t, "conflict", "nameless-stub", "main.conf")), ours)
		if len(got) != 0 {
			t.Errorf("FindConflicts() = %+v, want none (nameless stub has no identity)", got)
		}
	})

	t.Run("cross-kind same name does not conflict", func(t *testing.T) {
		ours := fixturePath(t, "conflict", "crosskind", "ours.conf")
		got := FindConflicts(mustParseFragment(t, ours), mustReadEffective(t, fixturePath(t, "conflict", "crosskind", "main.conf")), ours)
		if len(got) != 0 {
			t.Errorf("FindConflicts() = %+v, want none (forward-zone vs stub-zone)", got)
		}
	})

	t.Run("case and quote variants conflict", func(t *testing.T) {
		ours := fixturePath(t, "conflict", "casequote", "ours.conf")
		foreign := fixturePath(t, "conflict", "casequote", "foreign.conf")
		got := FindConflicts(mustParseFragment(t, ours), mustReadEffective(t, fixturePath(t, "conflict", "casequote", "main.conf")), ours)
		want := []Conflict{{Kind: "forward-zone", Name: "test.", Source: foreign}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FindConflicts() = %+v, want %+v", got, want)
		}
	})

	t.Run("multiple conflicts keep effective order", func(t *testing.T) {
		ours := fixturePath(t, "conflict", "multi", "ours.conf")
		fa := fixturePath(t, "conflict", "multi", "foreign-a.conf")
		fb := fixturePath(t, "conflict", "multi", "foreign-b.conf")
		fc := fixturePath(t, "conflict", "multi", "foreign-c.conf")
		got := FindConflicts(mustParseFragment(t, ours), mustReadEffective(t, fixturePath(t, "conflict", "multi", "main.conf")), ours)
		want := []Conflict{
			{Kind: "forward-zone", Name: "b.example", Source: fb},
			{Kind: "forward-zone", Name: "a.example", Source: fa},
			{Kind: "stub-zone", Name: "c.example", Source: fc},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FindConflicts() = %+v, want %+v", got, want)
		}
	})
}
