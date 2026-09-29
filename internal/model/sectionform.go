package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/config"
	"github.com/Martin-Winfred/unbound-tui/internal/domain"
	"github.com/Martin-Winfred/unbound-tui/internal/validate"
)

// isSpecializedKind reports whether a section kind gets the specialized
// form. Only forward-zone and stub-zone do; everything else stays on the
// generic editor.
func isSpecializedKind(kind string) bool {
	return kind == "forward-zone" || kind == "stub-zone"
}

// sectionManagedKeys returns the (address, tls/prime, first) entry keys the
// specialized form owns for a section kind. The forward keys are the default
// so a form only ever built for a specialized kind is always coherent.
func sectionManagedKeys(kind string) (addrKey, tlsUpKey, firstKey string) {
	if kind == "stub-zone" {
		return "stub-addr", "stub-prime", "stub-first"
	}
	return "forward-addr", "forward-tls-upstream", "forward-first"
}

// SectionForm is the specialized editor for a forward-zone/stub-zone section.
// It owns the section's managed keys (name, addresses, booleans) and leaves
// every unmanaged directive byte-identical in place. Inputs are pointers so
// rune edits survive the value-receiver Update copies.
type SectionForm struct {
	kind   string
	secIdx int

	labels     []string
	inputs     []*textinput.Model
	focusIndex int
	err        error

	nameIdx  int
	addrLo   int // first address input (inclusive)
	addrHi   int // one past the last address input (exclusive)
	tlsUpIdx int
	firstIdx int
}

// SectionFormSubmitMsg carries a validated specialized-form submission to the
// root model, which rebuilds the section's managed keys and raises the
// non-blocking add-time conflict warning.
type SectionFormSubmitMsg struct {
	Kind     string
	SecIndex int
	Name     string
	Addrs    []string
	TLSUp    string
	First    string
}

// newSectionForm builds the specialized form for section secIdx of f. Fields
// are linear: name, then one address input per existing managed address plus
// one trailing empty input, then the two booleans. An out-of-range index
// yields an empty form.
func newSectionForm(f domain.Fragment, secIdx int) SectionForm {
	sf := SectionForm{secIdx: secIdx}
	if secIdx < 0 || secIdx >= len(f.Sections) {
		return sf
	}
	s := f.Sections[secIdx]
	sf.kind = s.Kind
	addrKey, tlsKey, firstKey := sectionManagedKeys(s.Kind)

	var name string
	var addrs []string
	var tlsUp, first string
	seenName, seenTLS, seenFirst := false, false, false
	for _, e := range s.Entries {
		switch e.Key {
		case "name":
			if !seenName {
				name = e.Value
				seenName = true
			}
		case addrKey:
			addrs = append(addrs, e.Value)
		case tlsKey:
			if !seenTLS {
				tlsUp = e.Value
				seenTLS = true
			}
		case firstKey:
			if !seenFirst {
				first = e.Value
				seenFirst = true
			}
		}
	}

	sf.nameIdx = len(sf.inputs)
	sf.inputs = append(sf.inputs, newInput(name, `example.com (or "." for root)`))
	sf.labels = append(sf.labels, "Name")

	sf.addrLo = len(sf.inputs)
	for _, a := range addrs {
		sf.inputs = append(sf.inputs, newInput(a, "192.0.2.53"))
		sf.labels = append(sf.labels, fmt.Sprintf("Address %d", len(sf.labels)))
	}
	// The trailing empty line is how the user adds one more address.
	sf.inputs = append(sf.inputs, newInput("", "192.0.2.53"))
	sf.labels = append(sf.labels, fmt.Sprintf("Address %d", len(addrs)+1))
	sf.addrHi = len(sf.inputs)

	sf.tlsUpIdx = len(sf.inputs)
	sf.inputs = append(sf.inputs, newInput(tlsUp, "yes|no"))
	sf.labels = append(sf.labels, tlsKey)

	sf.firstIdx = len(sf.inputs)
	sf.inputs = append(sf.inputs, newInput(first, "yes|no"))
	sf.labels = append(sf.labels, firstKey)

	sf.inputs[0].Focus()
	return sf
}

