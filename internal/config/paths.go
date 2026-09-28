package config

// DefaultFragmentPath is the location of the fragment file the tool owns
// exclusively. Everything else in the Unbound configuration is read-only
// for this tool. Debian and Ubuntu ship
// `include-toplevel: "/etc/unbound/unbound.conf.d/*.conf"`, so this drop-in is
// loaded automatically; on layouts without that include the user adds an
// include line for it.
const DefaultFragmentPath = "/etc/unbound/unbound.conf.d/unbound-tui.conf"
