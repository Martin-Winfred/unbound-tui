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

// ZonesLoadedMsg carries the generic fragment read from disk at startup. The
// root model stores it as the source of truth and projects the zones from it.
type ZonesLoadedMsg struct {
	Fragment domain.Fragment
	// Upstreams is the load-time snapshot of the include graph's foreign
	// forward/stub sections, used for add-time conflict warnings. It is nil
	// when the upstream read failed.
	Upstreams []UpstreamRow
}

// AppliedMsg reports a successful write + reload.
type AppliedMsg struct{}

// ForeignLoadedMsg carries the read-only snapshot of what Unbound currently
// serves outside our fragment.
type ForeignLoadedMsg struct {
	Zones     []domain.LocalZone
	RRs       []string
	Upstreams []UpstreamRow
	// UpErr is a non-fatal upstream-fetch error: the list degrades to empty
	// and the message is shown in the upstreams tab.
	UpErr string
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
