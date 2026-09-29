package model

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/config"
	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

func TestForeignModelViewAndClose(t *testing.T) {
	f := newForeignModel(
		[]domain.LocalZone{{Name: "z.example.", Type: "static"}},
		[]string{"x.z.example. 300 IN A 192.0.2.1"},
		nil,
	)
	view := f.View()
	if !strings.Contains(view, "z.example.") || !strings.Contains(view, "x.z.example.") {
		t.Errorf("View missing entries:\n%s", view)
	}
	_, cmd := f.Update(key("esc"))
	if cmd == nil {
		t.Fatal("esc produced no command")
	}
	if _, ok := cmd().(ForeignCloseMsg); !ok {
		t.Fatalf("esc produced %T, want ForeignCloseMsg", cmd())
	}
}

func TestForeignModelEmpty(t *testing.T) {
	f := newForeignModel(nil, nil, nil)
	if !strings.Contains(f.View(), "None") {
		t.Errorf("empty view = %q, want a None message", f.View())
	}
}

func TestRequestForeignLoadsAndCloses(t *testing.T) {
	m, ctl := newTestModel(t)
	m.zones = []domain.Zone{{Name: "mine.example.", Type: "transparent"}}
	ctl.zones = []domain.LocalZone{{Name: "theirs.example.", Type: "static"}}
	ctl.rrs = []string{"x.theirs.example. 300 IN A 192.0.2.9"}

	next, cmd := m.Update(key("f"))
	root := asRoot(t, next)
	if root.state != StateForeign {
		t.Fatalf("state = %v, want StateForeign", root.state)
	}
	if cmd == nil {
		t.Fatal("foreign request produced no command")
	}
	root = asRoot(t, mustUpdate(t, root, cmd()))
	if !strings.Contains(root.foreign.View(), "theirs.example.") {
		t.Errorf("foreign view missing runtime entry:\n%s", root.foreign.View())
	}
}

func TestBuildUpstreamRows(t *testing.T) {
	own := "/etc/unbound/unbound.conf.d/unbound-tui.conf"
	active := []domain.Entry{
		{Key: "name", Value: `"test."`},
		{Key: "forward-addr", Value: "192.0.2.53"},
	}
	eff := config.Effective{Sections: []config.EffectiveSection{
		{Section: domain.Section{Kind: "server", Entries: active}, Source: "/etc/unbound/unbound.conf"},
		{Section: domain.Section{Kind: "forward-zone", Entries: active}, Source: "/etc/unbound/conf.d/fwd.conf"},
		{Section: domain.Section{Kind: "stub-zone", Entries: []domain.Entry{{Key: "name", Value: `"dead."`, Disabled: true}}}, Source: "/etc/unbound/conf.d/stub.conf"},
		{Section: domain.Section{Kind: "forward-zone", Entries: active}, Source: own},
		{Section: domain.Section{Kind: "forward-zone"}, Source: "/etc/unbound/conf.d/empty.conf"},
		{Section: domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "forward-addr", Value: "192.0.2.1"}}}, Source: "/etc/unbound/conf.d/nameless.conf"},
	}}
	got := buildUpstreamRows(eff, own)
	want := []UpstreamRow{
		{Kind: "forward-zone", Name: "test.", Source: "/etc/unbound/conf.d/fwd.conf", Entries: 2},
		{Kind: "stub-zone", Name: "dead.", Source: "/etc/unbound/conf.d/stub.conf", Entries: 1, Dead: true},
		{Kind: "forward-zone", Name: ".", Source: "/etc/unbound/conf.d/empty.conf", Dead: true},
		{Kind: "forward-zone", Name: ".", Source: "/etc/unbound/conf.d/nameless.conf", Entries: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildUpstreamRows:\n got %+v\nwant %+v", got, want)
	}
}

