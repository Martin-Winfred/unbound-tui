package model

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	defaultWidth  = 80
	defaultHeight = 24
	helpMinHeight = 12
	minSplitWidth = 72
)

// dims returns the terminal size, falling back to a safe default before the
// first WindowSizeMsg arrives.
func (m RootModel) dims() (w, h int) {
	w, h = m.width, m.height
	if w <= 0 {
		w = defaultWidth
	}
	if h <= 0 {
		h = defaultHeight
	}
	return w, h
}

// bodyHeightFor is the number of lines available to the pane area at height h,
// accounting for the title line, the blank line under it, the help line and
// the status line.
func (m RootModel) bodyHeightFor(h int) int {
	help := 0
	if h >= helpMinHeight {
		help = 1
	}
	b := h - 2 - 1 - help
	if b < 3 {
		b = 3
	}
	return b
}

// pageSize is the number of rows a ctrl+d/ctrl+u jump moves.
func (m RootModel) pageSize() int {
	_, h := m.dims()
	n := m.bodyHeightFor(h) - 3 // minus pane borders and title row
	if n < 1 {
		n = 1
	}
	return n
}

// --- text helpers ---

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// fit truncates then right-pads s to exactly w display columns.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = truncate(s, w)
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// fitRight right-aligns s in a field of width w.
func fitRight(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = truncate(s, w)
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s = strings.Repeat(" ", pad) + s
	}
	return s
}

// window returns the [start,end) slice of count items to show so that cursor
// stays visible within visible rows.
func window(count, cursor, visible int) (int, int) {
	if count <= 0 {
		return 0, 0
	}
	if visible < 1 {
		visible = 1
	}
	if visible >= count {
		return 0, count
	}
	start := 0
	if cursor >= visible {
		start = cursor - visible + 1
	}
	if start+visible > count {
		start = count - visible
	}
	if start < 0 {
		start = 0
	}
	return start, start + visible
}

// --- panel rendering ---

// row is one list line: plain text plus a dim flag for disabled entries.
type row struct {
	text string
	dim  bool
}

// renderPanel draws a bordered, titled, scrollable list. build receives the
// inner width so callers can align columns.
func renderPanel(title string, build func(innerW int) []row, cursor int, focused bool, width, height int) string {
	if width < 6 {
		width = 6
	}
	if height < 3 {
		height = 3
	}
	innerW := width - 2
	contentH := height - 3 // title row + top/bottom border
	if contentH < 1 {
		contentH = 1
	}

	rows := build(innerW)
	start, end := window(len(rows), cursor, contentH)

	header := th.headerBlur
	border := th.paneBlur
	if focused {
		header = th.headerFocus
		border = th.paneFocus
	}

	lines := make([]string, 0, contentH+1)
	lines = append(lines, header.Render(fit(title, innerW)))
	if len(rows) == 0 {
		lines = append(lines, th.dimStyle.Render(fit("(none)", innerW)))
	} else {
		for i := start; i < end; i++ {
			text := fit(rows[i].text, innerW)
			switch {
			case i == cursor:
				text = th.selected.Render(text)
			case rows[i].dim:
				text = th.disabled.Render(text)
			}
			lines = append(lines, text)
		}
	}
	for len(lines) < contentH+1 {
		lines = append(lines, strings.Repeat(" ", innerW))
	}
	return border.Render(strings.Join(lines, "\n"))
}

func (m RootModel) recordCount() int {
	n := 0
	for _, z := range m.zones {
		n += len(z.Records)
	}
	return n
}

func (m RootModel) zoneTitle() string {
	return fmt.Sprintf("Zones · %d", len(m.zones))
}

func (m RootModel) recordTitle() string {
	z, ok := m.zoneAt(m.zoneCursor)
	if !ok {
		return "Records"
	}
	return fmt.Sprintf("Records · %s · %d", m.zones[z].Name, len(m.zones[z].Records))
}

func (m RootModel) zoneRows() func(int) []row {
	return func(innerW int) []row {
		const typeW, offW = 11, 5
		nameW := innerW - typeW - offW - 2
		if nameW < 4 {
			nameW = 4
		}
		rows := make([]row, 0, len(m.zones))
		for _, z := range m.zones {
			off := ""
			if z.Disabled {
				off = "(off)"
			}
			text := fit(z.Name, nameW) + " " + fit(z.Type, typeW) + " " + fit(off, offW)
			rows = append(rows, row{text: text, dim: z.Disabled})
		}
		return rows
	}
}

func (m RootModel) recordRows() func(int) []row {
	return func(innerW int) []row {
		z, ok := m.zoneAt(m.zoneCursor)
		if !ok {
			return nil
		}
		const nameW, typeW, ttlW, offW = 16, 6, 5, 5
		valueW := innerW - nameW - typeW - ttlW - offW - 4
		if valueW < 6 {
			valueW = 6
		}
		rows := make([]row, 0, len(m.zones[z].Records))
		for _, r := range m.zones[z].Records {
			name := r.Name
			if name == "" {
				name = "@"
			}
			off := ""
			if r.Disabled {
				off = "(off)"
			}
			text := fit(name, nameW) + " " + fit(r.RType, typeW) + " " +
				fit(r.Value, valueW) + " " + fitRight(strconv.Itoa(r.TTL), ttlW) + " " + fit(off, offW)
			rows = append(rows, row{text: text, dim: r.Disabled})
		}
		return rows
	}
}

