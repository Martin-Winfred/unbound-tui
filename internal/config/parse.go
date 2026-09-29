package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// disabledMarker introduces the block of commented-out (disabled) entries.
// The parser enters disabled-block mode on it; the serializer emits it before
// the grouped disabled entries.
const disabledMarker = "# unbound-tui:disabled"

// disabledKeyRe matches the leading `<key>:` of a commented body that is a
// directive rather than prose. The key must be non-empty and consist solely of
// letters, digits, underscore or hyphen, immediately followed by the colon.
var disabledKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+:`)

// ParseFragment reads the fragment file into the generic section/entry model.
// A missing file yields an empty Fragment and a nil error.
func ParseFragment(path string) (domain.Fragment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.Fragment{}, nil
		}
		return domain.Fragment{}, fmt.Errorf("read fragment: %w", err)
	}
	return parseFragment(data)
}

// parseFragment parses a fragment file into its generic section/entry model.
//
// The file is a flat sequence of directives. A directive whose text after the
// first ":" is empty is a section header ("server:", "forward-zone:", ...) and
// closes the previous section; every other directive is an entry belonging to
// the section currently open, or to a synthetic top-level section (Kind "")
// when no header has been seen yet.
//
// Disabled data is kept as commented lines after the disabledMarker. Inside
// such a block the "# " prefix is stripped and, when the body looks like a
// directive, the line is parsed as usual with Disabled set. A disabled section
// header folds into the most recent active section of the same Kind, or opens
// a new fully-disabled section; a disabled entry with no header of its own
// attaches to the most recent active section. A blank line or an active
// directive ends the disabled block.
//
// A commented body inside the block is treated as a directive only when it
// starts with a `<key>:` whose key is non-empty and made solely of
// [A-Za-z0-9_-]; any other body (no colon, or a key with illegal characters
// such as spaces) is prose and is skipped entirely. This restores the v0.1
// behavior of recognizing only real directive comments. Known accepted
// limitation: prose shaped exactly like `word: text` is still parsed as an
// entry; the M2 editor supersedes this.
//
// Two round-trip limits are inherent to the append-at-end model layout (spec
// §4) and are accepted: (a) a fully-disabled section always re-emerges at the
// END of the disabled block, and (b) a headerless synthetic disabled group
// that is not the first group in the block is absorbed into the preceding
// group on reparse (the data is preserved; the tool's own writes never emit
// that shape).
//
// Parsing is total: unknown directives are data, not errors, and disabled
// comment blocks outside a section attach to the open section. v1 therefore
// always returns a nil error.
func parseFragment(src []byte) (domain.Fragment, error) {
	var (
		sections []domain.Section
		active   []bool // parallel to sections: opened by an uncommented header/entry
		cur      = -1   // section receiving active entries
		disCur   = -1   // section receiving entries inside a disabled block
	)

	for _, item := range scanConfig(src) {
		// Any active directive, and the first item of a disabled group, end
		// the previous group's attachment target. scanConfig marks the group
		// start so blank- and marker-separated groups reset exactly as the
		// old line walker did.
		if !item.disabled || item.groupStart {
			disCur = -1
		}

		if item.header {
			if item.disabled {
				if idx := lastActive(sections, active, item.kind); idx >= 0 {
					disCur = idx
				} else {
					sections = append(sections, domain.Section{Kind: item.kind})
					active = append(active, false)
					disCur = len(sections) - 1
				}
				continue
			}
			sections = append(sections, domain.Section{Kind: item.kind})
			active = append(active, true)
			cur = len(sections) - 1
			continue
		}

		if item.disabled {
			if disCur < 0 {
				disCur = cur
			}
			if disCur < 0 {
				sections = append(sections, domain.Section{Kind: ""})
				active = append(active, false)
				disCur = len(sections) - 1
				cur = disCur
			}
			sections[disCur].Entries = append(sections[disCur].Entries, item.entry)
			continue
		}

		if cur < 0 {
			sections = append(sections, domain.Section{Kind: ""})
			active = append(active, true)
			cur = len(sections) - 1
		}
		sections[cur].Entries = append(sections[cur].Entries, item.entry)
	}

	return domain.Fragment{Sections: sections}, nil
}

// scanItem is one grammatical item of a config file, in order.
type scanItem struct {
	header   bool         // true: section header
	kind     string       // header: the section kind
	entry    domain.Entry // non-header: a directive line (include entries included)
	disabled bool         // item came from inside the disabled block (commented)
	// groupStart marks the first disabled item of a group, where a group is a
	// run of disabled items preceded by the marker, a blank line or an active
	// directive. Callers that attach disabled items to a target section reset
	// that target on a group start, reproducing the line walker's boundaries.
	groupStart bool
}

// scanConfig classifies the lines of a config file into an ordered item
// stream. It owns the lexical concerns only: whitespace trimming (including
// CRLF), the section-header grammar (an empty value after the first ":"),
// ordinary comments, and disabled-block mode (the marker, the "# " body
// extraction and the prose skip). For disabled items it also reports
// groupStart, the lexical boundary between disabled groups (a blank line, the
// marker or an active directive), so callers can reset their
// disabled-attachment target there. scanConfig itself selects no target
// section; parseFragment folds the stream into sections and consumes
// groupStart for that reset.
func scanConfig(src []byte) []scanItem {
	var (
		items     []scanItem
		inDisable bool
		disGroup  bool // a disabled item was already emitted for this group
	)

	for _, raw := range strings.Split(string(src), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))

		switch {
		case line == "":
			// A blank line terminates a disabled block; it does not close
			// the open active section.
			inDisable = false
			disGroup = false

		case line == disabledMarker:
			inDisable = true
			disGroup = false

		case strings.HasPrefix(line, "#"):
			if !inDisable {
				continue // ordinary comment / file header
			}
			body := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "#"), " "))
			if body == "" {
				continue
			}
			if !disabledKeyRe.MatchString(body) {
				continue // prose / ordinary comment, not a disabled directive
			}
			key, value, header := cutDirective(body)
			item := scanItem{disabled: true, groupStart: !disGroup}
			if header {
				item.header = true
				item.kind = key
			} else {
				item.entry = domain.Entry{Key: key, Value: value, Disabled: true}
			}
			items = append(items, item)
			disGroup = true

		default:
			inDisable = false
			disGroup = false
			key, value, header := cutDirective(line)
			if header {
				items = append(items, scanItem{header: true, kind: key})
				continue
			}
			items = append(items, scanItem{entry: domain.Entry{Key: key, Value: value}})
		}
	}

	return items
}

// cutDirective splits a directive at its first ":" into the key and the raw
// text after it. header reports that the value is empty, marking a section
// header rather than an entry. A line without ":" is an entry keyed by the
// whole line.
func cutDirective(line string) (key, value string, header bool) {
	key, value, found := strings.Cut(line, ":")
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if !found || value != "" {
		return key, value, false
	}
	return key, "", true
}

// lastActive returns the index of the most recent active section of kind, or
// -1 when none exists.
func lastActive(sections []domain.Section, active []bool, kind string) int {
	for i := len(sections) - 1; i >= 0; i-- {
		if active[i] && sections[i].Kind == kind {
			return i
		}
	}
	return -1
}
