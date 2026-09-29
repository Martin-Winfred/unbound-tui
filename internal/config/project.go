package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// zoneDecl is a local-zone entry captured before records are attached.
type zoneDecl struct {
	name     string
	typ      string
	disabled bool
}

// recordDecl is a local-data entry awaiting attachment to its owning zone.
type recordDecl struct {
	owner    string // FQDN with a trailing dot
	ttl      int
	rtype    string
	value    string
	disabled bool
}

// ZonesFromFragment projects the generic section/entry model onto the v0.1
// zone model. local-zone entries declare zones; local-data entries attach to
// their longest-suffix owning zone, or spring an implicit transparent zone of
// the record's owner when no declared zone covers them. Records of a disabled
// zone are disabled too, so a disabled zone never contributes active data.
//
// The generic parser is total, but projection is not: a value that names a
// known directive yet cannot be parsed is a projection error naming the
// offending section and entry, never a silent drop. The Fragment itself stays
// total, so verbatim round-trips still work.
func ZonesFromFragment(f domain.Fragment) ([]domain.Zone, error) {
	var (
		zs []zoneDecl
		rs []recordDecl
	)
	for _, s := range f.Sections {
		for _, e := range s.Entries {
			switch e.Key {
			case "local-zone":
				z, err := parseZoneValue(e.Value)
				if err != nil {
					return nil, fmt.Errorf("invalid local-zone entry in section %s: %q: %w",
						sectionLabel(s.Kind), e.Value, err)
				}
				z.disabled = e.Disabled
				zs = append(zs, z)
			case "local-data":
				r, err := parseDataValue(e.Value)
				if err != nil {
					return nil, fmt.Errorf("invalid local-data entry in section %s: %q: %w",
						sectionLabel(s.Kind), e.Value, err)
				}
				r.disabled = e.Disabled
				rs = append(rs, r)
			}
		}
	}
	return attachRecords(zs, rs), nil
}

// sectionLabel renders a section kind for error messages; the synthetic
// top-level section (empty Kind) is named explicitly.
func sectionLabel(kind string) string {
	if kind == "" {
		return "top-level"
	}
	return kind
}

// parseZoneValue parses `"<name>" <type>` (the raw value of a local-zone
// entry); a missing type defaults to transparent.
func parseZoneValue(value string) (zoneDecl, error) {
	name, tail, err := cutToken(value)
	if err != nil {
		return zoneDecl{}, fmt.Errorf("local-zone: %w", err)
	}
	typ := strings.TrimSpace(tail)
	if typ == "" {
		typ = "transparent"
	}
	return zoneDecl{name: domain.FQDN(name), typ: typ}, nil
}

// parseDataValue parses `"<RR>"` (the raw value of a local-data entry) into
// its record parts.
func parseDataValue(value string) (recordDecl, error) {
	rr, _, err := cutToken(value)
	if err != nil {
		return recordDecl{}, fmt.Errorf("local-data: %w", err)
	}
	owner, ttl, class, rtype, rval, err := parseRR(rr)
	if err != nil {
		return recordDecl{}, fmt.Errorf("local-data %q: %w", rr, err)
	}
	if class != "" && !strings.EqualFold(class, "IN") {
		return recordDecl{}, fmt.Errorf("local-data %q: unsupported class %q", rr, class)
	}
	return recordDecl{owner: domain.FQDN(owner), ttl: ttl, rtype: rtype, value: rval}, nil
}

