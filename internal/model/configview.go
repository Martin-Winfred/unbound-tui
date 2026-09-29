package model

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/Martin-Winfred/unbound-tui/internal/config"
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

// sectionHasLocked reports whether a section holds any locked local-* entry,
// which makes it ineligible for deletion.
func sectionHasLocked(s domain.Section) bool {
	for _, e := range s.Entries {
		if isLocked(e) {
			return true
		}
	}
	return false
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

// focusedEntry returns the entry under the entries cursor within the selected
// section.
func (m RootModel) focusedEntry() (domain.Entry, bool) {
	s, ok := m.focusedSection()
	if !ok || m.cfgView.EntCursor < 0 || m.cfgView.EntCursor >= len(s.Entries) {
		return domain.Entry{}, false
	}
	return s.Entries[m.cfgView.EntCursor], true
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

// --- Config-view mutations ---

// lockedNotice is the one status-bar wording for a mutating key aimed at a
// locked local-* row; `space`, `d` and the section guard all reuse it.
const lockedNotice = "locked — edit local data in the Local data view"

// refreshZones re-projects the zones model the Local data view renders from
// frag. Projection is not total: a malformed local-* entry moves the model
// into StateError loudly rather than being silently dropped, mirroring the
// ZonesLoadedMsg handler.
func (m *RootModel) refreshZones() {
	zs, err := config.ZonesFromFragment(m.frag)
	if err != nil {
		// Never leave a stale projection paired with the mutated fragment;
		// the error state already blocks apply.
		m.zones = nil
		m.lastError = fmt.Errorf("project fragment: %w", err)
		m.state = StateError
		return
	}
	m.zones = zs
	m.clampCursors()
}

// cfgDeleteSection is the `D` handler. It refuses a section that holds any
// locked local-* entry (status notice, no confirmation) and otherwise starts a
// "section" confirmation. Only the focused sections pane acts.
func (m *RootModel) cfgDeleteSection() {
	if !m.cfgView.SecFocused {
		return
	}
	s, ok := m.focusedSection()
	if !ok {
		return
	}
	if sectionHasLocked(s) {
		m.notice = "local data belongs to the Local data view"
		return
	}
	m.confirmKind, m.confirmZone = "section", m.cfgView.SecCursor
	m.state = StateConfirm
}

// deleteSection removes the section at index from frag by index — so a
// duplicate named sibling is left untouched — then refreshes the projection.
func (m *RootModel) deleteSection(index int) {
	if index < 0 || index >= len(m.frag.Sections) {
		return
	}
	m.frag.Sections = append(m.frag.Sections[:index], m.frag.Sections[index+1:]...)
	m.refreshZones()
	m.clampCfgCursors()
	m.dirty = true
}

// cfgDeleteEntry is the `d` handler. Locked local-* rows are skipped with a
// notice; an unlocked entry starts an "entry" confirmation. Only the focused
// entries pane acts.
func (m *RootModel) cfgDeleteEntry() {
	if m.cfgView.SecFocused {
		return
	}
	e, ok := m.focusedEntry()
	if !ok {
		return
	}
	if isLocked(e) {
		m.notice = lockedNotice
		return
	}
	m.confirmKind = "entry"
	m.confirmZone, m.confirmRec = m.cfgView.SecCursor, m.cfgView.EntCursor
	m.state = StateConfirm
}

// cfgEditEntry is the `e` handler. A locked local-* row is skipped with the
// shared notice; an unlocked row opens the typed edit form (configform.go), so
// the Config view never routes `e` to the zones view.
func (m *RootModel) cfgEditEntry() {
	e, ok := m.focusedEntry()
	if !ok {
		return
	}
	if isLocked(e) {
		m.notice = lockedNotice
		return
	}
	m.editEntryForm(m.cfgView.SecCursor, m.cfgView.EntCursor, e)
}

// deleteEntry removes the entry at [sectionIndex][entryIndex] from frag by
// index, then refreshes the projection.
func (m *RootModel) deleteEntry(sectionIndex, entryIndex int) {
	if sectionIndex < 0 || sectionIndex >= len(m.frag.Sections) {
		return
	}
	entries := m.frag.Sections[sectionIndex].Entries
	if entryIndex < 0 || entryIndex >= len(entries) {
		return
	}
	m.frag.Sections[sectionIndex].Entries = append(entries[:entryIndex], entries[entryIndex+1:]...)
	m.refreshZones()
	m.clampCfgCursors()
	m.dirty = true
}

// cfgToggleDisabled is the `space` handler: the sections pane toggles every
// non-locked entry of the selected section, the entries pane toggles the one
// entry under the cursor. Locked local-* entries never change.
func (m *RootModel) cfgToggleDisabled() {
	if m.cfgView.SecFocused {
		m.cfgToggleSection()
		return
	}
	m.cfgToggleEntry()
}

// cfgToggleSection inverts every non-locked entry of the selected section. A
// section with no non-locked entries only produces a notice.
func (m *RootModel) cfgToggleSection() {
	i := m.cfgView.SecCursor
	if i < 0 || i >= len(m.frag.Sections) {
		return
	}
	entries := m.frag.Sections[i].Entries
	toggled := false
	for j := range entries {
		if isLocked(entries[j]) {
			continue
		}
		entries[j].Disabled = !entries[j].Disabled
		toggled = true
	}
	if !toggled {
		m.notice = lockedNotice
		return
	}
	m.dirty = true
	m.refreshZones()
}

// cfgToggleEntry inverts the entry under the entries cursor, or notices and
// does nothing when it is locked.
func (m *RootModel) cfgToggleEntry() {
	i := m.cfgView.SecCursor
	if i < 0 || i >= len(m.frag.Sections) {
		return
	}
	j := m.cfgView.EntCursor
	if j < 0 || j >= len(m.frag.Sections[i].Entries) {
		return
	}
	e := &m.frag.Sections[i].Entries[j]
	if isLocked(*e) {
		m.notice = lockedNotice
		return
	}
	e.Disabled = !e.Disabled
	m.dirty = true
	m.refreshZones()
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
