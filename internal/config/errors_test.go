package config

import "testing"

// TestConfigErrorFormat pins the actionable error text CheckInclude surfaces:
// "[TYPE] message" followed by a "Fix:" hint on its own line.
func TestConfigErrorFormat(t *testing.T) {
	e := &ConfigError{
		Type:    ErrMissingInclude,
		Message: "main config does not include our fragment",
		Fix:     "add it",
	}
	want := "[MISSING_INCLUDE] main config does not include our fragment\nFix: add it"
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
