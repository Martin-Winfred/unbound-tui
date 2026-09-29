// Package model implements the Bubble Tea TUI for the stateless, file-backed
// unbound-tui. It edits an in-memory copy of the fragment model; nothing is
// persisted until the user applies, at which point the fragment is written
// and Unbound is reloaded.
package model

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/config"
	"github.com/Martin-Winfred/unbound-tui/internal/domain"
	"github.com/Martin-Winfred/unbound-tui/internal/validate"
)

// View selects which top-level pane the root model renders. The zero value is
// the zones view.
type View int

const (
	ViewZones View = iota
	ViewConfig
)

// RootModel is the top-level tea.Model.
type RootModel struct {
	ctl     domain.Controller
	cfg     *config.Manager
	version string

	// frag is the single source of truth: the generic fragment model read
	// from disk and kept in sync with zones by regenLocal after every edit.
	frag    domain.Fragment
	view    View
	cfgView ConfigViewModel

	zones       []domain.Zone
	zoneCursor  int
	recCursor   int
	zoneFocused bool

	dirty     bool
	state     AppState
	lastError error
	notice    string

	form    RecordForm
	cfgForm configFormCtx // target of the active Config-view form

	// delete/quit confirmation context; kinds: "zone", "record",
	// "section", "entry", "quit". For "entry", confirmZone holds the
	// section index and confirmRec the entry index.
	confirmKind string
	confirmZone int
	confirmRec  int

	foreign ForeignModel

	// upstreams is the load-time snapshot of foreign forward/stub sections in
	// the include graph (add-time conflict warnings). The Foreign view keeps
	// its own, freshly fetched copy.
	upstreams []UpstreamRow

	width, height int
}

// NewRootModel builds the root model around its collaborators. The zones pane
// starts focused. version is shown in the title.
func NewRootModel(ctl domain.Controller, cfg *config.Manager, version string) RootModel {
	return RootModel{ctl: ctl, cfg: cfg, version: version, state: StateReady,
		zoneFocused: true, cfgView: ConfigViewModel{SecFocused: true}}
}

// Init loads the generic fragment model from disk. It also snapshots the
// include graph's foreign forward/stub sections for add-time conflict
// warnings; an upstream read failure degrades to an empty snapshot (never
// fatal) because the Foreign view re-reads it on demand.
func (m RootModel) Init() tea.Cmd {
	return func() tea.Msg {
		f, err := m.cfg.ReadFragment()
		if err != nil {
			return ErrorMsg{fmt.Errorf("read fragment: %w", err)}
		}
		var ups []UpstreamRow
		if eff, err := config.ReadEffective(m.cfg.MainConfPath()); err == nil {
			ups = buildUpstreamRows(eff, m.cfg.FragmentPath())
		}
		return ZonesLoadedMsg{Fragment: f, Upstreams: ups}
	}
}

// Update dispatches per state.
func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		m.notice = ""
		switch m.state {
		case StateConfirm:
			return m.handleConfirm(msg)
		case StateForm:
			var cmd tea.Cmd
			m.form, cmd = m.updateForm(msg)
			return m, cmd
		case StateForeign:
			var cmd tea.Cmd
			m.foreign, cmd = m.foreign.Update(msg)
			return m, cmd
		case StateApplying:
			return m, nil
		case StateError:
			m.lastError = nil
			m.state = StateReady
			return m, nil
		default:
			return m.handleKey(msg)
		}

	case ZonesLoadedMsg:
		m.frag = msg.Fragment
		m.upstreams = msg.Upstreams
		m.clampCfgCursors()
		zones, err := config.ZonesFromFragment(msg.Fragment)
		if err != nil {
			// Projection can fail on malformed local-* entries; surface it
			// loudly through the normal error path, never silently.
			return m, func() tea.Msg { return ErrorMsg{fmt.Errorf("project fragment: %w", err)} }
		}
		m.zones = zones
		m.clampCursors()
		m.state = StateReady
		return m, nil

	case AppliedMsg:
		m.dirty = false
		m.state = StateReady
		m.notice = "changes applied"
		return m, nil

	case ForeignLoadedMsg:
		m.foreign = newForeignModel(msg.Zones, msg.RRs, msg.Err)
		m.foreign.setUpstreams(msg.Upstreams, msg.UpErr)
		m.foreign.resize(m.width, m.bodyHeightFor(m.height))
		return m, nil

	case FormCancelMsg:
		m.form = RecordForm{}
		m.state = StateReady
		return m, nil

	case FormSubmitMsg:
		return m.applyForm(msg)

	case ConfigFormSubmitMsg:
		return m.applyConfigForm(msg)

	case ForeignCloseMsg:
		m.state = StateReady
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.foreign.resize(msg.Width, m.bodyHeightFor(msg.Height))
		return m, nil

	case ErrorMsg:
		m.lastError = msg.Error
		m.state = StateError
		return m, nil
	}
	return m, nil
}

