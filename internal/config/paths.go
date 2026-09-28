package config

// DefaultFragmentPath is the location of the fragment file the tool owns
// exclusively. Everything else in the Unbound configuration is read-only
// for this tool.
const DefaultFragmentPath = "/etc/unbound/unbound.d/unbound-tui.conf"
