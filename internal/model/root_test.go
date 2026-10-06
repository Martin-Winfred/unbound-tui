package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/config"
	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

type fakeCtl struct {
	reloads  int
	zones    []domain.LocalZone
	rrs      []string
	zonesErr error
	rrsErr   error
}

func (f *fakeCtl) Reload() error                      { f.reloads++; return nil }
func (f *fakeCtl) Status() (domain.StatusInfo, error) { return domain.StatusInfo{}, nil }
func (f *fakeCtl) ListLocalZones() ([]domain.LocalZone, error) {
	return f.zones, f.zonesErr
}
func (f *fakeCtl) ListLocalData() ([]string, error) { return f.rrs, f.rrsErr }

var _ domain.Controller = (*fakeCtl)(nil)

func newTestModel(t *testing.T) (RootModel, *fakeCtl) {
	t.Helper()
	dir := t.TempDir()
	confDir := filepath.Join(dir, "conf.d")
	if err := os.MkdirAll(confDir, 0755); err != nil {
		t.Fatalf("mkdir conf.d: %v", err)
	}
	conf := filepath.Join(dir, "unbound.conf")
	frag := filepath.Join(dir, "frag.conf")
	// Mirror Debian's stub (a conf.d glob that keeps the main config from
	// glob-including itself) plus a literal include of our fragment, which
	// lives outside that glob. The literal include is what apply's include
	// gate verifies; a missing fragment is tolerated (it is created on the
	// first apply), so this is safe before the first write.
	if err := os.WriteFile(conf, []byte(
		"include-toplevel: "+filepath.Join(confDir, "*.conf")+"\n"+
			"include: "+frag+"\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	mgr, err := config.NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.SetFragmentPath(frag)
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
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
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

// TestProjectionFailureBlocksLocalEdits pins the data-integrity guard: when
// projection fails on a malformed local-* entry, a later Local-data edit must
// not run regen from the empty projection (which would strip every existing
// local-zone/local-data) and apply must refuse before writing.
func TestProjectionFailureBlocksLocalEdits(t *testing.T) {
	m, ctl := newTestModel(t)
	src := "server:\n" +
		"local-zone: \"example.com.\" static\n" +
		"local-data: \"example.com. 300 IN A 192.0.2.1\"\n" +
		"local-data: \"broken\n"
	if err := os.WriteFile(m.cfg.FragmentPath(), []byte(src), 0644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	before, err := os.ReadFile(m.cfg.FragmentPath())
	if err != nil {
		t.Fatalf("read fragment before: %v", err)
	}

	// 1. The malformed entry surfaces loudly through the normal error path.
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
		t.Fatalf("state/error = %v/%v, want StateError and a non-nil error", root.state, root.lastError)
	}
	if len(root.zones) != 0 {
		t.Fatalf("zones = %+v, want empty after the projection failure", root.zones)
	}

	// Dismissing the error leaves the remaining model intact.
	root = asRoot(t, mustUpdate(t, root, key("x")))
	if root.state != StateReady {
		t.Fatalf("state = %v, want StateReady after dismissing the error", root.state)
	}

	// 2/3. The Local-data edit is refused: no regen from the empty projection,
	// no fragment mutation, and an actionable notice.
	root = submitInFormState(t, root, FormSubmitMsg{Mode: FormAddZone, Name: "new.example", Type: "transparent"})
	if len(root.zones) != 0 {
		t.Errorf("zones = %+v, want empty: the edit must not rebuild from the failed projection", root.zones)
	}
	if !hasEntry(root.frag, "local-zone", `"example.com." static`) {
		t.Errorf("frag lost the original local-zone: %+v", root.frag)
	}
	if !hasEntry(root.frag, "local-data", `"example.com. 300 IN A 192.0.2.1"`) {
		t.Errorf("frag lost the original local-data: %+v", root.frag)
	}
	if hasEntry(root.frag, "local-zone", `"new.example." transparent`) {
		t.Errorf("frag gained the refused zone: %+v", root.frag)
	}
	if !strings.Contains(root.notice, "local-*") {
		t.Errorf("notice = %q, want an actionable malformed-local-* hint", root.notice)
	}

	// 4. Apply refuses with an ErrorMsg and leaves the file byte-unchanged.
	msg := root.apply()()
	if _, ok := msg.(AppliedMsg); ok {
		t.Fatal("apply produced AppliedMsg, want an ErrorMsg while projection failed")
	}
	errMsg, ok = msg.(ErrorMsg)
	if !ok {
		t.Fatalf("apply produced %T (%v), want ErrorMsg", msg, msg)
	}
	if !strings.Contains(errMsg.Error.Error(), "local data") {
		t.Errorf("apply error %q does not name the missing local data", errMsg.Error.Error())
	}
	if ctl.reloads != 0 {
		t.Errorf("reloads = %d, want 0 on a refused apply", ctl.reloads)
	}
	after, err := os.ReadFile(m.cfg.FragmentPath())
	if err != nil {
		t.Fatalf("read fragment after: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("fragment changed on a refused apply:\n got %q\nwant %q", after, before)
	}
}

// TestZonesValidLifecycle pins the projection flag lifecycle: a successful
// projection sets it, a failed projection clears it, and a later successful
// re-projection adopted by a Config-view submission restores it.
func TestZonesValidLifecycle(t *testing.T) {
	m, _ := newTestModel(t) // Init projects an empty fragment successfully
	if !m.zonesValid {
		t.Fatal("zonesValid = false after a successful Init projection, want true")
	}

	// A failed projection clears the flag.
	m.frag = domain.Fragment{Sections: []domain.Section{{Kind: "server", Entries: []domain.Entry{
		{Key: "local-data", Value: `"unterminated`},
	}}}}
	root := asRoot(t, mustUpdate(t, m, ZonesLoadedMsg{Fragment: m.frag}))
	if root.zonesValid {
		t.Error("zonesValid = true after a failed projection, want false")
	}

	// A successful re-projection adopted by a Config-view submission sets it
	// again: setConfigFragment installs the valid fragment and re-projects,
	// then the form submit takes the same adoption path in the live update.
	root = setConfigFragment(t, root, domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "local-zone", Value: `"example.com." static`}}},
	}})
	root = submitInFormState(t, root, ConfigFormSubmitMsg{Mode: FormAddSection, Kind: "server"})
	if !root.zonesValid {
		t.Error("zonesValid = false after a successful re-projection, want true")
	}
	if len(root.zones) != 1 || root.zones[0].Name != "example.com." {
		t.Errorf("zones = %+v, want the re-projected example.com. zone", root.zones)
	}
}

