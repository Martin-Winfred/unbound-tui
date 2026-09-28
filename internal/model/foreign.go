package model

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// ForeignModel renders the read-only view of runtime entries that our
// fragment does not own. It never mutates anything.
type ForeignModel struct {
	lines  []string
	cursor int
	err    error
}

func newForeignModel(zones []domain.LocalZone, rrs []string, err error) ForeignModel {
	f := ForeignModel{err: err}
	for _, z := range zones {
		f.lines = append(f.lines, fmt.Sprintf("[%s] %s", z.Type, z.Name))
	}
	f.lines = append(f.lines, rrs...)
	return f
}

func (f ForeignModel) Update(msg tea.Msg) (ForeignModel, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	switch key.String() {
	case "up", "k":
		if f.cursor > 0 {
			f.cursor--
		}
	case "down", "j":
		if f.cursor < len(f.lines)-1 {
			f.cursor++
		}
	case "esc", "q", "f":
		return f, func() tea.Msg { return ForeignCloseMsg{} }
	}
	return f, nil
}

func (f ForeignModel) View() string {
	var s strings.Builder
	s.WriteString("Foreign entries (read-only)\n")
	switch {
	case f.err != nil:
		fmt.Fprintf(&s, "Error: %v\n", f.err)
	case len(f.lines) == 0:
		s.WriteString("None - Unbound serves only entries from our fragment.\n")
	}
	for i, line := range f.lines {
		marker := " "
		if i == f.cursor {
			marker = ">"
		}
		fmt.Fprintf(&s, "%s %s\n", marker, line)
	}
	s.WriteString("esc/f: back\n")
	return s.String()
}

// foreignEntries subtracts our own fragment from the runtime snapshot and
// returns what is left: zones and records Unbound serves that we do not own.
func foreignEntries(zones []domain.Zone, runtimeZones []domain.LocalZone, runtimeRRs []string) ([]domain.LocalZone, []string) {
	ownedZones := make(map[string]bool)
	ownedRRs := make(map[string]bool)
	for _, z := range zones {
		if z.Disabled {
			continue
		}
		ownedZones[z.Name] = true
		for _, r := range z.Records {
			if r.Disabled {
				continue
			}
			ownedRRs[normalizeRR(domain.RRString(z.Name, r))] = true
		}
	}

	var fz []domain.LocalZone
	for _, z := range runtimeZones {
		if !ownedZones[z.Name] {
			fz = append(fz, z)
		}
	}
	var fr []string
	for _, line := range runtimeRRs {
		if !ownedRRs[normalizeRR(line)] {
			fr = append(fr, line)
		}
	}
	return fz, fr
}

// normalizeRR collapses whitespace runs so comparisons tolerate unbound's
// own spacing of list_local_data lines.
func normalizeRR(line string) string {
	return strings.Join(strings.Fields(line), " ")
}
