package model

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/config"
	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

func TestSectionLabel(t *testing.T) {
	cases := []struct {
		name string
		sec  domain.Section
		want string
	}{
		{
			name: "named section with name entry",
			sec: domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
				{Key: "name", Value: `"."`},
				{Key: "forward-addr", Value: "192.0.2.53"},
				{Key: "forward-addr", Value: "1.1.1.1"},
			}},
			want: `forward-zone "." · 3 entries`,
		},
		{
			name: "unnamed server uses the singular",
			sec:  domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "port", Value: "53"}}},
			want: "server · 1 entry",
		},
		{
			name: "synthetic top level",
			sec:  domain.Section{},
			want: "(top level)",
		},
		{
			name: "all non-locked entries disabled",
			sec: domain.Section{Kind: "server", Entries: []domain.Entry{
				{Key: "edns-buffer-size", Value: "1232", Disabled: true},
				{Key: "hide-identity", Value: "yes", Disabled: true},
			}},
			want: "server · 2 entries · ⛔",
		},
		{
			name: "locked disabled entries do not light the marker",
			sec: domain.Section{Kind: "server", Entries: []domain.Entry{
				{Key: "local-zone", Value: `"example.com." static`, Disabled: true},
			}},
			want: "server · 1 entry",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sectionLabel(tc.sec); got != tc.want {
				t.Errorf("sectionLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEntryLabel(t *testing.T) {
	cases := []struct {
		name  string
		entry domain.Entry
		want  string
	}{
		{
			name:  "plain",
			entry: domain.Entry{Key: "forward-addr", Value: "192.0.2.53"},
			want:  "forward-addr: 192.0.2.53",
		},
		{
			name:  "disabled",
			entry: domain.Entry{Key: "forward-addr", Value: "192.0.2.53", Disabled: true},
			want:  "forward-addr: 192.0.2.53 · ⛔",
		},
		{
			name:  "locked local-data",
			entry: domain.Entry{Key: "local-data", Value: `"example.com. 300 IN A 192.0.2.1"`},
			want:  `local-data: "example.com. 300 IN A 192.0.2.1" (locked · edit in Local data)`,
		},
		{
			name:  "locked and disabled local-zone",
			entry: domain.Entry{Key: "local-zone", Value: `"example.com." static`, Disabled: true},
			want:  `local-zone: "example.com." static · ⛔ (locked · edit in Local data)`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := entryLabel(tc.entry); got != tc.want {
				t.Errorf("entryLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsLocked(t *testing.T) {
	for _, key := range []string{"local-zone", "local-data"} {
		if !isLocked(domain.Entry{Key: key}) {
			t.Errorf("isLocked(%q) = false, want true", key)
		}
	}
	if isLocked(domain.Entry{Key: "forward-addr"}) {
		t.Error("isLocked(forward-addr) = true, want false")
	}
}

// configFixture packs a server section with the locked local rows plus a
// second, named section so both panes have content.
func configFixture() domain.Fragment {
	return domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{
			{Key: "local-zone", Value: `"example.com." static`},
			{Key: "local-data", Value: `"example.com. 300 IN A 192.0.2.1"`},
			{Key: "edns-buffer-size", Value: "1232"},
		}},
		{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: `"."`},
			{Key: "forward-addr", Value: "192.0.2.53"},
		}},
	}}
}

func TestConfigViewRenders(t *testing.T) {
	m, _ := newTestModel(t)
	m.width, m.height = 120, 30
	m.frag = configFixture()
	m.view = ViewConfig
	m.clampCfgCursors()

	v := m.View()
	for _, want := range []string{
		"server",
		`forward-zone "."`,
		"local-data:",
		"(locked · edit in Local data)",
		"edns-buffer-size",
		"— config",
		"config",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("Config View missing %q:\n%s", want, v)
		}
	}

	// The right pane follows the selected section.
	m.cfgView.SecCursor = 1
	m.clampCfgCursors()
	if v := m.View(); !strings.Contains(v, "forward-addr: 192.0.2.53") {
		t.Errorf("Config View missing the selected section's entries:\n%s", v)
	}
}