func mustUpdate(t *testing.T, m RootModel, msg tea.Msg) tea.Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next
}

func TestAddZoneThenApplyWritesAndReloads(t *testing.T) {
	m, ctl := newTestModel(t)

	m = submitInFormState(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com", Type: "transparent"})
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
// byte-unchanged. The sentinel is our own fragment, which conflict detection
// excludes by path, so it never self-conflicts even though the main config
// includes it.
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

	m = submitInFormState(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com", Type: "transparent"})
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

// TestApplyRefusesWhenFragmentNotIncluded pins the include gate: when the main
// config does not cover our fragment, apply must stop before writing or
// reloading, name the missing include, and leave the fragment file
// byte-identical. Without the gate the UI would report "changes applied" while
// Unbound reloaded the old configuration.
func TestApplyRefusesWhenFragmentNotIncluded(t *testing.T) {
	m, ctl := newTestModel(t)
	sentinel := writeSentinelFragment(t, m)
	// A main config that is readable and conflict-free but never names our
	// fragment.
	if err := os.WriteFile(m.cfg.MainConfPath(),
		[]byte("server:\n  verbosity: 1\n"), 0644); err != nil {
		t.Fatalf("write main conf: %v", err)
	}
	m.frag = domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "edns-buffer-size", Value: "1232"}}},
	}}
	m.dirty = true

	msg := m.apply()()
	errMsg, ok := msg.(ErrorMsg)
	if !ok {
		t.Fatalf("apply produced %T (%v), want ErrorMsg", msg, msg)
	}
	for _, want := range []string{"apply blocked", config.ErrMissingInclude, m.cfg.FragmentPath()} {
		if !strings.Contains(errMsg.Error.Error(), want) {
			t.Errorf("error %q missing %q", errMsg.Error.Error(), want)
		}
	}
	// The status bar renders one row and does not split newlines, so the
	// apply-blocked message must be flattened to a single line while still
	// wrapping the underlying ConfigError for errors.As callers.
	if strings.Contains(errMsg.Error.Error(), "\n") {
		t.Errorf("apply-blocked error must be one line, got %q", errMsg.Error.Error())
	}
	var ce *config.ConfigError
	if !errors.As(errMsg.Error, &ce) || ce.Type != config.ErrMissingInclude {
		t.Errorf("apply-blocked error %v must wrap ConfigError %s", errMsg.Error, config.ErrMissingInclude)
	}
	if ctl.reloads != 0 {
		t.Errorf("reloads = %d, want 0 on a refused apply", ctl.reloads)
	}
	assertFragmentUnchanged(t, m, sentinel)
}

