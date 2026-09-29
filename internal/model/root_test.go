package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/config"
	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

type fakeCtl struct {
	reloads int
	zones   []domain.LocalZone
	rrs     []string
}

func (f *fakeCtl) Reload() error                      { f.reloads++; return nil }
func (f *fakeCtl) Status() (domain.StatusInfo, error) { return domain.StatusInfo{}, nil }
func (f *fakeCtl) ListLocalZones() ([]domain.LocalZone, error) {
	return f.zones, nil
}
func (f *fakeCtl) ListLocalData() ([]string, error) { return f.rrs, nil }

var _ domain.Controller = (*fakeCtl)(nil)

func newTestModel(t *testing.T) (RootModel, *fakeCtl) {
	t.Helper()
	dir := t.TempDir()
	confDir := filepath.Join(dir, "conf.d")
	if err := os.MkdirAll(confDir, 0755); err != nil {
		t.Fatalf("mkdir conf.d: %v", err)
	}
	conf := filepath.Join(dir, "unbound.conf")
	// Mirror Debian's stub: include-toplevel with a glob under conf.d, which
	// keeps the main config from glob-including itself. A literal include of
	// our fragment would break the first apply, before the fragment exists.
	if err := os.WriteFile(conf, []byte("include-toplevel: "+filepath.Join(confDir, "*.conf")+"\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	mgr, err := config.NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.SetFragmentPath(filepath.Join(dir, "frag.conf"))
	ctl := &fakeCtl{}
	m := NewRootModel(ctl, mgr, "test")
	// Run the Init path so models start with a valid (empty) fragment, which
	// mutations then regenerate into.
	next, _ := m.Update(m.Init()())
	return asRoot(t, next), ctl
}

// localEntries flattens the local-zone/local-data entries of a fragment.
func localEntries(f domain.Fragment) []domain.Entry {
	var out []domain.Entry
	for _, s := range f.Sections {
		for _, e := range s.Entries {
			if e.Key == "local-zone" || e.Key == "local-data" {
				out = append(out, e)
			}
		}
	}
	return out
}

func asRoot(t *testing.T, m tea.Model) RootModel {
	t.Helper()
	r, ok := m.(RootModel)
	if !ok {
		t.Fatalf("Update returned %T, want RootModel", m)
	}
	return r
}

func key(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestInitLoadsZones(t *testing.T) {
	m, _ := newTestModel(t)
	msg := m.Init()()
	loaded, ok := msg.(ZonesLoadedMsg)
	if !ok {
		t.Fatalf("Init produced %T, want ZonesLoadedMsg", msg)
	}
	m = asRoot(t, mustUpdate(t, m, loaded))
	if len(m.zones) != 0 {
		t.Errorf("zones = %+v, want empty", m.zones)
	}
	if len(m.frag.Sections) != 0 {
		t.Errorf("frag sections = %+v, want empty", m.frag.Sections)
	}
}

func TestInitStoresFragmentAndProjectsZones(t *testing.T) {
	m, _ := newTestModel(t)
	src := "server:\n" +
		"local-zone: \"example.com.\" static\n" +
		"local-data: \"example.com. 300 IN A 192.0.2.1\"\n"
	if err := os.WriteFile(m.cfg.FragmentPath(), []byte(src), 0644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}

	loaded := m.Init()().(ZonesLoadedMsg)
	if len(loaded.Fragment.Sections) != 1 {
		t.Fatalf("Init fragment = %+v, want one server section", loaded.Fragment)
	}
	m = asRoot(t, mustUpdate(t, m, loaded))
	if len(m.zones) != 1 || m.zones[0].Name != "example.com." {
		t.Fatalf("zones = %+v, want one example.com. zone", m.zones)
	}
	if got := localEntries(m.frag); len(got) != 2 {
		t.Errorf("stored frag local entries = %+v, want 2", got)
	}
}

// TestProjectionErrorSurfacesLoudly pins that a malformed local-* entry keeps
// its fragment but moves the model into StateError via the normal error path.
func TestProjectionErrorSurfacesLoudly(t *testing.T) {
	m, _ := newTestModel(t)
	src := "server:\nlocal-data: \"broken\n"
	if err := os.WriteFile(m.cfg.FragmentPath(), []byte(src), 0644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}

	loaded := m.Init()().(ZonesLoadedMsg)
	next, cmd := m.Update(loaded)
	if cmd == nil {
		t.Fatal("projection error produced no command, want an ErrorMsg")
	}
	errMsg, ok := cmd().(ErrorMsg)
	if !ok {
		t.Fatalf("projection error produced %T, want ErrorMsg", cmd())
	}
	root := asRoot(t, mustUpdate(t, asRoot(t, next), errMsg))
	if root.state != StateError || root.lastError == nil {
		t.Errorf("state/error = %v/%v, want StateError and a non-nil error", root.state, root.lastError)
	}
}

func mustUpdate(t *testing.T, m RootModel, msg tea.Msg) tea.Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next
}

func TestAddZoneThenApplyWritesAndReloads(t *testing.T) {
	m, ctl := newTestModel(t)

	m = asRoot(t, mustUpdate(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com", Type: "transparent"}))
	if !m.dirty {
		t.Fatal("dirty = false after add, want true")
	}
	if len(m.zones) != 1 || m.zones[0].Name != "example.com." {
		t.Fatalf("zones = %+v, want one normalized zone", m.zones)
	}
	if got := localEntries(m.frag); len(got) != 1 || got[0].Value != `"example.com." transparent` {
		t.Fatalf("frag local entries = %+v, want the new local-zone", got)
	}

	msg := m.apply()()
	if _, ok := msg.(AppliedMsg); !ok {
		t.Fatalf("apply produced %T (%v), want AppliedMsg", msg, msg)
	}
	m = asRoot(t, mustUpdate(t, m, msg))
	if m.dirty {
		t.Error("dirty = true after apply, want false")
	}
	if ctl.reloads != 1 {
		t.Errorf("reloads = %d, want 1", ctl.reloads)
	}
	got, err := os.ReadFile(m.cfg.FragmentPath())
	if err != nil {
		t.Fatalf("read fragment: %v", err)
	}
	if !strings.Contains(string(got), `local-zone: "example.com." transparent`) {
		t.Errorf("fragment missing zone:\n%s", got)
	}
}

// TestApplyRefusesMalformedFragment pins the hardened apply gate: after a
// failed projection (StateError) has been dismissed, `w` must re-project the
// fragment and refuse rather than writing a hand-malformed local-* value.
func TestApplyRefusesMalformedFragment(t *testing.T) {
	m, ctl := newTestModel(t)
	m.frag = domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{
			{Key: "edns-buffer-size", Value: "1232"},
			{Key: "local-data", Value: `"unterminated`},
		}},
	}}
	m.view = ViewConfig
	m.cfgView = ConfigViewModel{SecFocused: false}
	m.clampCfgCursors()

	// Toggle the unlocked entry: the mutation marks the model dirty, then the
	// re-projection fails and the model reports the error.
	next := asRoot(t, mustUpdate(t, m, key(" ")))
	if next.state != StateError || next.lastError == nil {
		t.Fatalf("state/error = %v/%v, want StateError after the projection failure", next.state, next.lastError)
	}
	if !next.dirty {
		t.Fatal("dirty = false after the Config mutation, want true")
	}

	// Any key dismisses the error; the dirty flag survives.
	next = asRoot(t, mustUpdate(t, next, key("x")))
	if next.state != StateReady {
		t.Fatalf("state = %v, want StateReady after dismissing the error", next.state)
	}

	// Applying must re-check the fragment and refuse with an ErrorMsg.
	applied, cmd := next.Update(key("w"))
	next = asRoot(t, applied)
	if next.state == StateApplying {
		t.Fatal("apply started despite the malformed fragment")
	}
	if cmd == nil {
		t.Fatal("apply produced no command, want an ErrorMsg")
	}
	if _, ok := cmd().(ErrorMsg); !ok {
		t.Fatalf("apply produced %T, want ErrorMsg", cmd())
	}
	if ctl.reloads != 0 {
		t.Errorf("reloads = %d, want 0 on a refused apply", ctl.reloads)
	}
	if _, err := os.Stat(m.cfg.FragmentPath()); !os.IsNotExist(err) {
		t.Errorf("fragment file exists after a refused apply (stat err = %v)", err)
	}
}

func TestApplyRejectsInvalidModel(t *testing.T) {
	m, ctl := newTestModel(t)
	m.zones = []domain.Zone{{Name: "example.com.", Type: "transparent", Records: []domain.Record{
		{Name: "www", RType: "A", Value: "not-an-ip", TTL: 300},
	}}}
	m.dirty = true
	msg := m.apply()()
	if _, ok := msg.(ErrorMsg); !ok {
		t.Fatalf("apply produced %T, want ErrorMsg", msg)
	}
	if ctl.reloads != 0 {
		t.Errorf("reloads = %d, want 0 on validation failure", ctl.reloads)
	}
}

// writeForeignConf drops a foreign config file into the test's conf.d
// directory, which newTestModel's main conf glob-includes. It returns the
// symlink-resolved path, which is the Source FindConflicts reports.
func writeForeignConf(t *testing.T, m RootModel, name, content string) string {
	t.Helper()
	confDir := filepath.Join(filepath.Dir(m.cfg.MainConfPath()), "conf.d")
	p := filepath.Join(confDir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("write foreign %s: %v", name, err)
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return resolved
}

// forwardZone builds a forward-zone section with one address, enough to make it
// an active, identity-bearing section.
func forwardZone(name string) domain.Section {
	return domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
		{Key: "name", Value: fmt.Sprintf("%q", name)},
		{Key: "forward-addr", Value: "192.0.2.99"},
	}}
}