// TestBuildUpstreamRowsExcludesSymlinkedOwn pins the ownPath trap: Source is
// symlink-resolved by ReadEffective while the caller may hand in a symlinked
// fragment path, so both sides must normalize before comparison.
func TestBuildUpstreamRowsExcludesSymlinkedOwn(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "frag.conf")
	if err := os.WriteFile(real, []byte("server:\n"), 0644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	link := filepath.Join(dir, "link.conf")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	source, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	eff := config.Effective{Sections: []config.EffectiveSection{
		{Section: domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"mine."`}}}, Source: source},
		{Section: domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"theirs."`}}}, Source: filepath.Join(dir, "other.conf")},
	}}
	got := buildUpstreamRows(eff, link)
	if len(got) != 1 || got[0].Name != "theirs." {
		t.Fatalf("buildUpstreamRows with symlinked ownPath = %+v, want only theirs.", got)
	}
}

func TestForeignModelUpstreamsTab(t *testing.T) {
	f := newForeignModel(
		[]domain.LocalZone{{Name: "z.example.", Type: "static"}},
		[]string{"x.z.example. 300 IN A 192.0.2.1"},
		nil,
	)
	f.setUpstreams([]UpstreamRow{
		{Kind: "forward-zone", Name: "test.", Source: "fwd.conf", Entries: 2},
		{Kind: "stub-zone", Name: "dead.", Source: "stub.conf", Entries: 1, Dead: true},
	}, "")
	f.resize(80, 24)

	if v := f.View(); !strings.Contains(v, "z.example.") || strings.Contains(v, "forward-zone test.") {
		t.Fatalf("runtime tab wrong:\n%s", v)
	}

	f, _ = f.Update(key("u"))
	if f.tab != 1 {
		t.Fatalf("tab after u = %d, want 1", f.tab)
	}
	v := f.View()
	if !strings.Contains(v, "forward-zone test. · 2 entries · fwd.conf") {
		t.Errorf("upstream row not rendered:\n%s", v)
	}
	if !strings.Contains(v, "stub-zone dead. · 1 entries · stub.conf ⛔") {
		t.Errorf("dead upstream row missing ⛔:\n%s", v)
	}
	if strings.Contains(v, "z.example.") {
		t.Errorf("upstreams tab leaked runtime zones:\n%s", v)
	}

	if _, cmd := f.Update(key("esc")); cmd == nil {
		t.Fatal("esc on upstreams tab produced no command")
	} else if _, ok := cmd().(ForeignCloseMsg); !ok {
		t.Fatalf("esc produced %T, want ForeignCloseMsg", cmd())
	}

	f, _ = f.Update(key("u"))
	if f.tab != 0 {
		t.Fatalf("tab after second u = %d, want 0", f.tab)
	}
	if v := f.View(); !strings.Contains(v, "z.example.") {
		t.Errorf("runtime tab not restored:\n%s", v)
	}
}

func TestForeignModelUpstreamsFilter(t *testing.T) {
	f := newForeignModel(nil, nil, nil)
	f.setUpstreams([]UpstreamRow{
		{Kind: "forward-zone", Name: "alpha.", Source: "a.conf", Entries: 1},
		{Kind: "stub-zone", Name: "beta.", Source: "b.conf", Entries: 1},
	}, "")
	f.resize(80, 24)
	f, _ = f.Update(key("u"))

	f.filter = "stub"
	f.applyFilter()
	if len(f.upShown) != 1 || f.upShown[0].Name != "beta." {
		t.Errorf("filter by kind = %+v, want beta.", f.upShown)
	}

	f.filter = "alpha"
	f.applyFilter()
	if len(f.upShown) != 1 || f.upShown[0].Kind != "forward-zone" {
		t.Errorf("filter by name = %+v, want alpha.", f.upShown)
	}

	f.filter = "b.conf"
	f.applyFilter()
	if len(f.upShown) != 1 || f.upShown[0].Name != "beta." {
		t.Errorf("filter by source = %+v, want beta.", f.upShown)
	}

	f.filter = ""
	f.applyFilter()
	f, _ = f.Update(key("/"))
	if !f.filtering || f.tab != 1 {
		t.Fatalf("filtering=%v tab=%d after /, want true/1", f.filtering, f.tab)
	}
}