// TestApplyProceedsWhenFragmentIncluded pins the other side of the gate: a main
// config that includes our fragment leaves apply unchanged (write + reload).
func TestApplyProceedsWhenFragmentIncluded(t *testing.T) {
	m, ctl := newTestModel(t)
	m.frag = domain.Fragment{Sections: []domain.Section{{Kind: "server"}}}
	m.dirty = true

	msg := m.apply()()
	if _, ok := msg.(AppliedMsg); !ok {
		t.Fatalf("apply produced %T (%v), want AppliedMsg", msg, msg)
	}
	if ctl.reloads != 1 {
		t.Errorf("reloads = %d, want 1", ctl.reloads)
	}
}

// TestApplyCapturesDeepCopyOfFragment pins finding M8: apply must capture a deep
// copy of the fragment before the background command runs, so a later UI edit
// that mutates a section/entry element in place cannot change what the command
// validates or writes. The injected mutation below must not leak into the
// written file.
func TestApplyCapturesDeepCopyOfFragment(t *testing.T) {
	m, ctl := newTestModel(t)
	m.frag = domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{
			{Key: "local-zone", Value: `"example.com." transparent`},
			{Key: "edns-buffer-size", Value: "1232"},
		}},
	}}
	m.zones = []domain.Zone{{Name: "example.com.", Type: "transparent"}}
	m.zonesValid = true

	cmd := m.apply()
	if cmd == nil {
		t.Fatal("apply returned no command, want the background apply cmd")
	}

	// Simulate an in-flight UI edit: mutate the live fragment in place (this is
	// exactly the shape configform/configview/sectionform use). The captured
	// command must be insulated from it.
	m.frag.Sections[0].Entries[0].Value = `"evil.example." static`
	m.frag.Sections[0].Entries[1].Value = "0"

	msg := cmd()
	if _, ok := msg.(AppliedMsg); !ok {
		t.Fatalf("apply produced %T (%v), want AppliedMsg", msg, msg)
	}
	got, err := os.ReadFile(m.cfg.FragmentPath())
	if err != nil {
		t.Fatalf("read fragment: %v", err)
	}
	if strings.Contains(string(got), "evil.example.") {
		t.Fatalf("apply wrote a later in-place mutation; fragment:\n%s", got)
	}
	if strings.Contains(string(got), "edns-buffer-size: 0") {
		t.Fatalf("apply wrote a later in-place scalar mutation; fragment:\n%s", got)
	}
	if !strings.Contains(string(got), `local-zone: "example.com." transparent`) {
		t.Fatalf("apply did not write the captured fragment:\n%s", got)
	}
	if ctl.reloads != 1 {
		t.Errorf("reloads = %d, want 1", ctl.reloads)
	}
}

func TestQuitWithDirtyConfirms(t *testing.T) {
	m, _ := newTestModel(t)
	m.dirty = true
	next := asRoot(t, mustUpdate(t, m, key("q")))
	if next.state != StateConfirm || next.confirmKind != "quit" {
		t.Errorf("state/kind = %v/%q, want StateConfirm/quit", next.state, next.confirmKind)
	}
}

// assertQuits runs one Update and asserts the returned command quits the
// program (tea.Quit yields tea.QuitMsg).
func assertQuits(t *testing.T, m RootModel, msg tea.Msg) RootModel {
	t.Helper()
	next, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatal("Update returned nil command, want tea.Quit")
	}
	if got := cmd(); got != (tea.QuitMsg{}) {
		t.Fatalf("command produced %T (%v), want tea.QuitMsg", got, got)
	}
	return asRoot(t, next)
}

