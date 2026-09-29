package model

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// --- helpers ---

func addrKeyFor(kind string) string {
	if kind == "stub-zone" {
		return "stub-addr"
	}
	return "forward-addr"
}

func tlsKeyFor(kind string) string {
	if kind == "stub-zone" {
		return "stub-prime"
	}
	return "forward-tls-upstream"
}

func firstKeyFor(kind string) string {
	if kind == "stub-zone" {
		return "stub-first"
	}
	return "forward-first"
}

// forwardSection bags a forward-zone named "." around the given addresses.
func forwardSection(addrs ...string) domain.Fragment {
	entries := []domain.Entry{{Key: "name", Value: "."}}
	for _, a := range addrs {
		entries = append(entries, domain.Entry{Key: "forward-addr", Value: a})
	}
	return domain.Fragment{Sections: []domain.Section{{Kind: "forward-zone", Entries: entries}}}
}

// openSpecialized opens the specialized form on section secIdx through the `E`
// key, failing the test when the key did not land.
func openSpecialized(t *testing.T, m RootModel, secIdx int) RootModel {
	t.Helper()
	m.cfgView.SecCursor = secIdx
	m = asRoot(t, mustUpdate(t, m, key("E")))
	if m.state != StateSectionForm {
		t.Fatalf("state = %v, want StateSectionForm (err=%v)", m.state, m.secForm.err)
	}
	return m
}

// stepSpecialized sends one key to the active specialized form and returns the
// resulting root model plus the command.
func stepSpecialized(t *testing.T, m RootModel, k string) (RootModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(key(k))
	return asRoot(t, next), cmd
}

// --- E gating ---

func TestSpecializeKeyOpensPrefilledForm(t *testing.T) {
	for _, kind := range []string{"forward-zone", "stub-zone"} {
		t.Run(kind, func(t *testing.T) {
			f := domain.Fragment{Sections: []domain.Section{
				{Kind: kind, Entries: []domain.Entry{
					{Key: "name", Value: "test."},
					{Key: addrKeyFor(kind), Value: "192.0.2.53"},
					{Key: addrKeyFor(kind), Value: "192.0.2.54"},
					{Key: tlsKeyFor(kind), Value: "yes"},
				}},
			}}
			m := openSpecialized(t, configModel(t, f), 0)
			sf := m.secForm
			if sf.kind != kind {
				t.Fatalf("kind = %q, want %q", sf.kind, kind)
			}
			if got := sf.inputs[sf.nameIdx].Value(); got != "test." {
				t.Errorf("name = %q, want test.", got)
			}
			if n := sf.addrHi - sf.addrLo; n != 3 {
				t.Fatalf("addr lines = %d, want 3 (two existing + one empty)", n)
			}
			if got := sf.inputs[sf.addrLo].Value(); got != "192.0.2.53" {
				t.Errorf("addr 1 = %q, want 192.0.2.53", got)
			}
			if got := sf.inputs[sf.addrLo+1].Value(); got != "192.0.2.54" {
				t.Errorf("addr 2 = %q, want 192.0.2.54", got)
			}
			if got := sf.inputs[sf.addrLo+2].Value(); got != "" {
				t.Errorf("trailing addr = %q, want empty", got)
			}
			if got := sf.inputs[sf.tlsUpIdx].Value(); got != "yes" {
				t.Errorf("tls/prime = %q, want yes", got)
			}
			if got := sf.inputs[sf.firstIdx].Value(); got != "" {
				t.Errorf("first = %q, want empty (absent)", got)
			}
			if got := sf.inputs[sf.tlsUpIdx].Placeholder; got != "yes|no" {
				t.Errorf("bool placeholder = %q, want yes|no", got)
			}
		})
	}
}

func TestSpecializeKeyRejectsNonForwardStub(t *testing.T) {
	f := domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "port", Value: "53"}}},
	}}
	m := configModel(t, f)
	m.cfgView.SecCursor = 0
	next := asRoot(t, mustUpdate(t, m, key("E")))
	if next.state != StateReady {
		t.Errorf("state = %v, want StateReady", next.state)
	}
	if next.notice != "E works on forward-zone and stub-zone sections" {
		t.Errorf("notice = %q, want the unsupported-kind notice", next.notice)
	}
	if !reflect.DeepEqual(next.frag, f) {
		t.Errorf("fragment changed: %+v", next.frag)
	}
}

