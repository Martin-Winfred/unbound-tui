package config

import (
	"errors"
	"io/fs"
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
