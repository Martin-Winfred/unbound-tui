package domain

// Entry is one directive line inside a fragment section. Key is the directive
// name (for example "local-zone" or "forward-addr"); Value keeps the raw text
// after "key:" byte-for-byte, quotes included.
type Entry struct {
	Key      string
	Value    string
	Disabled bool
}

// Section groups the entries declared under one directive header. Kind is the
// header directive name ("server", "forward-zone", "stub-zone", ...); an empty
// Kind is the synthetic top level holding entries that appear before any
// section header.
type Section struct {
	Kind    string
	Entries []Entry
}

// Fragment is the parsed fragment file: an ordered list of sections, each
// holding its entries in file order.
type Fragment struct {
	Sections []Section
}
