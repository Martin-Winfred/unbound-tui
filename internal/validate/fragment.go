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
// model. It rejects malformed directive keys and values, malformed non-empty
// section kinds, active named sections (forward-zone/stub-zone/view) that do
// not declare a usable `name`, and duplicate named sections before anything
// reaches the fragment file or unbound-control.
//
// Values stay deliberately permissive for legacy hand-written data: quotes,
// spaces, '#' and '\' are legal (they are the normal shape of
// local-zone/local-data values). But a value must remain a single value: it
// may not smuggle in a second directive, so a control character, an
// unbalanced double quote, or an unquoted `token:` (= a directive shape) is
// refused. Tab is rejected too, so a value that needs a column separator must
// be written with spaces.
func ValidateFragment(f domain.Fragment) error {
	seen := make(map[string][]string) // kind -> first-seen name per section
	for _, s := range f.Sections {
		if s.Kind != "" && !fragmentKeyRe.MatchString(s.Kind) {
			return fmt.Errorf("invalid section kind %q", s.Kind)
		}
		kind := s.Kind
		if kind == "" {
			kind = "(top level)"
		}
		for i, e := range s.Entries {
			if !fragmentKeyRe.MatchString(e.Key) {
				return fmt.Errorf("section %s: entry %d: invalid key %q", kind, i+1, e.Key)
			}
			if err := valueErr(e.Value); err != nil {
				return fmt.Errorf("section %s: entry %d: invalid value %q: %w", kind, i+1, e.Value, err)
			}
		}

		if !namedKinds[s.Kind] {
			continue
		}
		// A fully disabled section is defined-but-commented and cannot clash
		// with another same-named section, so it stays legal and never takes
		// part in the duplicate check (this mirrors config.HasActiveEntries).
		if !sectionHasActiveEntries(s) {
			continue
		}
		// An active forward-zone/stub-zone/view must identify itself with an
		// active `name` entry: unbound cannot parse such a section without
		// `name`, and a name that survives only in disabled entries is written
		// commented out. A name present only in disabled entries therefore
		// counts as missing.
		if _, ok := sectionActiveName(s); !ok {
			return fmt.Errorf("section %s: missing name", s.Kind)
		}
		// The duplicate identity still comes from the first `name` entry
		// regardless of its Disabled flag (Task 6 semantics): the rest of the
		// section is active, so a disabled first-name entry does not keep it out
		// of the duplicate check. Fully disabled sections are skipped above, so
		// their names never reach seen.
		name, _ := sectionName(s)
		for _, prev := range seen[s.Kind] {
			if domain.EqualName(prev, name) {
				return fmt.Errorf("duplicate %s %q", s.Kind, prev)
			}
		}
		seen[s.Kind] = append(seen[s.Kind], name)
	}
	return nil
}

// sectionHasActiveEntries reports whether s holds at least one enabled entry.
// It mirrors config.HasActiveEntries, kept local so validate stays a leaf over
// domain.
func sectionHasActiveEntries(s domain.Section) bool {
	for _, e := range s.Entries {
		if !e.Disabled {
			return true
		}
	}
	return false
}

// sectionActiveName returns the identity of a section read from an ACTIVE
// (non-disabled) `name` entry, normalized through domain.NormalizeName. It is
// false when no active `name` entry carries a non-empty value: a name that
// survives only in disabled entries counts as absent, because serialization
// writes it commented out and unbound refuses a forward-zone/stub-zone/view
// without `name:`.
func sectionActiveName(s domain.Section) (string, bool) {
	for _, e := range s.Entries {
		if e.Key != "name" || e.Disabled {
			continue
		}
		if n := domain.NormalizeName(e.Value); strings.TrimSpace(n) != "" {
			return n, true
		}
	}
	return "", false
}

// sectionName returns the identity of a section: the value of its first `name`
// entry normalized through domain.NormalizeName (whitespace trimmed, one
// leading and one trailing quote stripped). ok is false when the section
// declares no name.
func sectionName(s domain.Section) (string, bool) {
	for _, e := range s.Entries {
		if e.Key == "name" {
			// One-quote-per-side is intentional (M5 consolidation): `""x""` and `"x"` are distinct identities, not duplicates; do not "fix" back.
			return domain.NormalizeName(e.Value), true
		}
	}
	return "", false
}

// valueErr rejects values that cannot be written as a single `key: value`
// line. An empty or whitespace-only value is refused too: it serializes as
// `key: ` (trailing space), which the parser reads back as a section header,
// not an entry. It is the tolerant backstop for data that already exists in a
// fragment: control characters, unbalanced double quotes, and an unquoted
// `token:` (= a second directive) are refused; `#`, `\`, quoted content and
// IPv6 literals (`2001:db8::1`) stay allowed so hand-written values keep
// loading.
func valueErr(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("empty value")
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("control character")
		}
	}
	inQuote := false
	for i := 0; i < len(v); i++ {
		switch {
		case v[i] == '\\' && i+1 < len(v):
			i++ // skip the escaped byte
		case v[i] == '"':
			inQuote = !inQuote
		case !inQuote && v[i] == ':':
			j := i
			for j > 0 && isFragmentKeyByte(v[j-1]) {
				j--
			}
			if j == i {
				continue // empty key run, e.g. the second colon of "::"
			}
			// Only an IPv6 hex group before the colon (and a hex digit or
			// another colon after it) is a value, not a directive. This keeps
			// `2001:db8::1` and `dead:beef::1` while refusing `include:/tmp`
			// and `local-zone:evil.org`, which unbound lexes as directives.
			if isIPv6Group(v[j:i]) && i+1 < len(v) && (isHexByte(v[i+1]) || v[i+1] == ':') {
				continue
			}
			return fmt.Errorf("embedded directive %q", v[j:i+1])
		}
	}
	if inQuote {
		return fmt.Errorf("unbalanced quote")
	}
	return nil
}

func isFragmentKeyByte(c byte) bool {
	return c == '_' || c == '-' ||
		(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isIPv6Group reports whether s is one hex group of an IPv6 address: one to
// four hex digits.
func isIPv6Group(s string) bool {
	if len(s) == 0 || len(s) > 4 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isHexByte(s[i]) {
			return false
		}
	}
	return true
}

func isHexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