// TestCtrlCQuitsEveryState pins the hard-quit rule: ctrl+c returns tea.Quit
// from every state, including the modal forms and StateApplying, which route
// or swallow their own keys. The check is central in Update, so it cannot be
// bypassed by a state that handles its own keymap.
func TestCtrlCQuitsEveryState(t *testing.T) {
	states := map[string]func(t *testing.T) RootModel{
		"ready": func(t *testing.T) RootModel {
			m, _ := newTestModel(t)
			return m
		},
		"form": func(t *testing.T) RootModel {
			m := configModel(t, domain.Fragment{})
			m.openAddSectionForm()
			return m
		},
		"section-form": func(t *testing.T) RootModel {
			return openSpecialized(t, configModel(t, forwardSection("192.0.2.53")), 0)
		},
		"confirm": func(t *testing.T) RootModel {
			m, _ := newTestModel(t)
			m.dirty = true
			m.confirmKind = "quit"
			m.state = StateConfirm
			return m
		},
		"foreign": func(t *testing.T) RootModel {
			m, _ := newTestModel(t)
			next, _ := m.requestForeign()
			m = asRoot(t, next)
			if m.state != StateForeign {
				t.Fatalf("state = %v, want StateForeign", m.state)
			}
			return m
		},
		"error": func(t *testing.T) RootModel {
			m, _ := newTestModel(t)
			m.lastError = errors.New("boom")
			m.state = StateError
			return m
		},
		"applying": func(t *testing.T) RootModel {
			m, _ := newTestModel(t)
			m.state = StateApplying
			return m
		},
	}
	for name, build := range states {
		t.Run(name, func(t *testing.T) {
			assertQuits(t, build(t), key("ctrl+c"))
		})
	}
}

// TestCtrlCReadyKeepsQuitFlow pins that the central ctrl+c check excludes
// StateReady, where ctrl+c must behave exactly like q: quit immediately when
// clean, but open the unsaved-changes confirmation when dirty.
func TestCtrlCReadyKeepsQuitFlow(t *testing.T) {
	t.Run("clean quits", func(t *testing.T) {
		m, _ := newTestModel(t)
		assertQuits(t, m, key("ctrl+c"))
	})
	t.Run("dirty confirms", func(t *testing.T) {
		m, _ := newTestModel(t)
		m.dirty = true
		next, cmd := m.Update(key("ctrl+c"))
		if cmd != nil {
			t.Fatalf("ctrl+c returned a command in Ready with dirty state, want the confirm (no direct quit)")
		}
		r := asRoot(t, next)
		if r.state != StateConfirm || r.confirmKind != "quit" {
			t.Errorf("state/kind = %v/%q, want StateConfirm/quit", r.state, r.confirmKind)
		}
	})
}

