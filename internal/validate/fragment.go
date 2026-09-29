package validate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// fragmentKeyRe is the allowed shape of a fragment directive key: an unquoted
// bare token of letters, digits, underscore or hyphen.
var fragmentKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// namedKinds are the section kinds identified by their first `name` entry for
// the duplicate check.
var namedKinds = map[string]bool{
	"forward-zone": true,
	"stub-zone":    true,
	"view":         true,
}

// ValidateFragment is the single pre-write gate for the generic fragment
// model. It rejects malformed directive keys and values and duplicate named
// sections before anything reaches the fragment file or unbound-control.
//
// Values stay deliberately permissive: quotes, spaces and '#' are legal (they
// are the normal shape of local-zone/local-data values); only control
// characters are forbidden. Tab is rejected too, so a value that needs a
// column separator must be written with spaces.
func ValidateFragment(f domain.Fragment) error {
	seen := make(map[string][]string) // kind -> first-seen name per section
	for _, s := range f.Sections {
		kind := s.Kind
		if kind == "" {
			kind = "(top level)"
		}
		for i, e := range s.Entries {
			if !fragmentKeyRe.MatchString(e.Key) {
				return fmt.Errorf("section %s: entry %d: invalid key %q", kind, i+1, e.Key)
			}
			if badValue(e.Value) {
				return fmt.Errorf("section %s: entry %d: invalid value %q", kind, i+1, e.Value)
			}
		}

		name, ok := sectionName(s)
		if !ok || !namedKinds[s.Kind] {
			continue
		}
		for _, prev := range seen[s.Kind] {
			if strings.EqualFold(prev, name) {
				return fmt.Errorf("duplicate %s %q", s.Kind, prev)
			}
		}
		seen[s.Kind] = append(seen[s.Kind], name)
	}
	return nil
}

// sectionName returns the identity of a section: the value of its first `name`
// entry with surrounding double quotes trimmed. ok is false when the section
// declares no name.
func sectionName(s domain.Section) (string, bool) {
	for _, e := range s.Entries {
		if e.Key == "name" {
			return strings.Trim(e.Value, `"`), true
		}
	}
	return "", false
}

// badValue reports whether a value contains a control character (< 0x20, tab
// included) or DEL (0x7f), either of which could break the line-oriented
// fragment format or the unbound-control wire format.
func badValue(v string) bool {
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
