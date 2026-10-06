package model

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/config"
	"github.com/Martin-Winfred/unbound-tui/internal/domain"
	"github.com/Martin-Winfred/unbound-tui/internal/validate"
)

// configModel builds a model already in the Config view over the given
// fragment, with the zones projection and cursors in sync.
func configModel(t *testing.T, f domain.Fragment) RootModel {
	t.Helper()
	m, _ := newTestModel(t)
	m = setConfigFragment(t, m, f)
	m.view = ViewConfig
	return m
}

// submitFormKey sends one key to the active form and returns the resulting
// model plus the command (nil when the form stayed open, e.g. a soft error).
func submitFormKey(t *testing.T, m RootModel, k string) (RootModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(key(k))
	return asRoot(t, next), cmd
}

// nameEntries flattens the `name` entry values of a section.
func nameEntries(s domain.Section) []string {
	var out []string
	for _, e := range s.Entries {
		if e.Key == "name" {
			out = append(out, e.Value)
		}
	}
	return out
}

// --- new section ---

func TestConfigFormAddSectionFields(t *testing.T) {
	m := configModel(t, domain.Fragment{})
	m.openAddSectionForm()
	if m.state != StateForm || m.form.mode != FormAddSection {
		t.Fatalf("state/mode = %v/%v, want StateForm/FormAddSection", m.state, m.form.mode)
	}
	if len(m.form.labels) != 2 || len(m.form.inputs) != 2 {
		t.Fatalf("labels/inputs = %d/%d, want 2/2 (both fields always visible)",
			len(m.form.labels), len(m.form.inputs))
	}
	if got := m.form.inputs[1].Placeholder; got != "only for forward-zone/stub-zone/view" {
		t.Errorf("Name placeholder = %q, want the named-kinds hint", got)
	}
}

func TestConfigFormAddSectionSubmit(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		field    string
		wantName string // "" means no name entry at all
	}{
		{"named kind keeps the name", "forward-zone", ".", "."},
		{"unnamed kind drops the name", "server", "example.com", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := configModel(t, lifecycleFixture())
			before := len(m.frag.Sections)

			m.openAddSectionForm()
			m.form.inputs[0].SetValue(tc.kind)
			m.form.inputs[1].SetValue(tc.field)

			next, cmd := submitFormKey(t, m, "ctrl+s")
			if cmd == nil {
				t.Fatalf("submit produced no command; form error = %v", next.form.err)
			}
			msg, ok := cmd().(ConfigFormSubmitMsg)
			if !ok {
				t.Fatalf("submit produced %T, want ConfigFormSubmitMsg", cmd())
			}
			m = asRoot(t, mustUpdate(t, next, msg))

			if len(m.frag.Sections) != before+1 {
				t.Fatalf("sections = %d, want %d (appended at end)", len(m.frag.Sections), before+1)
			}
			got := m.frag.Sections[len(m.frag.Sections)-1]
			if got.Kind != tc.kind {
				t.Errorf("new section kind = %q, want %q", got.Kind, tc.kind)
			}
			if names := nameEntries(got); tc.wantName == "" {
				if len(names) != 0 {
					t.Errorf("name entries = %+v, want none", names)
				}
			} else if len(names) != 1 || names[0] != tc.wantName {
				t.Errorf("name entries = %+v, want one %q", names, tc.wantName)
			}
			if m.cfgView.SecCursor != len(m.frag.Sections)-1 || m.cfgView.EntCursor != 0 || m.cfgView.SecFocused {
				t.Errorf("cursor = %d/%d focused=%v, want last/0/unfocused",
					m.cfgView.SecCursor, m.cfgView.EntCursor, m.cfgView.SecFocused)
			}
			if !m.dirty {
				t.Error("dirty = false after adding a section")
			}
			want, err := config.ZonesFromFragment(m.frag)
			if err != nil {
				t.Fatalf("ZonesFromFragment: %v", err)
			}
			if !reflect.DeepEqual(m.zones, want) {
				t.Errorf("zones = %+v, want %+v (refreshed)", m.zones, want)
			}
		})
	}
}