// TestCtrlCLeavesOtherKeysRouting pins that the central ctrl+c check does not
// hijack the per-state routing: q still closes the foreign view (rather than
// quitting), StateApplying still swallows non-quit keys, and any other key
// still dismisses StateError.
func TestCtrlCLeavesOtherKeysRouting(t *testing.T) {
	t.Run("foreign q closes", func(t *testing.T) {
		m, _ := newTestModel(t)
		next, _ := m.requestForeign()
		m = asRoot(t, next)
		_, cmd := m.Update(key("q"))
		if cmd == nil {
			t.Fatal("q returned nil command, want ForeignCloseMsg")
		}
		if got := cmd(); got != (ForeignCloseMsg{}) {
			t.Fatalf("q produced %T (%v), want ForeignCloseMsg", got, got)
		}
	})
	t.Run("applying swallows other keys", func(t *testing.T) {
		m, _ := newTestModel(t)
		m.state = StateApplying
		next, cmd := m.Update(key("j"))
		if r := asRoot(t, next); r.state != StateApplying {
			t.Errorf("state = %v, want StateApplying", r.state)
		}
		if cmd != nil {
			t.Errorf("cmd = %v, want nil", cmd)
		}
	})
	t.Run("error dismisses on other key", func(t *testing.T) {
		m, _ := newTestModel(t)
		m.lastError = errors.New("boom")
		m.state = StateError
		next := asRoot(t, mustUpdate(t, m, key("x")))
		if next.state != StateReady {
			t.Errorf("state = %v, want StateReady", next.state)
		}
	})
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

// TestStatusLineUsesCachedRoot pins that the root flag is cached on the model
// at construction instead of syscalling os.Geteuid() on every frame, and that
// the status line reads the cached value.
func TestStatusLineUsesCachedRoot(t *testing.T) {
	if got, want := NewRootModel(nil, nil, "").isRoot, os.Geteuid() == 0; got != want {
		t.Errorf("NewRootModel isRoot = %v, want %v (from os.Geteuid)", got, want)
	}
	m, _ := newTestModel(t)
	m.isRoot = false
	if got := m.statusLine(400); !strings.Contains(got, "not root") {
		t.Errorf("non-root status line = %q, want the not-root marker", got)
	}
	m.isRoot = true
	if got := m.statusLine(400); strings.Contains(got, "not root") {
		t.Errorf("root status line = %q, should not carry the not-root marker", got)
	}
}

func TestAddDuplicateZoneRejected(t *testing.T) {
	m, _ := newTestModel(t)
	m = submitInFormState(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com", Type: "transparent"})
	next := submitInFormState(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com.", Type: "static"})
	if next.state != StateError {
		t.Fatalf("state = %v, want StateError for a duplicate zone", next.state)
	}
	if len(next.zones) != 1 {
		t.Errorf("zones = %+v, want the original only", next.zones)
	}
}

// TestAddZoneMovesCursorToNewZone pins that a successful add-zone leaves the
// selection on the zone just created (and resets the record cursor), so the
// next action targets what the user just added instead of the old selection.
func TestAddZoneMovesCursorToNewZone(t *testing.T) {
	m, _ := newTestModel(t)
	m = submitInFormState(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "a.example", Type: "transparent"})
	m.zoneCursor = 0
	m.recCursor = 0
	m = submitInFormState(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "z.example", Type: "transparent"})
	if m.zoneCursor != 1 {
		t.Errorf("zoneCursor = %d, want 1 (the new zone)", m.zoneCursor)
	}
	if m.recCursor != 0 {
		t.Errorf("recCursor = %d, want 0", m.recCursor)
	}
}

// TestInvalidFormSubmitIsNoOp pins that a submit whose target does not exist
// leaves the fragment and the dirty flag untouched: the old code fell through
// to regen()+dirty for these guard failures even though nothing changed.
func TestInvalidFormSubmitIsNoOp(t *testing.T) {
	cases := []struct {
		name string
		msg  FormSubmitMsg
	}{
		{"add-record out-of-range zone", FormSubmitMsg{Mode: FormAddRecord, ZoneIndex: 5, Name: "www", Type: "A", Value: "192.0.2.1", TTL: 300}},
		{"edit-ttl out-of-range record", FormSubmitMsg{Mode: FormEditTTL, ZoneIndex: 0, RecIndex: 5, TTL: 60}},
		{"set-type out-of-range zone", FormSubmitMsg{Mode: FormSetType, ZoneIndex: 9, Type: "static"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newTestModel(t)
			m = submitInFormState(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com", Type: "transparent"})
			m.dirty = false
			before := cloneFragment(m.frag)
			next := submitInFormState(t, m, tc.msg)
			if next.dirty {
				t.Error("dirty = true after a no-op submit, want it untouched")
			}
			if !reflect.DeepEqual(next.frag, before) {
				t.Errorf("fragment changed after a no-op submit:\n got %+v\nwant %+v", next.frag, before)
			}
			if next.state != StateReady {
				t.Errorf("state = %v, want StateReady", next.state)
			}
		})
	}
}

// TestMutationsRegenerateFragment checks that add-record, edit-ttl and
// set-type all fold into the stored fragment immediately.
func TestMutationsRegenerateFragment(t *testing.T) {
	m, _ := newTestModel(t)

	m = submitInFormState(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com", Type: "transparent"})
	m = submitInFormState(t, m, FormSubmitMsg{Mode: FormAddRecord, ZoneIndex: 0, Name: "www", Type: "A", Value: "192.0.2.1", TTL: 300})
	got := localEntries(m.frag)
	if len(got) != 2 || got[1].Key != "local-data" || got[1].Value != `"www.example.com. 300 IN A 192.0.2.1"` {
		t.Fatalf("frag after add-record = %+v, want the local-data entry", got)
	}

	m = submitInFormState(t, m, FormSubmitMsg{Mode: FormEditTTL, ZoneIndex: 0, RecIndex: 0, TTL: 60})
	if got := localEntries(m.frag); got[1].Value != `"www.example.com. 60 IN A 192.0.2.1"` {
		t.Errorf("frag after edit-ttl = %+v, want TTL 60", got)
	}

	m = submitInFormState(t, m, FormSubmitMsg{Mode: FormSetType, ZoneIndex: 0, Type: "static"})
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
	next := submitInFormState(t, m, FormSubmitMsg{
		Mode: FormAddRecord, ZoneIndex: 0, Name: "WWW", Type: "a", Value: "192.0.2.1", TTL: 60,
	})
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

// TestLocalMutationGuardsRefuseWhenProjectionInvalid pins the shared
// data-integrity guard on the Local-data mutations: when the fragment's
// local-* entries did not project (zonesValid false), deleteZone, deleteRecord
// and toggleDisabled must refuse with the actionable notice and touch neither
// the zones model, the fragment nor the dirty flag. These guards were
// previously untested; without them a stale zonesValid could let a mutation
// regen() from the failed (empty) projection and drop local data.
func TestLocalMutationGuardsRefuseWhenProjectionInvalid(t *testing.T) {
	build := func(t *testing.T) RootModel {
		t.Helper()
		m, _ := newTestModel(t)
		m.zones = []domain.Zone{{
			Name: "example.com.", Type: "transparent",
			Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}},
		}}
		m.frag = domain.Fragment{Sections: []domain.Section{
			{Kind: "server", Entries: []domain.Entry{
				{Key: "local-zone", Value: `"example.com." transparent`},
				{Key: "local-data", Value: `"www.example.com. 300 IN A 192.0.2.1"`},
			}},
		}}
		m.zonesValid = false
		m.zoneFocused = true
		return m
	}

	assertRefused := func(t *testing.T, m RootModel) {
		t.Helper()
		if m.notice != projectionFailedNotice {
			t.Errorf("notice = %q, want the projection-failed notice", m.notice)
		}
		if m.dirty {
			t.Error("dirty = true after a refused mutation")
		}
		if len(m.zones) != 1 || len(m.zones[0].Records) != 1 {
			t.Errorf("zones = %+v, want the guard to leave them untouched", m.zones)
		}
		if got := localEntries(m.frag); len(got) != 2 {
			t.Errorf("frag local entries = %+v, want the guard to leave them untouched", got)
		}
	}

	t.Run("deleteZone", func(t *testing.T) {
		m := build(t)
		m.deleteZone(0)
		assertRefused(t, m)
	})

	t.Run("deleteRecord", func(t *testing.T) {
		m := build(t)
		m.deleteRecord(0, 0)
		assertRefused(t, m)
	})

	t.Run("toggleDisabled", func(t *testing.T) {
		m := build(t)
		m.toggleDisabled()
		assertRefused(t, m)
	})
}

// TestConfigCrossViewZoneAddVisibleInConfig pins Review Focus #4: a zone added
// in the Local data view lands in frag and is immediately visible as a
// local-zone row in the Config view, which projects the same fragment.
func TestConfigCrossViewZoneAddVisibleInConfig(t *testing.T) {
	m, _ := newTestModel(t)
	m = submitInFormState(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "example.com", Type: "transparent"})
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

// TestCloneFragmentIsIndependent pins that cloneFragment detaches every slice
// and flag: mutating the original after cloning (including in-place element
// writes and slice growth) must not be visible through the clone.
func TestCloneFragmentIsIndependent(t *testing.T) {
	orig := domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{
			{Key: "local-zone", Value: `"example.com." transparent`},
			{Key: "local-data", Value: `"www.example.com. A 192.0.2.1"`, Disabled: true},
		}},
		{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: `"example.org."`},
		}},
	}}
	clone := cloneFragment(orig)

	// Mutate the original in place: element writes, a flag flip, and slice
	// growth on the outer and inner slices.
	orig.Sections[0].Kind = "changed"
	orig.Sections[0].Entries[0].Value = "changed"
	orig.Sections[0].Entries[1].Disabled = false
	orig.Sections = append(orig.Sections, domain.Section{Kind: "extra"})
	orig.Sections[0].Entries = append(orig.Sections[0].Entries, domain.Entry{Key: "extra"})

	if clone.Sections[0].Kind != "server" {
		t.Errorf("clone section kind = %q, want server", clone.Sections[0].Kind)
	}
	if clone.Sections[0].Entries[0].Value != `"example.com." transparent` {
		t.Errorf("clone entry value = %q, want the original", clone.Sections[0].Entries[0].Value)
	}
	if !clone.Sections[0].Entries[1].Disabled {
		t.Error("clone entry Disabled = false, want true")
	}
	if len(clone.Sections) != 2 {
		t.Errorf("clone has %d sections, want 2 (append leaked through)", len(clone.Sections))
	}
	if len(clone.Sections[0].Entries) != 2 {
		t.Errorf("clone has %d entries in section 0, want 2 (append leaked through)", len(clone.Sections[0].Entries))
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

// --- R3 Task 4: form messages must match the current state ---

// submitInFormState delivers a form message as if the corresponding form were
// open — the only state that honors it. Apply-path tests use it to exercise
// applyForm/applyConfigForm/applySectionForm without walking the form UI.
func submitInFormState(t *testing.T, m RootModel, msg tea.Msg) RootModel {
	t.Helper()
	if _, ok := msg.(SectionFormSubmitMsg); ok {
		m.state = StateSectionForm
	} else {
		m.state = StateForm
	}
	return asRoot(t, mustUpdate(t, m, msg))
}

// formsLoaded reports whether both the generic and the specialized form are
// still present. A zero RecordForm/SectionForm has no inputs (FormAddZone is
// the zero FormMode, so the mode alone cannot signal an empty form).
func formsLoaded(m RootModel) bool {
	return len(m.form.inputs) > 0 && len(m.secForm.inputs) > 0
}

// TestFormCancelOnlyHonoredInFormState pins that FormCancelMsg is only honored
// while a form is actually open (StateForm/StateSectionForm). In every other
// state it must be ignored completely, leaving the state and the loaded forms
// untouched.
func TestFormCancelOnlyHonoredInFormState(t *testing.T) {
	newModel := func(t *testing.T, state AppState) RootModel {
		t.Helper()
		m, _ := newTestModel(t)
		m.form = newZoneForm()
		m.secForm = newSectionForm(forwardSection("192.0.2.53"), 0)
		m.state = state
		return m
	}

	for _, tc := range []struct {
		name    string
		state   AppState
		honored bool
	}{
		{"ready ignored", StateReady, false},
		{"foreign ignored", StateForeign, false},
		{"applying ignored", StateApplying, false},
		{"confirm ignored", StateConfirm, false},
		{"error ignored", StateError, false},
		{"form honored", StateForm, true},
		{"section form honored", StateSectionForm, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(t, tc.state)
			next := asRoot(t, mustUpdate(t, m, FormCancelMsg{}))
			if !tc.honored {
				if next.state != tc.state {
					t.Errorf("state = %v, want %v (cancel must be ignored)", next.state, tc.state)
				}
				if !formsLoaded(next) {
					t.Errorf("cancel cleared a form while in %v", tc.state)
				}
				return
			}
			if next.state != StateReady {
				t.Errorf("state = %v, want StateReady", next.state)
			}
			if formsLoaded(next) {
				t.Errorf("cancel did not clear both forms (inputs: form=%d secForm=%d)",
					len(next.form.inputs), len(next.secForm.inputs))
			}
		})
	}
}