// attachRecords builds the sorted zone model: it attaches every record to its
// innermost declared zone and springs an implicit transparent zone for
// records no declared zone owns.
func attachRecords(zs []zoneDecl, rs []recordDecl) []domain.Zone {
	byName := make(map[string]*domain.Zone)
	var order []string
	addZone := func(name, typ string, disabled bool) *domain.Zone {
		z, ok := byName[name]
		if !ok {
			z = &domain.Zone{Name: name, Type: typ, Disabled: disabled}
			byName[name] = z
			order = append(order, name)
		}
		return z
	}
	for _, z := range zs {
		if z.name == "" {
			continue
		}
		zone := addZone(z.name, z.typ, z.disabled)
		zone.Type = z.typ
		zone.Disabled = z.disabled
	}

	for _, r := range rs {
		name := owningZoneName(r.owner, order)
		if name == "" {
			name = r.owner
			addZone(name, "transparent", r.disabled)
		}
		zone := byName[name]
		rel := relativeName(r.owner, zone.Name)
		zone.Records = append(zone.Records, domain.Record{
			Name:     rel,
			RType:    r.rtype,
			Value:    r.value,
			TTL:      r.ttl,
			Disabled: r.disabled || zone.Disabled,
		})
	}

	out := make([]domain.Zone, 0, len(order))
	for _, name := range order {
		z := byName[name]
		sort.SliceStable(z.Records, func(i, j int) bool {
			a, b := z.Records[i], z.Records[j]
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			if a.RType != b.RType {
				return a.RType < b.RType
			}
			return a.Value < b.Value
		})
		out = append(out, *z)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// cutToken returns the first token of s (unquoted when quoted) and the rest.
func cutToken(s string) (token, rest string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("empty value")
	}
	if s[0] == '"' {
		end := strings.IndexByte(s[1:], '"')
		if end < 0 {
			return "", "", fmt.Errorf("unterminated quote")
		}
		return s[1 : 1+end], strings.TrimSpace(s[2+end:]), nil
	}
	fields := strings.Fields(s)
	return fields[0], strings.TrimSpace(s[len(fields[0]):]), nil
}

// parseRR parses one zonefile RR: `owner [ttl] [class] type rdata`.
func parseRR(line string) (owner string, ttl int, class, rtype, value string, err error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", 0, "", "", "", fmt.Errorf("too few fields")
	}
	owner = fields[0]
	rest := fields[1:]
	ttl = 3600
	switch {
	case len(rest) >= 2 && strings.EqualFold(rest[1], "IN"):
		// owner ttl IN type rdata
		if t, e := strconv.Atoi(rest[0]); e == nil {
			ttl = t
		}
		class = rest[1]
		rest = rest[2:]
	case strings.EqualFold(rest[0], "IN"):
		// owner IN [ttl] type rdata
		class = rest[0]
		rest = rest[1:]
		if len(rest) >= 2 {
			if t, e := strconv.Atoi(rest[0]); e == nil {
				ttl = t
				rest = rest[1:]
			}
		}
	default:
		// owner [ttl] type rdata
		if t, e := strconv.Atoi(rest[0]); e == nil {
			ttl = t
			rest = rest[1:]
		}
	}
	if len(rest) < 2 {
		return "", 0, "", "", "", fmt.Errorf("too few fields after class/ttl")
	}
	return owner, ttl, class, strings.ToUpper(rest[0]), strings.Join(rest[1:], " "), nil
}

// asciiLower lowercases the ASCII bytes 'A'-'Z' and leaves every other byte
// untouched. RFC 4343 folds case for ASCII only, and unlike strings.ToLower it
// never changes a string's byte length, so a folded suffix match cannot desync
// from the original strings used to derive a record's relative name.
func asciiLower(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 'A' || c > 'Z' {
			continue
		}
		if b == nil {
			b = []byte(s)
		}
		b[i] = c + ('a' - 'A')
	}
	if b == nil {
		return s
	}
	return string(b)
}

// relativeName returns owner's name relative to zone while preserving the
// owner's original spelling. DNS names fold case (RFC 4343), so the
// owner/zone comparison ignores case; an owner equal to the zone becomes "@".
func relativeName(owner, zone string) string {
	lo, lz := asciiLower(owner), asciiLower(zone)
	switch {
	case lo == lz:
		return "@"
	case strings.HasSuffix(lo, "."+lz):
		return owner[:len(owner)-len(zone)-1]
	default:
		return owner
	}
}

// owningZoneName returns the longest declared zone that contains owner, or "".
// Matching folds case per RFC 4343 while the returned name keeps the zone
// entry's original spelling; the suffix must still fall on a label boundary
// ("."+zone). This is the M5 semantic change from the byte-exact v0.1 behavior
// that M2/M3 reviews had pinned.
func owningZoneName(owner string, zoneNames []string) string {
	lo := asciiLower(owner)
	best := ""
	for _, z := range zoneNames {
		lz := asciiLower(z)
		if lo == lz || strings.HasSuffix(lo, "."+lz) {
			if len(z) > len(best) {
				best = z
			}
		}
	}
	return best
}
