package model

import (
	"fmt"
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
}

func newInput(prefill, placeholder string) *textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.SetValue(prefill)
	return &ti
}

// newZoneForm builds the add-zone form.
func newZoneForm() RecordForm {
	f := RecordForm{
		mode:   FormAddZone,
		labels: []string{"Zone name", "Type"},
		inputs: []*textinput.Model{
			newInput("", "example.com"),
			newInput("transparent", "transparent"),
		},
	}
	f.inputs[0].Focus()
	return f
}

// newRecordForm builds the add-record form for a zone.
func newRecordForm(zoneIndex int, zone domain.Zone) RecordForm {
	f := RecordForm{
		mode:      FormAddRecord,
		labels:    []string{"Name", "Type", "Value", "TTL"},
		zoneIndex: zoneIndex,
		zone:      zone,
		inputs: []*textinput.Model{
			newInput("", "www (or @ for apex)"),
			newInput("", "A"),
			newInput("", "192.0.2.1"),
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

// newTypeForm builds the zone-type form for a zone.
func newTypeForm(zoneIndex int, zone domain.Zone) RecordForm {
	f := RecordForm{
		mode:      FormSetType,
		labels:    []string{"Type"},
		zoneIndex: zoneIndex,
		zone:      zone,
		inputs:    []*textinput.Model{newInput(zone.Type, "")},
	}
	f.inputs[0].Focus()
	return f
}

// Update routes keys: esc cancels, tab/up/down walk the fields, enter submits
// from the last field, ctrl+s submits anywhere.
func (f RecordForm) Update(msg tea.Msg) (RecordForm, tea.Cmd) {
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
	}
	if f.err != nil {
		fmt.Fprintf(&s, "Error: %v\n", f.err)
	}
	s.WriteString("tab: next field · enter/ctrl+s: submit · esc: cancel\n")
	return s.String()
}
