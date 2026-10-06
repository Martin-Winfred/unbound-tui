package model

import (
	"fmt"
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

// TestForeignFetchLoadingFlag pins the explicit in-flight state: the flag is
// set when the fetch is dispatched, the empty view says loading instead of the
// terminal None message, and the flag is cleared on both a successful and a
// failed fetch (the error path still renders).
func TestForeignFetchLoadingFlag(t *testing.T) {
	t.Run("set in flight and cleared on success", func(t *testing.T) {
		m, ctl := newTestModel(t)
		ctl.zones = []domain.LocalZone{{Name: "z.example.", Type: "static"}}
		next, cmd := m.Update(key("f"))
		root := asRoot(t, next)
		if !root.foreign.loading {
			t.Fatal("loading = false right after dispatch, want true")
		}
		if v := root.foreign.View(); !strings.Contains(v, "loading") {
			t.Errorf("in-flight foreign view = %q, want a loading placeholder", v)
		}
		if cmd == nil {
			t.Fatal("foreign request produced no command")
		}
		root = asRoot(t, mustUpdate(t, root, cmd()))
		if root.foreign.loading {
			t.Error("loading = true after ForeignLoadedMsg, want false")
		}
	})

	t.Run("cleared on fetch error", func(t *testing.T) {
		m, ctl := newTestModel(t)
		ctl.zonesErr = fmt.Errorf("list zones: boom")
		next, cmd := m.Update(key("f"))
		root := asRoot(t, next)
		if !root.foreign.loading {
			t.Fatal("loading = false right after dispatch, want true")
		}
		if cmd == nil {
			t.Fatal("foreign request produced no command")
		}
		root = asRoot(t, mustUpdate(t, root, cmd()))
		if root.foreign.loading {
			t.Error("loading = true after a failed fetch, want false")
		}
		if !strings.Contains(root.foreign.View(), "Error:") {
			t.Errorf("failed fetch view missing the error path:\n%s", root.foreign.View())
		}
	})
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
	if !strings.Contains(v, "stub-zone dead. · 1 entry · stub.conf ⛔") {
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

// TestForeignModelUpstreamsFilterMiss pins the filter-miss placeholder: an
// active filter with no matching upstream row names the query instead of the
// generic empty-list placeholder.
func TestForeignModelUpstreamsFilterMiss(t *testing.T) {
	f := newForeignModel(nil, nil, nil)
	f.setUpstreams([]UpstreamRow{
		{Kind: "forward-zone", Name: "alpha.", Source: "a.conf", Entries: 1},
	}, "")
	f.resize(80, 24)
	f, _ = f.Update(key("u"))

	f.filter = "zzz-no-such"
	f.applyFilter()
	if len(f.upShown) != 0 {
		t.Fatalf("upShown = %+v, want no matches", f.upShown)
	}
	want := `no upstreams match "zzz-no-such"`
	if v := f.View(); !strings.Contains(v, want) {
		t.Errorf("filter-miss view missing %q:\n%s", want, v)
	}
}

// TestUpstreamRowsPluralization pins the singular/plural entry count in the
// config-file upstream rows.
func TestUpstreamRowsPluralization(t *testing.T) {
	one := upstreamRows([]UpstreamRow{{Kind: "forward-zone", Name: "a.", Source: "a.conf", Entries: 1}})
	if len(one) != 1 || !strings.Contains(one[0].text, "· 1 entry ·") {
		t.Errorf("single-entry row = %+v, want `· 1 entry ·`", one)
	}
	two := upstreamRows([]UpstreamRow{{Kind: "forward-zone", Name: "b.", Source: "b.conf", Entries: 2}})
	if len(two) != 1 || !strings.Contains(two[0].text, "· 2 entries ·") {
		t.Errorf("two-entry row = %+v, want `· 2 entries ·`", two)
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

	// The Init snapshot feeds add-time conflict warnings, so it must be
	// populated and stored on the root model too.
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

// TestForeignLoadedMsgBackfillsUpstreamSnapshot pins the backflow from the
// Foreign fetch into the add-time conflict snapshot: a successful fetch
// refreshes m.upstreams with the fresher rows, while a read failure (UpErr)
// leaves the previous snapshot untouched so the warning never goes blind.
func TestForeignLoadedMsgBackfillsUpstreamSnapshot(t *testing.T) {
	t.Run("success refreshes the snapshot", func(t *testing.T) {
		m, _ := newTestModel(t)
		m.upstreams = []UpstreamRow{{Kind: "forward-zone", Name: "stale.", Source: "old.conf"}}
		fresh := []UpstreamRow{{Kind: "stub-zone", Name: "fresh.", Source: "new.conf"}}
		m = asRoot(t, mustUpdate(t, m, ForeignLoadedMsg{
			Zones:     []domain.LocalZone{{Name: "z.example.", Type: "static"}},
			Upstreams: fresh,
		}))
		if !reflect.DeepEqual(m.upstreams, fresh) {
			t.Errorf("m.upstreams = %+v, want the fresher fetch %+v", m.upstreams, fresh)
		}
	})

	t.Run("error keeps the previous snapshot", func(t *testing.T) {
		m, _ := newTestModel(t)
		prev := []UpstreamRow{{Kind: "forward-zone", Name: "stale.", Source: "old.conf"}}
		m.upstreams = prev
		m = asRoot(t, mustUpdate(t, m, ForeignLoadedMsg{
			Err:       fmt.Errorf("list zones: boom"),
			UpErr:     "read config: boom",
			Upstreams: []UpstreamRow{{Kind: "stub-zone", Name: "fresh.", Source: "new.conf"}},
		}))
		if !reflect.DeepEqual(m.upstreams, prev) {
			t.Errorf("m.upstreams = %+v, want the previous snapshot %+v", m.upstreams, prev)
		}
	})
}

// TestBuildScalarIndex pins the add-time warning snapshot: it keeps only
// singleton keys in a server/remote-control section, drops everything our own
// fragment declares, and lists the foreign sources in effective order.
func TestBuildScalarIndex(t *testing.T) {
	own := "/etc/unbound/unbound.conf.d/unbound-tui.conf"
	eff := config.Effective{Sections: []config.EffectiveSection{
		{Section: domain.Section{Kind: "server", Entries: []domain.Entry{
			{Key: "verbosity", Value: "5"},
			{Key: "access-control", Value: "192.0.2.0/24 allow"},
		}}, Source: "/etc/unbound/conf.d/zz-foreign.conf"},
		{Section: domain.Section{Kind: "remote-control", Entries: []domain.Entry{
			{Key: "control-port", Value: "8953"},
		}}, Source: "/etc/unbound/remote-control.conf"},
		{Section: domain.Section{Kind: "server", Entries: []domain.Entry{
			{Key: "verbosity", Value: "3"},
		}}, Source: own},
		{Section: domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "port", Value: "853"},
		}}, Source: "/etc/unbound/conf.d/fwd.conf"},
		{Section: domain.Section{Kind: "server", Entries: []domain.Entry{
			{Key: "verbosity", Value: "1"},
		}}, Source: "/etc/unbound/conf.d/other.conf"},
	}}
	got := buildScalarIndex(eff, own)
	want := map[[2]string][]string{
		{"server", "verbosity"}:            {"/etc/unbound/conf.d/zz-foreign.conf", "/etc/unbound/conf.d/other.conf"},
		{"remote-control", "control-port"}: {"/etc/unbound/remote-control.conf"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildScalarIndex:\n got %+v\nwant %+v", got, want)
	}
}

// TestBuildScalarIndexExcludesSymlinkedOwn pins the ownPath trap for the
// scalar snapshot, mirroring buildUpstreamRows: Source is symlink-resolved by
// ReadEffective while the caller may hand in a symlinked fragment path.
func TestBuildScalarIndexExcludesSymlinkedOwn(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "frag.conf")
	if err := os.WriteFile(real, []byte("server:\n  verbosity: 3\n"), 0644); err != nil {
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
		{Section: domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "verbosity", Value: "3"}}}, Source: source},
		{Section: domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "verbosity", Value: "1"}}}, Source: filepath.Join(dir, "other.conf")},
	}}
	got := buildScalarIndex(eff, link)
	if len(got) != 1 {
		t.Fatalf("buildScalarIndex with symlinked ownPath = %+v, want only the foreign pair", got)
	}
	sources := got[[2]string{"server", "verbosity"}]
	if len(sources) != 1 || sources[0] != filepath.Join(dir, "other.conf") {
		t.Errorf("sources = %+v, want only the foreign file", sources)
	}
}

