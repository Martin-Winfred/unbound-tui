package model

import (
	"strings"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

func TestForeignModelViewAndClose(t *testing.T) {
	f := newForeignModel(
		[]domain.LocalZone{{Name: "z.example.", Type: "static"}},
		[]string{"x.z.example. 300 IN A 192.0.2.1"},
		nil,
	)
	view := f.View()
	if !strings.Contains(view, "z.example.") || !strings.Contains(view, "x.z.example.") {
		t.Errorf("View missing entries:\n%s", view)
	}
	_, cmd := f.Update(key("esc"))
	if cmd == nil {
		t.Fatal("esc produced no command")
	}
	if _, ok := cmd().(ForeignCloseMsg); !ok {
		t.Fatalf("esc produced %T, want ForeignCloseMsg", cmd())
	}
}

func TestForeignModelEmpty(t *testing.T) {
	f := newForeignModel(nil, nil, nil)
	if !strings.Contains(f.View(), "None") {
		t.Errorf("empty view = %q, want a None message", f.View())
	}
}

func TestRequestForeignLoadsAndCloses(t *testing.T) {
	m, ctl := newTestModel(t)
	m.zones = []domain.Zone{{Name: "mine.example.", Type: "transparent"}}
	ctl.zones = []domain.LocalZone{{Name: "theirs.example.", Type: "static"}}
	ctl.rrs = []string{"x.theirs.example. 300 IN A 192.0.2.9"}

	next, cmd := m.Update(key("f"))
	root := asRoot(t, next)
	if root.state != StateForeign {
		t.Fatalf("state = %v, want StateForeign", root.state)
	}
	if cmd == nil {
		t.Fatal("foreign request produced no command")
	}
	root = asRoot(t, mustUpdate(t, root, cmd()))
	if !strings.Contains(root.foreign.View(), "theirs.example.") {
		t.Errorf("foreign view missing runtime entry:\n%s", root.foreign.View())
	}
}
