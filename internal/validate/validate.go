package validate

import (
	"fmt"
	"net/netip"
	"regexp"
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
// only ever produced from records that passed this check.
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
	if !rtypeWhitelist[r.RType] {
		return fmt.Errorf("unsupported record type %q", r.RType)
	}
	if r.TTL < 0 || r.TTL > maxTTL {
		return fmt.Errorf("ttl %d out of range [0, %d]", r.TTL, maxTTL)
	}
	return validateValue(r.RType, r.Value)
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
