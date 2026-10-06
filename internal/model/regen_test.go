package model

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/config"
	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// parseFrag parses fragment source through the real parser via a temp file.
func parseFrag(t *testing.T, src string) domain.Fragment {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "frag.conf")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	f, err := config.ParseFragment(p)
	if err != nil {
		t.Fatalf("ParseFragment: %v", err)
	}
	return f
}

// reparse runs a fragment through the real serialize/parse round trip.
func reparse(t *testing.T, f domain.Fragment) domain.Fragment {
	t.Helper()
	return parseFrag(t, string(config.SerializeFragment(f)))
}

// TestRegenLocal pins the regenerate-in-place helper: local-zone and
// local-data entries are stripped from every section and regenerated from the
// edited zone model into the first server section (created at the front when
// missing). All other entries and sections are preserved in place.
func TestRegenLocal(t *testing.T) {
	t.Run("only local entries touched", func(t *testing.T) {
		base := parseFrag(t, `server:
local-zone: "keep.example." transparent
forward-zone:
name: "."
forward-addr: 192.0.2.53
local-data: "stray.example. 300 IN A 192.0.2.1"
`)
		zones := []domain.Zone{{Name: "keep.example.", Type: "transparent"}}
		got := regenLocal(base, zones)
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
			t.Errorf("regenLocal =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("server created first when missing", func(t *testing.T) {
		base := parseFrag(t, `forward-zone:
name: "."
`)
		zones := []domain.Zone{{
			Name: "a.example.", Type: "transparent",
			Records: []domain.Record{{Name: "@", RType: "A", Value: "192.0.2.1", TTL: 300}},
		}}
		got := regenLocal(base, zones)
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
			t.Errorf("regenLocal =\n%+v\nwant\n%+v", got, want)
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
		got := regenLocal(domain.Fragment{}, zones)
		want := domain.Fragment{Sections: []domain.Section{
			{Kind: "server", Entries: []domain.Entry{
				{Key: "local-zone", Value: `"a.example." transparent`},
				{Key: "local-zone", Value: `"b.example." static`},
				{Key: "local-data", Value: `"a.example. 300 IN A 192.0.2.1"`},
				{Key: "local-data", Value: `"b.example. 300 IN A 192.0.2.2"`},
			}},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("regenLocal =\n%+v\nwant\n%+v", got, want)
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
		got := regenLocal(domain.Fragment{}, zones)
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
			t.Errorf("regenLocal =\n%+v\nwant\n%+v", got, want)
		}
	})

	// Review Focus #5: deleting every zone must leave a valid, round-trippable
	// fragment (a bare server section) rather than dropping the section or
	// leaking stale local-* entries.
	t.Run("all zones deleted round-trips", func(t *testing.T) {
		base := parseFrag(t, `server:
local-zone: "gone.example." static
local-data: "gone.example. 300 IN A 192.0.2.1"
`)
		got := regenLocal(base, nil)
		want := domain.Fragment{Sections: []domain.Section{{Kind: "server"}}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("regenLocal(no zones) =\n%+v\nwant\n%+v", got, want)
		}
		if back := reparse(t, got); !reflect.DeepEqual(back, want) {
			t.Errorf("round-trip =\n%+v\nwant\n%+v", back, want)
		}
	})

	// Review Focus #3: if the server section is gone, adding a zone must
	// recreate it at index 0, ahead of any pre-existing sections.
	t.Run("server deleted then zone added", func(t *testing.T) {
		base := parseFrag(t, `forward-zone:
name: "."
forward-addr: 192.0.2.53
remote-control:
control-enable: yes
`)
		zones := []domain.Zone{{Name: "a.example.", Type: "transparent"}}
		got := regenLocal(base, zones)
		want := domain.Fragment{Sections: []domain.Section{
			{Kind: "server", Entries: []domain.Entry{
				{Key: "local-zone", Value: `"a.example." transparent`},
			}},
			{Kind: "forward-zone", Entries: []domain.Entry{
				{Key: "name", Value: `"."`},
				{Key: "forward-addr", Value: "192.0.2.53"},
			}},
			{Kind: "remote-control", Entries: []domain.Entry{
				{Key: "control-enable", Value: "yes"},
			}},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("regenLocal =\n%+v\nwant\n%+v", got, want)
		}
		if len(got.Sections) == 0 || got.Sections[0].Kind != "server" {
			t.Errorf("server not recreated at index 0: %+v", got.Sections)
		}
	})
}

// TestRegeneratedEntriesRootZone pins that regenerating a root-zone fragment
// keeps the root owner and its refuse type: the record projects into the "."
// zone (so no implicit transparent zone of its own owner is sprung) and is
// rendered without a doubled dot.
func TestRegeneratedEntriesRootZone(t *testing.T) {
	f := parseFrag(t, `server:
local-zone: "." refuse
local-data: "example.com. 300 IN A 1.2.3.4"
`)
	zones, err := config.ZonesFromFragment(f)
	if err != nil {
		t.Fatalf("ZonesFromFragment: %v", err)
	}
	got := regeneratedEntries(zones)
	want := []domain.Entry{
		{Key: "local-zone", Value: `"." refuse`},
		{Key: "local-data", Value: `"example.com. 300 IN A 1.2.3.4"`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("regeneratedEntries =\n%+v\nwant\n%+v", got, want)
	}
}
