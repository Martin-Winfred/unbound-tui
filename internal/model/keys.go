package model

// Key bindings (tea.KeyMsg.String() spellings).
//
//	j / down, k / up   move the focused cursor
//	tab                switch focus between the panes
//	g / G              jump to the top / bottom of the focused pane
//	ctrl+d / ctrl+u    page down / up
//	a                  add a zone (Config view: add an entry to the focused section)
//	A                  Config view: add a new section
//	r                  add a record to the focused zone
//	e                  edit the focused record's TTL (Config view: edit the focused entry)
//	E                  Config view: specialized form for the selected forward-zone/stub-zone
//	t                  change the focused zone's type
//	d                  delete the focused record (confirmation)
//	D                  delete the focused zone and its records (confirmation)
//	space              enable/disable the focused entry
//	w                  apply: write the fragment and reload Unbound
//	c                  toggle between the Local data and Config views
//	f                  toggle the read-only foreign view (with / to filter)
//	q                  quit (asks first when there are unsaved changes)
//	ctrl+c             quit; also immediate from modal/apply states (Ready confirms)
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
	"A":      actionAddSection,
	"r":      actionAddRecord,
	"e":      actionEditTTL,
	"E":      actionSpecializeSection,
	"t":      actionSetType,
	"d":      actionDeleteRecord,
	"D":      actionDeleteZone,
	" ":      actionToggleDisabled,
	"w":      actionApply,
	"c":      actionSwitchView,
	"f":      actionForeign,
	"q":      actionQuit,
	keyQuit:  actionQuit,
}

// keyQuit is the ctrl+c binding. It is also checked centrally in Update so it
// quits immediately from the modal and apply states (which route or swallow
// their own keys); StateReady keeps q's confirm-when-dirty flow.
const keyQuit = "ctrl+c"

const (
	actionUp                = "up"
	actionDown              = "down"
	actionTop               = "top"
	actionBottom            = "bottom"
	actionPageDown          = "page-down"
	actionPageUp            = "page-up"
	actionTogglePane        = "toggle-pane"
	actionAddZone           = "add-zone"
	actionAddSection        = "add-section"
	actionAddRecord         = "add-record"
	actionEditTTL           = "edit-ttl"
	actionSpecializeSection = "specialize-section"
	actionSetType           = "set-zone-type"
	actionDeleteRecord      = "delete-record"
	actionDeleteZone        = "delete-zone"
	actionToggleDisabled    = "toggle-disabled"
	actionApply             = "apply"
	actionSwitchView        = "switch-view"
	actionForeign           = "foreign"
	actionQuit              = "quit"
)
