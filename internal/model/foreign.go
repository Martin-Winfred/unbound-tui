package model

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// noZoneName is the synthetic bucket for foreign RRs whose owner falls under
// no foreign zone.
const noZoneName = "(no zone)"

// foreignZone is one foreign zone and the foreign RRs grouped under it.
type foreignZone struct {
	name string
	typ  string
	rrs  []string
}

// ForeignModel is the read-only master/detail view of runtime entries our
// fragment does not own. It never mutates anything.
type ForeignModel struct {
	all     []foreignZone // full report, filtered into view
	shown   []foreignZone // current filtered list
	zCur    int
	rCur    int
	focusRR bool

	filter    string
	filtering bool
	input     textinput.Model

	err error
	w   int
	h   int
}

func newForeignModel(zones []domain.LocalZone, rrs []string, err error) ForeignModel {
	f := ForeignModel{err: err}
	byName := make(map[string]int, len(zones))
	for _, z := range zones {
		byName[z.Name] = len(f.all)
		f.all = append(f.all, foreignZone{name: z.Name, typ: z.Type})
	}
	for _, line := range rrs {
		owner := rrOwner(line)
		best := -1
		for i := range f.all {
			n := f.all[i].name
			if owner == n || strings.HasSuffix(owner, "."+n) {
				if best < 0 || len(n) > len(f.all[best].name) {
					best = i
				}
			}
		}
		if best < 0 {
			best = f.noZoneIndex()
		}
		f.all[best].rrs = append(f.all[best].rrs, line)
	}
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "substring"
	f.input = ti
	f.applyFilter()
	return f
}

func (f *ForeignModel) noZoneIndex() int {
	for i := range f.all {
		if f.all[i].name == noZoneName {
			return i
		}
	}
	f.all = append(f.all, foreignZone{name: noZoneName})
	return len(f.all) - 1
}

func (f *ForeignModel) resize(w, h int) { f.w, f.h = w, h }

func (f ForeignModel) currentRRs() []string {
	if f.zCur >= 0 && f.zCur < len(f.shown) {
		return f.shown[f.zCur].rrs
	}
	return nil
}

// applyFilter recomputes view from all using the current filter: a zone is
// kept when its name matches, or when any of its RRs match.
func (f *ForeignModel) applyFilter() {
	q := strings.ToLower(f.filter)
	var out []foreignZone
	for _, z := range f.all {
		if q == "" || strings.Contains(strings.ToLower(z.name), q) {
			out = append(out, z)
			continue
		}
		var rr []string
		for _, r := range z.rrs {
			if strings.Contains(strings.ToLower(r), q) {
				rr = append(rr, r)
			}
		}
		if len(rr) > 0 {
			out = append(out, foreignZone{name: z.name, typ: z.typ, rrs: rr})
		}
	}
	f.shown = out
	f.clamp()
}

func (f *ForeignModel) clamp() {
	if f.zCur >= len(f.shown) {
		f.zCur = len(f.shown) - 1
	}
	if f.zCur < 0 {
		f.zCur = 0
	}
	n := len(f.currentRRs())
	if f.rCur >= n {
		f.rCur = n - 1
	}
	if f.rCur < 0 {
		f.rCur = 0
	}
}