// TestLateSubmitAfterCancelIgnored pins that a submit command already in flight
// when the user cancels is ignored completely: no mutation, no regen, no
// dirty flag, no state change.
func TestLateSubmitAfterCancelIgnored(t *testing.T) {
	t.Run("zones form", func(t *testing.T) {
		m, _ := newTestModel(t)
		m.form = newZoneForm()
		m.state = StateForm
		m = asRoot(t, mustUpdate(t, m, FormCancelMsg{}))
		if m.state != StateReady {
			t.Fatalf("state = %v, want StateReady after cancel", m.state)
		}

		before := m.frag
		next := asRoot(t, mustUpdate(t, m, FormSubmitMsg{Mode: FormAddZone, Name: "late.example", Type: "transparent"}))
		if next.dirty {
			t.Error("late zone submit set dirty")
		}
		if len(next.zones) != 0 {
			t.Errorf("zones = %+v, want empty (late submit ignored)", next.zones)
		}
		if !reflect.DeepEqual(next.frag, before) {
			t.Errorf("fragment changed by a late submit: %+v", next.frag)
		}
		if next.state != StateReady {
			t.Errorf("state = %v, want StateReady", next.state)
		}
	})

	t.Run("config form", func(t *testing.T) {
		m := configModel(t, domain.Fragment{})
		m.openAddSectionForm()
		m = asRoot(t, mustUpdate(t, m, FormCancelMsg{}))
		if m.state != StateReady {
			t.Fatalf("state = %v, want StateReady after cancel", m.state)
		}

		before := m.frag
		next := asRoot(t, mustUpdate(t, m, ConfigFormSubmitMsg{Mode: FormAddSection, Kind: "server"}))
		if next.dirty {
			t.Error("late config submit set dirty")
		}
		if len(next.frag.Sections) != 0 {
			t.Errorf("sections = %+v, want none (late submit ignored)", next.frag.Sections)
		}
		if !reflect.DeepEqual(next.frag, before) {
			t.Errorf("fragment changed by a late submit: %+v", next.frag)
		}
	})

	t.Run("section form", func(t *testing.T) {
		m := configModel(t, forwardSection("192.0.2.53"))
		m = openSpecialized(t, m, 0)
		m = asRoot(t, mustUpdate(t, m, FormCancelMsg{}))
		if m.state != StateReady {
			t.Fatalf("state = %v, want StateReady after cancel", m.state)
		}

		before := m.frag
		next := asRoot(t, mustUpdate(t, m, SectionFormSubmitMsg{
			Kind: "forward-zone", SecIndex: 0, Name: "changed.", Addrs: []string{"192.0.2.99"},
		}))
		if next.dirty {
			t.Error("late section submit set dirty")
		}
		if !reflect.DeepEqual(next.frag, before) {
			t.Errorf("fragment changed by a late submit:\n got %+v\nwant %+v", next.frag, before)
		}
	})
}

