package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// project is a one-line helper: parse src as a Fragment and project it.
func project(t *testing.T, src string) []domain.Zone {
	t.Helper()
	f, err := parseFragment([]byte(src))
	if err != nil {
		t.Fatalf("parseFragment: %v", err)
	}
	zones, err := ZonesFromFragment(f)
	if err != nil {
		t.Fatalf("ZonesFromFragment: %v", err)
	}
	return zones
}

func TestZonesFromFragmentZoneWithRecords(t *testing.T) {
	zones := project(t, `server:
local-zone: "example.com." refuse
local-data: "example.com. 300 IN A 192.0.2.1"
local-data: "www.example.com. 300 IN CNAME host.example.com."
`)
	want := []domain.Zone{{
		Name: "example.com.", Type: "refuse",
		Records: []domain.Record{
			{Name: "@", RType: "A", Value: "192.0.2.1", TTL: 300},
			{Name: "www", RType: "CNAME", Value: "host.example.com.", TTL: 300},
		},
	}}
	if !reflect.DeepEqual(zones, want) {
		t.Errorf("ZonesFromFragment =\n%+v\nwant\n%+v", zones, want)
	}
}

func TestZonesFromFragmentImplicitZone(t *testing.T) {
	// Same input as TestParseFragmentImplicitZone: an uncovered local-data
	// line springs an implicit transparent zone of the record's owner.
	zones := project(t, `local-data: "solo.example.com. 300 IN A 192.0.2.1"`)
	want := []domain.Zone{{
		Name: "solo.example.com.", Type: "transparent",
		Records: []domain.Record{{Name: "@", RType: "A", Value: "192.0.2.1", TTL: 300}},
	}}
	if !reflect.DeepEqual(zones, want) {
		t.Errorf("ZonesFromFragment = %+v, want %+v", zones, want)
	}
}

// TestZonesFromFragmentImplicitZoneDisabledOrder pins that an implicit
// transparent zone is always active: the first uncovered record must not
// donate its own disabled flag to the implicit zone, which would then disable
// every later record. Both file orders of the same logical data must project
// identically, and each record keeps its own flag.
func TestZonesFromFragmentImplicitZoneDisabledOrder(t *testing.T) {
	const (
		activeLine   = "local-data: \"x.example. 300 IN A 192.0.2.1\"\n"
		disabledLine = "# unbound-tui:disabled\n# local-data: \"x.example. 300 IN TXT keep\"\n"
	)
	tests := []struct {
		name string
		src  string
	}{
		{"disabled first", "server:\n" + disabledLine + activeLine},
		{"active first", "server:\n" + activeLine + disabledLine},
	}
	var want []domain.Zone
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zones := project(t, tt.src)
			if len(zones) != 1 || zones[0].Type != "transparent" || zones[0].Disabled {
				t.Fatalf("zones = %+v, want one active transparent zone", zones)
			}
			if got := len(zones[0].Records); got != 2 {
				t.Fatalf("records = %d, want 2 (A + TXT): a dropped record must not pass", got)
			}
			var sawA, sawTXT bool
			for _, r := range zones[0].Records {
				switch r.RType {
				case "A":
					sawA = true
					if r.Disabled {
						t.Errorf("active A record was marked disabled (file order changed semantics): %+v", r)
					}
				case "TXT":
					sawTXT = true
					if !r.Disabled {
						t.Errorf("disabled TXT record lost its flag: %+v", r)
					}
				}
			}
			if !sawA || !sawTXT {
				t.Errorf("records = %+v, want both an A and a TXT record", zones[0].Records)
			}
			if i == 0 {
				want = zones
			} else if !reflect.DeepEqual(want, zones) {
				t.Errorf("disabled-first projection differs from active-first:\n%+v\n%+v", want, zones)
			}
		})
	}
}

func TestZonesFromFragmentDisabled(t *testing.T) {
	zones := project(t, `server:
local-zone: "example.com." transparent
local-data: "live.example.com. 300 IN A 192.0.2.1"

# unbound-tui:disabled
# local-zone: "off.example." static

local-data: "on.off.example. 60 IN A 192.0.2.9"

# unbound-tui:disabled
# local-data: "stale.example.com. 300 IN A 192.0.2.2"
`)
	want := []domain.Zone{
		{
			Name: "example.com.", Type: "transparent",
			Records: []domain.Record{
				{Name: "live", RType: "A", Value: "192.0.2.1", TTL: 300},
				{Name: "stale", RType: "A", Value: "192.0.2.2", TTL: 300, Disabled: true},
			},
		},
		{
			Name: "off.example.", Type: "static", Disabled: true,
			Records: []domain.Record{
				{Name: "on", RType: "A", Value: "192.0.2.9", TTL: 60, Disabled: true},
			},
		},
	}
	if !reflect.DeepEqual(zones, want) {
		t.Errorf("ZonesFromFragment =\n%+v\nwant\n%+v", zones, want)
	}
}

