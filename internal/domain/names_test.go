package domain

import (
	"slices"
	"sort"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "example.com", "example.com"},
		{"quoted", `"example.com"`, "example.com"},
		{"whitespace trimmed", `  example.com  `, "example.com"},
		{"quoted and padded", `  "example.com"  `, "example.com"},
		{"one quote per side only", `""example.com""`, `"example.com"`},
		// The task brief writes this as `""x""` -> `x`; the pinned
		// SectionKeyName semantics it also mandates (one quote per side)
		// make the correct result `"x"`, not `x`.
		{"stacked quotes one per side", `""x""`, `"x"`},
		// The asymmetry is the pinned config.SectionKeyName semantics:
		// exactly one leading and one trailing quote come off.
		{"two leading one trailing", `""example.com"`, `"example.com`},
		{"interior quotes kept", `ex"ample`, `ex"ample`},
		{"only quotes", `""`, ""},
		{"root zone", ".", "."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeName(tt.in); got != tt.want {
				t.Errorf("NormalizeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// canonicalZoneTypes is the full unbound.conf(5) local-zone type set the tool
// accepts. It mirrors the list moved into names.go; the literal here is
// deliberate so a dropped or renamed entry fails this test.
var canonicalZoneTypes = []string{
	"deny", "refuse", "static", "transparent",
	"typetransparent", "redirect", "nodefault",
	"inform", "inform_deny", "inform_redirect",
	"always_transparent", "always_refuse", "always_nxdomain",
	"always_nodata", "always_deny", "always_null", "noview",
	"block_a", "block_aaaa", "block_a_wdata", "block_aaaa_wdata",
}

func TestIsZoneTypeName(t *testing.T) {
	for _, typ := range canonicalZoneTypes {
		t.Run(typ, func(t *testing.T) {
			if !IsZoneTypeName(typ) {
				t.Errorf("IsZoneTypeName(%q) = false, want true", typ)
			}
		})
	}

	rejected := []string{
		"", "bogus", "Static", "TRANSPARENT", "type_transparent",
		"always_nxdomain ", "always_nxdomain\n",
	}
	for _, typ := range rejected {
		t.Run("reject_"+typ, func(t *testing.T) {
			if IsZoneTypeName(typ) {
				t.Errorf("IsZoneTypeName(%q) = true, want false", typ)
			}
		})
	}

	// No entry may exist beyond the canonical literal above: an extra type
	// would slip past every per-type assertion.
	if got, want := len(zoneTypes), len(canonicalZoneTypes); got != want {
		t.Errorf("zoneTypes has %d entries, want %d (the canonical set)", got, want)
	}
}

// TestZoneTypeNames pins the picker list to the canonical set: same values,
// sorted, and every one accepted by IsZoneTypeName.
func TestZoneTypeNames(t *testing.T) {
	got := ZoneTypeNames()
	want := append([]string(nil), canonicalZoneTypes...)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("ZoneTypeNames() = %v, want %v", got, want)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("ZoneTypeNames() not strictly sorted at %d: %q >= %q", i, got[i-1], got[i])
		}
	}
	for _, typ := range got {
		if !IsZoneTypeName(typ) {
			t.Errorf("IsZoneTypeName(%q) = false for a listed type", typ)
		}
	}
}
