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
	key, err := resolvedPath(path)
	if err != nil {
		return fmt.Errorf("resolve config %s: %w", path, err)
	}
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

	if strings.ContainsAny(value, "*?[") {
		pattern := filepath.Join(dir, value)
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

	target := filepath.Join(dir, value)
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("%s:%d: %s %q: %w", from, line, entry.Key, target, err)
	}
	return r.walk(target)
}

// isIncludeKey reports whether a directive key follows an include.
func isIncludeKey(key string) bool {
	return key == "include" || key == "include-toplevel"
}

// resolvedPath returns the absolute, symlink-resolved spelling of path. When
// the path does not exist, EvalSymlinks fails and the absolute path is used
// instead, so the key is still usable for reporting.
func resolvedPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
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
