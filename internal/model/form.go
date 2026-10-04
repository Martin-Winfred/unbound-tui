package model

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/config"
	"github.com/Martin-Winfred/unbound-tui/internal/domain"
	"github.com/Martin-Winfred/unbound-tui/internal/validate"
)

// FormMode selects which operation a RecordForm edits.
type FormMode int

const (
	FormAddZone FormMode = iota
	FormAddRecord
	FormEditTTL
	FormSetType
	// Config-view forms. They reuse RecordForm's inputs and key
	// handling but keep their own submit path (see configform.go).
	FormAddSection
	FormAddEntry
	FormEditEntry
)

// RecordForm is the input form. Inputs are pointers so rune edits survive
// the value-receiver Update copies.
type RecordForm struct {
	mode       FormMode
	labels     []string
	inputs     []*textinput.Model
	focusIndex int
	err        error

	zoneIndex int
	recIndex  int
	zone      domain.Zone
	record    domain.Record

	// choices maps a field index to the options a Type-style field offers.
	// A field with choices opens a filterable picker on enter instead of
	// advancing or submitting.
	choices map[int][]string

	// picker state; only meaningful while pickerOpen. The focused input then
	// holds the filter text and pickerPrev keeps the value to restore on esc.
	pickerOpen        bool
	pickerField       int
	pickerCursor      int
	pickerPrev        string
	pickerPlaceholder string
}

func newInput(prefill, placeholder string) *textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.SetValue(prefill)
	return &ti
}

// recordValueHints maps a record type to the Value placeholder shown after
// the type is picked in the add-record form. It is a hint only; the validator
// remains the authority on the value.
var recordValueHints = map[string]string{
	"A":     "192.0.2.1",
	"AAAA":  "2001:db8::1",
	"CNAME": "target.example.com",
	"PTR":   "target.example.com",
	"NS":    "ns.example.com",
	"MX":    "10 mail.example.com",
	"TXT":   "v=spf1 -all",
	"SRV":   "10 60 5060 sip.example.com",
}

// recordValueHint returns the Value placeholder for a record type.
func recordValueHint(rtype string) string {
	if h, ok := recordValueHints[rtype]; ok {
		return h
	}
	return "value"
}

// newZoneForm builds the add-zone form. The Type field is a picker over the
// canonical local-zone types.
func newZoneForm() RecordForm {
	f := RecordForm{
		mode:    FormAddZone,
		labels:  []string{"Zone name", "Type"},
		choices: map[int][]string{1: domain.ZoneTypeNames()},
		inputs: []*textinput.Model{
			newInput("", "example.com"),
			newInput("transparent", "transparent"),
		},
	}
	f.inputs[0].Focus()
	return f
}

// newRecordForm builds the add-record form for a zone. The Type field is a
// picker over the supported record types.
func newRecordForm(zoneIndex int, zone domain.Zone) RecordForm {
	f := RecordForm{
		mode:      FormAddRecord,
		labels:    []string{"Name", "Type", "Value", "TTL"},
		zoneIndex: zoneIndex,
		zone:      zone,
		choices:   map[int][]string{1: validate.RecordTypeNames()},
		inputs: []*textinput.Model{
			newInput("", "www (or @ for apex)"),
			newInput("", "A"),
			newInput("", "value"),
			newInput("300", "300"),
		},
	}
	f.inputs[0].Focus()
	return f
}

// newTTLForm builds the TTL edit form for a record.
func newTTLForm(zoneIndex, recIndex int, zone domain.Zone, rec domain.Record) RecordForm {
	f := RecordForm{
		mode:      FormEditTTL,
		labels:    []string{"TTL"},
		zoneIndex: zoneIndex,
		recIndex:  recIndex,
		zone:      zone,
		record:    rec,
		inputs:    []*textinput.Model{newInput(strconv.Itoa(rec.TTL), "")},
	}
	f.inputs[0].Focus()
	return f
}

// newTypeForm builds the zone-type form for a zone. The Type field is a
// picker over the canonical local-zone types.
func newTypeForm(zoneIndex int, zone domain.Zone) RecordForm {
	f := RecordForm{
		mode:      FormSetType,
		labels:    []string{"Type"},
		zoneIndex: zoneIndex,
		zone:      zone,
		choices:   map[int][]string{0: domain.ZoneTypeNames()},
		inputs:    []*textinput.Model{newInput(zone.Type, "")},
	}
	f.inputs[0].Focus()
	return f
}