// writeSentinelFragment installs a valid fragment file with content distinct
// from the in-memory model, so a refused apply can be shown to leave the file
// byte-unchanged. The sentinel lives outside newTestModel's conf.d glob, so it
// never joins the effective include graph.
func writeSentinelFragment(t *testing.T, m RootModel) string {
	t.Helper()
	const sentinel = "server:\n  edns-buffer-size: 4096\n"
	if err := os.WriteFile(m.cfg.FragmentPath(), []byte(sentinel), 0644); err != nil {
		t.Fatalf("write sentinel fragment: %v", err)
	}
	return sentinel
}

// assertFragmentUnchanged fails unless the on-disk fragment still equals want.
func assertFragmentUnchanged(t *testing.T, m RootModel, want string) {
	t.Helper()
	got, err := os.ReadFile(m.cfg.FragmentPath())
	if err != nil {
		t.Fatalf("read fragment after refusal: %v", err)
	}
	if string(got) != want {
		t.Errorf("fragment changed on a refused apply:\n got %q\nwant %q", got, want)
	}
}

// TestApplyRefusesIncludeGraphConflict pins the apply-time hard refusal: when a
// forward-zone we own collides by identity with one already declared elsewhere
// in the include graph, apply must name the foreign source and stop before any
// write or reload.
func TestApplyRefusesIncludeGraphConflict(t *testing.T) {
	m, ctl := newTestModel(t)
	foreign := writeForeignConf(t, m, "foreign.conf",
		"forward-zone:\n  name: \"dup.\"\n  forward-addr: 192.0.2.53\n")
	sentinel := writeSentinelFragment(t, m)
	m = setConfigFragment(t, m, domain.Fragment{Sections: []domain.Section{
		{Kind: "server"},
		forwardZone("dup."),
	}})
	m.dirty = true

	msg := m.apply()()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("apply produced %T (%v), want ErrorMsg", msg, msg)
	}
	want := `forward-zone "dup." already exists in ` + foreign
	if !strings.Contains(errMsg.Error.Error(), want) {
		t.Errorf("error %q missing %q", errMsg.Error.Error(), want)
	}
	if ctl.reloads != 0 {
		t.Errorf("reloads = %d, want 0 on a refused apply", ctl.reloads)
	}
	assertFragmentUnchanged(t, m, sentinel)
}