// --- A-chain ---

func TestAddSectionChainsIntoSpecializedForm(t *testing.T) {
	for _, kind := range []string{"forward-zone", "stub-zone"} {
		t.Run(kind, func(t *testing.T) {
			m := configModel(t, domain.Fragment{})
			m.openAddSectionForm()
			m.form.inputs[0].SetValue(kind)
			m.form.inputs[1].SetValue("test.")

			next, cmd := m.Update(key("ctrl+s"))
			if cmd == nil {
				t.Fatal("A submit produced no command")
			}
			msg, ok := cmd().(ConfigFormSubmitMsg)
			if !ok {
				t.Fatalf("A submit produced %T, want ConfigFormSubmitMsg", cmd())
			}
			m = asRoot(t, mustUpdate(t, asRoot(t, next), msg))

			if m.state != StateSectionForm {
				t.Fatalf("state = %v, want StateSectionForm (A must chain)", m.state)
			}
			if m.secForm.kind != kind || m.secForm.secIdx != len(m.frag.Sections)-1 {
				t.Fatalf("form target = %q@%d, want %s@%d",
					m.secForm.kind, m.secForm.secIdx, kind, len(m.frag.Sections)-1)
			}
			if got := m.secForm.inputs[m.secForm.nameIdx].Value(); got != "test." {
				t.Errorf("prefilled name = %q, want test.", got)
			}
			if !m.dirty {
				t.Error("dirty = false after A-chain")
			}
		})
	}
}

// --- rebuild ---

func TestSectionFormRebuildPreservesUnmanaged(t *testing.T) {
	f := domain.Fragment{Sections: []domain.Section{
		{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: "."},
			{Key: "forward-addr", Value: "192.0.2.53"},
			{Key: "forward-addr", Value: "192.0.2.54"},
			{Key: "forward-host", Value: "dns.example"},
			{Key: "dnssec", Value: "yes"},
		}},
	}}
	m := openSpecialized(t, configModel(t, f), 0)
	sf := m.secForm
	sf.inputs[sf.addrLo].SetValue("192.0.2.99")    // change one
	sf.inputs[sf.addrLo+1].SetValue("")            // clear one
	sf.inputs[sf.addrLo+2].SetValue("192.0.2.100") // add one via the trailing line
	m.secForm = sf

	next, cmd := stepSpecialized(t, m, "ctrl+s")
	if cmd == nil {
		t.Fatalf("submit produced no command; err = %v", next.secForm.err)
	}
	msg, ok := cmd().(SectionFormSubmitMsg)
	if !ok {
		t.Fatalf("submit produced %T, want SectionFormSubmitMsg", cmd())
	}
	m = asRoot(t, mustUpdate(t, next, msg))

	want := []domain.Entry{
		{Key: "forward-host", Value: "dns.example"},
		{Key: "dnssec", Value: "yes"},
		{Key: "name", Value: "."},
		{Key: "forward-addr", Value: "192.0.2.99"},
		{Key: "forward-addr", Value: "192.0.2.100"},
	}
	if got := m.frag.Sections[0].Entries; !reflect.DeepEqual(got, want) {
		t.Errorf("entries =\n  %+v\nwant\n  %+v", got, want)
	}
	if !m.dirty {
		t.Error("dirty = false after submit")
	}
	if m.state != StateReady {
		t.Errorf("state = %v, want StateReady", m.state)
	}
}

func TestRebuildSectionStubKeys(t *testing.T) {
	s := domain.Section{Kind: "stub-zone", Entries: []domain.Entry{
		{Key: "name", Value: "old."},
		{Key: "stub-addr", Value: "192.0.2.1"},
		{Key: "stub-host", Value: "host.example"},
		{Key: "stub-prime", Value: "yes"},
		{Key: "stub-first", Value: "no"},
	}}
	rebuildSection(&s, "stub-zone", "new.", []string{"192.0.2.2", "192.0.2.3"}, "no", "")
	want := []domain.Entry{
		{Key: "stub-host", Value: "host.example"},
		{Key: "name", Value: "new."},
		{Key: "stub-addr", Value: "192.0.2.2"},
		{Key: "stub-addr", Value: "192.0.2.3"},
		{Key: "stub-prime", Value: "no"},
	}
	if !reflect.DeepEqual(s.Entries, want) {
		t.Errorf("entries =\n  %+v\nwant\n  %+v", s.Entries, want)
	}
}