func TestZonesFromFragmentLongestSuffix(t *testing.T) {
	zones := project(t, `server:
local-zone: "example.com." transparent
local-zone: "b.example.com." transparent
local-data: "a.b.example.com. 300 IN A 192.0.2.5"
`)
	want := []domain.Zone{
		{
			Name: "b.example.com.", Type: "transparent",
			Records: []domain.Record{{Name: "a", RType: "A", Value: "192.0.2.5", TTL: 300}},
		},
		{Name: "example.com.", Type: "transparent"},
	}
	if !reflect.DeepEqual(zones, want) {
		t.Errorf("ZonesFromFragment =\n%+v\nwant\n%+v", zones, want)
	}
}

// TestZonesFromFragmentRootZoneOwnsRecords pins the root-zone case: a declared
// local-zone "." owns every name (unbound's parent-chain lookup bottoms out at
// the root), so its local-data attaches to the root zone instead of springing
// an implicit transparent zone of the record's own owner. The record's name is
// relative to the root, i.e. the owner with its trailing root dot removed.
func TestZonesFromFragmentRootZoneOwnsRecords(t *testing.T) {
	zones := project(t, `server:
local-zone: "." refuse
local-data: "example.com. 300 IN A 1.2.3.4"
`)
	want := []domain.Zone{{
		Name: ".", Type: "refuse",
		Records: []domain.Record{{Name: "example.com", RType: "A", Value: "1.2.3.4", TTL: 300}},
	}}
	if !reflect.DeepEqual(zones, want) {
		t.Errorf("ZonesFromFragment =\n%+v\nwant\n%+v", zones, want)
	}
}

func TestZonesFromFragmentSortedByName(t *testing.T) {
	zones := project(t, `server:
local-zone: "zzz.example." transparent
local-zone: "aaa.example." transparent
`)
	want := []domain.Zone{
		{Name: "aaa.example.", Type: "transparent"},
		{Name: "zzz.example.", Type: "transparent"},
	}
	if !reflect.DeepEqual(zones, want) {
		t.Errorf("ZonesFromFragment =\n%+v\nwant\n%+v", zones, want)
	}
}

// TestZonesFromFragmentCaseVariantOwnership pins RFC 4343 case-insensitive
// ownership: DNS names fold, so an upper-case owner attaches to a lower-case
// declared zone. The relative name keeps the owner's original spelling and no
// implicit zone is sprung.
func TestZonesFromFragmentCaseVariantOwnership(t *testing.T) {
	zones := project(t, `server:
local-zone: "example.com." transparent
local-data: "WWW.EXAMPLE.COM. 300 IN A 192.0.2.1"
`)
	want := []domain.Zone{
		{
			Name: "example.com.", Type: "transparent",
			Records: []domain.Record{{Name: "WWW", RType: "A", Value: "192.0.2.1", TTL: 300}},
		},
	}
	if !reflect.DeepEqual(zones, want) {
		t.Errorf("ZonesFromFragment =\n%+v\nwant\n%+v", zones, want)
	}
}

// TestZonesFromFragmentCaseInsensitiveOwnership exercises the case-folding
// ownership matrix: mixed-case zone spellings, a case-variant apex folding to
// "@", longest-suffix ordering under folding, and the label-boundary rule.
func TestZonesFromFragmentCaseInsensitiveOwnership(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []domain.Zone
	}{
		{
			name: "mixed-case zone spelling",
			src: `server:
local-zone: "Example.COM." transparent
local-data: "www.example.com. 300 IN A 192.0.2.1"`,
			want: []domain.Zone{{
				Name: "Example.COM.", Type: "transparent",
				Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}},
			}},
		},
		{
			name: "case-variant apex folds to at-sign",
			src: `server:
local-zone: "example.com." transparent
local-data: "EXAMPLE.COM. 300 IN A 192.0.2.1"`,
			want: []domain.Zone{{
				Name: "example.com.", Type: "transparent",
				Records: []domain.Record{{Name: "@", RType: "A", Value: "192.0.2.1", TTL: 300}},
			}},
		},
		{
			name: "longest suffix folds case",
			src: `server:
local-zone: "example.com." transparent
local-zone: "b.EXAMPLE.com." transparent
local-data: "a.B.example.COM. 300 IN A 192.0.2.5"`,
			want: []domain.Zone{
				{
					Name: "b.EXAMPLE.com.", Type: "transparent",
					Records: []domain.Record{{Name: "a", RType: "A", Value: "192.0.2.5", TTL: 300}},
				},
				{Name: "example.com.", Type: "transparent"},
			},
		},
		{
			name: "folding respects label boundary",
			src: `server:
local-zone: "example.com." transparent
local-data: "xexample.com. 300 IN A 192.0.2.7"`,
			want: []domain.Zone{
				{Name: "example.com.", Type: "transparent"},
				{
					Name: "xexample.com.", Type: "transparent",
					Records: []domain.Record{{Name: "@", RType: "A", Value: "192.0.2.7", TTL: 300}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zones := project(t, tt.src)
			if !reflect.DeepEqual(zones, tt.want) {
				t.Errorf("ZonesFromFragment =\n%+v\nwant\n%+v", zones, tt.want)
			}
		})
	}
}