// TestApplyListsAllIncludeGraphConflicts pins that every collision is reported
// and joined onto a single line, so none is cut off by the status line, rather
// than only the first.
func TestApplyListsAllIncludeGraphConflicts(t *testing.T) {
	m, ctl := newTestModel(t)
	foreign := writeForeignConf(t, m, "foreign.conf",
		"forward-zone:\n  name: \"dup.\"\n  forward-addr: 192.0.2.53\n"+
			"forward-zone:\n  name: \"also.\"\n  forward-addr: 192.0.2.54\n")
	sentinel := writeSentinelFragment(t, m)
	m = setConfigFragment(t, m, domain.Fragment{Sections: []domain.Section{
		{Kind: "server"},
		forwardZone("dup."),
		forwardZone("also."),
	}})
	m.dirty = true

	msg := m.apply()()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("apply produced %T (%v), want ErrorMsg", msg, msg)
	}
	if strings.Contains(errMsg.Error.Error(), "\n") {
		t.Errorf("refusal error contains a newline the status line would truncate: %q", errMsg.Error.Error())
	}
	for _, name := range []string{"dup.", "also."} {
		want := fmt.Sprintf("forward-zone %q already exists in %s", name, foreign)
		if !strings.Contains(errMsg.Error.Error(), want) {
			t.Errorf("error %q missing %q", errMsg.Error.Error(), want)
		}
	}
	if ctl.reloads != 0 {
		t.Errorf("reloads = %d, want 0 on a refused apply", ctl.reloads)
	}
	assertFragmentUnchanged(t, m, sentinel)
}

// TestApplyConflictRefusalRendersBoth pins the UI contract behind the
// single-line join: the refusal must reach the status line intact, with every
// conflict visible, instead of being cut off at an embedded newline.
func TestApplyConflictRefusalRendersBoth(t *testing.T) {
	m, _ := newTestModel(t)
	foreign := writeForeignConf(t, m, "foreign.conf",
		"forward-zone:\n  name: \"dup.\"\n  forward-addr: 192.0.2.53\n"+
			"forward-zone:\n  name: \"also.\"\n  forward-addr: 192.0.2.54\n")
	writeSentinelFragment(t, m)
	m = setConfigFragment(t, m, domain.Fragment{Sections: []domain.Section{
		{Kind: "server"},
		forwardZone("dup."),
		forwardZone("also."),
	}})
	m.dirty = true

	msg := m.apply()()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("apply produced %T (%v), want ErrorMsg", msg, msg)
	}

	root := asRoot(t, mustUpdate(t, m, errMsg))
	status := root.statusLine(400)
	if strings.Contains(status, "\n") {
		t.Errorf("status line contains an embedded newline, so it does not render on one row:\n%q", status)
	}
	for _, want := range []string{`"dup."`, `"also."`, foreign} {
		if !strings.Contains(status, want) {
			t.Errorf("status line missing %q after the refusal:\n%s", want, status)
		}
	}

	// The full screen must carry both descriptions too, with room to spare.
	root.width, root.height = 400, 40
	view := root.View()
	for _, want := range []string{`"dup."`, `"also."`, foreign} {
		if !strings.Contains(view, want) {
			t.Errorf("rendered view missing %q after the refusal:\n%s", want, view)
		}
	}
}

// TestApplyCreatesFragmentWithLiteralInclude pins the documented non-Debian
// layout: a literal include of our fragment that does not exist yet must not
// make apply refuse forever. The first apply creates it (write + reload).
func TestApplyCreatesFragmentWithLiteralInclude(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "unbound.conf")
	frag := filepath.Join(dir, "unbound-tui.conf")
	if err := os.WriteFile(conf, []byte("include: "+frag+"\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	mgr, err := config.NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.SetFragmentPath(frag)
	ctl := &fakeCtl{}
	m := NewRootModel(ctl, mgr, "test")
	m = asRoot(t, mustUpdate(t, m, m.Init()()))
	if _, err := os.Stat(frag); !os.IsNotExist(err) {
		t.Fatalf("fragment exists before first apply (stat err = %v)", err)
	}

	m = asRoot(t, mustUpdate(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com", Type: "transparent"}))
	m.dirty = true
	msg := m.apply()()
	if _, ok := msg.(AppliedMsg); !ok {
		t.Fatalf("first apply produced %T (%v), want AppliedMsg", msg, msg)
	}
	if ctl.reloads != 1 {
		t.Errorf("reloads = %d, want 1", ctl.reloads)
	}
	if _, err := os.Stat(frag); err != nil {
		t.Errorf("fragment not created on the first apply: %v", err)
	}
}

// TestApplyValidationErrorPrecedesConflict pins gate order: validateModel runs
// before the include-graph check, so an invalid model reports its validation
// error even when a foreign conflict also exists.
func TestApplyValidationErrorPrecedesConflict(t *testing.T) {
	m, _ := newTestModel(t)
	writeForeignConf(t, m, "foreign.conf",
		"forward-zone:\n  name: \"dup.\"\n  forward-addr: 192.0.2.53\n")
	sentinel := writeSentinelFragment(t, m)
	m = setConfigFragment(t, m, domain.Fragment{Sections: []domain.Section{
		{Kind: "server"},
		forwardZone("dup."),
	}})
	m.zones = []domain.Zone{{Name: "example.com.", Type: "transparent", Records: []domain.Record{
		{Name: "www", RType: "A", Value: "not-an-ip", TTL: 300},
	}}}
	m.dirty = true

	msg := m.apply()()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("apply produced %T (%v), want ErrorMsg", msg, msg)
	}
	if !strings.Contains(errMsg.Error.Error(), "invalid A address") {
		t.Errorf("error %q, want the validation error", errMsg.Error.Error())
	}
	if strings.Contains(errMsg.Error.Error(), "already exists") {
		t.Errorf("error %q reports the conflict; validation must run first", errMsg.Error.Error())
	}
	assertFragmentUnchanged(t, m, sentinel)
}

// TestApplyProceedsWhenForeignNamesDiffer pins that the refusal is name-based:
// a non-colliding foreign forward-zone must not block the apply.
func TestApplyProceedsWhenForeignNamesDiffer(t *testing.T) {
	m, ctl := newTestModel(t)
	writeForeignConf(t, m, "foreign.conf",
		"forward-zone:\n  name: \"other.\"\n  forward-addr: 192.0.2.53\n")
	m = setConfigFragment(t, m, domain.Fragment{Sections: []domain.Section{
		{Kind: "server"},
		forwardZone("dup."),
	}})
	m.dirty = true

	msg := m.apply()()
	if _, ok := msg.(AppliedMsg); !ok {
		t.Fatalf("apply produced %T (%v), want AppliedMsg", msg, msg)
	}
	if ctl.reloads != 1 {
		t.Errorf("reloads = %d, want 1", ctl.reloads)
	}
	if _, err := os.Stat(m.cfg.FragmentPath()); err != nil {
		t.Errorf("fragment not written on a clean apply: %v", err)
	}
}

// TestApplyRefusesScalarConflict pins the apply-time scalar hard refusal: when
// a singleton option we set actively is also set by a foreign server section
// elsewhere in the include graph, apply must name the key, the foreign source
// and the manual-resolution hint, and stop before any write or reload.
func TestApplyRefusesScalarConflict(t *testing.T) {
	m, ctl := newTestModel(t)
	foreign := writeForeignConf(t, m, "foreign.conf", "server:\n  verbosity: 5\n")
	sentinel := writeSentinelFragment(t, m)
	m = setConfigFragment(t, m, domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "verbosity", Value: "2"}}},
	}})
	m.dirty = true

	msg := m.apply()()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("apply produced %T (%v), want ErrorMsg", msg, msg)
	}
	for _, want := range []string{"verbosity", foreign, "deploy.md: Conflicts and manual resolution"} {
		if !strings.Contains(errMsg.Error.Error(), want) {
			t.Errorf("error %q missing %q", errMsg.Error.Error(), want)
		}
	}
	if ctl.reloads != 0 {
		t.Errorf("reloads = %d, want 0 on a refused apply", ctl.reloads)
	}
	assertFragmentUnchanged(t, m, sentinel)
}