func TestConfigFormAddSectionRejectsBadKind(t *testing.T) {
	m := configModel(t, domain.Fragment{})
	before := len(m.frag.Sections)

	m.openAddSectionForm()
	m.form.inputs[0].SetValue("foo bar")
	next, cmd := submitFormKey(t, m, "ctrl+s")

	if cmd != nil {
		t.Fatal("bad kind produced a submit command")
	}
	if next.form.err == nil || !strings.Contains(next.form.err.Error(), "invalid section kind") {
		t.Errorf("err = %v, want an invalid section kind error", next.form.err)
	}
	if next.state != StateForm {
		t.Errorf("state = %v, want StateForm (form stays open)", next.state)
	}
	if len(next.frag.Sections) != before {
		t.Error("rejected submit changed the fragment")
	}
}

// TestConfigFormAddSectionRejectsNamedKindWithoutName pins the pre-write gate's
// rule at the form boundary: a named section (forward-zone/stub-zone/view) must
// carry a name, so an empty Name field is refused instead of silently creating
// a nameless section that unbound cannot parse.
func TestConfigFormAddSectionRejectsNamedKindWithoutName(t *testing.T) {
	for _, kind := range []string{"forward-zone", "stub-zone", "view"} {
		t.Run(kind, func(t *testing.T) {
			m := configModel(t, domain.Fragment{})
			before := len(m.frag.Sections)

			m.openAddSectionForm()
			m.form.inputs[0].SetValue(kind)
			// Name field deliberately left empty.
			next, cmd := submitFormKey(t, m, "ctrl+s")

			if cmd != nil {
				t.Fatalf("named kind %q without a name produced a submit command", kind)
			}
			if next.form.err == nil || !strings.Contains(next.form.err.Error(), "name is required") {
				t.Errorf("err = %v, want a name-required error", next.form.err)
			}
			if next.state != StateForm {
				t.Errorf("state = %v, want StateForm (form stays open)", next.state)
			}
			if len(next.frag.Sections) != before {
				t.Error("rejected submit changed the fragment")
			}
		})
	}
}

// --- new entry ---

