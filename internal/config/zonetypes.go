package config

import "github.com/Martin-Winfred/unbound-tui/internal/domain"

// IsZoneTypeName reports whether typ is a known local-zone type name. The
// canonical set lives in domain so config and validate share one list.
func IsZoneTypeName(typ string) bool { return domain.IsZoneTypeName(typ) }