// TestApplyListsNamedThenScalarConflicts pins that when both a named-section
// collision and a scalar option collision exist, the refusal is a single line
// listing every named conflict first, then every scalar conflict, so nothing
// is cut off by the status line.
func TestApplyListsNamedThenScalarConflicts(t *testing.T) {
	m, ctl := newTestModel(t)
	foreign := writeForeignConf(t, m, "foreign.conf",
		"forward-zone:\n  name: \"dup.\"\n  forward-addr: 192.0.2.53\n"+
			"server:\n  verbosity: 5\n")
	sentinel := writeSentinelFragment(t, m)
	m = setConfigFragment(t, m, domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "verbosity", Value: "2"}}},
		forwardZone("dup."),
	}})
	m.dirty = true

	msg := m.apply()()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("apply produced %T (%v), want ErrorMsg", msg, msg)
	}
	if strings.Contains(errMsg.Error.Error(), "\n") {
		t.Errorf("refusal error contains a newline the status line would truncate: %q", errMsg.Error.Error())
	}
	named := fmt.Sprintf("forward-zone %q already exists in %s", "dup.", foreign)
	scalar := fmt.Sprintf("server: verbosity already set in %s — edit that file manually (see deploy.md: Conflicts and manual resolution)", foreign)
	i, j := strings.Index(errMsg.Error.Error(), named), strings.Index(errMsg.Error.Error(), scalar)
	if i < 0 {
		t.Fatalf("error %q missing the named conflict %q", errMsg.Error.Error(), named)
	}
	if j < 0 {
		t.Fatalf("error %q missing the scalar conflict %q", errMsg.Error.Error(), scalar)
	}
	if j < i {
		t.Errorf("scalar conflict listed before the named conflict: %q", errMsg.Error.Error())
	}
	if ctl.reloads != 0 {
		t.Errorf("reloads = %d, want 0 on a refused apply", ctl.reloads)
	}
	assertFragmentUnchanged(t, m, sentinel)
}

// TestApplyScalarRefusalRendersBoth pins the UI contract behind the single-line
// join: the scalar refusal must reach the status line intact, naming the key,
// the foreign source and the manual-resolution hint.
func TestApplyScalarRefusalRendersBoth(t *testing.T) {
	m, _ := newTestModel(t)
	foreign := writeForeignConf(t, m, "foreign.conf", "server:\n  verbosity: 5\n")
	writeSentinelFragment(t, m)
	m = setConfigFragment(t, m, domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "verbosity", Value: "2"}}},
	}})
	m.dirty = true

	msg := m.apply()()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("apply produced %T (%v), want ErrorMsg", msg, msg)
	}

	root := asRoot(t, mustUpdate(t, m, errMsg))
	status := root.statusLine(400)
	if strings.Contains(status, "\n") {
		t.Errorf("status line contains an embedded newline, so it does not render on one row:\n%q", status)
	}
	for _, want := range []string{"verbosity", foreign, "deploy.md: Conflicts and manual resolution"} {
		if !strings.Contains(status, want) {
			t.Errorf("status line missing %q after the refusal:\n%s", want, status)
		}
	}
}

// TestApplyProceedsWhenForeignScalarDiffers pins that the scalar refusal is
// key-based: a foreign server section setting a different singleton must not
// block the apply.
func TestApplyProceedsWhenForeignScalarDiffers(t *testing.T) {
	m, ctl := newTestModel(t)
	writeForeignConf(t, m, "foreign.conf", "server:\n  username: \"other\"\n")
	m = setConfigFragment(t, m, domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "verbosity", Value: "2"}}},
	}})
	m.dirty = true

	msg := m.apply()()
	if _, ok := msg.(AppliedMsg); !ok {
		t.Fatalf("apply produced %T (%v), want AppliedMsg", msg, msg)
	}
	if ctl.reloads != 1 {
		t.Errorf("reloads = %d, want 1", ctl.reloads)
	}
	if _, err := os.Stat(m.cfg.FragmentPath()); err != nil {
		t.Errorf("fragment not written on a clean apply: %v", err)
	}
}

