package model

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// ConfigViewModel is the cursor state of the generic Config view. The data it
// navigates is RootModel.frag; this is only the position within it.
type ConfigViewModel struct {
	SecCursor, EntCursor int
	SecFocused           bool // true = left pane (sections)
}

// isLocked reports whether an entry is owned by the Local data view. Locked
// rows render read-only in the Config view and never light the disabled
// marker; Task 6 reuses this to skip mutating keys on them.
func isLocked(e domain.Entry) bool {
	return e.Key == "local-zone" || e.Key == "local-data"
}

// sectionLabel renders one section-list row: the header kind, the section's
// name value when it has one, its entry count and the all-disabled marker.
func sectionLabel(s domain.Section) string {
	if s.Kind == "" {
		return "(top level)"
	}
	label := s.Kind
	if name, ok := sectionName(s); ok {
		label += " " + name
	}
	label += " · " + entryCount(len(s.Entries))
	if allDisabled(s) {
		label += " · ⛔"
	}
	return label
}

// entryLabel renders one entry-list row: "key: value", a ⛔ marker when the
// entry is disabled, and the locked marker for local-* rows.
func entryLabel(e domain.Entry) string {
	label := e.Key + ": " + e.Value
	if e.Disabled {
		label += " · ⛔"
	}
	if isLocked(e) {
		label += " (locked · edit in Local data)"
	}
	return label
}

// sectionName returns the section's display name: the value of its "name"
// entry (the identity directive of forward-zone/stub-zone/view), if present.
func sectionName(s domain.Section) (string, bool) {
	for _, e := range s.Entries {
		if e.Key == "name" {
			return e.Value, true
		}
	}
	return "", false
}

// allDisabled reports whether the section has at least one non-locked entry
// and every non-locked entry is disabled. Locked rows are managed in the
// Local data view and never light the marker.
func allDisabled(s domain.Section) bool {
	seen := false
	for _, e := range s.Entries {
		if isLocked(e) {
			continue
		}
		seen = true
		if !e.Disabled {
			return false
		}
	}
	return seen
}

// entryCount pluralizes an entry count for a section label.
func entryCount(n int) string {
	if n == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", n)
}

// --- Config-view cursor movement ---

// focusedSection returns the section under the section cursor.
func (m RootModel) focusedSection() (domain.Section, bool) {
	if m.cfgView.SecCursor < 0 || m.cfgView.SecCursor >= len(m.frag.Sections) {
		return domain.Section{}, false
	}
	return m.frag.Sections[m.cfgView.SecCursor], true
}

// focusedSectionEntryCount is the entry count of the section under the cursor,
// or zero when the section list is empty.
func (m RootModel) focusedSectionEntryCount() int {
	if s, ok := m.focusedSection(); ok {
		return len(s.Entries)
	}
	return 0
}

// clampCfgCursors keeps both Config cursors inside their panes and keeps the
// entry cursor inside the section under the section cursor. It is safe on
// empty panes (both cursors settle on the zero value).
func (m *RootModel) clampCfgCursors() {
	if m.cfgView.SecCursor >= len(m.frag.Sections) {
		m.cfgView.SecCursor = len(m.frag.Sections) - 1
	}
	if m.cfgView.SecCursor < 0 {
		m.cfgView.SecCursor = 0
	}
	if n := m.focusedSectionEntryCount(); m.cfgView.EntCursor >= n {
		m.cfgView.EntCursor = n - 1
	}
	if m.cfgView.EntCursor < 0 {
		m.cfgView.EntCursor = 0
	}
}

// cfgMove moves the focused pane's cursor by delta and re-clamps, so moving
// the section cursor onto a shorter section pulls the entry cursor in range.
func (m *RootModel) cfgMove(delta int) {
	if m.cfgView.SecFocused {
		m.cfgView.SecCursor += delta
	} else {
		m.cfgView.EntCursor += delta
	}
	m.clampCfgCursors()
}

// cfgTop jumps the focused pane to its first row.
func (m *RootModel) cfgTop() {
	if m.cfgView.SecFocused {
		m.cfgView.SecCursor = 0
	} else {
		m.cfgView.EntCursor = 0
	}
	m.clampCfgCursors()
}

// cfgBottom jumps the focused pane to its last row.
func (m *RootModel) cfgBottom() {
	if m.cfgView.SecFocused {
		m.cfgView.SecCursor = len(m.frag.Sections) - 1
	} else {
		m.cfgView.EntCursor = m.focusedSectionEntryCount() - 1
	}
	m.clampCfgCursors()
}

// cfgPage moves the focused pane by one page (delta: +1 down, -1 up).
func (m *RootModel) cfgPage(delta int) {
	m.cfgMove(delta * m.pageSize())
}

// --- Config-view rendering ---

func (m RootModel) sectionTitle() string {
	return fmt.Sprintf("Sections · %d", len(m.frag.Sections))
}

func (m RootModel) entryTitle() string {
	s, ok := m.focusedSection()
	if !ok {
		return "Entries"
	}
	kind := s.Kind
	if kind == "" {
		kind = "(top level)"
	}
	return fmt.Sprintf("Entries · %s · %d", kind, len(s.Entries))
}

func (m RootModel) configSectionRows() func(int) []row {
	return func(innerW int) []row {
		rows := make([]row, 0, len(m.frag.Sections))
		for _, s := range m.frag.Sections {
			rows = append(rows, row{text: sectionLabel(s), dim: allDisabled(s)})
		}
		return rows
	}
}

func (m RootModel) configEntryRows() func(int) []row {
	return func(innerW int) []row {
		s, ok := m.focusedSection()
		if !ok {
			return nil
		}
		rows := make([]row, 0, len(s.Entries))
		for _, e := range s.Entries {
			rows = append(rows, row{text: entryLabel(e), dim: e.Disabled})
		}
		return rows
	}
}

// configPanes lays the sections and entries panes side by side, or stacked
// when the terminal is narrow — mirroring mainPanes for the zones view.
func (m RootModel) configPanes(w, h int) string {
	stacked := w < minSplitWidth
	secW := w * 38 / 100
	if secW < 18 {
		stacked = true
	}
	if stacked {
		topH := h / 2
		if topH < 3 {
			topH = 3
		}
		botH := h - topH - 1
		if botH < 3 {
			botH = 3
		}
		top := renderPanel(m.sectionTitle(), m.configSectionRows(), m.cfgView.SecCursor, m.cfgView.SecFocused, w, topH)
		bot := renderPanel(m.entryTitle(), m.configEntryRows(), m.cfgView.EntCursor, !m.cfgView.SecFocused, w, botH)
		return top + "\n" + bot
	}
	entW := w - secW - 1
	left := renderPanel(m.sectionTitle(), m.configSectionRows(), m.cfgView.SecCursor, m.cfgView.SecFocused, secW, h)
	right := renderPanel(m.entryTitle(), m.configEntryRows(), m.cfgView.EntCursor, !m.cfgView.SecFocused, entW, h)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}