func (m RootModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action := keyMap[msg.String()]

	// Actions shared by both views.
	switch action {
	case actionSwitchView:
		m.switchView()
		return m, nil
	case actionApply:
		if m.dirty {
			// Re-project before writing: a hand-malformed local-* entry can
			// survive in frag after its projection error was dismissed, so
			// the dirty flag alone is not enough to authorise a write.
			if _, err := config.ZonesFromFragment(m.frag); err != nil {
				return m, func() tea.Msg { return ErrorMsg{fmt.Errorf("cannot apply: %w", err)} }
			}
			m.state = StateApplying
			return m, m.apply()
		}
		m.notice = "nothing to apply"
		return m, nil
	case actionForeign:
		return m.requestForeign()
	case actionQuit:
		if m.dirty {
			m.confirmKind = "quit"
			m.state = StateConfirm
			return m, nil
		}
		return m, tea.Quit
	}

	// The Config view routes movement, focus, its own lifecycle keys
	// (d/D/space) and the a/A/e forms to its panes.
	if m.view == ViewConfig {
		switch action {
		case actionUp:
			m.cfgMove(-1)
		case actionDown:
			m.cfgMove(1)
		case actionTop:
			m.cfgTop()
		case actionBottom:
			m.cfgBottom()
		case actionPageDown:
			m.cfgPage(1)
		case actionPageUp:
			m.cfgPage(-1)
		case actionTogglePane:
			m.cfgView.SecFocused = !m.cfgView.SecFocused
		case actionAddSection:
			m.newSectionForm()
		case actionAddZone:
			m.newEntryForm(m.cfgView.SecCursor)
		case actionDeleteRecord:
			m.cfgDeleteEntry()
		case actionDeleteZone:
			m.cfgDeleteSection()
		case actionToggleDisabled:
			m.cfgToggleDisabled()
		case actionEditTTL:
			m.cfgEditEntry()
		}
		return m, nil
	}

	switch action {
	case actionUp:
		m.moveUp()
	case actionDown:
		m.moveDown()
	case actionTop:
		m.moveTop()
	case actionBottom:
		m.moveBottom()
	case actionPageDown:
		m.movePageDown()
	case actionPageUp:
		m.movePageUp()
	case actionTogglePane:
		m.zoneFocused = !m.zoneFocused
	case actionAddZone:
		m.form = newZoneForm()
		m.state = StateForm
	case actionAddRecord:
		if z, ok := m.focusedZone(); ok {
			m.form = newRecordForm(m.zoneCursor, z)
			m.state = StateForm
		}
	case actionEditTTL:
		if !m.zoneFocused {
			if z, ok := m.focusedZone(); ok {
				if r, ok := m.focusedRecord(); ok {
					m.form = newTTLForm(m.zoneCursor, m.recCursor, z, r)
					m.state = StateForm
				}
			}
		}
	case actionSetType:
		if m.zoneFocused {
			if z, ok := m.focusedZone(); ok {
				m.form = newTypeForm(m.zoneCursor, z)
				m.state = StateForm
			}
		}
	case actionDeleteRecord:
		if !m.zoneFocused {
			if _, ok := m.focusedRecord(); ok {
				m.confirmKind, m.confirmZone, m.confirmRec = "record", m.zoneCursor, m.recCursor
				m.state = StateConfirm
			}
		}
	case actionDeleteZone:
		if m.zoneFocused {
			if _, ok := m.focusedZone(); ok {
				m.confirmKind, m.confirmZone = "zone", m.zoneCursor
				m.state = StateConfirm
			}
		}
	case actionToggleDisabled:
		m.toggleDisabled()
	}
	return m, nil
}