// TestApplyRefusesWhenIncludeGraphUnreadable pins that a broken main config is
// surfaced rather than silently written over: no write and no reload.
func TestApplyRefusesWhenIncludeGraphUnreadable(t *testing.T) {
	m, ctl := newTestModel(t)
	sentinel := writeSentinelFragment(t, m)
	if err := os.Remove(m.cfg.MainConfPath()); err != nil {
		t.Fatalf("remove main conf: %v", err)
	}
	m.frag = domain.Fragment{Sections: []domain.Section{{Kind: "server"}}}
	m.dirty = true

	msg := m.apply()()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("apply produced %T (%v), want ErrorMsg", msg, msg)
	}
	if !strings.Contains(errMsg.Error.Error(), "read config") {
		t.Errorf("error %q does not name the read failure", errMsg.Error.Error())
	}
	if ctl.reloads != 0 {
		t.Errorf("reloads = %d, want 0 when the include graph is unreadable", ctl.reloads)
	}
	assertFragmentUnchanged(t, m, sentinel)
}

func TestQuitWithDirtyConfirms(t *testing.T) {
	m, _ := newTestModel(t)
	m.dirty = true
	next := asRoot(t, mustUpdate(t, m, key("q")))
	if next.state != StateConfirm || next.confirmKind != "quit" {
		t.Errorf("state/kind = %v/%q, want StateConfirm/quit", next.state, next.confirmKind)
	}
}

func TestToggleZoneDisabledEnablesRecords(t *testing.T) {
	m, _ := newTestModel(t)
	m.zones = []domain.Zone{{
		Name: "example.com.", Type: "transparent", Disabled: true,
		Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300, Disabled: true}},
	}}
	m.zoneFocused = true
	m.toggleDisabled() // disable -> enable
	if m.zones[0].Disabled {
		t.Error("zone still disabled after toggle")
	}
	if m.zones[0].Records[0].Disabled {
		t.Error("record still disabled after enabling its zone")
	}
	if got := localEntries(m.frag); len(got) != 2 || got[0].Disabled || got[1].Disabled {
		t.Errorf("frag local entries = %+v, want both re-enabled", got)
	}
}

func TestToggleRecordDisabled(t *testing.T) {
	m, _ := newTestModel(t)
	m.zones = []domain.Zone{{
		Name: "example.com.", Type: "transparent",
		Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}},
	}}
	m.zoneFocused = false
	m.toggleDisabled()
	if !m.zones[0].Records[0].Disabled {
		t.Error("record not disabled after toggle")
	}
	if got := localEntries(m.frag); len(got) != 2 || !got[1].Disabled {
		t.Errorf("frag local entries = %+v, want the record disabled", got)
	}
}

func TestForeignEntries(t *testing.T) {
	ours := []domain.Zone{{
		Name: "mine.example.", Type: "transparent",
		Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}},
	}}
	runtimeZones := []domain.LocalZone{
		{Name: "mine.example.", Type: "transparent"},
		{Name: "theirs.example.", Type: "static"},
	}
	runtimeRRs := []string{
		"www.mine.example. 300 IN A 192.0.2.1",
		"x.theirs.example. 300 IN A 192.0.2.9",
	}
	fz, fr := foreignEntries(ours, runtimeZones, runtimeRRs)
	if len(fz) != 1 || fz[0].Name != "theirs.example." {
		t.Errorf("foreign zones = %+v, want theirs.example.", fz)
	}
	if len(fr) != 1 || fr[0] != "x.theirs.example. 300 IN A 192.0.2.9" {
		t.Errorf("foreign rrs = %+v, want theirs only", fr)
	}
}

func TestForeignEntriesIgnoresOurWhitespace(t *testing.T) {
	ours := []domain.Zone{{
		Name: "mine.example.", Type: "transparent",
		Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}},
	}}
	_, fr := foreignEntries(ours, nil, []string{"www.mine.example.  300  IN  A 192.0.2.1"})
	if len(fr) != 0 {
		t.Errorf("foreign rrs = %+v, want none (spacing tolerated)", fr)
	}
}

func TestViewRendersZonesAndRecords(t *testing.T) {
	m, _ := newTestModel(t)
	m.zones = []domain.Zone{{
		Name: "example.com.", Type: "transparent",
		Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}},
	}}
	m.zoneFocused = false
	m.dirty = true
	view := m.View()
	for _, want := range []string{"example.com.", "www", "192.0.2.1", "UNSAVED"} {
		if !strings.Contains(view, want) {
			t.Errorf("View missing %q:\n%s", want, view)
		}
	}
}

func TestDeleteRecordAndZoneFlow(t *testing.T) {
	m, _ := newTestModel(t)
	m.zones = []domain.Zone{
		{Name: "a.example.", Type: "transparent", Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}}},
		{Name: "b.example.", Type: "transparent"},
	}

	// Delete the record: record pane focused.
	m.zoneFocused = false
	next := asRoot(t, mustUpdate(t, m, key("d")))
	if next.state != StateConfirm || next.confirmKind != "record" {
		t.Fatalf("state/kind = %v/%q, want StateConfirm/record", next.state, next.confirmKind)
	}
	next = asRoot(t, mustUpdate(t, next, key("y")))
	if len(next.zones[0].Records) != 0 || !next.dirty {
		t.Errorf("record not deleted or not dirty: %+v dirty=%v", next.zones[0], next.dirty)
	}
	if got := localEntries(next.frag); len(got) != 2 {
		t.Errorf("frag local entries after record delete = %+v, want only the two zones", got)
	}

	// Delete a zone: zone pane focused.
	next.zoneFocused = true
	next = asRoot(t, mustUpdate(t, next, key("D")))
	if next.confirmKind != "zone" {
		t.Fatalf("confirmKind = %q, want zone", next.confirmKind)
	}
	next = asRoot(t, mustUpdate(t, next, key("y")))
	if len(next.zones) != 1 || next.zones[0].Name != "b.example." {
		t.Errorf("zones after delete = %+v, want only b.example.", next.zones)
	}
	if got := localEntries(next.frag); len(got) != 1 || got[0].Value != `"b.example." transparent` {
		t.Errorf("frag local entries after zone delete = %+v, want only b.example.", got)
	}
}