// TestZonesFromFragmentNonASCIIOwnership guards RFC 4343 folding against the
// byte-length change strings.ToLower can cause for non-ASCII runes: folding is
// ASCII-only, so a hand-edited fragment with non-ASCII names never yields a
// suffix match whose relative-name slice offset is negative. The Kelvin sign
// (U+212A) folds to "k" and shrinks, so under strings.ToLower it matched an
// ASCII "k." zone and made relativeName slice below zero.
func TestZonesFromFragmentNonASCIIOwnership(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []domain.Zone
	}{
		{
			name: "non-ASCII label before ASCII zone attaches",
			src: "server:\n" +
				"local-zone: \"example.\" transparent\n" +
				"local-data: \"\u0130.example. 300 IN A 1.2.3.4\"\n",
			want: []domain.Zone{{
				Name: "example.", Type: "transparent",
				Records: []domain.Record{{Name: "\u0130", RType: "A", Value: "1.2.3.4", TTL: 300}},
			}},
		},
		{
			name: "non-ASCII case variant does not fold",
			src: "server:\n" +
				"local-zone: \"\u212a.\" transparent\n" +
				"local-data: \"k.k. 300 IN A 1.2.3.4\"\n",
			want: []domain.Zone{
				{
					Name: "k.k.", Type: "transparent",
					Records: []domain.Record{{Name: "@", RType: "A", Value: "1.2.3.4", TTL: 300}},
				},
				{Name: "\u212a.", Type: "transparent"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := parseFragment([]byte(tt.src))
			if err != nil {
				t.Fatalf("parseFragment: %v", err)
			}
			var (
				zones []domain.Zone
				perr  error
				rec   any
			)
			func() {
				defer func() { rec = recover() }()
				zones, perr = ZonesFromFragment(f)
			}()
			if rec != nil {
				t.Fatalf("ZonesFromFragment panicked: %v", rec)
			}
			if perr != nil {
				t.Fatalf("ZonesFromFragment: %v", perr)
			}
			if !reflect.DeepEqual(zones, tt.want) {
				t.Errorf("ZonesFromFragment =\n%+v\nwant\n%+v", zones, tt.want)
			}
		})
	}
}

// TestZonesFromFragmentSkipsEmptyNameLocalZone pins the restored empty-name
// guard: a local-zone entry whose unquoted name token is empty is skipped by
// the projection, not normalized into the root zone "." (FQDN("") == ".").
func TestZonesFromFragmentSkipsEmptyNameLocalZone(t *testing.T) {
	zones := project(t, `server:
local-zone: ""
local-zone: "example.com." static
`)
	want := []domain.Zone{{Name: "example.com.", Type: "static"}}
	if !reflect.DeepEqual(zones, want) {
		t.Errorf("ZonesFromFragment =\n%+v\nwant\n%+v", zones, want)
	}
}

// TestParseFragmentEmptyNameLocalZoneRoundTrips pins the skip and the raw
// entry's survival through the full ParseFragment file path: the projection
// drops the empty-name zone, but the entry stays in the fragment so a
// serialize/parse round trip is unchanged.
func TestParseFragmentEmptyNameLocalZoneRoundTrips(t *testing.T) {
	const src = "server:\nlocal-zone: \"\"\nlocal-zone: \"example.com.\" static\n"
	path := filepath.Join(t.TempDir(), "frag.conf")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	f, err := ParseFragment(path)
	if err != nil {
		t.Fatalf("ParseFragment: %v", err)
	}
	zones, err := ZonesFromFragment(f)
	if err != nil {
		t.Fatalf("ZonesFromFragment: %v", err)
	}
	want := []domain.Zone{{Name: "example.com.", Type: "static"}}
	if !reflect.DeepEqual(zones, want) {
		t.Errorf("ZonesFromFragment =\n%+v\nwant\n%+v", zones, want)
	}
	back, err := parseFragment(SerializeFragment(f))
	if err != nil {
		t.Fatalf("parseFragment(SerializeFragment(f)): %v", err)
	}
	if !reflect.DeepEqual(back, f) {
		t.Errorf("round trip =\n%+v\nwant\n%+v", back, f)
	}
}