func TestSectionFormRebuildCarriesDisabled(t *testing.T) {
	f := domain.Fragment{Sections: []domain.Section{
		{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: ".", Disabled: true},
			{Key: "forward-addr", Value: "192.0.2.53", Disabled: true},
			{Key: "forward-addr", Value: "192.0.2.54"},
			{Key: "forward-tls-upstream", Value: "yes", Disabled: true},
			{Key: "forward-first", Value: "no"},
		}},
	}}
	m := openSpecialized(t, configModel(t, f), 0)
	sf := m.secForm
	sf.inputs[sf.addrLo+1].SetValue("")           // drop the active address
	sf.inputs[sf.addrLo+2].SetValue("192.0.2.55") // add a new one
	m.secForm = sf

	next, cmd := stepSpecialized(t, m, "ctrl+s")
	if cmd == nil {
		t.Fatalf("submit produced no command; err = %v", next.secForm.err)
	}
	m = asRoot(t, mustUpdate(t, next, cmd().(SectionFormSubmitMsg)))

	want := []domain.Entry{
		{Key: "name", Value: ".", Disabled: true},
		{Key: "forward-addr", Value: "192.0.2.53", Disabled: true}, // unchanged value keeps its flag
		{Key: "forward-addr", Value: "192.0.2.55"},                 // new value is active
		{Key: "forward-tls-upstream", Value: "yes", Disabled: true},
		{Key: "forward-first", Value: "no"},
	}
	if got := m.frag.Sections[0].Entries; !reflect.DeepEqual(got, want) {
		t.Errorf("entries =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestSectionFormRebuildCollapsesDuplicateManagedKeys(t *testing.T) {
	f := domain.Fragment{Sections: []domain.Section{
		{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: "first."},
			{Key: "name", Value: "second."},
			{Key: "forward-tls-upstream", Value: "yes"},
			{Key: "forward-tls-upstream", Value: "no"},
		}},
	}}
	m := openSpecialized(t, configModel(t, f), 0)
	if got := m.secForm.inputs[m.secForm.nameIdx].Value(); got != "first." {
		t.Errorf("prefilled name = %q, want the first duplicate's value", got)
	}
	if got := m.secForm.inputs[m.secForm.tlsUpIdx].Value(); got != "yes" {
		t.Errorf("prefilled tls = %q, want the first duplicate's value", got)
	}

	next, cmd := stepSpecialized(t, m, "ctrl+s")
	if cmd == nil {
		t.Fatalf("submit produced no command; err = %v", next.secForm.err)
	}
	m = asRoot(t, mustUpdate(t, next, cmd().(SectionFormSubmitMsg)))

	want := []domain.Entry{
		{Key: "name", Value: "first."},
		{Key: "forward-tls-upstream", Value: "yes"},
	}
	if got := m.frag.Sections[0].Entries; !reflect.DeepEqual(got, want) {
		t.Errorf("entries =\n  %+v\nwant exactly one name and one tls (first wins)", got)
	}
}

func TestRebuildSectionForwardSkipsForwardHost(t *testing.T) {
	s := domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
		{Key: "forward-host", Value: "dns.example"},
		{Key: "forward-addr", Value: "192.0.2.1"},
	}}
	rebuildSection(&s, "forward-zone", "", nil, "", "")
	want := []domain.Entry{
		{Key: "forward-host", Value: "dns.example"},
	}
	if !reflect.DeepEqual(s.Entries, want) {
		t.Errorf("entries = %+v, want forward-host preserved only", s.Entries)
	}
}

// --- bools ---