// Update routes keys: esc cancels, tab/up/down walk the fields, enter opens the
// picker on a choice field (say the Type field) and otherwise submits from the
// last field, ctrl+s submits anywhere.
func (f RecordForm) Update(msg tea.Msg) (RecordForm, tea.Cmd) {
	if len(f.inputs) == 0 {
		return f, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		if f.pickerOpen {
			return f.updatePicker(key)
		}
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
			if len(f.choicesFor(f.focusIndex)) > 0 {
				return f.openPicker()
			}
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

// pickerRows is how many options the picker shows at once.
const pickerRows = 8

// choicesFor returns the options of field i, or nil when the field is a plain
// text field.
func (f RecordForm) choicesFor(i int) []string {
	if f.choices == nil {
		return nil
	}
	return f.choices[i]
}

// openPicker turns the focused choice field into a filter box: the current
// value is stashed for esc, the filter starts empty, and the highlight starts
// on the current value when it is one of the options.
func (f RecordForm) openPicker() (RecordForm, tea.Cmd) {
	opts := f.choicesFor(f.focusIndex)
	if len(opts) == 0 {
		return f, nil
	}
	f.pickerOpen = true
	f.pickerField = f.focusIndex
	f.pickerPrev = f.inputs[f.focusIndex].Value()
	f.pickerPlaceholder = f.inputs[f.focusIndex].Placeholder
	f.pickerCursor = 0
	if i := slices.Index(opts, f.pickerPrev); i >= 0 {
		f.pickerCursor = i
	}
	f.inputs[f.focusIndex].SetValue("")
	f.inputs[f.focusIndex].Placeholder = "type to filter"
	return f, nil
}

// pickerShown returns the options matching the current filter. While the
// picker is open the focused input holds the filter text, not the value.
func (f RecordForm) pickerShown() []string {
	opts := f.choicesFor(f.pickerField)
	q := strings.ToLower(strings.TrimSpace(f.inputs[f.pickerField].Value()))
	if q == "" {
		return opts
	}
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		if strings.Contains(strings.ToLower(o), q) {
			out = append(out, o)
		}
	}
	return out
}

// pickerApply commits the highlighted option: the field gets the value, focus
// advances when there is a next field, and the add-record form points the
// Value placeholder at the chosen type's format.
func (f RecordForm) pickerApply() (RecordForm, tea.Cmd) {
	shown := f.pickerShown()
	if len(shown) == 0 {
		return f, nil
	}
	val := shown[f.pickerCursor]
	f.pickerOpen = false
	f.inputs[f.pickerField].SetValue(val)
	f.inputs[f.pickerField].Placeholder = f.pickerPlaceholder
	if f.mode == FormAddRecord && f.pickerField == 1 {
		f.inputs[2].Placeholder = recordValueHint(val)
	}
	if f.pickerField < len(f.inputs)-1 {
		f.setFocus(f.pickerField + 1)
	}
	return f, nil
}

// pickerCancel closes the picker and restores the pre-open value.
func (f RecordForm) pickerCancel() (RecordForm, tea.Cmd) {
	f.pickerOpen = false
	f.inputs[f.pickerField].SetValue(f.pickerPrev)
	f.inputs[f.pickerField].Placeholder = f.pickerPlaceholder
	return f, nil
}

// updatePicker routes keys while the picker is open: esc cancels, enter/tab
// commit, up/down and ctrl+u/ctrl+d move the highlight, and everything else
// edits the filter.
func (f RecordForm) updatePicker(key tea.KeyMsg) (RecordForm, tea.Cmd) {
	switch key.String() {
	case "esc":
		return f.pickerCancel()
	case "enter", "tab":
		return f.pickerApply()
	case "up":
		if f.pickerCursor > 0 {
			f.pickerCursor--
		}
		return f, nil
	case "down":
		if n := len(f.pickerShown()) - 1; f.pickerCursor < n {
			f.pickerCursor++
		}
		return f, nil
	case "ctrl+u":
		f.pickerCursor -= pickerRows
		if f.pickerCursor < 0 {
			f.pickerCursor = 0
		}
		return f, nil
	case "ctrl+d":
		f.pickerCursor += pickerRows
		if n := len(f.pickerShown()) - 1; f.pickerCursor > n {
			f.pickerCursor = n
			if f.pickerCursor < 0 {
				f.pickerCursor = 0
			}
		}
		return f, nil
	}
	updated, cmd := f.inputs[f.pickerField].Update(key)
	*f.inputs[f.pickerField] = updated
	f.pickerCursor = 0
	return f, cmd
}

func (f RecordForm) submit() (RecordForm, tea.Cmd) {
	switch f.mode {
	case FormAddZone:
		return f.submitZone()
	case FormAddRecord:
		return f.submitRecord()
	case FormEditTTL:
		return f.submitTTL()
	case FormSetType:
		return f.submitType()
	}
	return f, nil
}

func (f RecordForm) submitZone() (RecordForm, tea.Cmd) {
	zone := strings.TrimSpace(f.inputs[0].Value())
	typ := strings.TrimSpace(f.inputs[1].Value())
	if err := validate.ValidateZoneName(zone); err != nil {
		f.err = err
		return f, nil
	}
	if !config.IsZoneTypeName(typ) {
		f.err = fmt.Errorf("unsupported zone type %q", typ)
		return f, nil
	}
	return f.emit(FormSubmitMsg{Mode: FormAddZone, Name: zone, Type: typ})
}

func (f RecordForm) submitRecord() (RecordForm, tea.Cmd) {
	ttl, err := strconv.Atoi(strings.TrimSpace(f.inputs[3].Value()))
	if err != nil {
		f.err = fmt.Errorf("TTL %q is not a number", f.inputs[3].Value())
		return f, nil
	}
	rec := domain.Record{
		Name:  strings.TrimSpace(f.inputs[0].Value()),
		RType: strings.ToUpper(strings.TrimSpace(f.inputs[1].Value())),
		Value: strings.TrimSpace(f.inputs[2].Value()),
		TTL:   ttl,
	}
	if err := validate.ValidateRecord(f.zone.Name, rec); err != nil {
		f.err = err
		return f, nil
	}
	return f.emit(FormSubmitMsg{
		Mode: FormAddRecord, ZoneIndex: f.zoneIndex,
		Name: rec.Name, Type: rec.RType, Value: rec.Value, TTL: rec.TTL,
	})
}

func (f RecordForm) submitTTL() (RecordForm, tea.Cmd) {
	ttl, err := strconv.Atoi(strings.TrimSpace(f.inputs[0].Value()))
	if err != nil {
		f.err = fmt.Errorf("TTL %q is not a number", f.inputs[0].Value())
		return f, nil
	}
	check := f.record
	check.TTL = ttl
	if err := validate.ValidateRecord(f.zone.Name, check); err != nil {
		f.err = err
		return f, nil
	}
	return f.emit(FormSubmitMsg{Mode: FormEditTTL, ZoneIndex: f.zoneIndex, RecIndex: f.recIndex, TTL: ttl})
}

func (f RecordForm) submitType() (RecordForm, tea.Cmd) {
	typ := strings.TrimSpace(f.inputs[0].Value())
	if !config.IsZoneTypeName(typ) {
		f.err = fmt.Errorf("unsupported zone type %q", typ)
		return f, nil
	}
	return f.emit(FormSubmitMsg{Mode: FormSetType, ZoneIndex: f.zoneIndex, Type: typ})
}

func (f RecordForm) emit(msg FormSubmitMsg) (RecordForm, tea.Cmd) {
	f.err = nil
	return f, func() tea.Msg { return msg }
}

func (f *RecordForm) setFocus(i int) {
	f.inputs[f.focusIndex].Blur()
	f.focusIndex = i
	f.inputs[i].Focus()
}

func (f *RecordForm) next() { f.setFocus((f.focusIndex + 1) % len(f.inputs)) }
func (f *RecordForm) prev() { f.setFocus((f.focusIndex - 1 + len(f.inputs)) % len(f.inputs)) }

func (f RecordForm) title() string {
	switch f.mode {
	case FormAddZone:
		return "New zone"
	case FormAddRecord:
		return "New record"
	case FormEditTTL:
		return "Edit TTL"
	case FormSetType:
		return "Change zone type"
	case FormAddSection:
		return "New section"
	case FormAddEntry:
		return "New entry"
	case FormEditEntry:
		return "Edit entry"
	}
	return "Form"
}

// View renders the form: title, labeled inputs, inline error, hint line.
func (f RecordForm) View() string {
	var s strings.Builder
	s.WriteString(f.title())
	if f.mode == FormAddRecord || f.mode == FormEditTTL || f.mode == FormSetType {
		s.WriteString(fmt.Sprintf(" (%s)", f.zone.Name))
	}
	s.WriteString("\n")
	for i, label := range f.labels {
		marker := " "
		if i == f.focusIndex {
			marker = ">"
		}
		fmt.Fprintf(&s, "%s %s: %s\n", marker, label, f.inputs[i].View())
		if f.pickerOpen && i == f.pickerField {
			s.WriteString(f.pickerList())
		}
	}
	if f.err != nil {
		fmt.Fprintf(&s, "Error: %v\n", f.err)
	}
	s.WriteString(f.hint())
	return s.String()
}

// pickerList renders the visible options under the open picker's field,
// windowed around the highlight.
func (f RecordForm) pickerList() string {
	shown := f.pickerShown()
	if len(shown) == 0 {
		return "    (no match)\n"
	}
	start, end := window(len(shown), f.pickerCursor, pickerRows)
	var s strings.Builder
	for i := start; i < end; i++ {
		if i == f.pickerCursor {
			fmt.Fprintf(&s, "  > %s\n", shown[i])
			continue
		}
		fmt.Fprintf(&s, "    %s\n", shown[i])
	}
	return s.String()
}

// hint returns the key hints for the current state.
func (f RecordForm) hint() string {
	switch {
	case f.pickerOpen:
		return "type: filter · up/down: move · enter: select · esc: close\n"
	case len(f.choicesFor(f.focusIndex)) > 0:
		return "enter: choose · tab: next field · ctrl+s: submit · esc: cancel\n"
	default:
		return "tab: next field · enter/ctrl+s: submit · esc: cancel\n"
	}
}