func TestConfigViewTruncatesLongValues(t *testing.T) {
	m, _ := newTestModel(t)
	m.width, m.height = 80, 24
	m.view = ViewConfig
	m.frag = domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{
			{Key: "control-enable", Value: strings.Repeat("9", 200)},
		}},
	}}
	m.clampCfgCursors()
	if v := m.View(); !strings.Contains(v, "…") {
		t.Errorf("Config View did not truncate a long value:\n%s", v)
	}
}

func TestConfigTogglePreservesCursors(t *testing.T) {
	m, _ := newTestModel(t)
	m.frag = configFixture()
	m.view = ViewZones
	want := ConfigViewModel{SecCursor: 1, EntCursor: 1, SecFocused: false}
	m.cfgView = want

	next := asRoot(t, mustUpdate(t, m, key("c")))
	if next.view != ViewConfig {
		t.Fatalf("view = %v, want ViewConfig", next.view)
	}
	if next.cfgView != want {
		t.Errorf("cfgView after switch = %+v, want %+v", next.cfgView, want)
	}

	back := asRoot(t, mustUpdate(t, next, key("c")))
	if back.view != ViewZones {
		t.Fatalf("view = %v, want ViewZones", back.view)
	}
	if back.cfgView != want {
		t.Errorf("cfgView after round trip = %+v, want %+v", back.cfgView, want)
	}
}

