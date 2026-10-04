package domain

import (
	"sort"
	"strings"
)

// NormalizeName returns the normalized identity spelling of a directive value:
// the value with surrounding whitespace trimmed and exactly one leading and
// one trailing double quote stripped. It is the single quote-trim authority
// shared by the config and validate layers.
//
// The one-quote-per-side rule (not strings.Trim, which removes every quote) is
// the semantics config.SectionKeyName pinned in M3 and is authoritative here.
// Pathological values with stacked quotes therefore trim symmetrically, for
// example `""x""` normalizes to `"x"`.
func NormalizeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, `"`)
	s = strings.TrimSuffix(s, `"`)
	return s
}

// zoneTypes is the canonical set of local-zone types accepted by the tool. It
// mirrors unbound.conf(5); types outside it are rejected at input time. The
// set lives here so config and validate share one list without importing each
// other (both import domain, which stays a leaf).
var zoneTypes = map[string]bool{
	"deny": true, "refuse": true, "static": true, "transparent": true,
	"typetransparent": true, "redirect": true, "nodefault": true,
	"inform": true, "inform_deny": true, "inform_redirect": true,
	"always_transparent": true, "always_refuse": true, "always_nxdomain": true,
	"always_nodata": true, "always_deny": true, "always_null": true, "noview": true,
	"block_a": true, "block_aaaa": true,
	"block_a_wdata": true, "block_aaaa_wdata": true,
}

// IsZoneTypeName reports whether t is a known local-zone type name.
func IsZoneTypeName(t string) bool { return zoneTypes[t] }

// ZoneTypeNames returns the canonical local-zone type names in sorted order.
// It is the choice list behind the Type picker in the zone forms; the gate
// itself stays IsZoneTypeName, so the list can never drift from what is
// accepted.
func ZoneTypeNames() []string {
	out := make([]string, 0, len(zoneTypes))
	for t := range zoneTypes {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
