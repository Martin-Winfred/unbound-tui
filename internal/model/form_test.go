package model

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

func TestFormNavigationAndSubmitOnLastField(t *testing.T) {
	f := newZoneForm()
	f.inputs[0].SetValue("example.com")
	f.inputs[1].SetValue("transparent")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyTab})
	if f.focusIndex != 1 {
		t.Fatalf("focusIndex = %d, want 1 after tab", f.focusIndex)
	}
	_, cmd := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on the last field produced no command")
	}
	if _, ok := cmd().(FormSubmitMsg); !ok {
		t.Fatalf("enter produced %T, want FormSubmitMsg", cmd())
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