// switchView toggles between the Local data view and the Config view.
func (m *RootModel) switchView() {
	if m.view == ViewConfig {
		m.view = ViewZones
		return
	}
	m.view = ViewConfig
	// A Local-view mutation can shift the section layout (regenLocal inserts
	// a server section at index 0), so re-clamp before the Config panes render.
	m.clampCfgCursors()
}

func (m RootModel) handleConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key != "y" && key != "Y" && key != "enter" {
		m.confirmKind = ""
		m.state = StateReady
		return m, nil
	}
	kind := m.confirmKind
	m.confirmKind = ""
	if kind == "quit" {
		return m, tea.Quit
	}
	switch kind {
	case "zone":
		m.deleteZone(m.confirmZone)
	case "record":
		m.deleteRecord(m.confirmZone, m.confirmRec)
	case "section":
		m.deleteSection(m.confirmZone)
	case "entry":
		m.deleteEntry(m.confirmZone, m.confirmRec)
	}
	// A Config-view mutation can move into StateError when re-projecting a
	// hand-broken local-* entry; keep that loud error instead of clobbering it.
	if m.state != StateError {
		m.state = StateReady
	}
	return m, nil
}

// applyForm folds a validated form submission into the in-memory model.
func (m RootModel) applyForm(msg FormSubmitMsg) (tea.Model, tea.Cmd) {
	m.form = RecordForm{}
	m.state = StateReady
	switch msg.Mode {
	case FormAddZone:
		name := domain.FQDN(msg.Name)
		if m.hasZone(name) {
			m.lastError = fmt.Errorf("zone %q already exists", name)
			m.state = StateError
			return m, nil
		}
		m.zones = append(m.zones, domain.Zone{Name: name, Type: msg.Type})
		sort.SliceStable(m.zones, func(i, j int) bool { return m.zones[i].Name < m.zones[j].Name })
	case FormAddRecord:
		if msg.ZoneIndex >= 0 && msg.ZoneIndex < len(m.zones) {
			rec := domain.Record{Name: msg.Name, RType: msg.Type, Value: msg.Value, TTL: msg.TTL}
			if hasRecord(m.zones[msg.ZoneIndex].Records, rec) {
				m.lastError = fmt.Errorf("record %s %s %s already exists in %s",
					msg.Name, msg.Type, msg.Value, m.zones[msg.ZoneIndex].Name)
				m.state = StateError
				return m, nil
			}
			m.zones[msg.ZoneIndex].Records = append(m.zones[msg.ZoneIndex].Records, rec)
		}
	case FormEditTTL:
		if z, ok := m.zoneAt(msg.ZoneIndex); ok {
			if msg.RecIndex >= 0 && msg.RecIndex < len(m.zones[z].Records) {
				m.zones[z].Records[msg.RecIndex].TTL = msg.TTL
			}
		}
	case FormSetType:
		if z, ok := m.zoneAt(msg.ZoneIndex); ok {
			m.zones[z].Type = msg.Type
		}
	}
	m.regen()
	m.dirty = true
	return m, nil
}

func (m RootModel) requestForeign() (tea.Model, tea.Cmd) {
	zones := cloneZones(m.zones)
	ctl := m.ctl
	cfg := m.cfg
	m.state = StateForeign
	m.foreign = ForeignModel{}
	return m, func() tea.Msg {
		rtZones, err := ctl.ListLocalZones()
		if err != nil {
			return ForeignLoadedMsg{Err: err}
		}
		rtRRs, err := ctl.ListLocalData()
		if err != nil {
			return ForeignLoadedMsg{Err: err}
		}
		// Fresh read of the include graph for the upstreams tab; a failure is
		// non-fatal and shown as a notice in that tab.
		var ups []UpstreamRow
		var upErr string
		if eff, err := config.ReadEffective(cfg.MainConfPath()); err != nil {
			upErr = err.Error()
		} else {
			ups = buildUpstreamRows(eff, cfg.FragmentPath())
		}
		fz, fr := foreignEntries(zones, rtZones, rtRRs)
		return ForeignLoadedMsg{Zones: fz, RRs: fr, Upstreams: ups, UpErr: upErr}
	}
}