// TestFormMessagesIgnoredWhileApplying pins that the StateApplying seal covers
// every form message, not just KeyMsg: a duplicate or late submit/cancel that
// arrives after an apply started must not be applied.
func TestFormMessagesIgnoredWhileApplying(t *testing.T) {
	newModel := func(t *testing.T) RootModel {
		t.Helper()
		m := configModel(t, forwardSection("192.0.2.53"))
		m.form = newZoneForm()
		m.secForm = newSectionForm(m.frag, 0)
		m.state = StateApplying
		m.dirty = true
		return m
	}

	for _, msg := range []tea.Msg{
		FormCancelMsg{},
		FormSubmitMsg{Mode: FormAddZone, Name: "late.example", Type: "transparent"},
		ConfigFormSubmitMsg{Mode: FormAddSection, Kind: "server"},
		SectionFormSubmitMsg{Kind: "forward-zone", SecIndex: 0, Name: "changed.", Addrs: []string{"192.0.2.99"}},
		key("a"),
	} {
		m := newModel(t)
		before := m.frag
		next, cmd := m.Update(msg)
		got := asRoot(t, next)
		if cmd != nil {
			t.Errorf("%T: produced a command while applying, want nil", msg)
		}
		if got.state != StateApplying {
			t.Errorf("%T: state = %v, want StateApplying (seal)", msg, got.state)
		}
		if !got.dirty {
			t.Errorf("%T: dirty flag changed while applying", msg)
		}
		if !reflect.DeepEqual(got.frag, before) {
			t.Errorf("%T: fragment changed while applying", msg)
		}
		if !formsLoaded(got) {
			t.Errorf("%T: a form was cleared while applying", msg)
		}
	}
}

