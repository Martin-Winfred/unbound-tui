package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// EffectiveSection is one section of the flattened effective configuration,
// tagged with the absolute, symlink-resolved file it was declared in.
type EffectiveSection struct {
	domain.Section
	Source string
}

// Effective is the fully expanded configuration: every section reachable from
// the main config through the include graph, in effective order, plus the
// deduplicated list of files that contributed to it.
type Effective struct {
	Sections []EffectiveSection
	Files    []string
}

// ReadEffective flattens the include graph rooted at mainConfPath into the
// effective ordered section list. include/include-toplevel directives are
// followed in place, with their value resolved relative to the directory of
// the file that declares them.
//
// Unlike ParseFragment, the main config must exist: a missing main config, or
// a missing literal include, is an error naming the resolved absolute path and
// wrapping the OS error. An include value containing any of `*?[` is a glob,
// expanded with filepath.Glob in sorted order; a glob matching nothing is
// skipped silently, while a malformed glob is an error naming the pattern.
//
// Files are deduplicated and reads are cycle-safe: a file is keyed by its
// symlink-resolved absolute path (falling back to the absolute path when the
// file does not exist yet), and an already-visited file is skipped. A cyclic
// include graph therefore terminates with each file's sections appearing
// exactly once. Files lists every file actually parsed, in first-visit order.
//
// Sections are per-file units (M2 scope). An include between sections inlines
// the included file's sections at that point. An include that appears while a
// section is open closes that section first, recurses, and then any entries
// following the include in the same file start a NEW section of the same kind.
// This deliberately simplifies Unbound's textual inlining, where an include
// inside an open section continues that section: the M2 reader only needs
// section order and origins, not the merged clause.
//
// Disabled content (a `# unbound-tui:disabled` block) is passed through
// honestly: disabled entries — including those following a disabled section
// header — keep Disabled set. A commented-out include line is never followed.
func ReadEffective(mainConfPath string) (Effective, error) {
	r := &effectiveReader{visited: make(map[string]bool)}
	if err := r.walk(mainConfPath); err != nil {
		return Effective{}, err
	}
	return Effective{Sections: r.sections, Files: r.files}, nil
}

// effectiveReader accumulates the flattened graph across recursive walks.
type effectiveReader struct {
	sections []EffectiveSection
	files    []string
	visited  map[string]bool
}

// walk parses one file, inlining includes at their position. The visited check
// happens before the read, so a file reached twice (directly or through a
// cycle) is parsed once.
func (r *effectiveReader) walk(path string) error {
	key := resolveOwn(path)
	if r.visited[key] {
		return nil
	}
	r.visited[key] = true
	r.files = append(r.files, key)

	data, err := os.ReadFile(key)
	if err != nil {
		return fmt.Errorf("read config %s: %w", key, err)
	}

	dir := filepath.Dir(key)
	includeLines := activeIncludeLines(data)

	var (
		cur        = -1   // index of the section receiving entries, -1 for none
		resumeKind string // kind to reopen after an include closed the section
		lineIdx    int    // position into includeLines
	)
	open := func(kind string) {
		r.sections = append(r.sections, EffectiveSection{
			Section: domain.Section{Kind: kind},
			Source:  key,
		})
		cur = len(r.sections) - 1
	}

	for _, item := range scanConfig(data) {
		if item.header {
			open(item.kind)
			resumeKind = ""
			continue
		}

		if isIncludeKey(item.entry.Key) && !item.disabled {
			// Close the open section so the included content lands before
			// this file's remaining entries; remember the kind so they reopen
			// a new section of the same kind rather than a synthetic one.
			if cur >= 0 {
				resumeKind = r.sections[cur].Kind
			}
			cur = -1

			line := 0
			if lineIdx < len(includeLines) {
				line = includeLines[lineIdx]
			}
			lineIdx++
			if err := r.followInclude(key, dir, line, item.entry); err != nil {
				return err
			}
			continue
		}

		if cur < 0 {
			open(resumeKind)
			resumeKind = ""
		}
		r.sections[cur].Entries = append(r.sections[cur].Entries, item.entry)
	}

	return nil
}