// apply validates the whole model, writes the fragment atomically and reloads
// Unbound. It runs as a tea.Cmd because reload can block. The fragment is
// already in sync with the zone model (regenLocal runs on every edit).
func (m RootModel) apply() tea.Cmd {
	zones := cloneZones(m.zones)
	frag := m.frag
	cfg := m.cfg
	ctl := m.ctl
	return func() tea.Msg {
		if err := validateModel(zones); err != nil {
			return ErrorMsg{err}
		}
		if err := cfg.WriteFragment(frag); err != nil {
			return ErrorMsg{fmt.Errorf("write fragment: %w", err)}
		}
		if err := ctl.Reload(); err != nil {
			return ErrorMsg{fmt.Errorf("reload unbound: %w", err)}
		}
		return AppliedMsg{}
	}
}

// --- in-memory mutations (all synchronous, all set dirty) ---

func (m *RootModel) moveUp() {
	if m.zoneFocused {
		if m.zoneCursor > 0 {
			m.zoneCursor--
			m.recCursor = 0
		}
		return
	}
	if m.recCursor > 0 {
		m.recCursor--
	}
}

func (m *RootModel) moveDown() {
	if m.zoneFocused {
		if m.zoneCursor < len(m.zones)-1 {
			m.zoneCursor++
			m.recCursor = 0
		}
		return
	}
	if z, ok := m.zoneAt(m.zoneCursor); ok && m.recCursor < len(m.zones[z].Records)-1 {
		m.recCursor++
	}
}

func (m *RootModel) moveTop() {
	if m.zoneFocused {
		m.zoneCursor = 0
		m.recCursor = 0
		return
	}
	m.recCursor = 0
}

func (m *RootModel) moveBottom() {
	if m.zoneFocused {
		if len(m.zones) > 0 {
			m.zoneCursor = len(m.zones) - 1
		}
		m.recCursor = 0
		return
	}
	if z, ok := m.zoneAt(m.zoneCursor); ok {
		m.recCursor = len(m.zones[z].Records) - 1
		if m.recCursor < 0 {
			m.recCursor = 0
		}
	}
}

func (m *RootModel) movePageDown() {
	step := m.pageSize()
	if m.zoneFocused {
		m.zoneCursor += step
		if m.zoneCursor > len(m.zones)-1 {
			m.zoneCursor = len(m.zones) - 1
		}
		if m.zoneCursor < 0 {
			m.zoneCursor = 0
		}
		m.recCursor = 0
		return
	}
	if z, ok := m.zoneAt(m.zoneCursor); ok {
		m.recCursor += step
		if m.recCursor > len(m.zones[z].Records)-1 {
			m.recCursor = len(m.zones[z].Records) - 1
		}
		if m.recCursor < 0 {
			m.recCursor = 0
		}
	}
}

func (m *RootModel) movePageUp() {
	step := m.pageSize()
	if m.zoneFocused {
		m.zoneCursor -= step
		if m.zoneCursor < 0 {
			m.zoneCursor = 0
		}
		m.recCursor = 0
		return
	}
	m.recCursor -= step
	if m.recCursor < 0 {
		m.recCursor = 0
	}
}

func (m *RootModel) toggleDisabled() {
	if m.zoneFocused {
		z, ok := m.zoneAt(m.zoneCursor)
		if !ok {
			return
		}
		m.zones[z].Disabled = !m.zones[z].Disabled
		if !m.zones[z].Disabled {
			// Enabling a zone re-enables its records.
			for i := range m.zones[z].Records {
				m.zones[z].Records[i].Disabled = false
			}
		}
	} else {
		z, ok := m.zoneAt(m.zoneCursor)
		if !ok || m.recCursor >= len(m.zones[z].Records) {
			return
		}
		m.zones[z].Records[m.recCursor].Disabled = !m.zones[z].Records[m.recCursor].Disabled
	}
	m.regen()
	m.dirty = true
}

func (m *RootModel) deleteZone(index int) {
	z, ok := m.zoneAt(index)
	if !ok {
		return
	}
	m.zones = append(m.zones[:z], m.zones[z+1:]...)
	m.regen()
	m.clampCursors()
	m.dirty = true
}

func (m *RootModel) deleteRecord(zoneIndex, recIndex int) {
	z, ok := m.zoneAt(zoneIndex)
	if !ok || recIndex < 0 || recIndex >= len(m.zones[z].Records) {
		return
	}
	recs := m.zones[z].Records
	m.zones[z].Records = append(recs[:recIndex], recs[recIndex+1:]...)
	m.regen()
	m.clampCursors()
	m.dirty = true
}

