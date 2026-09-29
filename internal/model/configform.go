package model

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
	"github.com/Martin-Winfred/unbound-tui/internal/validate"
)

// directiveKeyRe is the allowed shape of a fragment directive: a bare token of
// letters, digits, underscore or hyphen. It mirrors validate.fragmentKeyRe,
// which stays unexported in that package (the model package must not reach
// into validate for its internals), and is applied to both section kinds and
// entry keys.
var directiveKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// namedSectionKinds are the section kinds identified by their `name` entry.
// Only these get a typed Name when a new section is created; it mirrors
// validate.namedKinds (unexported there).
var namedSectionKinds = map[string]bool{
	"forward-zone": true,
	"stub-zone":    true,
	"view":         true,
}

// ConfigFormSubmitMsg carries a validated Config-view form submission to the
// root model. Config forms reuse RecordForm's input machinery but own their
// submit path (see updateForm), so this is a distinct message rather than
// overloading FormSubmitMsg.
type ConfigFormSubmitMsg struct {
	Mode     FormMode
	SecIndex int
	EntIndex int
	Kind     string // section kind (FormAddSection)
	Name     string // section name (FormAddSection); empty when omitted
	Key      string // entry key (FormAddEntry/FormEditEntry)
	Value    string // entry value (FormAddEntry/FormEditEntry)
}

// configFormCtx is the Config-view target of the active form: which section
// and entry it edits, and the section kind used for the value schema hint and
// soft-check.
type configFormCtx struct {
	secIndex int
	entIndex int
	kind     string
}

// isConfigFormMode reports whether mode belongs to a Config-view form.
func isConfigFormMode(mode FormMode) bool {
	switch mode {
	case FormAddSection, FormAddEntry, FormEditEntry:
		return true
	}
	return false
}

// sectionAt returns the section at index i, or ok=false when out of range.
func (m RootModel) sectionAt(i int) (domain.Section, bool) {
	if i < 0 || i >= len(m.frag.Sections) {
		return domain.Section{}, false
	}
	return m.frag.Sections[i], true
}

// --- constructors (wire the form plus its config-view target into the root) ---

// newSectionForm opens the new-section form. Both fields are always visible;
// Name is only applied when the Kind is a named kind.
func (m *RootModel) newSectionForm() {
	m.form = RecordForm{
		mode:   FormAddSection,
		labels: []string{"Kind", "Name"},
		inputs: []*textinput.Model{
			newInput("", "server"),
			newInput("", "only for forward-zone/stub-zone/view"),
		},
	}
	m.form.inputs[0].Focus()
	m.cfgForm = configFormCtx{}
	m.state = StateForm
}

// newEntryForm opens the new-entry form for the section at secIdx. The key is
// typed by the user, so the value placeholder is the text default.
func (m *RootModel) newEntryForm(secIdx int) {
	s, ok := m.sectionAt(secIdx)
	if !ok {
		return
	}
	m.cfgForm = configFormCtx{secIndex: secIdx, kind: s.Kind}
	m.form = RecordForm{
		mode:   FormAddEntry,
		labels: []string{"Key", "Value"},
		inputs: []*textinput.Model{
			newInput("", "forward-addr"),
			// The key is still unknown, so only the text default applies.
			newInput("", schemaPlaceholder(validate.TypeText)),
		},
	}
	m.form.inputs[0].Focus()
	m.state = StateForm
}

// editEntryForm opens the edit form for the entry at [secIdx][entIdx]. The
// value placeholder reflects the schema type of the edited (kind, key) pair.
func (m *RootModel) editEntryForm(secIdx, entIdx int, e domain.Entry) {
	s, ok := m.sectionAt(secIdx)
	if !ok {
		return
	}
	m.cfgForm = configFormCtx{secIndex: secIdx, entIndex: entIdx, kind: s.Kind}
	m.form = RecordForm{
		mode:   FormEditEntry,
		labels: []string{"Key", "Value"},
		inputs: []*textinput.Model{
			newInput(e.Key, "forward-addr"),
			newInput(e.Value, schemaPlaceholder(validate.SchemaFor(s.Kind, e.Key))),
		},
	}
	m.form.inputs[0].Focus()
	m.state = StateForm
}

// schemaPlaceholder is the per-type hint shown in a value input.
func schemaPlaceholder(t validate.Type) string {
	switch t {
	case validate.TypeBool:
		return "yes|no"
	case validate.TypeInt:
		return "300"
	case validate.TypePath:
		return "/etc/ssl/certs.pem"
	case validate.TypeAddr:
		return "192.0.2.1"
	case validate.TypeCIDR:
		return "192.0.2.0/24"
	case validate.TypeRR:
		return "host.example. 300 IN A 192.0.2.1"
	case validate.TypeZone:
		return `"example." transparent`
	default: // TypeText and the empty Type
		return "value"
	}
}

// --- submit path ---

// updateForm routes a StateForm key: Config-view forms own their submit, all
// other forms use RecordForm's built-in submit.
func (m RootModel) updateForm(msg tea.Msg) (RecordForm, tea.Cmd) {
	if isConfigFormMode(m.form.mode) {
		return m.cfgFormUpdate(msg)
	}
	return m.form.Update(msg)
}

// cfgFormUpdate intercepts the submit keys (ctrl+s anywhere, enter on the last
// field) and hands everything else to RecordForm.Update for navigation and
// typing.
func (m RootModel) cfgFormUpdate(msg tea.Msg) (RecordForm, tea.Cmd) {
	f := m.form
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+s":
			return f.submitConfig(m.cfgForm)
		case "enter":
			if f.focusIndex == len(f.inputs)-1 {
				return f.submitConfig(m.cfgForm)
			}
		}
	}
	updated, cmd := f.Update(msg)
	return updated, cmd
}

