package model

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// TestFormNavigationAndSubmitOnLastField pins navigation and the picker-era
// submit path: enter on the (choice) Type field opens the picker instead of
// submitting, and ctrl+s still submits the form from anywhere.
func TestFormNavigationAndSubmitOnLastField(t *testing.T) {
	f := newZoneForm()
	f.inputs[0].SetValue("example.com")
	f.inputs[1].SetValue("transparent")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyTab})
	if f.focusIndex != 1 {
		t.Fatalf("focusIndex = %d, want 1 after tab", f.focusIndex)
	}
	f, cmd := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("enter on the Type field produced %T, want the picker", cmd())
	}
	if !f.pickerOpen {
		t.Fatal("enter on the Type field did not open the picker")
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select the highlighted value
	if f.pickerOpen {
		t.Fatal("picker still open after selecting")
	}
	_, cmd = f.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("ctrl+s produced no command")
	}
	if _, ok := cmd().(FormSubmitMsg); !ok {
		t.Fatalf("ctrl+s produced %T, want FormSubmitMsg", cmd())
	}
}

func TestFormSubmitAddZone(t *testing.T) {
	f := newZoneForm()
	f.inputs[0].SetValue("example.com")
	f.inputs[1].SetValue("transparent")
	_, cmd := f.submit()
	if cmd == nil {
		t.Fatal("submit produced no command")
	}
	msg, ok := cmd().(FormSubmitMsg)
	if !ok || msg.Mode != FormAddZone || msg.Name != "example.com" || msg.Type != "transparent" {
		t.Fatalf("submit msg = %+v", cmd())
	}
}

func TestFormSubmitAddZoneRejectsBadType(t *testing.T) {
	f := newZoneForm()
	f.inputs[0].SetValue("example.com")
	f.inputs[1].SetValue("not-a-type")
	got, cmd := f.submit()
	if cmd != nil {
		t.Fatal("expected no submit command for a bad type")
	}
	if got.err == nil {
		t.Fatal("expected an inline error")
	}
}

func TestFormSubmitAddRecordRejectsBadIP(t *testing.T) {
	zone := domain.Zone{Name: "example.com.", Type: "transparent"}
	f := newRecordForm(0, zone)
	f.inputs[0].SetValue("www")
	f.inputs[1].SetValue("A")
	f.inputs[2].SetValue("not-an-ip")
	f.inputs[3].SetValue("300")
	got, cmd := f.submit()
	if cmd != nil {
		t.Fatal("expected no submit command for a bad value")
	}
	if got.err == nil {
		t.Fatal("expected an inline error")
	}
}

func TestFormSubmitAddRecord(t *testing.T) {
	zone := domain.Zone{Name: "example.com.", Type: "transparent"}
	f := newRecordForm(3, zone)
	f.inputs[0].SetValue("www")
	f.inputs[1].SetValue("a")
	f.inputs[2].SetValue("192.0.2.1")
	f.inputs[3].SetValue("300")
	_, cmd := f.submit()
	msg, ok := cmd().(FormSubmitMsg)
	if !ok || msg.Mode != FormAddRecord || msg.ZoneIndex != 3 || msg.Type != "A" {
		t.Fatalf("submit msg = %+v", cmd())
	}
}

func TestFormSubmitEditTTL(t *testing.T) {
	zone := domain.Zone{Name: "example.com.", Type: "transparent"}
	rec := domain.Record{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}
	f := newTTLForm(0, 1, zone, rec)
	f.inputs[0].SetValue("60")
	_, cmd := f.submit()
	msg, ok := cmd().(FormSubmitMsg)
	if !ok || msg.Mode != FormEditTTL || msg.TTL != 60 || msg.RecIndex != 1 {
		t.Fatalf("submit msg = %+v", cmd())
	}
}

func TestFormUpdateEscCancels(t *testing.T) {
	f := newZoneForm()
	_, cmd := f.Update(key("esc"))
	if cmd == nil {
		t.Fatal("esc produced no command")
	}
	if _, ok := cmd().(FormCancelMsg); !ok {
		t.Fatalf("esc produced %T, want FormCancelMsg", cmd())
	}
}