// regen folds the edited zone model back into the fragment, keeping the
// fragment the single source of truth. It is a pure copy-on-write helper.
func (m *RootModel) regen() {
	m.frag = regenLocal(m.frag, m.zones)
}

// --- helpers ---

// zoneAt returns the index of the zone at position i (identity; kept as a
// named helper for readability and future filtering).
func (m RootModel) zoneAt(i int) (int, bool) {
	if i < 0 || i >= len(m.zones) {
		return 0, false
	}
	return i, true
}

func (m RootModel) focusedZone() (domain.Zone, bool) {
	if m.zoneCursor < 0 || m.zoneCursor >= len(m.zones) {
		return domain.Zone{}, false
	}
	return m.zones[m.zoneCursor], true
}

func (m RootModel) focusedRecord() (domain.Record, bool) {
	z, ok := m.zoneAt(m.zoneCursor)
	if !ok || m.recCursor < 0 || m.recCursor >= len(m.zones[z].Records) {
		return domain.Record{}, false
	}
	return m.zones[z].Records[m.recCursor], true
}

func (m *RootModel) clampCursors() {
	if m.zoneCursor >= len(m.zones) {
		m.zoneCursor = len(m.zones) - 1
	}
	if m.zoneCursor < 0 {
		m.zoneCursor = 0
	}
	if z, ok := m.zoneAt(m.zoneCursor); ok {
		if m.recCursor >= len(m.zones[z].Records) {
			m.recCursor = len(m.zones[z].Records) - 1
		}
	}
	if m.recCursor < 0 {
		m.recCursor = 0
	}
}

func cloneZones(zones []domain.Zone) []domain.Zone {
	out := make([]domain.Zone, len(zones))
	for i, z := range zones {
		out[i] = z
		out[i].Records = append([]domain.Record(nil), z.Records...)
	}
	return out
}

// validateModel validates every zone and record, and rejects duplicates,
// before any write.
func validateModel(zones []domain.Zone) error {
	seenZones := make(map[string]bool, len(zones))
	for _, z := range zones {
		if err := validate.ValidateZoneName(z.Name); err != nil {
			return fmt.Errorf("zone %q: %w", z.Name, err)
		}
		if !config.IsZoneTypeName(z.Type) {
			return fmt.Errorf("zone %q: unsupported type %q", z.Name, z.Type)
		}
		zk := strings.ToLower(domain.FQDN(z.Name))
		if seenZones[zk] {
			return fmt.Errorf("duplicate zone %q", z.Name)
		}
		seenZones[zk] = true

		seenRecs := make(map[string]bool, len(z.Records))
		for _, r := range z.Records {
			if err := validate.ValidateRecord(z.Name, r); err != nil {
				return fmt.Errorf("zone %q record %q: %w", z.Name, r.Name, err)
			}
			rk := recordKey(z.Name, r)
			if seenRecs[rk] {
				return fmt.Errorf("duplicate record %s in zone %s", recordLabel(r), z.Name)
			}
			seenRecs[rk] = true
		}
	}
	return nil
}

// recordKey is the identity used for duplicate detection: DNS names and types
// are case-insensitive and the apex is spelled "" or "@".
func recordKey(zone string, r domain.Record) string {
	return strings.ToLower(domain.FQDN(zone)) + "|" + recordName(r.Name) + "|" +
		strings.ToUpper(r.RType) + "|" + r.Value
}

func recordName(name string) string {
	if name == "" || name == "@" {
		return "@"
	}
	return strings.ToLower(name)
}

func recordLabel(r domain.Record) string {
	name := r.Name
	if name == "" {
		name = "@"
	}
	return fmt.Sprintf("%s %s %s", name, r.RType, r.Value)
}

// hasZone reports whether a zone with the same (case-insensitive) name exists.
func (m RootModel) hasZone(name string) bool {
	want := strings.ToLower(domain.FQDN(name))
	for _, z := range m.zones {
		if strings.ToLower(domain.FQDN(z.Name)) == want {
			return true
		}
	}
	return false
}

// hasRecord reports whether an equivalent record already exists.
func hasRecord(recs []domain.Record, r domain.Record) bool {
	for _, existing := range recs {
		if recordName(existing.Name) == recordName(r.Name) &&
			strings.EqualFold(existing.RType, r.RType) &&
			existing.Value == r.Value {
			return true
		}
	}
	return false
}
