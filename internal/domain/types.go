package domain

// Zone is a local-zone owned by the tool together with its records.
type Zone struct {
	Name     string
	Type     string
	Disabled bool
	Records  []Record
}

// Record is one local-data entry, named relative to its owning zone.
// An empty Name or "@" denotes the zone apex.
type Record struct {
	Name     string
	RType    string
	Value    string
	TTL      int
	Disabled bool
}

// StatusInfo is the parsed result of `unbound-control status`.
type StatusInfo struct {
	Version     string // "1.26.0"
	ControlType string // "namedpipe" | "ssl" | "" (not enabled)
	Raw         string
}

// LocalZone is a runtime zone entry as reported by
// `unbound-control list_local_zones`.
type LocalZone struct {
	Name string
	Type string
}