// TestFormPrevWraps pins shift+tab/up moving backwards through the fields and
// wrapping around at the first one.
func TestFormPrevWraps(t *testing.T) {
	f := newZoneForm()
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if f.focusIndex != 1 {
		t.Fatalf("focusIndex after shift+tab = %d, want 1 (wrapped)", f.focusIndex)
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyUp})
	if f.focusIndex != 0 {
		t.Fatalf("focusIndex after up = %d, want 0", f.focusIndex)
	}
}

// TestFormSubmitSetType pins the zone-type form: a known type emits a
// FormSetType message for the edited zone, an unknown one leaves the form open
// with an inline error, and View names the mode, the zone and the error.
func TestFormSubmitSetType(t *testing.T) {
	zone := domain.Zone{Name: "example.com.", Type: "transparent"}
	f := newTypeForm(2, zone)

	f.inputs[0].SetValue("static")
	got, cmd := f.submit()
	if cmd == nil {
		t.Fatal("valid type: submit produced no command")
	}
	msg, ok := cmd().(FormSubmitMsg)
	if !ok || msg.Mode != FormSetType || msg.ZoneIndex != 2 || msg.Type != "static" {
		t.Fatalf("submit msg = %+v", cmd())
	}
	if v := got.View(); !strings.Contains(v, "Change zone type (example.com.)") {
		t.Errorf("View = %q, want the type-form title with the zone", v)
	}

	f.inputs[0].SetValue("not-a-type")
	got, cmd = f.submit()
	if cmd != nil {
		t.Fatal("invalid type produced a submit command")
	}
	if got.err == nil {
		t.Fatal("invalid type produced no inline error")
	}
	if v := got.View(); !strings.Contains(v, "Error: unsupported zone type") {
		t.Errorf("View = %q, want the inline error", v)
	}
}