func TestConfirmCancel(t *testing.T) {
	m, _ := newTestModel(t)
	m.zones = []domain.Zone{{Name: "a.example.", Type: "transparent"}}
	m.zoneFocused = true
	next := asRoot(t, mustUpdate(t, m, key("D")))
	next = asRoot(t, mustUpdate(t, next, key("n")))
	if len(next.zones) != 1 || next.state != StateReady {
		t.Errorf("cancel changed state: zones=%+v state=%v", next.zones, next.state)
	}
}

func TestMoveCursors(t *testing.T) {
	m, _ := newTestModel(t)
	m.zones = []domain.Zone{
		{Name: "a.example.", Type: "transparent", Records: []domain.Record{{Name: "x", RType: "A", Value: "192.0.2.1", TTL: 300}, {Name: "y", RType: "A", Value: "192.0.2.2", TTL: 300}}},
		{Name: "b.example.", Type: "transparent"},
	}
	m.zoneFocused = true
	next := asRoot(t, mustUpdate(t, m, key("down")))
	if next.zoneCursor != 1 {
		t.Errorf("zoneCursor = %d, want 1", next.zoneCursor)
	}
	next.zoneFocused = false
	next.zoneCursor = 0
	next = asRoot(t, mustUpdate(t, next, key("down")))
	if next.recCursor != 1 {
		t.Errorf("recCursor = %d, want 1", next.recCursor)
	}
}