func TestSectionFormBoolAbsentAndValidated(t *testing.T) {
	t.Run("empty bool is absent", func(t *testing.T) {
		f := domain.Fragment{Sections: []domain.Section{{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: "."},
			{Key: "forward-tls-upstream", Value: "yes"},
		}}}}
		m := openSpecialized(t, configModel(t, f), 0)
		m.secForm.inputs[m.secForm.tlsUpIdx].SetValue("")
		next, cmd := stepSpecialized(t, m, "ctrl+s")
		if cmd == nil {
			t.Fatalf("err = %v", next.secForm.err)
		}
		m = asRoot(t, mustUpdate(t, next, cmd().(SectionFormSubmitMsg)))
		for _, e := range m.frag.Sections[0].Entries {
			if e.Key == "forward-tls-upstream" || e.Key == "forward-first" {
				t.Errorf("bool %s survived an empty field: %+v", e.Key, m.frag.Sections[0].Entries)
			}
		}
	})

	t.Run("invalid bool blocks submit", func(t *testing.T) {
		m := openSpecialized(t, configModel(t, forwardSection("192.0.2.53")), 0)
		before := m.frag
		m.secForm.inputs[m.secForm.tlsUpIdx].SetValue("maybe")
		next, cmd := stepSpecialized(t, m, "ctrl+s")
		if cmd != nil {
			t.Fatal("invalid bool produced a submit command")
		}
		if next.secForm.err == nil || !strings.Contains(next.secForm.err.Error(), "invalid boolean") {
			t.Errorf("err = %v, want an invalid boolean error", next.secForm.err)
		}
		if !reflect.DeepEqual(next.frag, before) {
			t.Errorf("rejected submit changed the fragment: %+v", next.frag)
		}
	})
}

// --- address lines ---

func TestSectionFormAddrLineValidation(t *testing.T) {
	t.Run("bad port names the line", func(t *testing.T) {
		m := openSpecialized(t, configModel(t, forwardSection("192.0.2.53")), 0)
		m.secForm.inputs[m.secForm.addrLo+1].SetValue("192.0.2.53@70000")
		next, cmd := stepSpecialized(t, m, "ctrl+s")
		if cmd != nil {
			t.Fatal("bad address produced a submit command")
		}
		if next.secForm.err == nil || !strings.Contains(next.secForm.err.Error(), "line 2") {
			t.Errorf("err = %v, want a line 2 address error", next.secForm.err)
		}
	})

	t.Run("empty lines are dropped", func(t *testing.T) {
		m := openSpecialized(t, configModel(t, forwardSection("192.0.2.53", "192.0.2.54")), 0)
		m.secForm.inputs[m.secForm.addrLo].SetValue("")
		next, cmd := stepSpecialized(t, m, "ctrl+s")
		if cmd == nil {
			t.Fatalf("err = %v", next.secForm.err)
		}
		msg := cmd().(SectionFormSubmitMsg)
		if !reflect.DeepEqual(msg.Addrs, []string{"192.0.2.54"}) {
			t.Errorf("addrs = %+v, want [192.0.2.54]", msg.Addrs)
		}
	})
}

// --- add-time warning ---

func TestSectionFormAddTimeWarning(t *testing.T) {
	t.Run("live match warns but proceeds", func(t *testing.T) {
		m := configModel(t, domain.Fragment{})
		m.openAddSectionForm()
		m.form.inputs[0].SetValue("forward-zone")
		m.form.inputs[1].SetValue("test.")
		next, cmd := m.Update(key("ctrl+s"))
		if cmd == nil {
			t.Fatal("section submit produced no command")
		}
		m = asRoot(t, mustUpdate(t, asRoot(t, next), cmd().(ConfigFormSubmitMsg)))
		m.upstreams = []UpstreamRow{
			{Kind: "forward-zone", Name: "test.", Source: "/etc/unbound/foreign.conf"},
		}

		next2, cmd2 := stepSpecialized(t, m, "ctrl+s")
		if cmd2 == nil {
			t.Fatalf("specialized submit produced no command; err = %v", next2.secForm.err)
		}
		m = asRoot(t, mustUpdate(t, next2, cmd2().(SectionFormSubmitMsg)))

		for _, want := range []string{"forward-zone", "test.", "/etc/unbound/foreign.conf", "apply will be refused"} {
			if !strings.Contains(m.notice, want) {
				t.Errorf("notice = %q, want it to contain %q", m.notice, want)
			}
		}
		if !m.dirty {
			t.Error("dirty = false; the warning must be non-blocking")
		}
		if m.state != StateReady {
			t.Errorf("state = %v, want StateReady", m.state)
		}
	})

	t.Run("dead match is silent", func(t *testing.T) {
		m := configModel(t, forwardSection("192.0.2.53"))
		m.upstreams = []UpstreamRow{
			{Kind: "forward-zone", Name: ".", Source: "/etc/unbound/foreign.conf", Dead: true},
		}
		m = openSpecialized(t, m, 0)
		next, cmd := stepSpecialized(t, m, "ctrl+s")
		if cmd == nil {
			t.Fatalf("err = %v", next.secForm.err)
		}
		m = asRoot(t, mustUpdate(t, next, cmd().(SectionFormSubmitMsg)))
		if m.notice != "" {
			t.Errorf("notice = %q, want empty for a dead row", m.notice)
		}
	})
}