// submitConfig validates the active Config-view form and, on success, emits a
// ConfigFormSubmitMsg. A failure sets f.err and leaves the form open.
func (f RecordForm) submitConfig(ctx configFormCtx) (RecordForm, tea.Cmd) {
	switch f.mode {
	case FormAddSection:
		return f.submitSection()
	case FormAddEntry:
		return f.submitEntry(ctx)
	case FormEditEntry:
		return f.submitEditEntry(ctx)
	}
	return f, nil
}

func (f RecordForm) submitSection() (RecordForm, tea.Cmd) {
	kind := strings.TrimSpace(f.inputs[0].Value())
	name := strings.TrimSpace(f.inputs[1].Value())
	if !directiveKeyRe.MatchString(kind) {
		f.err = fmt.Errorf("invalid section kind %q", kind)
		return f, nil
	}
	return f.emitConfig(ConfigFormSubmitMsg{Mode: FormAddSection, Kind: kind, Name: name})
}

func (f RecordForm) submitEntry(ctx configFormCtx) (RecordForm, tea.Cmd) {
	key, value, err := f.entryValues(ctx.kind)
	if err != nil {
		f.err = err
		return f, nil
	}
	return f.emitConfig(ConfigFormSubmitMsg{
		Mode: FormAddEntry, SecIndex: ctx.secIndex, Key: key, Value: value,
	})
}

func (f RecordForm) submitEditEntry(ctx configFormCtx) (RecordForm, tea.Cmd) {
	key, value, err := f.entryValues(ctx.kind)
	if err != nil {
		f.err = err
		return f, nil
	}
	return f.emitConfig(ConfigFormSubmitMsg{
		Mode: FormEditEntry, SecIndex: ctx.secIndex, EntIndex: ctx.entIndex, Key: key, Value: value,
	})
}

// entryValues reads and soft-checks the Key/Value fields: key shape first,
// then the locked-key guard (local-* belongs to the Local data view), then the
// schema value check.
func (f RecordForm) entryValues(kind string) (key, value string, err error) {
	key = strings.TrimSpace(f.inputs[0].Value())
	value = strings.TrimSpace(f.inputs[1].Value())
	if !directiveKeyRe.MatchString(key) {
		return "", "", fmt.Errorf("invalid key %q", key)
	}
	if isLocked(domain.Entry{Key: key}) {
		return "", "", fmt.Errorf("managed in the Local data view")
	}
	if err := validateEntryValue(kind, key, value); err != nil {
		return "", "", err
	}
	return key, value, nil
}

// validateEntryValue applies the schema soft-check to an entry value: a
// registered (kind, key) runs the typed validator; an unregistered one falls
// back to the text rules (control characters only), which ValidateValue
// already implements for TypeText.
func validateEntryValue(kind, key, value string) error {
	return validate.ValidateValue(validate.SchemaFor(kind, key), value)
}

// emitConfig clears any prior error and returns a command carrying the
// submission. It mirrors RecordForm.emit for the Config-view submit path.
func (f RecordForm) emitConfig(msg ConfigFormSubmitMsg) (RecordForm, tea.Cmd) {
	f.err = nil
	return f, func() tea.Msg { return msg }
}

// --- apply ---

// applyConfigForm folds a validated Config-view submission into frag, then
// refreshes the zones projection and marks the model dirty. regenLocal is
// deliberately NOT called: Config-view edits never touch local-* rows.
func (m RootModel) applyConfigForm(msg ConfigFormSubmitMsg) (tea.Model, tea.Cmd) {
	m.form = RecordForm{}
	m.cfgForm = configFormCtx{}
	m.state = StateReady

	switch msg.Mode {
	case FormAddSection:
		sec := domain.Section{Kind: msg.Kind}
		if namedSectionKinds[msg.Kind] && msg.Name != "" {
			sec.Entries = []domain.Entry{{Key: "name", Value: msg.Name}}
		}
		m.frag.Sections = append(m.frag.Sections, sec)
		m.cfgView.SecCursor = len(m.frag.Sections) - 1
		m.cfgView.EntCursor = 0
		m.cfgView.SecFocused = false

	case FormAddEntry:
		if msg.SecIndex < 0 || msg.SecIndex >= len(m.frag.Sections) {
			return m, nil
		}
		m.frag.Sections[msg.SecIndex].Entries = append(m.frag.Sections[msg.SecIndex].Entries,
			domain.Entry{Key: msg.Key, Value: msg.Value})
		m.cfgView.SecCursor = msg.SecIndex
		m.cfgView.EntCursor = len(m.frag.Sections[msg.SecIndex].Entries) - 1
		m.cfgView.SecFocused = false

	case FormEditEntry:
		if msg.SecIndex < 0 || msg.SecIndex >= len(m.frag.Sections) {
			return m, nil
		}
		entries := m.frag.Sections[msg.SecIndex].Entries
		if msg.EntIndex < 0 || msg.EntIndex >= len(entries) {
			return m, nil
		}
		entries[msg.EntIndex].Key = msg.Key
		entries[msg.EntIndex].Value = msg.Value

	default:
		return m, nil
	}

	m.refreshZones()
	m.dirty = true
	// A named forward-zone/stub-zone submission chains straight into the
	// specialized form on the section just created. A projection failure
	// (StateError) keeps its loud error instead of opening the form.
	if msg.Mode == FormAddSection && isSpecializedKind(msg.Kind) && m.state != StateError {
		m.secForm = newSectionForm(m.frag, len(m.frag.Sections)-1)
		m.state = StateSectionForm
	}
	return m, nil
}
