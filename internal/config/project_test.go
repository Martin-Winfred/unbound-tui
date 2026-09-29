package config

import (
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

// TestZonesFromFragmentCaseVariantOwnership pins zone ownership under case
// differences (plan Review Focus #4): DNS names are case-insensitive, so an
// upper-case owner attaches to the declared zone exactly as its lower-case
// spelling does, rather than springing an implicit zone of its own.
func TestZonesFromFragmentCaseVariantOwnership(t *testing.T) {
	upper := project(t, `server:
local-zone: "example.com." transparent
local-data: "WWW.EXAMPLE.COM. 300 IN A 192.0.2.1"
`)
	lower := project(t, `server:
local-zone: "example.com." transparent
local-data: "www.example.com. 300 IN A 192.0.2.1"
`)
	if len(upper) != 1 || upper[0].Name != "example.com." {
		t.Fatalf("upper-case owner = %+v, want a single zone example.com.", upper)
	}
	if len(upper[0].Records) != 1 || !strings.EqualFold(upper[0].Records[0].Name, lower[0].Records[0].Name) {
		t.Errorf("upper-case record = %+v, want relative name %q (case-insensitive)",
			upper[0].Records, lower[0].Records[0].Name)
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