// --- cancel and stub keys ---

func TestSectionFormEscCancels(t *testing.T) {
	m := openSpecialized(t, configModel(t, forwardSection("192.0.2.53")), 0)
	m.secForm.inputs[m.secForm.nameIdx].SetValue("changed.")
	before := m.frag

	next, cmd := stepSpecialized(t, m, "esc")
	if cmd == nil {
		t.Fatal("esc produced no command")
	}
	cancel, ok := cmd().(FormCancelMsg)
	if !ok {
		t.Fatalf("esc produced %T, want FormCancelMsg", cmd())
	}
	m = asRoot(t, mustUpdate(t, next, cancel))
	if m.state != StateReady {
		t.Errorf("state = %v, want StateReady", m.state)
	}
	if !reflect.DeepEqual(m.frag, before) {
		t.Errorf("cancel changed the fragment: %+v", m.frag)
	}
}

func TestSectionFormStubSubmitUsesStubKeys(t *testing.T) {
	m := configModel(t, domain.Fragment{})
	m.openAddSectionForm()
	m.form.inputs[0].SetValue("stub-zone")
	m.form.inputs[1].SetValue("stub.example")
	next, cmd := m.Update(key("ctrl+s"))
	if cmd == nil {
		t.Fatal("section submit produced no command")
	}
	m = asRoot(t, mustUpdate(t, asRoot(t, next), cmd().(ConfigFormSubmitMsg)))

	m.secForm.inputs[m.secForm.addrLo].SetValue("192.0.2.1")
	m.secForm.inputs[m.secForm.tlsUpIdx].SetValue("yes") // stub-prime
	m.secForm.inputs[m.secForm.firstIdx].SetValue("no")  // stub-first
	next2, cmd2 := stepSpecialized(t, m, "ctrl+s")
	if cmd2 == nil {
		t.Fatalf("err = %v", next2.secForm.err)
	}
	m = asRoot(t, mustUpdate(t, next2, cmd2().(SectionFormSubmitMsg)))

	want := []domain.Entry{
		{Key: "name", Value: "stub.example"},
		{Key: "stub-addr", Value: "192.0.2.1"},
		{Key: "stub-prime", Value: "yes"},
		{Key: "stub-first", Value: "no"},
	}
	if got := m.frag.Sections[0].Entries; !reflect.DeepEqual(got, want) {
		t.Errorf("entries =\n  %+v\nwant\n  %+v", got, want)
	}
}

// --- name is required ---