func TestAddDuplicateZoneRejected(t *testing.T) {
	m, _ := newTestModel(t)
	m = asRoot(t, mustUpdate(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com", Type: "transparent"}))
	next := asRoot(t, mustUpdate(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com.", Type: "static"}))
	if next.state != StateError {
		t.Fatalf("state = %v, want StateError for a duplicate zone", next.state)
	}
	if len(next.zones) != 1 {
		t.Errorf("zones = %+v, want the original only", next.zones)
	}
}

// TestMutationsRegenerateFragment checks that add-record, edit-ttl and
// set-type all fold into the stored fragment immediately.
func TestMutationsRegenerateFragment(t *testing.T) {
	m, _ := newTestModel(t)

	m = asRoot(t, mustUpdate(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com", Type: "transparent"}))
	m = asRoot(t, mustUpdate(t, m, FormSubmitMsg{Mode: FormAddRecord, ZoneIndex: 0, Name: "www", Type: "A", Value: "192.0.2.1", TTL: 300}))
	got := localEntries(m.frag)
	if len(got) != 2 || got[1].Key != "local-data" || got[1].Value != `"www.example.com. 300 IN A 192.0.2.1"` {
		t.Fatalf("frag after add-record = %+v, want the local-data entry", got)
	}

	m = asRoot(t, mustUpdate(t, m, FormSubmitMsg{Mode: FormEditTTL, ZoneIndex: 0, RecIndex: 0, TTL: 60}))
	if got := localEntries(m.frag); got[1].Value != `"www.example.com. 60 IN A 192.0.2.1"` {
		t.Errorf("frag after edit-ttl = %+v, want TTL 60", got)
	}

	m = asRoot(t, mustUpdate(t, m, FormSubmitMsg{Mode: FormSetType, ZoneIndex: 0, Type: "static"}))
	if got := localEntries(m.frag); got[0].Value != `"example.com." static` {
		t.Errorf("frag after set-type = %+v, want static", got)
	}
}

func TestAddDuplicateRecordRejected(t *testing.T) {
	m, _ := newTestModel(t)
	m.zones = []domain.Zone{{
		Name: "example.com.", Type: "transparent",
		Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}},
	}}
	// Same record, differing only in name case and record type case.
	next := asRoot(t, mustUpdate(t, m, FormSubmitMsg{
		Mode: FormAddRecord, ZoneIndex: 0, Name: "WWW", Type: "a", Value: "192.0.2.1", TTL: 60,
	}))
	if next.state != StateError {
		t.Fatalf("state = %v, want StateError for a duplicate record", next.state)
	}
	if len(next.zones[0].Records) != 1 {
		t.Errorf("records = %+v, want the original only", next.zones[0].Records)
	}
}

func TestValidateModelRejectsDuplicates(t *testing.T) {
	dupZone := []domain.Zone{
		{Name: "example.com.", Type: "transparent"},
		{Name: "Example.com", Type: "static"},
	}
	if err := validateModel(dupZone); err == nil {
		t.Error("validateModel = nil for duplicate zones, want error")
	}

	dupRec := []domain.Zone{{
		Name: "example.com.", Type: "transparent",
		Records: []domain.Record{
			{Name: "@", RType: "A", Value: "192.0.2.1", TTL: 300},
			{Name: "", RType: "A", Value: "192.0.2.1", TTL: 60},
		},
	}}
	if err := validateModel(dupRec); err == nil {
		t.Error("validateModel = nil for apex duplicates, want error")
	}
}

func TestFocusGuards(t *testing.T) {
	m, _ := newTestModel(t)
	m.zones = []domain.Zone{{
		Name: "example.com.", Type: "transparent",
		Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}},
	}}

	m.zoneFocused = true
	if next := asRoot(t, mustUpdate(t, m, key("t"))); next.state != StateForm {
		t.Error("'t' in the zones pane should open the type form")
	}
	if next := asRoot(t, mustUpdate(t, m, key("e"))); next.state == StateForm {
		t.Error("'e' in the zones pane should be a no-op")
	}

	m.zoneFocused = false
	if next := asRoot(t, mustUpdate(t, m, key("e"))); next.state != StateForm {
		t.Error("'e' in the records pane should open the TTL form")
	}
	if next := asRoot(t, mustUpdate(t, m, key("D"))); next.confirmKind == "zone" {
		t.Error("'D' in the records pane should be a no-op")
	}
}

func TestErrorRecovery(t *testing.T) {
	m, _ := newTestModel(t)
	next := asRoot(t, mustUpdate(t, m, ErrorMsg{Error: fmt.Errorf("boom")}))
	if next.state != StateError {
		t.Fatalf("state = %v, want StateError", next.state)
	}
	next = asRoot(t, mustUpdate(t, next, key("x")))
	if next.state != StateReady || next.lastError != nil {
		t.Errorf("state/error = %v/%v, want StateReady/nil", next.state, next.lastError)
	}
}

func TestToggleDisabledOnEmptyModel(t *testing.T) {
	m, _ := newTestModel(t)
	m.zoneFocused = true
	m.toggleDisabled() // must not panic
	if m.dirty {
		t.Error("dirty = true after a no-op toggle")
	}
}

// TestConfigCrossViewZoneAddVisibleInConfig pins Review Focus #4: a zone added
// in the Local data view lands in frag and is immediately visible as a
// local-zone row in the Config view, which projects the same fragment.
func TestConfigCrossViewZoneAddVisibleInConfig(t *testing.T) {
	m, _ := newTestModel(t)
	m = asRoot(t, mustUpdate(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com", Type: "transparent"}))
	m.view = ViewConfig
	m.clampCfgCursors()

	if !hasEntry(m.frag, "local-zone", `"example.com." transparent`) {
		t.Fatalf("frag = %+v, want the added local-zone entry", m.frag)
	}
	if v := m.View(); !strings.Contains(v, `local-zone: "example.com." transparent`) {
		t.Errorf("Config view missing the new local-zone row:\n%s", v)
	}
}

// TestInitLoadsScalarIndex pins that the startup snapshot carries the scalar
// warning index built from the same effective read that feeds the upstreams
// snapshot, and that the ZonesLoadedMsg handler stores it.
func TestInitLoadsScalarIndex(t *testing.T) {
	m, _ := newTestModel(t)
	if err := os.WriteFile(m.cfg.FragmentPath(), []byte("server:\n  verbosity: 3\n"), 0644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	foreign := filepath.Join(filepath.Dir(m.cfg.FragmentPath()), "zz-foreign.conf")
	if err := os.WriteFile(foreign, []byte("server:\n  verbosity: 1\n"), 0644); err != nil {
		t.Fatalf("write foreign: %v", err)
	}
	main := m.cfg.MainConfPath()
	// Includes are resolved relative to the declaring file's directory; keep
	// them relative so this test does not depend on absolute-include support.
	if err := os.WriteFile(main, []byte("include: frag.conf\ninclude: zz-foreign.conf\n"), 0644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(foreign)
	if err != nil {
		resolved = foreign
	}

	loaded, ok := m.Init()().(ZonesLoadedMsg)
	if !ok {
		t.Fatalf("Init produced %T, want ZonesLoadedMsg", m.Init()())
	}
	got := loaded.ScalarIdx[[2]string{"server", "verbosity"}]
	if len(got) != 1 || got[0] != resolved {
		t.Fatalf("Init ScalarIdx[server,verbosity] = %+v, want [%s]", got, resolved)
	}
	m = asRoot(t, mustUpdate(t, m, loaded))
	if got := m.scalarIdx[[2]string{"server", "verbosity"}]; len(got) != 1 || got[0] != resolved {
		t.Errorf("m.scalarIdx = %+v, want the Init snapshot via [%s]", m.scalarIdx, resolved)
	}
}

// TestInitWarnsOnEmptyNameLocalZone pins the one-time load warning: an
// empty-name local-zone entry is skipped by the projection (so only the
// normal zones load) but still sets a notice and stays in the fragment.
func TestInitWarnsOnEmptyNameLocalZone(t *testing.T) {
	m, _ := newTestModel(t)
	src := "server:\n" +
		"local-zone: \"\"\n" +
		"local-zone: \"example.com.\" static\n"
	if err := os.WriteFile(m.cfg.FragmentPath(), []byte(src), 0644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}

	loaded := m.Init()().(ZonesLoadedMsg)
	m = asRoot(t, mustUpdate(t, m, loaded))
	if len(m.zones) != 1 || m.zones[0].Name != "example.com." {
		t.Errorf("zones = %+v, want only example.com.", m.zones)
	}
	if got := strings.Count(m.notice, "empty-name local-zone entry ignored"); got != 1 {
		t.Errorf("notice = %q, want exactly one empty-name warning", m.notice)
	}
	if !hasEntry(m.frag, "local-zone", `""`) {
		t.Errorf("frag = %+v, want the raw empty-name entry retained", m.frag)
	}
	if out := string(config.SerializeFragment(m.frag)); !strings.Contains(out, `local-zone: ""`) {
		t.Errorf("serialized fragment lost the raw empty-name entry:\n%s", out)
	}
}

// TestInitNoWarnOnCleanFragment pins that a fragment without an empty-name
// local-zone entry sets no notice.
func TestInitNoWarnOnCleanFragment(t *testing.T) {
	m, _ := newTestModel(t)
	src := "server:\n" +
		"local-zone: \"example.com.\" static\n"
	if err := os.WriteFile(m.cfg.FragmentPath(), []byte(src), 0644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}

	loaded := m.Init()().(ZonesLoadedMsg)
	m = asRoot(t, mustUpdate(t, m, loaded))
	if m.notice != "" {
		t.Errorf("notice = %q, want empty on a clean fragment", m.notice)
	}
}

func TestCloneZonesIsIndependent(t *testing.T) {
	orig := []domain.Zone{{
		Name: "example.com.", Type: "transparent",
		Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}},
	}}
	clone := cloneZones(orig)
	clone[0].Records[0].TTL = 60
	clone[0].Name = "changed."
	if orig[0].Records[0].TTL != 300 || orig[0].Name != "example.com." {
		t.Errorf("clone mutated the original: %+v", orig)
	}
}

// TestZonesViewNavigation drives every movement key in the Local data view and
// pins cursor clamping at both ends, for the zone pane and the record pane. A
// short terminal (height 8) makes pageSize 2, so the ctrl+d/ctrl+u jumps are
// observable without a tall fixture.
func TestZonesViewNavigation(t *testing.T) {
	m, _ := newTestModel(t)
	m.zones = []domain.Zone{
		{Name: "a.example.", Type: "transparent", Records: []domain.Record{
			{Name: "r1", RType: "A", Value: "192.0.2.1", TTL: 300},
			{Name: "r2", RType: "A", Value: "192.0.2.2", TTL: 300},
			{Name: "r3", RType: "A", Value: "192.0.2.3", TTL: 300},
		}},
		{Name: "b.example.", Type: "transparent"},
		{Name: "c.example.", Type: "transparent"},
	}
	m.width, m.height = 80, 8
	if got := m.pageSize(); got != 2 {
		t.Fatalf("pageSize = %d, want 2 for the fixture height", got)
	}

	// Zone pane (starts focused).
	m.zoneFocused = true
	m = asRoot(t, mustUpdate(t, m, key("k"))) // moveUp clamps at the top
	if m.zoneCursor != 0 {
		t.Fatalf("k at top: zoneCursor = %d, want 0", m.zoneCursor)
	}
	m = asRoot(t, mustUpdate(t, m, key("j"))) // moveDown
	if m.zoneCursor != 1 || m.recCursor != 0 {
		t.Fatalf("j: zoneCursor/recCursor = %d/%d, want 1/0", m.zoneCursor, m.recCursor)
	}
	m = asRoot(t, mustUpdate(t, m, key("k"))) // moveUp back off the second zone
	if m.zoneCursor != 0 {
		t.Fatalf("k: zoneCursor = %d, want 0", m.zoneCursor)
	}
	m = asRoot(t, mustUpdate(t, m, key("j")))
	m = asRoot(t, mustUpdate(t, m, key("j"))) // moveDown clamps at the bottom
	if m.zoneCursor != 2 {
		t.Fatalf("j at bottom: zoneCursor = %d, want 2", m.zoneCursor)
	}
	m = asRoot(t, mustUpdate(t, m, key("g"))) // moveTop
	if m.zoneCursor != 0 {
		t.Fatalf("g: zoneCursor = %d, want 0", m.zoneCursor)
	}
	m = asRoot(t, mustUpdate(t, m, key("G"))) // moveBottom
	if m.zoneCursor != 2 {
		t.Fatalf("G: zoneCursor = %d, want 2", m.zoneCursor)
	}
	m = asRoot(t, mustUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})) // movePageUp
	if m.zoneCursor != 0 {
		t.Fatalf("ctrl+u: zoneCursor = %d, want 0", m.zoneCursor)
	}
	m = asRoot(t, mustUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})) // clamps at the top
	if m.zoneCursor != 0 {
		t.Fatalf("ctrl+u at top: zoneCursor = %d, want 0", m.zoneCursor)
	}
	m = asRoot(t, mustUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})) // movePageDown
	if m.zoneCursor != 2 {
		t.Fatalf("ctrl+d: zoneCursor = %d, want 2", m.zoneCursor)
	}
	m = asRoot(t, mustUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})) // clamps at the bottom
	if m.zoneCursor != 2 {
		t.Fatalf("ctrl+d at bottom: zoneCursor = %d, want 2", m.zoneCursor)
	}

	// Record pane: tab moves focus off the zone pane.
	m = asRoot(t, mustUpdate(t, m, key("tab")))
	if m.zoneFocused {
		t.Fatal("tab did not move focus to the record pane")
	}
	m.zoneCursor = 0
	m = asRoot(t, mustUpdate(t, m, key("j"))) // moveDown
	if m.recCursor != 1 {
		t.Fatalf("record j: recCursor = %d, want 1", m.recCursor)
	}
	m = asRoot(t, mustUpdate(t, m, key("j")))
	m = asRoot(t, mustUpdate(t, m, key("j"))) // moveDown clamps at the last record
	if m.recCursor != 2 {
		t.Fatalf("record j at bottom: recCursor = %d, want 2", m.recCursor)
	}
	m = asRoot(t, mustUpdate(t, m, key("k"))) // moveUp
	if m.recCursor != 1 {
		t.Fatalf("record k: recCursor = %d, want 1", m.recCursor)
	}
	m = asRoot(t, mustUpdate(t, m, key("k")))
	m = asRoot(t, mustUpdate(t, m, key("k"))) // moveUp clamps at the top
	if m.recCursor != 0 {
		t.Fatalf("record k at top: recCursor = %d, want 0", m.recCursor)
	}
	m = asRoot(t, mustUpdate(t, m, key("G"))) // moveBottom
	if m.recCursor != 2 {
		t.Fatalf("record G: recCursor = %d, want 2", m.recCursor)
	}
	m = asRoot(t, mustUpdate(t, m, key("g"))) // moveTop
	if m.recCursor != 0 {
		t.Fatalf("record g: recCursor = %d, want 0", m.recCursor)
	}
	m = asRoot(t, mustUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})) // movePageDown
	if m.recCursor != 2 {
		t.Fatalf("record ctrl+d: recCursor = %d, want 2", m.recCursor)
	}
	m = asRoot(t, mustUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})) // clamps at the last record
	if m.recCursor != 2 {
		t.Fatalf("record ctrl+d at bottom: recCursor = %d, want 2", m.recCursor)
	}
	m = asRoot(t, mustUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})) // movePageUp
	if m.recCursor != 0 {
		t.Fatalf("record ctrl+u: recCursor = %d, want 0", m.recCursor)
	}
	m = asRoot(t, mustUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})) // clamps at the top
	if m.recCursor != 0 {
		t.Fatalf("record ctrl+u at top: recCursor = %d, want 0", m.recCursor)
	}
}