// mainPanes lays the zones and records panes side by side, or stacked when the
// terminal is narrow.
func (m RootModel) mainPanes(w, h int) string {
	stacked := w < minSplitWidth
	zoneW := w * 38 / 100
	if zoneW < 18 {
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
		top := renderPanel(m.zoneTitle(), m.zoneRows(), m.zoneCursor, m.zoneFocused, w, topH)
		bot := renderPanel(m.recordTitle(), m.recordRows(), m.recCursor, !m.zoneFocused, w, botH)
		return top + "\n" + bot
	}
	recW := w - zoneW - 1
	left := renderPanel(m.zoneTitle(), m.zoneRows(), m.zoneCursor, m.zoneFocused, zoneW, h)
	right := renderPanel(m.recordTitle(), m.recordRows(), m.recCursor, !m.zoneFocused, recW, h)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

// --- overlays and footer ---

func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	parts := strings.Split(s, "\n")
	for i := range parts {
		parts[i] = pad + parts[i]
	}
	return strings.Join(parts, "\n")
}

func (m RootModel) overlayBox(w int) string {
	var content string
	switch m.state {
	case StateForm:
		content = m.form.View()
	case StateConfirm:
		content = m.confirmMessage()
	default:
		return ""
	}
	innerW := w - 8
	if innerW > 64 {
		innerW = 64
	}
	if innerW < 20 {
		innerW = 20
	}
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = fit(lines[i], innerW)
	}
	return indent(th.box.Render(strings.Join(lines, "\n")), 2)
}

func (m RootModel) confirmMessage() string {
	switch m.confirmKind {
	case "zone":
		if m.confirmZone >= 0 && m.confirmZone < len(m.zones) {
			return fmt.Sprintf("Delete zone %s and all its records?", m.zones[m.confirmZone].Name)
		}
	case "record":
		if m.confirmZone >= 0 && m.confirmZone < len(m.zones) {
			recs := m.zones[m.confirmZone].Records
			if m.confirmRec >= 0 && m.confirmRec < len(recs) {
				r := recs[m.confirmRec]
				name := r.Name
				if name == "" {
					name = "@"
				}
				return fmt.Sprintf("Delete record %s %s %s?", name, r.RType, r.Value)
			}
		}
	case "section":
		return "Delete this section and all its entries?"
	case "entry":
		return "Delete this entry?"
	case "quit":
		return "Unsaved changes - quit anyway?"
	}
	return ""
}

func (m RootModel) helpText() string {
	switch m.state {
	case StateForm:
		return "tab next · enter submit · esc cancel"
	case StateConfirm:
		return "y confirm · any other key cancels"
	case StateForeign:
		return "j/k move · tab pane · / filter · g/G top/bottom · ctrl+d/u page · esc back"
	default:
		if m.view == ViewConfig {
			return "c local data · tab pane · j/k move · g/G top/bottom · ctrl+d/u page · w apply · f foreign · q quit"
		}
		return "a add-zone · r add-record · e ttl · t type · space toggle · d/D delete · w apply · f foreign · q quit"
	}
}

func (m RootModel) statusLine(w int) string {
	var segs []string
	switch m.state {
	case StateApplying:
		segs = append(segs, th.warn.Render("applying…"))
	case StateError:
		msg := "error"
		if m.lastError != nil {
			msg = "error: " + m.lastError.Error()
		}
		segs = append(segs, th.bad.Render(msg))
	default:
		word := "ready"
		if m.view == ViewConfig {
			word = "config"
		}
		segs = append(segs, th.ok.Render(word))
	}
	segs = append(segs, th.dimStyle.Render(fmt.Sprintf("zones %d · records %d", len(m.zones), m.recordCount())))
	if m.dirty {
		segs = append(segs, th.warn.Render("UNSAVED"))
	} else {
		segs = append(segs, th.ok.Render("saved"))
	}
	if os.Geteuid() != 0 {
		segs = append(segs, th.dimStyle.Render("not root"))
	}
	return fit(strings.Join(segs, th.dimStyle.Render(" · ")), w)
}

// titleText is the header line, including the version when known.
func (m RootModel) titleText() string {
	base := "unbound-tui"
	if m.version != "" {
		base += " " + m.version
	}
	if m.view == ViewConfig {
		base += " — config"
	}
	return base
}

// View renders the whole screen.
func (m RootModel) View() string {
	w, h := m.dims()

	overlay := ""
	overlayH := 0
	switch m.state {
	case StateForm, StateConfirm:
		overlay = m.overlayBox(w)
		overlayH = lipgloss.Height(overlay) + 1 // plus the blank separator line
	}
	notice := ""
	noticeH := 0
	if m.notice != "" {
		notice = th.dimStyle.Render(truncate(m.notice, w))
		noticeH = 1
	}

	bodyH := m.bodyHeightFor(h) - overlayH - noticeH
	if bodyH < 3 {
		bodyH = 3
	}

	var body string
	if m.state == StateForeign {
		body = m.foreign.view(w, bodyH)
	} else if m.view == ViewConfig {
		body = m.configPanes(w, bodyH)
	} else {
		body = m.mainPanes(w, bodyH)
	}

	lines := []string{th.title.Render(m.titleText()), ""}
	lines = append(lines, strings.Split(body, "\n")...)
	if overlay != "" {
		lines = append(lines, "", overlay)
	}
	if notice != "" {
		lines = append(lines, notice)
	}
	if h >= helpMinHeight {
		lines = append(lines, th.help.Render(truncate(m.helpText(), w)))
	}
	lines = append(lines, m.statusLine(w))
	return strings.Join(lines, "\n")
}
