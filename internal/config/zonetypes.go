package config

// zoneTypes is the whitelist of local-zone types accepted by the tool.
// The list mirrors unbound.conf(5); types the tool does not know are
// rejected at input time.
var zoneTypes = map[string]bool{
	"deny": true, "refuse": true, "static": true, "transparent": true,
	"typetransparent": true, "redirect": true, "nodefault": true,
	"inform": true, "inform_deny": true, "inform_redirect": true,
	"always_transparent": true, "always_refuse": true, "always_nxdomain": true,
	"always_nodata": true, "always_deny": true, "always_null": true, "noview": true,
	"block_a": true, "block_aaaa": true,
	"block_a_wdata": true, "block_aaaa_wdata": true,
}

// IsZoneTypeName reports whether typ is a known local-zone type name.
func IsZoneTypeName(typ string) bool { return zoneTypes[typ] }