// TestConfigCursorsClampOnSwitchFromLocalView pins that switching into the
// Config view re-clamps its cursors: a Local-view mutation regenerates the
// fragment and can insert a server section at index 0, leaving the config
// cursors pointing past the selected section's entries.
func TestConfigCursorsClampOnSwitchFromLocalView(t *testing.T) {
	m, _ := newTestModel(t)
	m.frag = domain.Fragment{Sections: []domain.Section{
		{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: `"."`},
			{Key: "forward-addr", Value: "192.0.2.53"},
		}},
	}}
	m.view = ViewZones
	m.cfgView = ConfigViewModel{SecCursor: 0, EntCursor: 1, SecFocused: false}

	// A Local-view mutation regenerates the fragment, inserting a server
	// section at index 0; the section cursor now points at a shorter section.
	m = asRoot(t, mustUpdate(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com", Type: "transparent"}))
	if m.frag.Sections[0].Kind != "server" {
		t.Fatalf("section 0 = %q, want server (inserted by regenLocal)", m.frag.Sections[0].Kind)
	}

	next := asRoot(t, mustUpdate(t, m, key("c")))
	if next.view != ViewConfig {
		t.Fatalf("view = %v, want ViewConfig", next.view)
	}
	if next.cfgView.SecCursor < 0 || next.cfgView.SecCursor >= len(next.frag.Sections) {
		t.Fatalf("SecCursor = %d, out of range for %d sections", next.cfgView.SecCursor, len(next.frag.Sections))
	}
	sec := next.frag.Sections[next.cfgView.SecCursor]
	if next.cfgView.EntCursor != 0 || next.cfgView.EntCursor >= len(sec.Entries) {
		t.Errorf("EntCursor = %d, want 0 in range for %d entries", next.cfgView.EntCursor, len(sec.Entries))
	}
}

func TestConfigTabFlipsFocus(t *testing.T) {
	m, _ := newTestModel(t)
	m.view = ViewConfig
	m.cfgView.SecFocused = true

	next := asRoot(t, mustUpdate(t, m, key("tab")))
	if next.cfgView.SecFocused {
		t.Error("tab did not move focus to the entries pane")
	}
	next = asRoot(t, mustUpdate(t, next, key("tab")))
	if !next.cfgView.SecFocused {
		t.Error("tab did not move focus back to the sections pane")
	}
}

func TestConfigMovementClamps(t *testing.T) {
	newConfigModel := func(t *testing.T) RootModel {
		t.Helper()
		m, _ := newTestModel(t)
		m.view = ViewConfig
		m.frag = domain.Fragment{Sections: []domain.Section{
			{Kind: "server", Entries: []domain.Entry{
				{Key: "a", Value: "1"},
				{Key: "b", Value: "2"},
				{Key: "c", Value: "3"},
			}},
			{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"."`}}},
		}}
		m.cfgView = ConfigViewModel{SecFocused: true}
		return m
	}

	t.Run("section cursor clamps at the ends", func(t *testing.T) {
		m := newConfigModel(t)
		if got := asRoot(t, mustUpdate(t, m, key("k"))).cfgView.SecCursor; got != 0 {
			t.Errorf("SecCursor after up = %d, want 0", got)
		}
		m = asRoot(t, mustUpdate(t, m, key("G")))
		if m.cfgView.SecCursor != 1 {
			t.Errorf("SecCursor after G = %d, want 1", m.cfgView.SecCursor)
		}
		m = asRoot(t, mustUpdate(t, m, key("j")))
		if m.cfgView.SecCursor != 1 {
			t.Errorf("SecCursor after down = %d, want 1 (clamped)", m.cfgView.SecCursor)
		}
		m = asRoot(t, mustUpdate(t, m, key("g")))
		if m.cfgView.SecCursor != 0 {
			t.Errorf("SecCursor after g = %d, want 0", m.cfgView.SecCursor)
		}
	})

	t.Run("moving the section clamps the entry cursor", func(t *testing.T) {
		m := newConfigModel(t)
		m.cfgView.SecCursor = 0
		m.cfgView.EntCursor = 2
		m = asRoot(t, mustUpdate(t, m, key("j")))
		if m.cfgView.SecCursor != 1 || m.cfgView.EntCursor != 0 {
			t.Errorf("cursor = %d/%d, want 1/0", m.cfgView.SecCursor, m.cfgView.EntCursor)
		}
	})

	t.Run("entry cursor clamps in the focused pane", func(t *testing.T) {
		m := newConfigModel(t)
		m.cfgView.SecFocused = false
		m.cfgView.SecCursor = 0
		for i := 0; i < 5; i++ {
			m = asRoot(t, mustUpdate(t, m, key("j")))
		}
		if m.cfgView.EntCursor != 2 {
			t.Errorf("EntCursor = %d, want 2 (clamped)", m.cfgView.EntCursor)
		}
		m = asRoot(t, mustUpdate(t, m, key("G")))
		if m.cfgView.EntCursor != 2 {
			t.Errorf("EntCursor after G = %d, want 2", m.cfgView.EntCursor)
		}
		m = asRoot(t, mustUpdate(t, m, key("g")))
		if m.cfgView.EntCursor != 0 {
			t.Errorf("EntCursor after g = %d, want 0", m.cfgView.EntCursor)
		}
	})

	t.Run("paging clamps", func(t *testing.T) {
		m := newConfigModel(t)
		m = asRoot(t, mustUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlD}))
		if m.cfgView.SecCursor != 1 {
			t.Errorf("SecCursor after ctrl+d = %d, want 1", m.cfgView.SecCursor)
		}
		m = asRoot(t, mustUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlU}))
		if m.cfgView.SecCursor != 0 {
			t.Errorf("SecCursor after ctrl+u = %d, want 0", m.cfgView.SecCursor)
		}
	})
}

func TestConfigMovementOnEmptyPanes(t *testing.T) {
	m, _ := newTestModel(t)
	m.view = ViewConfig
	m.frag = domain.Fragment{}
	m.cfgView = ConfigViewModel{SecFocused: true}

	for _, k := range []tea.KeyMsg{
		key("j"), key("k"), key("g"), key("G"),
		{Type: tea.KeyCtrlD}, {Type: tea.KeyCtrlU},
	} {
		m = asRoot(t, mustUpdate(t, m, k))
		if m.cfgView.SecCursor != 0 || m.cfgView.EntCursor != 0 {
			t.Fatalf("empty sections cursors = %d/%d, want 0/0",
				m.cfgView.SecCursor, m.cfgView.EntCursor)
		}
	}

	m.frag = domain.Fragment{Sections: []domain.Section{{Kind: "server"}}}
	m.cfgView = ConfigViewModel{SecFocused: false}
	for _, k := range []tea.KeyMsg{key("j"), key("k"), key("g"), key("G")} {
		m = asRoot(t, mustUpdate(t, m, k))
		if m.cfgView.SecCursor != 0 || m.cfgView.EntCursor != 0 {
			t.Fatalf("empty entries cursors = %d/%d, want 0/0",
				m.cfgView.SecCursor, m.cfgView.EntCursor)
		}
	}
}

// TestConfigCursorsClampOnReload pins the clamp after an external fragment
// change.
func TestConfigCursorsClampOnReload(t *testing.T) {
	m, _ := newTestModel(t)
	m.view = ViewConfig
	m.cfgView = ConfigViewModel{SecCursor: 5, EntCursor: 5, SecFocused: true}

	loaded := domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "edns-buffer-size", Value: "1232"}}},
		{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: `"."`},
			{Key: "forward-addr", Value: "192.0.2.53"},
		}},
	}}
	m = asRoot(t, mustUpdate(t, m, ZonesLoadedMsg{Fragment: loaded}))

	if m.cfgView.SecCursor != 1 {
		t.Errorf("SecCursor = %d, want 1", m.cfgView.SecCursor)
	}
	if m.cfgView.EntCursor != 1 {
		t.Errorf("EntCursor = %d, want 1", m.cfgView.EntCursor)
	}
}

// --- section/entry lifecycle and guards ---

// setConfigFragment installs a fragment and brings the zones projection and
// the config cursors in sync, mirroring what startup does.
func setConfigFragment(t *testing.T, m RootModel, f domain.Fragment) RootModel {
	t.Helper()
	m.frag = f
	m.refreshZones()
	m.clampCfgCursors()
	return m
}

// hasEntry reports whether the fragment holds a key/value entry anywhere.
func hasEntry(f domain.Fragment, key, value string) bool {
	for _, s := range f.Sections {
		for _, e := range s.Entries {
			if e.Key == key && e.Value == value {
				return true
			}
		}
	}
	return false
}

// lifecycleFixture packs a locked server section (two local rows plus one
// unlocked row) and a clean named section.
func lifecycleFixture() domain.Fragment {
	return domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{
			{Key: "local-zone", Value: `"example.com." static`},
			{Key: "local-data", Value: `"www.example.com. 300 IN A 192.0.2.1"`},
			{Key: "edns-buffer-size", Value: "1232"},
		}},
		{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: `"."`},
			{Key: "forward-addr", Value: "192.0.2.53"},
		}},
	}}
}

func TestConfigDeleteSectionRefusesLocked(t *testing.T) {
	m, _ := newTestModel(t)
	m = setConfigFragment(t, m, lifecycleFixture())
	m.view = ViewConfig
	m.cfgView = ConfigViewModel{SecFocused: true}
	before := m.frag

	next := asRoot(t, mustUpdate(t, m, key("D")))
	if next.state == StateConfirm {
		t.Fatal("D on a locked section entered the confirm state")
	}
	if !strings.Contains(next.notice, "local data belongs to the Local data view") {
		t.Errorf("notice = %q, want the locked-section guard notice", next.notice)
	}
	if !reflect.DeepEqual(next.frag, before) {
		t.Errorf("fragment changed: %+v", next.frag)
	}
	if next.dirty {
		t.Error("dirty = true after a refused section delete")
	}
}

func TestConfigDeleteSectionConfirmed(t *testing.T) {
	m, _ := newTestModel(t)
	m = setConfigFragment(t, m, lifecycleFixture())
	m.view = ViewConfig
	m.cfgView = ConfigViewModel{SecCursor: 1, SecFocused: true}

	next := asRoot(t, mustUpdate(t, m, key("D")))
	if next.state != StateConfirm || next.confirmKind != "section" {
		t.Fatalf("state/kind = %v/%q, want StateConfirm/section", next.state, next.confirmKind)
	}
	next = asRoot(t, mustUpdate(t, next, key("y")))
	if len(next.frag.Sections) != 1 || next.frag.Sections[0].Kind != "server" {
		t.Fatalf("sections = %+v, want only the server section", next.frag.Sections)
	}
	if !next.dirty {
		t.Error("dirty = false after deleting a section")
	}
	if next.cfgView.SecCursor != 0 {
		t.Errorf("SecCursor = %d, want 0 (clamped)", next.cfgView.SecCursor)
	}
	want, err := config.ZonesFromFragment(next.frag)
	if err != nil {
		t.Fatalf("ZonesFromFragment: %v", err)
	}
	if !reflect.DeepEqual(next.zones, want) {
		t.Errorf("zones = %+v, want %+v (projection refreshed)", next.zones, want)
	}
}

func TestConfigDeleteSectionCancelled(t *testing.T) {
	m, _ := newTestModel(t)
	m = setConfigFragment(t, m, lifecycleFixture())
	m.view = ViewConfig
	m.cfgView = ConfigViewModel{SecCursor: 1, SecFocused: true}
	before := m.frag

	next := asRoot(t, mustUpdate(t, m, key("D")))
	next = asRoot(t, mustUpdate(t, next, key("n")))
	if len(next.frag.Sections) != 2 || next.state != StateReady {
		t.Errorf("cancel changed state: sections=%d state=%v", len(next.frag.Sections), next.state)
	}
	if !reflect.DeepEqual(next.frag, before) {
		t.Errorf("cancel changed the fragment: %+v", next.frag)
	}
}

func TestConfigDeleteEntry(t *testing.T) {
	t.Run("locked is skipped with a notice", func(t *testing.T) {
		m, _ := newTestModel(t)
		m = setConfigFragment(t, m, lifecycleFixture())
		m.view = ViewConfig
		m.cfgView = ConfigViewModel{SecFocused: false}
		before := m.frag

		next := asRoot(t, mustUpdate(t, m, key("d")))
		if next.state == StateConfirm {
			t.Fatal("d on a locked entry entered the confirm state")
		}
		if !strings.Contains(next.notice, "locked — edit local data in the Local data view") {
			t.Errorf("notice = %q, want the locked-entry notice", next.notice)
		}
		if !reflect.DeepEqual(next.frag, before) {
			t.Errorf("fragment changed: %+v", next.frag)
		}
		if next.dirty {
			t.Error("dirty = true after a skipped locked delete")
		}
	})

	t.Run("unlocked confirms then removes", func(t *testing.T) {
		m, _ := newTestModel(t)
		m = setConfigFragment(t, m, lifecycleFixture())
		m.view = ViewConfig
		m.cfgView = ConfigViewModel{SecFocused: false, EntCursor: 2} // edns-buffer-size

		next := asRoot(t, mustUpdate(t, m, key("d")))
		if next.state != StateConfirm || next.confirmKind != "entry" {
			t.Fatalf("state/kind = %v/%q, want StateConfirm/entry", next.state, next.confirmKind)
		}
		next = asRoot(t, mustUpdate(t, next, key("y")))
		if hasEntry(next.frag, "edns-buffer-size", "1232") {
			t.Error("entry still present after a confirmed delete")
		}
		if !next.dirty {
			t.Error("dirty = false after deleting an entry")
		}
		if next.cfgView.EntCursor != 1 {
			t.Errorf("EntCursor = %d, want 1 (clamped)", next.cfgView.EntCursor)
		}
	})
}

func TestConfigEditEntryGuard(t *testing.T) {
	t.Run("locked is skipped with a notice", func(t *testing.T) {
		m, _ := newTestModel(t)
		m = setConfigFragment(t, m, lifecycleFixture())
		m.view = ViewConfig
		m.cfgView = ConfigViewModel{SecFocused: false, EntCursor: 0} // local-zone
		before := m.frag

		next := asRoot(t, mustUpdate(t, m, key("e")))
		if !strings.Contains(next.notice, "locked — edit local data in the Local data view") {
			t.Errorf("notice = %q, want the locked-entry notice", next.notice)
		}
		if next.state != StateReady {
			t.Errorf("state = %v, want StateReady (no form yet)", next.state)
		}
		if !reflect.DeepEqual(next.frag, before) {
			t.Errorf("fragment changed: %+v", next.frag)
		}
	})

	t.Run("unlocked opens the edit form", func(t *testing.T) {
		m, _ := newTestModel(t)
		m = setConfigFragment(t, m, lifecycleFixture())
		m.view = ViewConfig
		m.cfgView = ConfigViewModel{SecFocused: false, EntCursor: 2} // edns-buffer-size
		before := m.frag

		next := asRoot(t, mustUpdate(t, m, key("e")))
		if next.state != StateForm || next.form.mode != FormEditEntry {
			t.Fatalf("state/mode = %v/%v, want StateForm/FormEditEntry", next.state, next.form.mode)
		}
		if got := next.form.inputs[0].Value(); got != "edns-buffer-size" {
			t.Errorf("edit form key = %q, want the focused entry's key", got)
		}
		if !reflect.DeepEqual(next.frag, before) {
			t.Errorf("opening the form changed the fragment: %+v", next.frag)
		}
	})
}

func TestConfigSpaceSectionToggle(t *testing.T) {
	m, _ := newTestModel(t)
	// Entry 1 is a locked row that is ALREADY disabled; a section toggle must
	// leave every locked row exactly as it was, whether enabled or disabled.
	m = setConfigFragment(t, m, domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{
			{Key: "local-zone", Value: `"example.com." static`},
			{Key: "local-data", Value: `"www.example.com. 300 IN A 192.0.2.1"`, Disabled: true},
			{Key: "edns-buffer-size", Value: "1232"},
		}},
	}})
	m.view = ViewConfig
	m.cfgView = ConfigViewModel{SecFocused: true}

	next := asRoot(t, mustUpdate(t, m, key(" ")))
	if next.frag.Sections[0].Entries[0].Disabled {
		t.Error("space left pane changed enabled locked entry 0")
	}
	if !next.frag.Sections[0].Entries[1].Disabled {
		t.Error("space left pane flipped an already-disabled locked entry")
	}
	if !next.frag.Sections[0].Entries[2].Disabled {
		t.Error("space left pane did not disable the non-locked entry")
	}
	if !next.dirty {
		t.Error("dirty = false after a section toggle")
	}

	back := asRoot(t, mustUpdate(t, next, key(" ")))
	if back.frag.Sections[0].Entries[0].Disabled || !back.frag.Sections[0].Entries[1].Disabled {
		t.Error("space left pane changed a locked entry on the second toggle")
	}
	if back.frag.Sections[0].Entries[2].Disabled {
		t.Error("space left pane did not re-enable the non-locked entry")
	}
}

func TestConfigSpaceAllLockedSectionNotices(t *testing.T) {
	m, _ := newTestModel(t)
	m = setConfigFragment(t, m, domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{
			{Key: "local-zone", Value: `"example.com." static`},
		}},
	}})
	m.view = ViewConfig
	m.cfgView = ConfigViewModel{SecFocused: true}

	next := asRoot(t, mustUpdate(t, m, key(" ")))
	if !strings.Contains(next.notice, "locked — edit local data in the Local data view") {
		t.Errorf("notice = %q, want the locked-entry notice", next.notice)
	}
	if next.dirty {
		t.Error("dirty = true with no non-locked entry to toggle")
	}
}

func TestConfigSpaceEntryToggle(t *testing.T) {
	t.Run("unlocked toggles", func(t *testing.T) {
		m, _ := newTestModel(t)
		m = setConfigFragment(t, m, lifecycleFixture())
		m.view = ViewConfig
		m.cfgView = ConfigViewModel{SecFocused: false, EntCursor: 2}

		next := asRoot(t, mustUpdate(t, m, key(" ")))
		if !next.frag.Sections[0].Entries[2].Disabled {
			t.Error("space right pane did not disable the entry")
		}
		if !next.dirty {
			t.Error("dirty = false after an entry toggle")
		}
	})

	t.Run("locked is skipped with a notice", func(t *testing.T) {
		m, _ := newTestModel(t)
		m = setConfigFragment(t, m, lifecycleFixture())
		m.view = ViewConfig
		m.cfgView = ConfigViewModel{SecFocused: false, EntCursor: 0} // local-zone

		next := asRoot(t, mustUpdate(t, m, key(" ")))
		if !strings.Contains(next.notice, "locked — edit local data in the Local data view") {
			t.Errorf("notice = %q, want the locked-entry notice", next.notice)
		}
		if next.frag.Sections[0].Entries[0].Disabled {
			t.Error("space right pane toggled a locked local-* entry")
		}
		if next.dirty {
			t.Error("dirty = true after a skipped locked toggle")
		}
	})
}

func TestConfigDuplicateSectionsDeleteByIndex(t *testing.T) {
	m, _ := newTestModel(t)
	m = setConfigFragment(t, m, domain.Fragment{Sections: []domain.Section{
		{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: `"."`},
			{Key: "forward-addr", Value: "1.1.1.1"},
		}},
		{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: `"."`},
			{Key: "forward-addr", Value: "8.8.8.8"},
		}},
	}})
	m.view = ViewConfig
	m.cfgView = ConfigViewModel{SecCursor: 1, SecFocused: true}

	next := asRoot(t, mustUpdate(t, m, key("D")))
	next = asRoot(t, mustUpdate(t, next, key("y")))
	if len(next.frag.Sections) != 1 {
		t.Fatalf("sections = %+v, want one sibling left", next.frag.Sections)
	}
	if !hasEntry(next.frag, "forward-addr", "1.1.1.1") {
		t.Error("the first duplicate section was disturbed")
	}
	if hasEntry(next.frag, "forward-addr", "8.8.8.8") {
		t.Error("the second duplicate section was not deleted")
	}
}

// TestConfigEntryEditKeepsZonesProjection pins that a Config-view entry
// mutation leaves the Local data projection refreshed and consistent (the
// locked local-* rows are untouched, so the projected zone survives).
func TestConfigEntryEditKeepsZonesProjection(t *testing.T) {
	m, _ := newTestModel(t)
	m = setConfigFragment(t, m, lifecycleFixture())
	m.view = ViewConfig
	m.cfgView = ConfigViewModel{SecFocused: false, EntCursor: 2}

	next := asRoot(t, mustUpdate(t, m, key(" ")))
	if !next.frag.Sections[0].Entries[2].Disabled {
		t.Fatal("entry edit did not land in frag")
	}
	want, err := config.ZonesFromFragment(next.frag)
	if err != nil {
		t.Fatalf("ZonesFromFragment: %v", err)
	}
	if !reflect.DeepEqual(next.zones, want) {
		t.Errorf("zones = %+v, want %+v", next.zones, want)
	}
	if len(next.zones) != 1 || len(next.zones[0].Records) != 1 {
		t.Errorf("zones = %+v, want the locked local data intact", next.zones)
	}
}

// TestConfigRefreshZonesSurfacesProjectionError pins the loud error path: a
// hand-broken local-* entry must never be silently swallowed.
func TestConfigRefreshZonesSurfacesProjectionError(t *testing.T) {
	m, _ := newTestModel(t)
	m.frag = domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "local-zone", Value: `"unterminated`}}},
	}}

	m.refreshZones()
	if m.state != StateError || m.lastError == nil {
		t.Errorf("state/error = %v/%v, want StateError and a non-nil error", m.state, m.lastError)
	}
}
