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
	conf := filepath.Join(dir, "unbound.conf")
	if err := os.WriteFile(conf, []byte("include: "+filepath.Join(dir, "frag.conf")+"\n"), 0644); err != nil {
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