// Update routes keys exactly like RecordForm: esc cancels, tab/up/down walk
// the fields, enter submits from the last field, ctrl+s submits anywhere.
func (f SectionForm) Update(msg tea.Msg) (SectionForm, tea.Cmd) {
	if len(f.inputs) == 0 {
		return f, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return f, func() tea.Msg { return FormCancelMsg{} }
		case "tab", "down":
			f.next()
			return f, nil
		case "shift+tab", "up":
			f.prev()
			return f, nil
		case "enter":
			if f.focusIndex == len(f.inputs)-1 {
				return f.submit()
			}
			f.next()
			return f, nil
		case "ctrl+s":
			return f.submit()
		}
	}
	updated, cmd := f.inputs[f.focusIndex].Update(msg)
	*f.inputs[f.focusIndex] = updated
	return f, cmd
}

// submit soft-checks every field and, on success, emits a
// SectionFormSubmitMsg. A failure sets f.err and leaves the form open: the name
// is required (the explicit "." is how the root zone is targeted; an omitted
// name would silently rebuild the section as the root zone), addresses name the
// offending 1-based line, booleans must pass TypeBool, and an empty boolean
// means the directive is absent.
func (f SectionForm) submit() (SectionForm, tea.Cmd) {
	name := strings.TrimSpace(f.inputs[f.nameIdx].Value())
	if name == "" {
		f.err = errors.New(`name is required — use "." for the root zone`)
		return f, nil
	}
	if err := validate.ValidateValue(validate.TypeText, name); err != nil {
		f.err = err
		return f, nil
	}

	addrs := make([]string, 0, f.addrHi-f.addrLo)
	for i := f.addrLo; i < f.addrHi; i++ {
		line := strings.TrimSpace(f.inputs[i].Value())
		if line == "" {
			continue
		}
		if err := validate.ValidateValue(validate.TypeUpstream, line); err != nil {
			f.err = fmt.Errorf("line %d: %w", i-f.addrLo+1, err)
			return f, nil
		}
		addrs = append(addrs, line)
	}

	tlsUp, err := f.optionalBool(f.tlsUpIdx)
	if err != nil {
		f.err = err
		return f, nil
	}
	first, err := f.optionalBool(f.firstIdx)
	if err != nil {
		f.err = err
		return f, nil
	}

	f.err = nil
	msg := SectionFormSubmitMsg{
		Kind: f.kind, SecIndex: f.secIdx,
		Name: name, Addrs: addrs, TLSUp: tlsUp, First: first,
	}
	return f, func() tea.Msg { return msg }
}

// optionalBool reads a boolean field: empty means the directive is absent,
// otherwise the value must pass TypeBool.
func (f SectionForm) optionalBool(idx int) (string, error) {
	v := strings.TrimSpace(f.inputs[idx].Value())
	if v == "" {
		return "", nil
	}
	if err := validate.ValidateValue(validate.TypeBool, v); err != nil {
		return "", err
	}
	return v, nil
}

func (f *SectionForm) setFocus(i int) {
	f.inputs[f.focusIndex].Blur()
	f.focusIndex = i
	f.inputs[i].Focus()
}

func (f *SectionForm) next() { f.setFocus((f.focusIndex + 1) % len(f.inputs)) }
func (f *SectionForm) prev() { f.setFocus((f.focusIndex - 1 + len(f.inputs)) % len(f.inputs)) }

// View renders the form: title, labeled inputs, inline error, hint line.
func (f SectionForm) View() string {
	var s strings.Builder
	switch f.kind {
	case "stub-zone":
		s.WriteString("Stub zone")
	default:
		s.WriteString("Forward zone")
	}
	s.WriteString("\n")
	for i, label := range f.labels {
		marker := " "
		if i == f.focusIndex {
			marker = ">"
		}
		fmt.Fprintf(&s, "%s %s: %s\n", marker, label, f.inputs[i].View())
	}
	if f.err != nil {
		fmt.Fprintf(&s, "Error: %v\n", f.err)
	}
	s.WriteString("tab: next field · enter/ctrl+s: submit · esc: cancel\n")
	return s.String()
}

