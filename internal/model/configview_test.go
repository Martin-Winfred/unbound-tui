package model

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
// change (only load mutates frag in Task 5).
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