func (f *ForeignModel) pageRows() int {
	h := f.h - 3
	if f.filtering || f.filter != "" {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

func (f *ForeignModel) moveUp() {
	if f.focusRR {
		if f.rCur > 0 {
			f.rCur--
		}
		return
	}
	if f.zCur > 0 {
		f.zCur--
		f.rCur = 0
	}
}

func (f *ForeignModel) moveDown() {
	if f.focusRR {
		if f.rCur < len(f.currentRRs())-1 {
			f.rCur++
		}
		return
	}
	if f.zCur < len(f.shown)-1 {
		f.zCur++
		f.rCur = 0
	}
}

func (f *ForeignModel) moveBottom() {
	if f.focusRR {
		f.rCur = len(f.currentRRs()) - 1
		if f.rCur < 0 {
			f.rCur = 0
		}
		return
	}
	f.zCur = len(f.shown) - 1
	if f.zCur < 0 {
		f.zCur = 0
	}
	f.rCur = 0
}

func (f *ForeignModel) page(dir int) {
	step := f.pageRows()
	if f.focusRR {
		f.rCur += dir * step
		n := len(f.currentRRs()) - 1
		if f.rCur > n {
			f.rCur = n
		}
		if f.rCur < 0 {
			f.rCur = 0
		}
		return
	}
	f.zCur += dir * step
	if f.zCur > len(f.shown)-1 {
		f.zCur = len(f.shown) - 1
	}
	if f.zCur < 0 {
		f.zCur = 0
	}
	f.rCur = 0
}

// Update routes the foreign view keys.
func (f ForeignModel) Update(msg tea.Msg) (ForeignModel, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	if f.filtering {
		switch key.String() {
		case "esc":
			f.filtering = false
			f.filter = ""
			f.input.SetValue("")
			f.input.Blur()
			f.applyFilter()
			return f, nil
		case "enter":
			f.filtering = false
			f.input.Blur()
			return f, nil
		}
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		f.filter = f.input.Value()
		f.applyFilter()
		return f, cmd
	}
	switch key.String() {
	case "up", "k":
		f.moveUp()
	case "down", "j":
		f.moveDown()
	case "g":
		f.zCur, f.rCur = 0, 0
	case "G":
		f.moveBottom()
	case "ctrl+d":
		f.page(1)
	case "ctrl+u":
		f.page(-1)
	case "tab", "h", "l", "left", "right":
		f.focusRR = !f.focusRR
	case "/":
		f.filtering = true
		f.input.SetValue(f.filter)
		f.input.Focus()
	case "esc", "q", "f":
		return f, func() tea.Msg { return ForeignCloseMsg{} }
	}
	return f, nil
}

// View renders with the last known size (test/convenience entry point).
func (f ForeignModel) View() string {
	w, h := f.w, f.h
	if w <= 0 {
		w = defaultWidth
	}
	if h <= 0 {
		h = defaultHeight
	}
	return f.view(w, h)
}

// view renders the master/detail panes at the given size.
func (f ForeignModel) view(w, h int) string {
	if f.err != nil {
		return indent(th.box.Render(fit("Error: "+f.err.Error(), min(w-8, 76))), 2)
	}
	if len(f.all) == 0 {
		return th.dimStyle.Render(truncate("None - Unbound serves only entries from our fragment.", w))
	}

	var top string
	topH := 0
	if f.filtering || f.filter != "" {
		val := f.filter
		if f.filtering {
			val = f.input.View()
		}
		top = th.dimStyle.Render(truncate("filter: "+val, w))
		topH = 1
	}
	ph := h - topH
	if ph < 3 {
		ph = 3
	}

	zoneTitle := fmt.Sprintf("Foreign zones · %d", len(f.shown))
	rrTitle := "RRs"
	if f.zCur >= 0 && f.zCur < len(f.shown) {
		rrTitle = fmt.Sprintf("RRs · %s · %d", f.shown[f.zCur].name, len(f.shown[f.zCur].rrs))
	}

	zBuild := func(innerW int) []row {
		const typeW = 12
		nameW := innerW - typeW - 1
		if nameW < 4 {
			nameW = 4
		}
		rows := make([]row, 0, len(f.shown))
		for _, z := range f.shown {
			rows = append(rows, row{text: fit(z.name, nameW) + " " + fit(z.typ, typeW)})
		}
		return rows
	}
	rBuild := func(innerW int) []row {
		rows := make([]row, 0, len(f.currentRRs()))
		for _, r := range f.currentRRs() {
			rows = append(rows, row{text: fit(r, innerW)})
		}
		return rows
	}

	var panels string
	if w < minSplitWidth {
		topH2 := ph / 2
		if topH2 < 3 {
			topH2 = 3
		}
		botH := ph - topH2 - 1
		if botH < 3 {
			botH = 3
		}
		panels = renderPanel(zoneTitle, zBuild, f.zCur, !f.focusRR, w, topH2) + "\n" +
			renderPanel(rrTitle, rBuild, f.rCur, f.focusRR, w, botH)
	} else {
		zw := w * 35 / 100
		if zw < 18 {
			zw = 18
		}
		rw := w - zw - 1
		panels = lipgloss.JoinHorizontal(lipgloss.Top,
			renderPanel(zoneTitle, zBuild, f.zCur, !f.focusRR, zw, ph), " ",
			renderPanel(rrTitle, rBuild, f.rCur, f.focusRR, rw, ph))
	}
	if top != "" {
		return top + "\n" + panels
	}
	return panels
}

// rrOwner returns the owner name (first whitespace field) of an RR line.
func rrOwner(rr string) string {
	fields := strings.Fields(rr)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
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