// rebuildSection strips the section's managed keys (kind-dependent) and
// appends the regenerated block in order name -> addresses -> booleans. Every
// unmanaged entry keeps its relative order and position, so it ends up before
// the appended managed block byte-identical. An empty name emits no name entry
// (the form gate in submit rejects it, so this only matters to direct callers);
// an empty boolean emits no boolean entry.
//
// Disabled is carried through so a specialize submit never silently re-enables
// a disabled section: the regenerated name and booleans inherit the Disabled
// flag of the first existing entry with the same key, and an address inherits
// it only when an existing managed address has the identical value (changed
// and newly added addresses are active).
func rebuildSection(s *domain.Section, kind, name string, addrs []string, tlsUp, first string) {
	addrKey, tlsKey, firstKey := sectionManagedKeys(kind)
	managed := map[string]bool{
		"name": true, addrKey: true, tlsKey: true, firstKey: true,
	}

	// Capture the pre-rebuild flags of the entries we are about to replace.
	nameDisabled := false
	nameSeen := false
	boolDisabled := map[string]bool{} // tlsKey/firstKey -> first occurrence's flag
	boolSeen := map[string]bool{}
	addrDisabled := map[string]bool{} // addr value -> first matching entry's flag
	addrSeen := map[string]bool{}

	kept := make([]domain.Entry, 0, len(s.Entries)+len(addrs)+3)
	for _, e := range s.Entries {
		if !managed[e.Key] {
			kept = append(kept, e)
			continue
		}
		switch e.Key {
		case "name":
			if !nameSeen {
				nameDisabled = e.Disabled
				nameSeen = true
			}
		case addrKey:
			if !addrSeen[e.Value] {
				addrDisabled[e.Value] = e.Disabled
				addrSeen[e.Value] = true
			}
		default: // tlsKey, firstKey
			if !boolSeen[e.Key] {
				boolDisabled[e.Key] = e.Disabled
				boolSeen[e.Key] = true
			}
		}
	}

	if name != "" {
		kept = append(kept, domain.Entry{Key: "name", Value: name, Disabled: nameDisabled})
	}
	for _, a := range addrs {
		kept = append(kept, domain.Entry{Key: addrKey, Value: a, Disabled: addrDisabled[a]})
	}
	if tlsUp != "" {
		kept = append(kept, domain.Entry{Key: tlsKey, Value: tlsUp, Disabled: boolDisabled[tlsKey]})
	}
	if first != "" {
		kept = append(kept, domain.Entry{Key: firstKey, Value: first, Disabled: boolDisabled[firstKey]})
	}
	s.Entries = kept
}

// applySectionForm folds a validated specialized submission into frag, then
// refreshes the zones projection, marks the model dirty and raises the
// non-blocking add-time warning when the submitted identity already exists in
// a live foreign forward/stub section.
func (m RootModel) applySectionForm(msg SectionFormSubmitMsg) (tea.Model, tea.Cmd) {
	// A projection failure elsewhere (StateError) is louder than a late
	// specialized submission; keep it instead of folding over it. Mirrors the
	// A-chain guard in applyConfigForm.
	if m.state == StateError {
		return m, nil
	}
	m.form = RecordForm{}
	m.secForm = SectionForm{}
	m.state = StateReady

	if msg.SecIndex < 0 || msg.SecIndex >= len(m.frag.Sections) {
		return m, nil
	}
	sec := &m.frag.Sections[msg.SecIndex]
	rebuildSection(sec, msg.Kind, msg.Name, msg.Addrs, msg.TLSUp, msg.First)
	m.refreshZones()
	m.dirty = true

	name := config.SectionKeyName(*sec)
	for _, row := range m.upstreams {
		if row.Kind == msg.Kind && !row.Dead && strings.EqualFold(row.Name, name) {
			m.notice = fmt.Sprintf("%s %s already exists in %s — apply will be refused",
				row.Kind, row.Name, row.Source)
			break
		}
	}
	return m, nil
}
