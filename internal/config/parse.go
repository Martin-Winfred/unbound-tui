package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

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
// such a block the "# " prefix is stripped and the line is parsed as usual with
// Disabled set. A disabled section header folds into the most recent active
// section of the same Kind, or opens a new fully-disabled section; a disabled
// entry with no header of its own attaches to the most recent active section.
// A blank line or an active directive ends the disabled block.
//
// Parsing is total: unknown directives are data, not errors, and disabled
// comment blocks outside a section attach to the open section. v1 therefore
// always returns a nil error.
func parseFragment(src []byte) (domain.Fragment, error) {
	var (
		sections  []domain.Section
		active    []bool // parallel to sections: opened by an uncommented header/entry
		cur       = -1   // section receiving active entries
		inDisable bool
		disCur    = -1 // section receiving entries inside a disabled block
	)

	for _, raw := range strings.Split(string(src), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))

		switch {
		case line == "":
			// A blank line terminates a disabled block; it does not close
			// the open active section.
			inDisable = false
			disCur = -1
			continue

		case line == disabledMarker:
			inDisable = true
			disCur = -1
			continue

		case strings.HasPrefix(line, "#"):
			if !inDisable {
				continue // ordinary comment / file header
			}
			body := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "#"), " "))
			if body == "" {
				continue
			}
			key, value, header := cutDirective(body)
			if header {
				if idx := lastActive(sections, active, key); idx >= 0 {
					disCur = idx
				} else {
					sections = append(sections, domain.Section{Kind: key})
					active = append(active, false)
					disCur = len(sections) - 1
				}
				continue
			}
			if disCur < 0 {
				disCur = cur
			}
			if disCur < 0 {
				sections = append(sections, domain.Section{Kind: ""})
				active = append(active, false)
				disCur = len(sections) - 1
				cur = disCur
			}
			sections[disCur].Entries = append(sections[disCur].Entries,
				domain.Entry{Key: key, Value: value, Disabled: true})

		default:
			inDisable = false
			disCur = -1
			key, value, header := cutDirective(line)
			if header {
				sections = append(sections, domain.Section{Kind: key})
				active = append(active, true)
				cur = len(sections) - 1
				continue
			}
			if cur < 0 {
				sections = append(sections, domain.Section{Kind: ""})
				active = append(active, true)
				cur = len(sections) - 1
			}
			sections[cur].Entries = append(sections[cur].Entries,
				domain.Entry{Key: key, Value: value})
		}
	}

	return domain.Fragment{Sections: sections}, nil
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