// TestBuildScalarIndexSkipsDeadEntries pins the active-entry rule: a foreign
// section contributes a {kind,key} source only when it has an active entry of
// that key, mirroring FindScalarConflicts. A key whose foreign entries are all
// disabled (commented out) must not raise an add-time warning.
func TestBuildScalarIndexSkipsDeadEntries(t *testing.T) {
	own := "/etc/unbound/unbound.conf.d/unbound-tui.conf"
	const src = "/etc/unbound/conf.d/zz.conf"
	cases := []struct {
		name  string
		entry domain.Entry
		want  map[[2]string][]string
	}{
		{
			name:  "all key entries disabled contributes nothing",
			entry: domain.Entry{Key: "verbosity", Value: "1", Disabled: true},
			want:  map[[2]string][]string{},
		},
		{
			name:  "active key entry contributes",
			entry: domain.Entry{Key: "verbosity", Value: "5"},
			want:  map[[2]string][]string{{"server", "verbosity"}: {src}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eff := config.Effective{Sections: []config.EffectiveSection{
				{Section: domain.Section{Kind: "server", Entries: []domain.Entry{tc.entry}}, Source: src},
			}}
			if got := buildScalarIndex(eff, own); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildScalarIndex = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestBuildScalarIndexSkipsDeadEntriesMixed pins the per-key rule: a disabled
// entry does not suppress a sibling active entry of the same key.
func TestBuildScalarIndexSkipsDeadEntriesMixed(t *testing.T) {
	own := "/etc/unbound/unbound.conf.d/unbound-tui.conf"
	const src = "/etc/unbound/conf.d/zz.conf"
	eff := config.Effective{Sections: []config.EffectiveSection{
		{Section: domain.Section{Kind: "server", Entries: []domain.Entry{
			{Key: "verbosity", Value: "1", Disabled: true},
			{Key: "verbosity", Value: "5"},
		}}, Source: src},
	}}
	want := map[[2]string][]string{{"server", "verbosity"}: {src}}
	if got := buildScalarIndex(eff, own); !reflect.DeepEqual(got, want) {
		t.Errorf("buildScalarIndex = %+v, want %+v", got, want)
	}
}

// TestForeignLoadedMsgBackfillsScalarIndex pins the backflow from the Foreign
// fetch into the add-time scalar snapshot: a successful fetch refreshes
// m.scalarIdx with the fresher index, while a read failure (UpErr) leaves the
// previous snapshot untouched so the warning never goes blind.
func TestForeignLoadedMsgBackfillsScalarIndex(t *testing.T) {
	t.Run("success refreshes the snapshot", func(t *testing.T) {
		m, _ := newTestModel(t)
		m.scalarIdx = map[[2]string][]string{{"server", "verbosity"}: {"old.conf"}}
		fresh := map[[2]string][]string{{"server", "verbosity"}: {"new.conf"}}
		m = asRoot(t, mustUpdate(t, m, ForeignLoadedMsg{
			Zones:     []domain.LocalZone{{Name: "z.example.", Type: "static"}},
			ScalarIdx: fresh,
		}))
		if !reflect.DeepEqual(m.scalarIdx, fresh) {
			t.Errorf("m.scalarIdx = %+v, want the fresher fetch %+v", m.scalarIdx, fresh)
		}
	})

	t.Run("error keeps the previous snapshot", func(t *testing.T) {
		m, _ := newTestModel(t)
		prev := map[[2]string][]string{{"server", "verbosity"}: {"old.conf"}}
		m.scalarIdx = prev
		m = asRoot(t, mustUpdate(t, m, ForeignLoadedMsg{
			Err:       fmt.Errorf("list zones: boom"),
			UpErr:     "read config: boom",
			ScalarIdx: map[[2]string][]string{{"server", "verbosity"}: {"new.conf"}},
		}))
		if !reflect.DeepEqual(m.scalarIdx, prev) {
			t.Errorf("m.scalarIdx = %+v, want the previous snapshot %+v", m.scalarIdx, prev)
		}
	})
}

// TestForeignModelNavigation drives the movement keys on both tabs and pins the
// clamping: the runtime-zone pane, the RR detail pane, and the upstreams tab.
func TestForeignModelNavigation(t *testing.T) {
	f := newForeignModel(
		[]domain.LocalZone{
			{Name: "a.example.", Type: "static"},
			{Name: "b.example.", Type: "static"},
		},
		[]string{
			"x.a.example. 300 IN A 192.0.2.1",
			"y.a.example. 300 IN A 192.0.2.2",
			"p.b.example. 300 IN A 192.0.2.3",
		},
		nil,
	)
	f.setUpstreams([]UpstreamRow{
		{Kind: "forward-zone", Name: "u1.", Source: "a.conf"},
		{Kind: "stub-zone", Name: "u2.", Source: "b.conf"},
	}, "")
	f.resize(80, 8)

	// Runtime-zone pane.
	f, _ = f.Update(key("down"))
	if f.zCur != 1 {
		t.Fatalf("zone down: zCur = %d, want 1", f.zCur)
	}
	f, _ = f.Update(key("down")) // clamp at the last zone
	if f.zCur != 1 {
		t.Fatalf("zone down at bottom: zCur = %d, want 1", f.zCur)
	}
	f, _ = f.Update(key("up"))
	f, _ = f.Update(key("up")) // clamp at the first zone
	if f.zCur != 0 {
		t.Fatalf("zone up at top: zCur = %d, want 0", f.zCur)
	}
	f, _ = f.Update(key("G"))
	if f.zCur != 1 {
		t.Fatalf("zone G: zCur = %d, want 1", f.zCur)
	}
	f, _ = f.Update(key("g"))
	if f.zCur != 0 {
		t.Fatalf("zone g: zCur = %d, want 0", f.zCur)
	}

	// RR detail pane: tab focuses it.
	f, _ = f.Update(key("tab"))
	if !f.focusRR {
		t.Fatal("tab did not focus the RR pane")
	}
	f, _ = f.Update(key("down"))
	if f.rCur != 1 {
		t.Fatalf("rr down: rCur = %d, want 1", f.rCur)
	}
	f, _ = f.Update(key("down")) // clamp at the last RR
	if f.rCur != 1 {
		t.Fatalf("rr down at bottom: rCur = %d, want 1", f.rCur)
	}
	f, _ = f.Update(key("up"))
	f, _ = f.Update(key("up")) // clamp at the first RR
	if f.rCur != 0 {
		t.Fatalf("rr up at top: rCur = %d, want 0", f.rCur)
	}
	f, _ = f.Update(key("G"))
	if f.rCur != 1 {
		t.Fatalf("rr G: rCur = %d, want 1", f.rCur)
	}

	// Upstreams tab.
	f, _ = f.Update(key("u"))
	if f.tab != 1 {
		t.Fatalf("tab after u = %d, want 1", f.tab)
	}
	f, _ = f.Update(key("down"))
	if f.upCur != 1 {
		t.Fatalf("upstream down: upCur = %d, want 1", f.upCur)
	}
	f, _ = f.Update(key("down")) // clamp at the last upstream
	if f.upCur != 1 {
		t.Fatalf("upstream down at bottom: upCur = %d, want 1", f.upCur)
	}
	f, _ = f.Update(key("up"))
	f, _ = f.Update(key("up")) // clamp at the first upstream
	if f.upCur != 0 {
		t.Fatalf("upstream up: upCur = %d, want 0", f.upCur)
	}
	f, _ = f.Update(key("G"))
	if f.upCur != 1 {
		t.Fatalf("upstream G: upCur = %d, want 1", f.upCur)
	}

	// Empty panes: jumping to the bottom must pin the cursor at 0, not -1.
	f.setUpstreams(nil, "")
	f, _ = f.Update(key("G"))
	if f.upCur != 0 {
		t.Fatalf("upstream G on an empty list: upCur = %d, want 0", f.upCur)
	}
	empty := newForeignModel([]domain.LocalZone{{Name: "empty.example.", Type: "static"}}, nil, nil)
	empty.resize(80, 8)
	empty, _ = empty.Update(key("tab")) // focus the (empty) RR pane
	empty, _ = empty.Update(key("G"))
	if empty.rCur != 0 {
		t.Fatalf("rr G on an empty list: rCur = %d, want 0", empty.rCur)
	}
}