// TestSectionFormRequiresName pins that the specialized form refuses a missing
// name instead of silently omitting the name entry: an omitted name makes the
// section identity the root zone ".", which is a legal-but-unintended retarget.
// The explicit "." spelling stays the only way to target the root zone.
func TestSectionFormRequiresName(t *testing.T) {
	const wantMsg = `name is required — use "." for the root zone`
	cases := []struct {
		name    string
		kind    string
		input   string
		wantErr bool
	}{
		{name: "forward-zone empty", kind: "forward-zone", input: "", wantErr: true},
		{name: "forward-zone whitespace", kind: "forward-zone", input: "   ", wantErr: true},
		{name: "forward-zone root is allowed", kind: "forward-zone", input: ".", wantErr: false},
		{name: "stub-zone empty", kind: "stub-zone", input: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := domain.Fragment{Sections: []domain.Section{{Kind: tc.kind, Entries: []domain.Entry{
				{Key: "name", Value: "test."},
				{Key: addrKeyFor(tc.kind), Value: "192.0.2.1"},
			}}}}
			m := openSpecialized(t, configModel(t, f), 0)
			m.secForm.inputs[m.secForm.nameIdx].SetValue(tc.input)

			next, cmd := stepSpecialized(t, m, "ctrl+s")
			if tc.wantErr {
				if cmd != nil {
					t.Fatal("empty name produced a submit command")
				}
				if next.secForm.err == nil || next.secForm.err.Error() != wantMsg {
					t.Fatalf("err = %v, want %q", next.secForm.err, wantMsg)
				}
				if got := nameEntries(next.frag.Sections[0]); !reflect.DeepEqual(got, []string{"test."}) {
					t.Errorf("rejected submit changed the section name: %v", got)
				}
				return
			}
			if cmd == nil {
				t.Fatalf("submit produced no command; err = %v", next.secForm.err)
			}
			got := asRoot(t, mustUpdate(t, next, cmd().(SectionFormSubmitMsg)))
			if names := nameEntries(got.frag.Sections[0]); !reflect.DeepEqual(names, []string{"."}) {
				t.Errorf("section name = %v, want the root zone %q", names, ".")
			}
		})
	}
}

// --- navigation ---

func TestSectionFormKeyNavigation(t *testing.T) {
	m := openSpecialized(t, configModel(t, forwardSection("192.0.2.53")), 0)
	if m.secForm.focusIndex != 0 {
		t.Fatalf("initial focus = %d, want 0", m.secForm.focusIndex)
	}
	next := asRoot(t, mustUpdate(t, m, key("tab")))
	if next.secForm.focusIndex != 1 {
		t.Errorf("focus after tab = %d, want 1", next.secForm.focusIndex)
	}

	last := len(next.secForm.inputs) - 1
	next.secForm.setFocus(last)
	_, cmd := next.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on the last field did not submit")
	}
	if _, ok := cmd().(SectionFormSubmitMsg); !ok {
		t.Fatalf("enter produced %T, want SectionFormSubmitMsg", cmd())
	}
}

// TestApplySectionFormKeepsStateError pins the guard mirrored from the A-chain:
// a late specialized submission must not clobber a loud projection error. The
// model stays in StateError and the fragment is untouched.
func TestApplySectionFormKeepsStateError(t *testing.T) {
	m := configModel(t, forwardSection("192.0.2.53"))
	m.state = StateError
	before := m.frag
	msg := SectionFormSubmitMsg{
		Kind: "forward-zone", SecIndex: 0, Name: "changed.", Addrs: []string{"192.0.2.99"},
	}

	next, cmd := m.applySectionForm(msg)
	got := asRoot(t, next)
	if cmd != nil {
		t.Errorf("applySectionForm produced a command, want nil on StateError")
	}
	if got.state != StateError {
		t.Errorf("state = %v, want StateError preserved", got.state)
	}
	if !reflect.DeepEqual(got.frag, before) {
		t.Errorf("fragment changed under StateError:\n got %+v\nwant %+v", got.frag, before)
	}
}

// TestSectionFormPrevWraps pins shift+tab/up walking the specialized form's
// fields backwards and wrapping at the first field.
func TestSectionFormPrevWraps(t *testing.T) {
	sf := newSectionForm(forwardSection("192.0.2.53"), 0)
	if len(sf.inputs) < 2 {
		t.Fatalf("form has %d inputs, want at least 2", len(sf.inputs))
	}
	sf, _ = sf.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if sf.focusIndex != len(sf.inputs)-1 {
		t.Fatalf("focusIndex after shift+tab = %d, want %d (wrapped)", sf.focusIndex, len(sf.inputs)-1)
	}
	sf, _ = sf.Update(tea.KeyMsg{Type: tea.KeyUp})
	if sf.focusIndex != len(sf.inputs)-2 {
		t.Fatalf("focusIndex after up = %d, want %d", sf.focusIndex, len(sf.inputs)-2)
	}
}
