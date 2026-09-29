package validate

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// zoneLabelRe is the RFC 1035 label rule: letters/digits/hyphens, 1-63
// chars, no leading or trailing hyphen.
var zoneLabelRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// recordLabelRe additionally permits a leading underscore, which real records
// use (_dmarc, _acme-challenge, SRV service names).
var recordLabelRe = regexp.MustCompile(`^[a-zA-Z0-9_]([a-zA-Z0-9_-]{0,61}[a-zA-Z0-9_])?$`)

var rtypeWhitelist = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "PTR": true,
	"MX": true, "TXT": true, "SRV": true, "NS": true,
	// Note: CAA values contain quotes, which conflicts with injection
	// defense; not supported for now (re-evaluate later if needed).
}

const maxTTL = 604800 // 7 days

// ValidateRecord validates a record in the context of the zone it belongs to.
// It is the single entry point for every write path: the fragment file is
// only ever produced from records that passed this check. The gate is
// deliberately tightened beyond unbound's own permissive parser — the owner
// shape, injection characters and unknown record types are rejected here, and
// the record type is matched case-insensitively. TTL, type and rdata are
// validated by ValidateRRLine (the owner by validRecordName above), so a
// record and the local-data line it renders to accept the same values.
func ValidateRecord(zone string, r domain.Record) error {
	if err := ValidateZoneName(zone); err != nil {
		return fmt.Errorf("zone: %w", err)
	}
	if !validRecordName(r.Name) {
		return fmt.Errorf("invalid record name %q", r.Name)
	}
	if injectable(zone) || injectable(r.Name) || injectable(r.Value) {
		return fmt.Errorf("record contains forbidden characters (quote/backslash/control/;/#)")
	}
	name := r.Name
	if name == "" {
		name = "@" // apex records render as the zone name, never an empty owner
	}
	return ValidateRRLine(fmt.Sprintf("%s %d %s %s", name, r.TTL, r.RType, r.Value))
}

// ValidateRRLine validates one local-data RR line: `owner ttl [class] rtype
// rdata`, where the class (IN) is optional. The TTL is required: unlike
// unbound's own parser, this gate applies no default when the field is
// missing. A single pair of surrounding double quotes is stripped first, since
// the raw value of a local-data entry keeps them. The owner may be a relative
// name (as accepted by ValidateRecord) or a fully-qualified name with a
// trailing dot, as unbound writes it.
func ValidateRRLine(line string) error {
	line = strings.TrimSpace(line)
	if len(line) >= 2 && line[0] == '"' && line[len(line)-1] == '"' {
		line = strings.TrimSpace(line[1 : len(line)-1])
	}
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return fmt.Errorf("invalid RR line %q: want owner ttl [class] rtype rdata", line)
	}
	owner := fields[0]
	if !validRROwner(owner) {
		return fmt.Errorf("invalid record name %q", owner)
	}
	ttl, err := strconv.Atoi(fields[1])
	if err != nil {
		return fmt.Errorf("invalid ttl %q", fields[1])
	}
	if ttl < 0 || ttl > maxTTL {
		return fmt.Errorf("ttl %d out of range [0, %d]", ttl, maxTTL)
	}
	i := 2
	if strings.EqualFold(fields[i], "IN") {
		i++
	}
	if len(fields) < i+2 {
		return fmt.Errorf("invalid RR line %q: missing rtype or rdata", line)
	}
	rtype := strings.ToUpper(fields[i])
	if !rtypeWhitelist[rtype] {
		return fmt.Errorf("unsupported record type %q", rtype)
	}
	return validateValue(rtype, strings.Join(fields[i+1:], " "))
}

// ValidateZoneName validates a zone name (an optional trailing dot is allowed).
func ValidateZoneName(name string) error {
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 253 {
		return fmt.Errorf("invalid zone name length: %q", name)
	}
	for _, label := range strings.Split(name, ".") {
		if !zoneLabelRe.MatchString(label) {
			return fmt.Errorf("invalid label %q", label)
		}
	}
	return nil
}

// validRecordName accepts the apex ("@"), a wildcard ("*" or "*.<labels>"),
// or a dot-separated relative name whose every label is non-empty and valid.
func validRecordName(name string) bool {
	if name == "" || name == "@" || name == "*" {
		return true
	}
	if strings.HasPrefix(name, "*.") {
		name = strings.TrimPrefix(name, "*.")
		if name == "" {
			return false
		}
	}
	for _, label := range strings.Split(name, ".") {
		if !recordLabelRe.MatchString(label) {
			return false
		}
	}
	return true
}

// validRROwner accepts an owner as it appears on a local-data RR line: either
// a relative record name (the ValidateRecord form) or a fully-qualified name
// with a trailing dot.
func validRROwner(name string) bool {
	if name == "." {
		return false
	}
	return validRecordName(strings.TrimSuffix(name, "."))
}

// injectable reports whether s contains a character that could escape the
// config or command quoting: quotes, backslash, ";", "#", or any control
// character (including tab, CR and LF).
func injectable(s string) bool {
	if strings.ContainsAny(s, "\"'\\;#") {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// validateValue validates the value format per record type.
func validateValue(rtype, value string) error {
	switch rtype {
	case "A":
		addr, err := netip.ParseAddr(value)
		if err != nil || !addr.Is4() {
			return fmt.Errorf("invalid A address %q", value)
		}
	case "AAAA":
		addr, err := netip.ParseAddr(value)
		if err != nil || !addr.Is6() {
			return fmt.Errorf("invalid AAAA address %q", value)
		}
	case "CNAME", "PTR", "NS":
		if strings.Contains(value, " ") {
			return fmt.Errorf("invalid %s target %q", rtype, value)
		}
		if err := ValidateZoneName(value); err != nil {
			return fmt.Errorf("invalid %s target: %w", rtype, err)
		}
	case "TXT":
		if len(value) > 255 {
			return fmt.Errorf("TXT string exceeds 255 bytes")
		}
	case "MX":
		var pref int
		var host string
		if n, err := fmt.Sscanf(value, "%d %s", &pref, &host); err != nil || n != 2 {
			return fmt.Errorf("invalid MX value %q (want \"<pref> <host>\")", value)
		}
		if err := ValidateZoneName(host); err != nil {
			return fmt.Errorf("invalid MX host: %w", err)
		}
	case "SRV":
		var prio, weight, port int
		var target string
		if n, err := fmt.Sscanf(value, "%d %d %d %s", &prio, &weight, &port, &target); err != nil || n != 4 {
			return fmt.Errorf("invalid SRV value %q (want \"<prio> <weight> <port> <target>\")", value)
		}
		if err := ValidateZoneName(target); err != nil {
			return fmt.Errorf("invalid SRV target: %w", err)
		}
	}
	return nil
}