// TestFormSubmitAndCancelStillWork is the positive control for the state
// guards: a submit from the matching form state still lands, and a cancel from
// the matching form state still returns to Ready.
func TestFormSubmitAndCancelStillWork(t *testing.T) {
	t.Run("zones form submit", func(t *testing.T) {
		m, _ := newTestModel(t)
		m = asRoot(t, mustUpdate(t, m, key("a")))
		if m.state != StateForm {
			t.Fatalf("state = %v, want StateForm after 'a'", m.state)
		}
		m.form.inputs[0].SetValue("example.com")
		m.form.inputs[1].SetValue("transparent")
		_, cmd := m.Update(key("ctrl+s"))
		if cmd == nil {
			t.Fatalf("ctrl+s produced no command (err=%v)", m.form.err)
		}
		submit, ok := cmd().(FormSubmitMsg)
		if !ok {
			t.Fatalf("ctrl+s produced %T, want FormSubmitMsg", cmd())
		}
		next := asRoot(t, mustUpdate(t, m, submit))
		if len(next.zones) != 1 || !next.dirty {
			t.Fatalf("zones/dirty = %+v/%v, want one zone and dirty", next.zones, next.dirty)
		}
	})

	t.Run("config form submit", func(t *testing.T) {
		m := configModel(t, domain.Fragment{})
		m.openAddSectionForm()
		m.form.inputs[0].SetValue("server")
		next, cmd := submitFormKey(t, m, "ctrl+s")
		if cmd == nil {
			t.Fatalf("ctrl+s produced no command (err=%v)", next.form.err)
		}
		submit, ok := cmd().(ConfigFormSubmitMsg)
		if !ok {
			t.Fatalf("ctrl+s produced %T, want ConfigFormSubmitMsg", cmd())
		}
		got := asRoot(t, mustUpdate(t, next, submit))
		if len(got.frag.Sections) != 1 || got.frag.Sections[0].Kind != "server" {
			t.Fatalf("sections = %+v, want one server section", got.frag.Sections)
		}
	})

	t.Run("section form cancel", func(t *testing.T) {
		m := configModel(t, forwardSection("192.0.2.53"))
		m = openSpecialized(t, m, 0)
		next, cmd := stepSpecialized(t, m, "esc")
		if cmd == nil {
			t.Fatal("esc produced no command")
		}
		cancel, ok := cmd().(FormCancelMsg)
		if !ok {
			t.Fatalf("esc produced %T, want FormCancelMsg", cmd())
		}
		got := asRoot(t, mustUpdate(t, next, cancel))
		if got.state != StateReady {
			t.Errorf("state = %v, want StateReady after cancel", got.state)
		}
	})
}
