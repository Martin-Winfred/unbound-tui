package model

import "github.com/Martin-Winfred/unbound-tui/internal/domain"

// AppState enumerates the TUI states.
type AppState int

const (
	StateReady AppState = iota
	StateForm
	StateForeign
	StateApplying
	StateConfirm
	StateError
)

// ZonesLoadedMsg carries the model parsed from the fragment file at startup.
type ZonesLoadedMsg struct {
	Zones []domain.Zone
}

// AppliedMsg reports a successful write + reload.
type AppliedMsg struct{}

// ForeignLoadedMsg carries the read-only snapshot of what Unbound currently
// serves outside our fragment.
type ForeignLoadedMsg struct {
	Zones []domain.LocalZone
	RRs   []string
	Err   error
}

// FormSubmitMsg carries validated form values to the root model.
type FormSubmitMsg struct {
	Mode      FormMode
	ZoneIndex int
	RecIndex  int
	Name      string
	Type      string
	Value     string
	TTL       int
}

// FormCancelMsg reports an esc from the form.
type FormCancelMsg struct{}

// ForeignCloseMsg reports a close of the foreign view.
type ForeignCloseMsg struct{}

// ErrorMsg moves the model into StateError.
type ErrorMsg struct {
	Error error
}
