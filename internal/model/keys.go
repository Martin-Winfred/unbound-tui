package model

// Key bindings (tea.KeyMsg.String() spellings).
//
//	j / down, k / up   move the focused cursor
//	tab                switch focus between the panes
//	g / G              jump to the top / bottom of the focused pane
//	ctrl+d / ctrl+u    page down / up
//	a                  add a zone
//	r                  add a record to the focused zone
//	e                  edit the focused record's TTL
//	t                  change the focused zone's type
//	d                  delete the focused record (confirmation)
//	D                  delete the focused zone and its records (confirmation)
//	space              enable/disable the focused entry
//	w                  apply: write the fragment and reload Unbound
//	f                  toggle the read-only foreign view (with / to filter)
//	q / ctrl+c         quit (asks first when there are unsaved changes)
var keyMap = map[string]string{
	"up":     actionUp,
	"k":      actionUp,
	"down":   actionDown,
	"j":      actionDown,
	"g":      actionTop,
	"G":      actionBottom,
	"ctrl+d": actionPageDown,
	"ctrl+u": actionPageUp,
	"tab":    actionTogglePane,
	"a":      actionAddZone,
	"r":      actionAddRecord,
	"e":      actionEditTTL,
	"t":      actionSetType,
	"d":      actionDeleteRecord,
	"D":      actionDeleteZone,
	" ":      actionToggleDisabled,
	"w":      actionApply,
	"f":      actionForeign,
	"q":      actionQuit,
	"ctrl+c": actionQuit,
}

const (
	actionUp             = "up"
	actionDown           = "down"
	actionTop            = "top"
	actionBottom         = "bottom"
	actionPageDown       = "page-down"
	actionPageUp         = "page-up"
	actionTogglePane     = "toggle-pane"
	actionAddZone        = "add-zone"
	actionAddRecord      = "add-record"
	actionEditTTL        = "edit-ttl"
	actionSetType        = "set-zone-type"
	actionDeleteRecord   = "delete-record"
	actionDeleteZone     = "delete-zone"
	actionToggleDisabled = "toggle-disabled"
	actionApply          = "apply"
	actionForeign        = "foreign"
	actionQuit           = "quit"
)