// TestFormPickerOpensFiltersAndSelects pins the searchable Type picker on the
// add-zone form: enter opens the list, typing filters it, enter commits the
// highlighted option, and ctrl+s submits.
func TestFormPickerOpensFiltersAndSelects(t *testing.T) {
	f := newZoneForm()
	f.inputs[0].SetValue("example.com")
	f, _ = f.Update(key("tab"))

	f, cmd := f.Update(key("enter"))
	if cmd != nil {
		t.Fatalf("enter on the Type field produced %T, want the picker", cmd())
	}
	if !f.pickerOpen {
		t.Fatal("enter on the Type field did not open the picker")
	}
	if v := f.View(); !strings.Contains(v, "deny") {
		t.Errorf("View = %q, want the option list", v)
	}

	for _, r := range "stat" {
		f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if shown := f.pickerShown(); len(shown) != 1 || shown[0] != "static" {
		t.Fatalf("filtered options = %v, want [static]", shown)
	}

	f, cmd = f.Update(key("enter"))
	if cmd != nil {
		t.Fatalf("selecting produced %T, want no command", cmd())
	}
	if f.pickerOpen {
		t.Fatal("picker still open after selecting")
	}
	if got := f.inputs[1].Value(); got != "static" {
		t.Fatalf("type value = %q, want static", got)
	}

	_, cmd = f.Update(key("ctrl+s"))
	msg, ok := cmd().(FormSubmitMsg)
	if !ok || msg.Mode != FormAddZone || msg.Type != "static" {
		t.Fatalf("submit msg = %+v", cmd())
	}
}

// TestFormPickerEscRestoresValue pins that esc closes the picker and brings
// the pre-open value back, and that a following esc cancels the form as before.
func TestFormPickerEscRestoresValue(t *testing.T) {
	f := newZoneForm()
	f.inputs[1].SetValue("static")
	f, _ = f.Update(key("tab"))
	f, _ = f.Update(key("enter"))
	if !f.pickerOpen {
		t.Fatal("picker did not open")
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if got := f.inputs[1].Value(); got != "t" {
		t.Fatalf("filter value = %q, want t while the picker is open", got)
	}

	f, cmd := f.Update(key("esc"))
	if cmd != nil || f.pickerOpen {
		t.Fatalf("esc: cmd = %T, pickerOpen = %v, want closed", cmd, f.pickerOpen)
	}
	if got := f.inputs[1].Value(); got != "static" {
		t.Fatalf("type value = %q, want static restored", got)
	}

	_, cmd = f.Update(key("esc"))
	if _, ok := cmd().(FormCancelMsg); !ok {
		t.Fatalf("second esc produced %T, want FormCancelMsg", cmd())
	}
}

// TestFormPickerRecordTypeSetsValueHintAndAdvances pins the add-record flow:
// picking a type advances focus to Value and updates its format placeholder.
func TestFormPickerRecordTypeSetsValueHintAndAdvances(t *testing.T) {
	f := newRecordForm(0, domain.Zone{Name: "example.com.", Type: "transparent"})
	f.inputs[0].SetValue("mail")
	f, _ = f.Update(key("tab"))

	f, _ = f.Update(key("enter"))
	if !f.pickerOpen {
		t.Fatal("picker did not open")
	}
	for _, r := range "mx" {
		f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	f, _ = f.Update(key("enter"))

	if got := f.inputs[1].Value(); got != "MX" {
		t.Fatalf("type value = %q, want MX", got)
	}
	if f.focusIndex != 2 {
		t.Fatalf("focusIndex = %d, want 2 (Value)", f.focusIndex)
	}
	if got := f.inputs[2].Placeholder; got != "10 mail.example.com" {
		t.Fatalf("Value placeholder = %q, want the MX format hint", got)
	}
}

// TestFormPickerNoMatchKeepsPickerOpen pins that enter with no matching option
// changes nothing.
func TestFormPickerNoMatchKeepsPickerOpen(t *testing.T) {
	f := newRecordForm(0, domain.Zone{Name: "example.com."})
	f, _ = f.Update(key("tab"))
	f, _ = f.Update(key("enter"))
	for _, r := range "zzz" {
		f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if shown := f.pickerShown(); len(shown) != 0 {
		t.Fatalf("options = %v, want none for zzz", shown)
	}

	f, cmd := f.Update(key("enter"))
	if cmd != nil || !f.pickerOpen {
		t.Fatalf("enter with no match: cmd = %T, pickerOpen = %v, want unchanged", cmd, f.pickerOpen)
	}
}

// TestFormPickerChangeZoneType pins the t form: the single Type field opens
// the picker and ctrl+s submits the selected type.
func TestFormPickerChangeZoneType(t *testing.T) {
	f := newTypeForm(2, domain.Zone{Name: "example.com.", Type: "transparent"})
	f, _ = f.Update(key("enter"))
	if !f.pickerOpen {
		t.Fatal("picker did not open")
	}
	for _, r := range "always_nx" {
		f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if shown := f.pickerShown(); len(shown) != 1 || shown[0] != "always_nxdomain" {
		t.Fatalf("filtered options = %v, want [always_nxdomain]", shown)
	}
	f, _ = f.Update(key("enter"))
	if f.focusIndex != 0 {
		t.Fatalf("focusIndex = %d, want 0 (only field stays focused)", f.focusIndex)
	}
	_, cmd := f.Update(key("ctrl+s"))
	msg, ok := cmd().(FormSubmitMsg)
	if !ok || msg.Mode != FormSetType || msg.Type != "always_nxdomain" {
		t.Fatalf("submit msg = %+v", cmd())
	}
}

// TestFormEnterOnNonChoiceLastFieldStillSubmits guards that plain forms keep
// the old enter-on-last-field submit.
func TestFormEnterOnNonChoiceLastFieldStillSubmits(t *testing.T) {
	zone := domain.Zone{Name: "example.com.", Type: "transparent"}
	rec := domain.Record{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}
	f := newTTLForm(0, 0, zone, rec)
	f.inputs[0].SetValue("60")
	_, cmd := f.Update(key("enter"))
	msg, ok := cmd().(FormSubmitMsg)
	if !ok || msg.Mode != FormEditTTL || msg.TTL != 60 {
		t.Fatalf("submit msg = %+v", cmd())
	}
}