func TestForeignModelUpstreamsEmptyAndError(t *testing.T) {
	f := newForeignModel(nil, nil, nil)
	f.setUpstreams(nil, "")
	f.resize(80, 24)
	f, _ = f.Update(key("u"))
	if !strings.Contains(f.View(), "no foreign forward/stub sections") {
		t.Errorf("empty upstreams missing placeholder:\n%s", f.View())
	}

	f = newForeignModel(nil, nil, nil)
	f.setUpstreams(nil, "read config: boom")
	f.resize(80, 24)
	f, _ = f.Update(key("u"))
	if !strings.Contains(f.View(), "read config: boom") {
		t.Errorf("upstream read error not shown:\n%s", f.View())
	}
}

// TestRequestForeignLoadsUpstreams covers the fresh ReadEffective wired into
// requestForeign: it overrides the startup snapshot for the Foreign view.
func TestRequestForeignLoadsUpstreams(t *testing.T) {
	m, ctl := newTestModel(t)
	if err := os.WriteFile(m.cfg.FragmentPath(), []byte("server:\n"), 0644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	foreign := filepath.Join(filepath.Dir(m.cfg.FragmentPath()), "zz-foreign.conf")
	if err := os.WriteFile(foreign, []byte("forward-zone:\n  name: \"test.\"\n  forward-addr: 192.0.2.53\n"), 0644); err != nil {
		t.Fatalf("write foreign: %v", err)
	}
	main := m.cfg.MainConfPath()
	// Includes are resolved relative to the declaring file's directory; keep
	// them relative so this test does not depend on absolute-include support.
	if err := os.WriteFile(main, []byte("include: frag.conf\ninclude: zz-foreign.conf\n"), 0644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	ctl.zones = []domain.LocalZone{{Name: "theirs.example.", Type: "static"}}
	resolved, err := filepath.EvalSymlinks(foreign)
	if err != nil {
		resolved = foreign
	}

	// The Init snapshot feeds add-time conflict warnings (Task 4), so it must
	// be populated and stored on the root model too.
	loaded, ok := m.Init()().(ZonesLoadedMsg)
	if !ok {
		t.Fatalf("Init produced %T, want ZonesLoadedMsg", m.Init()())
	}
	if len(loaded.Upstreams) != 1 || loaded.Upstreams[0].Name != "test." {
		t.Fatalf("Init upstream snapshot = %+v, want one forward-zone test.", loaded.Upstreams)
	}
	m = asRoot(t, mustUpdate(t, m, loaded))
	if len(m.upstreams) != 1 || m.upstreams[0].Source != resolved {
		t.Fatalf("m.upstreams = %+v, want the Init snapshot", m.upstreams)
	}

	next, cmd := m.Update(key("f"))
	root := asRoot(t, next)
	if cmd == nil {
		t.Fatal("foreign request produced no command")
	}
	msg, ok := cmd().(ForeignLoadedMsg)
	if !ok {
		t.Fatalf("foreign request produced %T, want ForeignLoadedMsg", cmd())
	}
	if len(msg.Upstreams) != 1 || msg.Upstreams[0].Kind != "forward-zone" || msg.Upstreams[0].Name != "test." {
		t.Fatalf("upstreams = %+v (upErr=%q), want one forward-zone test.", msg.Upstreams, msg.UpErr)
	}
	if msg.Upstreams[0].Source != resolved {
		t.Errorf("source = %q, want symlink-resolved %q", msg.Upstreams[0].Source, resolved)
	}
	root = asRoot(t, mustUpdate(t, root, msg))
	root.foreign.resize(80, 24)
	root.foreign, _ = root.foreign.Update(key("u"))
	if v := root.foreign.View(); !strings.Contains(v, "forward-zone test. · 2 entries · ") {
		t.Errorf("upstream row missing from foreign view:\n%s", v)
	}
}