// TestZonesFromFragmentMalformedEntry pins loud failure: a known directive
// whose value cannot be parsed is a projection error naming the section and
// entry, never a silent drop. The Fragment model itself stays total.
func TestZonesFromFragmentMalformedEntry(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		mentions []string
	}{
		{
			name:     "unterminated local-data quote",
			src:      "server:\nlocal-data: \"broken\n",
			mentions: []string{"local-data", "server", `"broken`},
		},
		{
			name:     "too few local-data fields",
			src:      "server:\nlocal-data: \"only.two\"\n",
			mentions: []string{"local-data", "server", "only.two"},
		},
		{
			name:     "unterminated local-zone quote",
			src:      "server:\nlocal-zone: \"broken\n",
			mentions: []string{"local-zone", "server", `"broken`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := parseFragment([]byte(tt.src))
			if err != nil {
				t.Fatalf("parseFragment (must stay total): %v", err)
			}
			zones, err := ZonesFromFragment(f)
			if err == nil {
				t.Fatalf("ZonesFromFragment = %+v, nil error; want error", zones)
			}
			for _, want := range tt.mentions {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// projectErr parses src as a Fragment and projects it, returning the
// projection error rather than failing the test, so error cases can assert on
// the message.
func projectErr(t *testing.T, src string) ([]domain.Zone, error) {
	t.Helper()
	f, err := parseFragment([]byte(src))
	if err != nil {
		t.Fatalf("parseFragment: %v", err)
	}
	return ZonesFromFragment(f)
}

// TestZonesFromFragmentRejectsUnitTTL pins architecture decision P2-4: a
// TTL-position token that starts with an ASCII digit but is not a plain
// integer (for example "2H" or "1h30m") is a projection error naming the
// offending RR, never a silent fallback to the 3600 default. A token that does
// not start with a digit is a record type, so the no-TTL default still holds.
func TestZonesFromFragmentRejectsUnitTTL(t *testing.T) {
	rejected := []struct {
		name     string
		rr       string
		mentions []string
	}{
		{"unit ttl before IN", "x.example. 2H IN A 1.2.3.4", []string{"x.example.", "TTL"}},
		{"unit ttl before type", "x.example. 2h A 1.2.3.4", []string{"x.example.", "TTL"}},
		{"unit ttl after IN", "x.example. IN 1h30m A 1.2.3.4", []string{"x.example.", "TTL"}},
		{"signed positive ttl before IN", "x.example. +300 IN A 1.2.3.4", []string{"x.example.", "TTL", "+300"}},
		{"signed negative ttl before type", "x.example. -5 A 1.2.3.4", []string{"x.example.", "TTL", "-5"}},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			_, err := projectErr(t, "server:\nlocal-data: \""+tc.rr+"\"\n")
			if err == nil {
				t.Fatalf("projection of %q succeeded; want error", tc.rr)
			}
			for _, want := range tc.mentions {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}

	accepted := []struct {
		name string
		rr   string
		ttl  int
	}{
		{"plain ttl before IN", "x.example. 3600 IN A 1.2.3.4", 3600},
		{"plain ttl after IN", "x.example. IN 300 A 1.2.3.4", 300},
		{"no ttl defaults to 3600", "x.example. A 1.2.3.4", 3600},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			zones, err := projectErr(t, "server:\nlocal-data: \""+tc.rr+"\"\n")
			if err != nil {
				t.Fatalf("projection of %q: %v", tc.rr, err)
			}
			if len(zones) != 1 || len(zones[0].Records) != 1 {
				t.Fatalf("zones = %+v, want one zone with one record", zones)
			}
			if got := zones[0].Records[0].TTL; got != tc.ttl {
				t.Errorf("TTL = %d, want %d", got, tc.ttl)
			}
		})
	}
}

// TestIsEmptyNameLocalZone pins the projection predicate behind the empty-name
// skip. Only a value whose unquoted name token is actually empty counts;
// whitespace is a (weird but real) name, and an unparseable value is not
// empty-name — the projection surfaces it as an error instead.
func TestIsEmptyNameLocalZone(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{`""`, true},
		{`"" static`, true},
		{`"example.com." transparent`, false},
		{`" " static`, false},
		{`"unterminated`, false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			if got := IsEmptyNameLocalZone(tc.value); got != tc.want {
				t.Errorf("IsEmptyNameLocalZone(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
