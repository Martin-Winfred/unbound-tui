package unbound

import (
	"fmt"
	"os/exec"
	"strings"
)

// UnboundError is the error type every failed unbound-control call returns.
type UnboundError struct {
	Code    string
	Message string
	Detail  string
}

// Error renders the error as "[CODE] message: detail". When Detail is empty
// it renders "[CODE] message" without a dangling ": " separator.
//
// NOTE: this is the single sanctioned deviation from design doc §4.3
// (line 628 always appends ": %s"): timeout errors carry no Detail, and the
// TUI status bar must not display a trailing colon.
func (e *UnboundError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("[%s] %s: %s", e.Code, e.Message, e.Detail)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Error codes carried by UnboundError.Code.
const (
	ErrSyntax         = "SYNTAX_ERROR"      // invalid RR data (input failed validation or shape mismatch)
	ErrPermission     = "PERMISSION_DENIED" // insufficient permissions on the control interface
	ErrControlTimeout = "CONTROL_TIMEOUT"   // unbound-control did not finish within the client timeout
	ErrNotRunning     = "NOT_RUNNING"       // unbound is not running
	ErrExit           = "EXIT_ERROR"        // any other non-zero exit
)

// wrapError maps an exec error plus the captured combined output to an
// *UnboundError.
func wrapError(err error, output string) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*exec.ExitError); ok {
		detail := output
		switch {
		case strings.Contains(detail, "syntax error"):
			return &UnboundError{Code: ErrSyntax, Message: "invalid RR data", Detail: detail}
		case strings.Contains(detail, "permission denied"):
			return &UnboundError{Code: ErrPermission, Message: "check control interface permissions", Detail: detail}
		}
		return &UnboundError{Code: ErrExit, Message: "command exited with error", Detail: detail}
	}
	return &UnboundError{Code: ErrExit, Message: err.Error()}
}
