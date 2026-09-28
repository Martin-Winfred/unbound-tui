package config

import "fmt"

// ErrMissingInclude is the ConfigError code reported when the main Unbound
// config does not include the unbound-tui fragment.
const ErrMissingInclude = "MISSING_INCLUDE"

// ConfigError is the error type for actionable configuration problems.
// The design doc (§5.1) returns ConfigError without defining it; this gap
// fill pins the structure and the format rendered by Error.
type ConfigError struct {
	Type    string
	Message string
	Fix     string
}

// Error renders the error as "[TYPE] message" followed by a "Fix:" hint.
func (e *ConfigError) Error() string {
	return fmt.Sprintf("[%s] %s\nFix: %s", e.Type, e.Message, e.Fix)
}