func TestConfigFormAddEntry(t *testing.T) {
	f := domain.Fragment{Sections: []domain.Section{
		{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"."`}}},
	}}
	m := configModel(t, f)
	m.cfgView = ConfigViewModel{SecCursor: 0, SecFocused: true}

	m.newEntryForm(0)
	if m.state != StateForm || m.form.mode != FormAddEntry {
		t.Fatalf("state/mode = %v/%v, want StateForm/FormAddEntry", m.state, m.form.mode)
	}
	m.form.inputs[0].SetValue("forward-addr")
	m.form.inputs[1].SetValue("192.0.2.53")

	next, cmd := submitFormKey(t, m, "ctrl+s")
	if cmd == nil {
		t.Fatalf("submit produced no command; form error = %v", next.form.err)
	}
	msg, ok := cmd().(ConfigFormSubmitMsg)
	if !ok {
		t.Fatalf("submit produced %T, want ConfigFormSubmitMsg", cmd())
	}
	m = asRoot(t, mustUpdate(t, next, msg))

	entries := m.frag.Sections[0].Entries
	if len(entries) != 2 || entries[1].Key != "forward-addr" || entries[1].Value != "192.0.2.53" {
		t.Fatalf("entries = %+v, want the new entry appended", entries)
	}
	if m.cfgView.SecCursor != 0 || m.cfgView.EntCursor != 1 || m.cfgView.SecFocused {
		t.Errorf("cursor = %d/%d focused=%v, want 0/1/unfocused",
			m.cfgView.SecCursor, m.cfgView.EntCursor, m.cfgView.SecFocused)
	}
	if !m.dirty {
		t.Error("dirty = false after adding an entry")
	}
}

// TestConfigFormAddEntryKeepsDuplicateKeys pins that key uniqueness is NOT
// enforced at the form level: multiple forward-addr rows are legal.
func TestConfigFormAddEntryKeepsDuplicateKeys(t *testing.T) {
	f := domain.Fragment{Sections: []domain.Section{
		{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: `"."`},
			{Key: "forward-addr", Value: "192.0.2.53"},
		}},
	}}
	m := configModel(t, f)

	m.newEntryForm(0)
	m.form.inputs[0].SetValue("forward-addr")
	m.form.inputs[1].SetValue("1.1.1.1")
	next, cmd := submitFormKey(t, m, "ctrl+s")
	if cmd == nil {
		t.Fatalf("duplicate key submit produced no command; err = %v", next.form.err)
	}
	m = asRoot(t, mustUpdate(t, next, cmd().(ConfigFormSubmitMsg)))

	entries := m.frag.Sections[0].Entries
	if len(entries) != 3 {
		t.Fatalf("entries = %+v, want the duplicate-key entry appended", entries)
	}
	if entries[2].Key != "forward-addr" || entries[2].Value != "1.1.1.1" {
		t.Errorf("appended entry = %+v, want forward-addr/1.1.1.1", entries[2])
	}
}

func TestConfigFormAddEntryRejects(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"bad key shape", "bad key", "x", "invalid key"},
		{"locked local-zone", "local-zone", `"example.com." static`, "managed in the Local data view"},
		{"locked local-data", "local-data", `"example.com. 300 IN A 192.0.2.1"`, "managed in the Local data view"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := configModel(t, lifecycleFixture())
			before := m.frag

			m.newEntryForm(0)
			m.form.inputs[0].SetValue(tc.key)
			m.form.inputs[1].SetValue(tc.value)
			next, cmd := submitFormKey(t, m, "ctrl+s")

			if cmd != nil {
				t.Fatal("rejected entry produced a submit command")
			}
			if next.form.err == nil || !strings.Contains(next.form.err.Error(), tc.want) {
				t.Errorf("err = %v, want containing %q", next.form.err, tc.want)
			}
			if !reflect.DeepEqual(next.frag, before) {
				t.Errorf("rejected submit changed the fragment: %+v", next.frag)
			}
		})
	}
}

// TestConfigFormRejectsEmptyEntryValue covers the submit path for +a/+e: an
// empty or whitespace-only value must set f.err and emit no submission, since
// an empty value serializes as `key: ` and reparses as a section header.
func TestConfigFormRejectsEmptyEntryValue(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"tab only", "\t"},
	}
	for _, tc := range cases {
		t.Run("add "+tc.name, func(t *testing.T) {
			m := configModel(t, lifecycleFixture())
			before := m.frag

			m.newEntryForm(0)
			m.form.inputs[0].SetValue("forward-addr")
			m.form.inputs[1].SetValue(tc.value)
			next, cmd := submitFormKey(t, m, "ctrl+s")

			if cmd != nil {
				t.Fatal("empty value produced a submit command")
			}
			if next.form.err == nil || !strings.Contains(next.form.err.Error(), "value is required") {
				t.Errorf("err = %v, want a value is required error", next.form.err)
			}
			if !reflect.DeepEqual(next.frag, before) {
				t.Errorf("rejected submit changed the fragment: %+v", next.frag)
			}
		})

		t.Run("edit "+tc.name, func(t *testing.T) {
			m := configModel(t, lifecycleFixture())
			before := m.frag

			m.editEntryForm(0, 2, m.frag.Sections[0].Entries[2])
			m.form.inputs[1].SetValue(tc.value)
			next, cmd := submitFormKey(t, m, "ctrl+s")

			if cmd != nil {
				t.Fatal("empty value produced a submit command")
			}
			if next.form.err == nil || !strings.Contains(next.form.err.Error(), "value is required") {
				t.Errorf("err = %v, want a value is required error", next.form.err)
			}
			if !reflect.DeepEqual(next.frag, before) {
				t.Errorf("rejected submit changed the fragment: %+v", next.frag)
			}
		})
	}
}

// TestValidateEntryValue pins the schema soft-check: a registered (kind,key)
// runs the typed validator (even for a key the UI normally locks), a miss
// falls back to the text rules (control characters only).
func TestValidateEntryValue(t *testing.T) {
	if err := validateEntryValue("server", "local-data", "not an rr"); err == nil {
		t.Error("server/local-data accepted an invalid RR line")
	}
	if err := validateEntryValue("server", "local-data", `"example.com. 300 IN A 192.0.2.1"`); err != nil {
		t.Errorf("server/local-data rejected a valid RR line: %v", err)
	}
	if err := validateEntryValue("server", "forward-addr", "plain value"); err != nil {
		t.Errorf("unregistered key rejected plain text: %v", err)
	}
	if err := validateEntryValue("server", "forward-addr", "bad\tcontrol"); err == nil {
		t.Error("unregistered key accepted a control character")
	}
}

func TestSchemaPlaceholder(t *testing.T) {
	cases := []struct {
		typ  validate.Type
		want string
	}{
		{validate.TypeBool, "yes|no"},
		{validate.TypeInt, "300"},
		{validate.TypePath, "/etc/ssl/certs.pem"},
		{validate.TypeAddr, "192.0.2.1"},
		{validate.TypeCIDR, "192.0.2.0/24"},
		{validate.TypeRR, "host.example. 300 IN A 192.0.2.1"},
		{validate.TypeZone, `"example." transparent`},
		{validate.TypePort, "53"},
		{validate.TypeAccessCtrl, "192.0.2.0/24 allow"},
		{validate.TypeControlAddr, "/run/unbound.ctl"},
		{validate.TypeText, "value"},
		{validate.Type(""), "value"},
	}
	for _, tc := range cases {
		if got := schemaPlaceholder(tc.typ); got != tc.want {
			t.Errorf("schemaPlaceholder(%q) = %q, want %q", tc.typ, got, tc.want)
		}
	}
}

// --- edit entry ---

func TestConfigFormEditEntryRewritesByIndex(t *testing.T) {
	f := domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{
			{Key: "port", Value: "53"},
			{Key: "edns-buffer-size", Value: "1232"},
			{Key: "hide-identity", Value: "yes"},
		}},
	}}
	m := configModel(t, f)
	m.cfgView = ConfigViewModel{SecCursor: 0, EntCursor: 1, SecFocused: false}

	m.editEntryForm(0, 1, m.frag.Sections[0].Entries[1])
	if m.form.mode != FormEditEntry {
		t.Fatalf("mode = %v, want FormEditEntry", m.form.mode)
	}
	if got := m.form.inputs[0].Value(); got != "edns-buffer-size" {
		t.Errorf("prefilled key = %q, want the edited entry's key", got)
	}
	m.form.inputs[0].SetValue("msg-buffer-size")
	m.form.inputs[1].SetValue("4096")

	next, cmd := submitFormKey(t, m, "ctrl+s")
	if cmd == nil {
		t.Fatalf("submit produced no command; err = %v", next.form.err)
	}
	msg, ok := cmd().(ConfigFormSubmitMsg)
	if !ok {
		t.Fatalf("submit produced %T, want ConfigFormSubmitMsg", cmd())
	}
	m = asRoot(t, mustUpdate(t, next, msg))

	entries := m.frag.Sections[0].Entries
	if len(entries) != 3 {
		t.Fatalf("entries = %+v, want 3 (rewrite, not append)", entries)
	}
	if entries[0].Key != "port" || entries[2].Key != "hide-identity" {
		t.Errorf("neighbouring rows changed: %+v", entries)
	}
	if entries[1].Key != "msg-buffer-size" || entries[1].Value != "4096" {
		t.Errorf("edited row = %+v, want msg-buffer-size/4096", entries[1])
	}
	if !m.dirty {
		t.Error("dirty = false after editing an entry")
	}
}

// --- form keys ---

func TestConfigFormSubmitKeyRouting(t *testing.T) {
	t.Run("ctrl+s submits from the first field", func(t *testing.T) {
		m := configModel(t, domain.Fragment{})
		m.openAddSectionForm()
		m.form.inputs[0].SetValue("server")
		next, cmd := submitFormKey(t, m, "ctrl+s")
		if cmd == nil {
			t.Fatalf("ctrl+s did not submit; err = %v", next.form.err)
		}
		if _, ok := cmd().(ConfigFormSubmitMsg); !ok {
			t.Fatalf("ctrl+s produced %T, want ConfigFormSubmitMsg", cmd())
		}
	})

	t.Run("enter on the last field submits", func(t *testing.T) {
		m := configModel(t, domain.Fragment{})
		m.openAddSectionForm()
		m.form.inputs[0].SetValue("server")
		m.form.setFocus(1)
		next, cmd := submitFormKey(t, m, "enter")
		if cmd == nil {
			t.Fatalf("enter on the last field did not submit; err = %v", next.form.err)
		}
		if _, ok := cmd().(ConfigFormSubmitMsg); !ok {
			t.Fatalf("enter produced %T, want ConfigFormSubmitMsg", cmd())
		}
	})

	t.Run("enter on a non-last field advances", func(t *testing.T) {
		m := configModel(t, domain.Fragment{})
		m.openAddSectionForm()
		next, cmd := submitFormKey(t, m, "enter")
		if cmd != nil {
			t.Fatalf("enter advanced but produced %T", cmd())
		}
		if next.form.focusIndex != 1 {
			t.Errorf("focusIndex = %d, want 1", next.form.focusIndex)
		}
	})

	t.Run("esc cancels cleanly", func(t *testing.T) {
		m := configModel(t, lifecycleFixture())
		before := m.frag
		m.newEntryForm(1)

		next, cmd := submitFormKey(t, m, "esc")
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
	})
}

// --- key routing ---

func TestConfigFormKeyRouting(t *testing.T) {
	t.Run("A opens the new-section form", func(t *testing.T) {
		m := configModel(t, lifecycleFixture())
		m = asRoot(t, mustUpdate(t, m, key("A")))
		if m.state != StateForm || m.form.mode != FormAddSection {
			t.Fatalf("state/mode = %v/%v, want StateForm/FormAddSection", m.state, m.form.mode)
		}
	})

	t.Run("a opens the new-entry form for the selected section", func(t *testing.T) {
		m := configModel(t, lifecycleFixture())
		m.cfgView.SecCursor = 1 // forward-zone
		m = asRoot(t, mustUpdate(t, m, key("a")))
		if m.state != StateForm || m.form.mode != FormAddEntry {
			t.Fatalf("state/mode = %v/%v, want StateForm/FormAddEntry", m.state, m.form.mode)
		}
		if m.cfgForm.kind != "forward-zone" || m.cfgForm.secIndex != 1 {
			t.Errorf("cfgForm = %+v, want forward-zone@1", m.cfgForm)
		}
	})

	t.Run("e opens the edit form on an unlocked row", func(t *testing.T) {
		m := configModel(t, lifecycleFixture())
		m.cfgView = ConfigViewModel{SecFocused: false, EntCursor: 2} // edns-buffer-size
		m = asRoot(t, mustUpdate(t, m, key("e")))
		if m.state != StateForm || m.form.mode != FormEditEntry {
			t.Fatalf("state/mode = %v/%v, want StateForm/FormEditEntry", m.state, m.form.mode)
		}
		if got := m.form.inputs[0].Value(); got != "edns-buffer-size" {
			t.Errorf("edit form key = %q, want the focused entry's key", got)
		}
	})
}

// --- scalar add-time warning ---

const scalarWarnFixture = "server: verbosity already set in /etc/unbound/conf.d/zz.conf — edit that file manually (see deploy.md: Conflicts and manual resolution)"

// submitEntryToServer opens the entry form on section 0 of a one-server
// fragment, types key/value and folds the submission back into the model.
func submitEntryToServer(t *testing.T, m RootModel, key, value string) RootModel {
	t.Helper()
	m.newEntryForm(0)
	m.form.inputs[0].SetValue(key)
	m.form.inputs[1].SetValue(value)
	next, cmd := submitFormKey(t, m, "ctrl+s")
	if cmd == nil {
		t.Fatalf("submit of %q produced no command; form error = %v", key, next.form.err)
	}
	msg, ok := cmd().(ConfigFormSubmitMsg)
	if !ok {
		t.Fatalf("submit produced %T, want ConfigFormSubmitMsg", cmd())
	}
	return asRoot(t, mustUpdate(t, next, msg))
}

func TestConfigFormAddEntryScalarWarning(t *testing.T) {
	f := domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "edns-buffer-size", Value: "1232"}}},
	}}
	m := configModel(t, f)
	m.cfgView = ConfigViewModel{SecCursor: 0, SecFocused: true}
	m.scalarIdx = map[[2]string][]string{{"server", "verbosity"}: {"/etc/unbound/conf.d/zz.conf"}}

	m = submitEntryToServer(t, m, "verbosity", "3")

	entries := m.frag.Sections[0].Entries
	if len(entries) != 2 || entries[1].Key != "verbosity" || entries[1].Value != "3" {
		t.Fatalf("entries = %+v, want the verbosity entry created", entries)
	}
	if !m.dirty {
		t.Error("dirty = false, want the warning to stay non-blocking")
	}
	if m.state != StateReady {
		t.Errorf("state = %v, want StateReady (no block)", m.state)
	}
	if m.notice != scalarWarnFixture {
		t.Errorf("notice = %q, want %q", m.notice, scalarWarnFixture)
	}
}

// TestConfigFormAddEntryScalarWarningExempt pins the boundaries: a repeatable
// key (access-control) and a singleton key with no foreign hit produce no
// warning.
func TestConfigFormAddEntryScalarWarningExempt(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{"repeatable access-control", "access-control", "192.0.2.0/24 allow"},
		{"singleton key with no foreign hit", "port", "53"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := domain.Fragment{Sections: []domain.Section{{Kind: "server"}}}
			m := configModel(t, f)
			m.cfgView = ConfigViewModel{SecCursor: 0, SecFocused: true}
			m.scalarIdx = map[[2]string][]string{{"server", "verbosity"}: {"/etc/unbound/conf.d/zz.conf"}}
			m = submitEntryToServer(t, m, tc.key, tc.value)
			if m.notice != "" {
				t.Errorf("notice = %q, want empty for %s", m.notice, tc.name)
			}
			if !hasEntry(m.frag, tc.key, tc.value) {
				t.Errorf("entry %q not created", tc.key)
			}
		})
	}
}

// TestConfigFormEditEntryScalarWarning covers the edit branch of the generic
// submit path.
func TestConfigFormEditEntryScalarWarning(t *testing.T) {
	f := domain.Fragment{Sections: []domain.Section{
		{Kind: "server", Entries: []domain.Entry{{Key: "verbosity", Value: "1"}}},
	}}
	m := configModel(t, f)
	m.scalarIdx = map[[2]string][]string{{"server", "verbosity"}: {"/etc/unbound/conf.d/zz.conf"}}

	m.editEntryForm(0, 0, m.frag.Sections[0].Entries[0])
	m.form.inputs[1].SetValue("3")
	next, cmd := submitFormKey(t, m, "ctrl+s")
	if cmd == nil {
		t.Fatalf("edit submit produced no command; err = %v", next.form.err)
	}
	m = asRoot(t, mustUpdate(t, next, cmd().(ConfigFormSubmitMsg)))

	if got := m.frag.Sections[0].Entries[0].Value; got != "3" {
		t.Errorf("edited value = %q, want 3", got)
	}
	if m.notice != scalarWarnFixture {
		t.Errorf("notice = %q, want %q", m.notice, scalarWarnFixture)
	}
}

// TestConfigFormEditEntryScalarWarningDisabled is the two-sided false-alarm
// regression for the submitted entry: editing a commented-out (disabled)
// singleton must not warn, because apply-time FindScalarConflicts ignores a
// disabled entry on our side too. The enabled case keeps the warning.
func TestConfigFormEditEntryScalarWarningDisabled(t *testing.T) {
	cases := []struct {
		name     string
		disabled bool
		want     string
	}{
		{"disabled entry is silent", true, ""},
		{"enabled entry still warns", false, scalarWarnFixture},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := domain.Fragment{Sections: []domain.Section{
				{Kind: "server", Entries: []domain.Entry{{Key: "verbosity", Value: "1", Disabled: tc.disabled}}},
			}}
			m := configModel(t, f)
			m.scalarIdx = map[[2]string][]string{{"server", "verbosity"}: {"/etc/unbound/conf.d/zz.conf"}}

			m.editEntryForm(0, 0, m.frag.Sections[0].Entries[0])
			m.form.inputs[1].SetValue("3")
			next, cmd := submitFormKey(t, m, "ctrl+s")
			if cmd == nil {
				t.Fatalf("edit submit produced no command; err = %v", next.form.err)
			}
			m = asRoot(t, mustUpdate(t, next, cmd().(ConfigFormSubmitMsg)))

			if m.notice != tc.want {
				t.Errorf("notice = %q, want %q", m.notice, tc.want)
			}
		})
	}
}

// TestConfigFormAddEntryScalarWarningRemoteControl pins the remote-control
// branch of scalarWarning: control-port is a singleton in a remote-control
// section, so a foreign hit warns with the same wording as the server branch.
func TestConfigFormAddEntryScalarWarningRemoteControl(t *testing.T) {
	const src = "/etc/unbound/remote-control.conf"
	f := domain.Fragment{Sections: []domain.Section{{Kind: "remote-control"}}}
	m := configModel(t, f)
	m.cfgView = ConfigViewModel{SecCursor: 0, SecFocused: true}
	m.scalarIdx = map[[2]string][]string{{"remote-control", "control-port"}: {src}}

	m = submitEntryToServer(t, m, "control-port", "8953")

	want := "remote-control: control-port already set in " + src +
		" — edit that file manually (see deploy.md: Conflicts and manual resolution)"
	if m.notice != want {
		t.Errorf("notice = %q, want %q", m.notice, want)
	}
}

// TestConfigFormAddEntryScalarWarningDeadForeign is the false-alarm
// regression: a foreign scalar that is commented out (disabled) must not
// raise the add-time warning, because apply-time FindScalarConflicts ignores
// dead entries on both sides.
func TestConfigFormAddEntryScalarWarningDeadForeign(t *testing.T) {
	f := domain.Fragment{Sections: []domain.Section{{Kind: "server"}}}
	m := configModel(t, f)
	m.cfgView = ConfigViewModel{SecCursor: 0, SecFocused: true}
	eff := config.Effective{Sections: []config.EffectiveSection{
		{Section: domain.Section{Kind: "server", Entries: []domain.Entry{
			{Key: "verbosity", Value: "1", Disabled: true},
		}}, Source: "/etc/unbound/conf.d/zz.conf"},
	}}
	m.scalarIdx = buildScalarIndex(eff, m.cfg.FragmentPath())

	m = submitEntryToServer(t, m, "verbosity", "3")

	if m.notice != "" {
		t.Errorf("notice = %q, want empty for a commented-out foreign scalar", m.notice)
	}
	if !hasEntry(m.frag, "verbosity", "3") {
		t.Error("verbosity entry not created")
	}
}
