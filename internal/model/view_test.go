package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

func TestWindow(t *testing.T) {
	cases := []struct {
		count, cursor, visible int
		wantStart, wantEnd     int
	}{
		{0, 0, 5, 0, 0},
		{3, 0, 5, 0, 3},
		{10, 0, 4, 0, 4},
		{10, 5, 4, 2, 6},
		{10, 9, 4, 6, 10},
		{10, 9, 1, 9, 10},
	}
	for _, tc := range cases {
		s, e := window(tc.count, tc.cursor, tc.visible)
		if s != tc.wantStart || e != tc.wantEnd {
			t.Errorf("window(%d,%d,%d) = (%d,%d), want (%d,%d)",
				tc.count, tc.cursor, tc.visible, s, e, tc.wantStart, tc.wantEnd)
		}
	}
}

func TestFitAndTruncate(t *testing.T) {
	if got := fit("abc", 5); got != "abc  " {
		t.Errorf("fit short = %q, want %q", got, "abc  ")
	}
	if got := ansi.StringWidth(fit("abcdef", 3)); got != 3 {
		t.Errorf("fit long width = %d, want 3", got)
	}
	if got := fitRight("7", 3); got != "  7" {
		t.Errorf("fitRight = %q, want %q", got, "  7")
	}
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short = %q, want hello", got)
	}
}

func TestViewFooterAndReady(t *testing.T) {
	m, _ := newTestModel(t)
	m.width, m.height = 120, 30
	m.zones = []domain.Zone{{
		Name: "example.com.", Type: "transparent",
		Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}},
	}}
	v := m.View()
	for _, want := range []string{"unbound-tui", "ready", "saved", "a add-zone", "w apply"} {
		if !strings.Contains(v, want) {
			t.Errorf("View missing %q:\n%s", want, v)
		}
	}
}

// TestViewZonesHelpMentionsConfig pins that the zones-view help line names the
// key that opens the Config view, so the view is discoverable.
func TestViewZonesHelpMentionsConfig(t *testing.T) {
	m, _ := newTestModel(t)
	m.width, m.height = 120, 30
	if v := m.View(); !strings.Contains(v, "c config") {
		t.Errorf("zones View help missing %q:\n%s", "c config", v)
	}
}

func TestViewNarrowStacks(t *testing.T) {
	m, _ := newTestModel(t)
	m.width, m.height = 40, 20
	m.zones = []domain.Zone{{
		Name: "example.com.", Type: "transparent",
		Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}},
	}}
	v := m.View()
	if !strings.Contains(v, "Zones") || !strings.Contains(v, "Records") {
		t.Errorf("narrow View missing a pane:\n%s", v)
	}
}

func TestViewConfirmOverlay(t *testing.T) {
	m, _ := newTestModel(t)
	m.zones = []domain.Zone{{Name: "example.com.", Type: "transparent"}}
	m.state = StateConfirm
	m.confirmKind = "zone"
	m.confirmZone = 0
	if v := m.View(); !strings.Contains(v, "Delete zone example.com.") {
		t.Errorf("confirm overlay missing:\n%s", v)
	}
}

func TestViewConfirmOverlaySectionAndEntry(t *testing.T) {
	cases := []struct {
		kind, want string
	}{
		{"section", "Delete this section and all its entries?"},
		{"entry", "Delete this entry?"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			m, _ := newTestModel(t)
			m.state = StateConfirm
			m.confirmKind = tc.kind
			if v := m.View(); !strings.Contains(v, tc.want) {
				t.Errorf("confirm overlay missing %q:\n%s", tc.want, v)
			}
		})
	}
}

// TestViewForeignHelpMentionsUpstreams pins the discoverability hint for the
// Foreign upstreams tab: the help line must name the `u` key.
func TestViewForeignHelpMentionsUpstreams(t *testing.T) {
	m, _ := newTestModel(t)
	m.width, m.height = 120, 30
	m.state = StateForeign
	if v := m.View(); !strings.Contains(v, "u upstreams") {
		t.Errorf("Foreign View help missing %q:\n%s", "u upstreams", v)
	}
}

func TestForeignFilterAndPaging(t *testing.T) {
	var zones []domain.LocalZone
	for i := 0; i < 20; i++ {
		zones = append(zones, domain.LocalZone{Name: fmt.Sprintf("z%02d.example.", i), Type: "static"})
	}
	f := newForeignModel(zones,
		[]string{"a.z00.example. 300 IN A 192.0.2.1", "b.z01.example. 300 IN A 192.0.2.2"}, nil)
	if len(f.shown) != 20 {
		t.Fatalf("shown = %d, want 20", len(f.shown))
	}

	f.filter = "z00"
	f.applyFilter()
	if len(f.shown) != 1 || f.shown[0].name != "z00.example." {
		t.Errorf("filter by name = %+v, want z00.example.", f.shown)
	}

	f.filter = "192.0.2.2"
	f.applyFilter()
	if len(f.shown) != 1 || f.shown[0].name != "z01.example." {
		t.Errorf("filter by rr = %+v, want z01.example.", f.shown)
	}

	f.filter = ""
	f.applyFilter()
	f.resize(80, 10) // pageRows = 10-3 = 7
	f.page(1)
	if f.zCur != 7 {
		t.Errorf("zCur after page down = %d, want 7", f.zCur)
	}
	f.focusRR = true
	f.zCur = 0
	f.page(1)
	if f.zCur != 0 || f.rCur != 0 {
		t.Errorf("page with rr focus changed zone: zCur=%d rCur=%d", f.zCur, f.rCur)
	}
}

func TestForeignOrphansBucket(t *testing.T) {
	f := newForeignModel(
		[]domain.LocalZone{{Name: "mine.example.", Type: "static"}},
		[]string{"orphan.other.example. 300 IN A 192.0.2.9"}, nil)
	var found bool
	for _, z := range f.all {
		if z.name == noZoneName {
			found = true
			if len(z.rrs) != 1 {
				t.Errorf("orphan bucket rrs = %v, want 1", z.rrs)
			}
		}
	}
	if !found {
		t.Errorf("orphan RR not bucketed under %q: %+v", noZoneName, f.all)
	}
}

// TestViewSectionFormOverlay pins that StateSectionForm renders the specialized
// form box: the kind-derived header, a field label/value, and the help hint.
func TestViewSectionFormOverlay(t *testing.T) {
	for _, kind := range []string{"forward-zone", "stub-zone"} {
		t.Run(kind, func(t *testing.T) {
			f := domain.Fragment{Sections: []domain.Section{
				{Kind: kind, Entries: []domain.Entry{
					{Key: "name", Value: "test."},
					{Key: addrKeyFor(kind), Value: "192.0.2.53"},
				}},
			}}
			m := configModel(t, f)
			m.width, m.height = 120, 30
			m.cfgView.SecCursor = 0
			m = asRoot(t, mustUpdate(t, m, key("E")))
			if m.state != StateSectionForm {
				t.Fatalf("state = %v, want StateSectionForm", m.state)
			}

			v := m.View()
			for _, want := range []string{
				"edit " + kind,
				"Name",
				"192.0.2.53",
				"esc cancel · tab next · ctrl+s apply",
			} {
				if !strings.Contains(v, want) {
					t.Errorf("section-form View missing %q:\n%s", want, v)
				}
			}
		})
	}
}
