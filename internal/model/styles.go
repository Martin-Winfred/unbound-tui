package model

import "github.com/charmbracelet/lipgloss"

// theme centralizes every style so the palette can be changed in one place.
// Colors are restrained: a single accent, a dim tone, and three status colors.
type theme struct {
	accent lipgloss.Color
	dim    lipgloss.Color
	green  lipgloss.Color
	yellow lipgloss.Color
	red    lipgloss.Color

	title       lipgloss.Style
	headerFocus lipgloss.Style
	headerBlur  lipgloss.Style
	paneFocus   lipgloss.Style
	paneBlur    lipgloss.Style
	selected    lipgloss.Style
	disabled    lipgloss.Style
	help        lipgloss.Style
	dimStyle    lipgloss.Style
	ok          lipgloss.Style
	warn        lipgloss.Style
	bad         lipgloss.Style
	box         lipgloss.Style
}

var th = newTheme()

func newTheme() theme {
	accent := lipgloss.Color("45")
	dim := lipgloss.Color("240")
	green := lipgloss.Color("42")
	yellow := lipgloss.Color("220")
	red := lipgloss.Color("203")

	return theme{
		accent: accent, dim: dim, green: green, yellow: yellow, red: red,

		title:       lipgloss.NewStyle().Bold(true).Foreground(accent),
		headerFocus: lipgloss.NewStyle().Bold(true).Foreground(accent),
		headerBlur:  lipgloss.NewStyle().Bold(true).Foreground(dim),
		paneFocus:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent),
		paneBlur:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dim),
		selected:    lipgloss.NewStyle().Reverse(true),
		disabled:    lipgloss.NewStyle().Foreground(dim),
		help:        lipgloss.NewStyle().Foreground(dim),
		dimStyle:    lipgloss.NewStyle().Foreground(dim),
		ok:          lipgloss.NewStyle().Foreground(green),
		warn:        lipgloss.NewStyle().Foreground(yellow),
		bad:         lipgloss.NewStyle().Foreground(red),
		box:         lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent),
	}
}
