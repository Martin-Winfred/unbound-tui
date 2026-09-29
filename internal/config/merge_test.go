package config

import (
	"reflect"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// frag parses src into a Fragment for use as merge input.
func frag(t *testing.T, src string) domain.Fragment {
	t.Helper()
	f, err := parseFragment([]byte(src))
	if err != nil {
		t.Fatalf("parseFragment: %v", err)
	}
	return f
}

// TestFragmentFromZones pins the pure merge shim: regenerate local-zone and
// local-data entries from the edited zone model into the base fragment's
// server section, touching nothing else.
func TestFragmentFromZones(t *testing.T) {
	t.Run("only local entries touched", func(t *testing.T) {
		base := frag(t, `server:
local-zone: "keep.example." transparent
forward-zone:
name: "."
forward-addr: 192.0.2.53
local-data: "stray.example. 300 IN A 192.0.2.1"
`)
		zones := []domain.Zone{{Name: "keep.example.", Type: "transparent"}}
		got := FragmentFromZones(base, zones)
		want := domain.Fragment{Sections: []domain.Section{
			{Kind: "server", Entries: []domain.Entry{
				{Key: "local-zone", Value: `"keep.example." transparent`},
			}},
			{Kind: "forward-zone", Entries: []domain.Entry{
				{Key: "name", Value: `"."`},
				{Key: "forward-addr", Value: "192.0.2.53"},
			}},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FragmentFromZones =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("server created first when missing", func(t *testing.T) {
		base := frag(t, `forward-zone:
name: "."
`)
		zones := []domain.Zone{{
			Name: "a.example.", Type: "transparent",
			Records: []domain.Record{{Name: "@", RType: "A", Value: "192.0.2.1", TTL: 300}},
		}}
		got := FragmentFromZones(base, zones)
		want := domain.Fragment{Sections: []domain.Section{
			{Kind: "server", Entries: []domain.Entry{
				{Key: "local-zone", Value: `"a.example." transparent`},
				{Key: "local-data", Value: `"a.example. 300 IN A 192.0.2.1"`},
			}},
			{Kind: "forward-zone", Entries: []domain.Entry{
				{Key: "name", Value: `"."`},
			}},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FragmentFromZones =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("regeneration order", func(t *testing.T) {
		zones := []domain.Zone{
			{
				Name: "b.example.", Type: "static",
				Records: []domain.Record{{Name: "@", RType: "A", Value: "192.0.2.2", TTL: 300}},
			},
			{
				Name: "a.example.", Type: "transparent",
				Records: []domain.Record{{Name: "@", RType: "A", Value: "192.0.2.1", TTL: 300}},
			},
		}
		got := FragmentFromZones(domain.Fragment{}, zones)
		want := domain.Fragment{Sections: []domain.Section{
			{Kind: "server", Entries: []domain.Entry{
				{Key: "local-zone", Value: `"a.example." transparent`},
				{Key: "local-zone", Value: `"b.example." static`},
				{Key: "local-data", Value: `"a.example. 300 IN A 192.0.2.1"`},
				{Key: "local-data", Value: `"b.example. 300 IN A 192.0.2.2"`},
			}},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FragmentFromZones =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("disabled preserved", func(t *testing.T) {
		zones := []domain.Zone{
			{
				Name: "part.example.", Type: "transparent",
				Records: []domain.Record{{Name: "@", RType: "A", Value: "192.0.2.3", TTL: 300, Disabled: true}},
			},
			{
				Name: "off.example.", Type: "static", Disabled: true,
				Records: []domain.Record{{Name: "@", RType: "A", Value: "192.0.2.2", TTL: 300}},
			},
			{
				Name: "on.example.", Type: "transparent",
				Records: []domain.Record{{Name: "@", RType: "A", Value: "192.0.2.1", TTL: 300}},
			},
		}
		got := FragmentFromZones(domain.Fragment{}, zones)
		want := domain.Fragment{Sections: []domain.Section{
			{Kind: "server", Entries: []domain.Entry{
				{Key: "local-zone", Value: `"on.example." transparent`},
				{Key: "local-zone", Value: `"part.example." transparent`},
				{Key: "local-data", Value: `"on.example. 300 IN A 192.0.2.1"`},
				{Key: "local-zone", Value: `"off.example." static`, Disabled: true},
				{Key: "local-data", Value: `"off.example. 300 IN A 192.0.2.2"`, Disabled: true},
				{Key: "local-data", Value: `"part.example. 300 IN A 192.0.2.3"`, Disabled: true},
			}},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FragmentFromZones =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("emptied sections kept", func(t *testing.T) {
		base := frag(t, `server:
local-data: "only.example. 300 IN A 192.0.2.1"
`)
		got := FragmentFromZones(base, nil)
		want := domain.Fragment{Sections: []domain.Section{{Kind: "server"}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FragmentFromZones =\n%+v\nwant\n%+v", got, want)
		}
	})
}