// followInclude resolves and recursively walks the target of one include
// directive. from and line locate the directive for error messages.
func (r *effectiveReader) followInclude(from, dir string, line int, entry domain.Entry) error {
	value, _, err := cutToken(entry.Value)
	if err != nil {
		return fmt.Errorf("%s:%d: %s: %w", from, line, entry.Key, err)
	}

	// An absolute target is used verbatim; only relative values resolve
	// against the declaring file's directory. filepath.Join would otherwise
	// append an absolute value to dir (Join("/a", "/b") == "/a/b").
	resolve := func(v string) string {
		if filepath.IsAbs(v) {
			return v
		}
		return filepath.Join(dir, v)
	}

	if strings.ContainsAny(value, "*?[") {
		pattern := resolve(value)
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("%s:%d: %s glob %q: %w", from, line, entry.Key, pattern, err)
		}
		for _, match := range matches {
			if err := r.walk(match); err != nil {
				return err
			}
		}
		return nil
	}

	target := resolve(value)
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("%s:%d: %s %q: %w", from, line, entry.Key, target, err)
	}
	return r.walk(target)
}

// isIncludeKey reports whether a directive key follows an include.
func isIncludeKey(key string) bool {
	return key == "include" || key == "include-toplevel"
}

// resolveOwn returns the absolute, symlink-resolved spelling of path. When the
// path does not exist, EvalSymlinks fails and the absolute path is used
// instead; if even Abs fails the original path is returned. This is the single
// normalization shared by the include-graph visited keys and the Source tag on
// every EffectiveSection, and it is also applied to FindConflicts' ownPath —
// so two spellings of the same file (relative, symlinked) compare equal on
// both sides instead of making our own fragment look foreign.
func resolveOwn(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// Conflict is one foreign forward-zone/stub-zone that collides with a section
// of ours. Kind and Name are the foreign section's kind and normalized
// identity name; Source is the file it was declared in, so the user can go
// find it.
type Conflict struct {
	Kind   string
	Name   string
	Source string
}

// SectionKeyName returns the normalized identity name of a section: the value
// of its first `name` entry, whitespace trimmed, with exactly one leading and
// one trailing double quote stripped. A forward-zone with no usable name is
// the root zone ".". A stub-zone or view with no usable name has no identity
// (""), and so does any non-identity-bearing kind.
func SectionKeyName(s domain.Section) string {
	switch s.Kind {
	case "forward-zone", "stub-zone", "view":
	default:
		return ""
	}
	name := ""
	for _, e := range s.Entries {
		if e.Key == "name" {
			name = strings.TrimSpace(e.Value)
			break
		}
	}
	name = strings.TrimPrefix(name, `"`)
	name = strings.TrimSuffix(name, `"`)
	if name == "" && s.Kind == "forward-zone" {
		return "."
	}
	return name
}

// FindConflicts reports every active forward-zone/stub-zone of the fragment we
// own (f) that collides, by kind and case-insensitive identity name, with an
// active foreign section elsewhere in the effective include graph (eff).
//
// Only sections with at least one active entry participate, on either side: a
// fully disabled section is defined-but-commented and cannot clash. The
// identity name is still read from the first name entry even when that entry
// is disabled, because the rest of the section is active. ownPath is the
// fragment path as the caller spells it; it is normalized with the same
// resolver used for every Source, so our own included fragment never
// self-conflicts. Conflicts are reported once per foreign section, in
// effective order.
func FindConflicts(f domain.Fragment, eff Effective, ownPath string) []Conflict {
	own := resolveOwn(ownPath)

	type identity struct{ kind, name string }
	var ours []identity
	for _, s := range f.Sections {
		if !isForwardOrStub(s.Kind) || !hasActive(s.Entries) {
			continue
		}
		if name := SectionKeyName(s); name != "" {
			ours = append(ours, identity{s.Kind, name})
		}
	}
	if len(ours) == 0 {
		return nil
	}

	var out []Conflict
	for _, fs := range eff.Sections {
		if !isForwardOrStub(fs.Kind) || !hasActive(fs.Entries) || fs.Source == own {
			continue
		}
		name := SectionKeyName(fs.Section)
		if name == "" {
			continue
		}
		for _, o := range ours {
			if o.kind == fs.Kind && strings.EqualFold(o.name, name) {
				out = append(out, Conflict{Kind: fs.Kind, Name: name, Source: fs.Source})
				break
			}
		}
	}
	return out
}

// isForwardOrStub reports whether a section kind takes part in conflict
// detection.
func isForwardOrStub(kind string) bool {
	return kind == "forward-zone" || kind == "stub-zone"
}

// activeIncludeLines returns the 1-based line numbers of the active include
// directives, in order. scanConfig carries no line numbers, so walk pairs its
// active include items with this list positionally to keep errors actionable.
// Commented (disabled) and section-header includes never appear here.
func activeIncludeLines(src []byte) []int {
	var lines []int
	for i, raw := range strings.Split(string(src), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, header := cutDirective(line)
		if !header && isIncludeKey(key) {
			lines = append(lines, i+1)
		}
	}
	return lines
}
