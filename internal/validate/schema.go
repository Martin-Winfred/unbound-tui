package validate

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Type is the schema type of a configuration value. It tells the editor how
// to hint and validate the value of a directive; unknown directives are plain
// text.
type Type string

const (
	TypeBool Type = "bool"
	TypeInt  Type = "int"
	TypePath Type = "path"
	TypeAddr Type = "address"
	TypeCIDR Type = "cidr"
	TypeRR   Type = "rr"
	// TypeZone is a `name [zone-type]` line, the raw value of a local-zone
	// entry (for example `"example.com" static`).
	TypeZone Type = "zone"
	TypeText Type = "text"
)

// zoneTypeWhitelist is the set of local-zone types the schema accepts. It
// intentionally mirrors the standard unbound.conf(5) set only and is kept
// separate from config.IsZoneTypeName: config and validate must not import
// each other (both are leaves over domain), so the short list is duplicated
// here on purpose.
var zoneTypeWhitelist = map[string]bool{
	"deny": true, "refuse": true, "static": true,
	"transparent": true, "redirect": true,
}

// schemaRegistry seeds the (section kind, entry key) -> Type map. Only the
// two local-* directives are registered in M2; everything else is free text.
var schemaRegistry = map[[2]string]Type{
	{"server", "local-data"}: TypeRR,
	{"server", "local-zone"}: TypeZone,
}

// SchemaFor returns the schema type registered for a section kind and entry
// key, or TypeText when nothing is registered. The empty Type and TypeText
// are interchangeable to ValidateValue.
func SchemaFor(kind, key string) Type {
	if t, ok := schemaRegistry[[2]string{kind, key}]; ok {
		return t
	}
	return TypeText
}

// ValidateValue validates a value against its schema type. An empty or
// unregistered Type is treated as TypeText: only control characters are
// rejected.
func ValidateValue(t Type, value string) error {
	switch t {
	case TypeBool:
		return validateBool(value)
	case TypeInt:
		return validateInt(value)
	case TypePath:
		return validatePath(value)
	case TypeAddr:
		return validateAddress(value)
	case TypeCIDR:
		return validateCIDR(value)
	case TypeRR:
		return ValidateRRLine(value)
	case TypeZone:
		return validateZoneLine(value)
	default: // TypeText and the empty Type
		return validateText(value)
	}
}

func validateBool(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "no", "true", "false", "on", "off", "0", "1":
		return nil
	}
	return fmt.Errorf("invalid boolean %q (want yes/no/true/false/on/off/0/1)", value)
}

func validateInt(value string) error {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("invalid integer %q", value)
	}
	if n < 0 {
		return fmt.Errorf("integer %d must be >= 0", n)
	}
	return nil
}

func validatePath(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("empty path")
	}
	return checkControl(value)
}

// validateAddress accepts a bare host (hostname or IP literal), "host:port",
// or "[ipv6]:port". A port, when present, must be in [1, 65535].
func validateAddress(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("empty address")
	}
	if strings.HasPrefix(value, "[") {
		end := strings.IndexByte(value, ']')
		if end < 0 {
			return fmt.Errorf("invalid address %q: missing ']'", value)
		}
		addr, err := netip.ParseAddr(value[1:end])
		if err != nil || !addr.Is6() {
			return fmt.Errorf("invalid IPv6 address %q", value[1:end])
		}
		rest := value[end+1:]
		if rest == "" {
			return nil
		}
		if !strings.HasPrefix(rest, ":") {
			return fmt.Errorf("invalid address %q", value)
		}
		return validatePort(rest[1:], value)
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return nil
	}
	host, port, hasPort := strings.Cut(value, ":")
	if err := validateHost(host, value); err != nil {
		return err
	}
	if hasPort {
		return validatePort(port, value)
	}
	return nil
}

func validateHost(host, whole string) error {
	if host == "" {
		return fmt.Errorf("invalid address %q: empty host", whole)
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil
	}
	if err := ValidateZoneName(host); err != nil {
		return fmt.Errorf("invalid host %q: %w", host, err)
	}
	return nil
}

func validatePort(port, whole string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid port %q in address %q", port, whole)
	}
	return nil
}

func validateCIDR(value string) error {
	value = strings.TrimSpace(value)
	p, err := netip.ParsePrefix(value)
	if err != nil {
		return fmt.Errorf("invalid CIDR %q", value)
	}
	if p.Masked() != p {
		return fmt.Errorf("invalid CIDR %q: host bits set", value)
	}
	return nil
}

// validateZoneLine validates a local-zone value: `name [zone-type]`, with the
// name optionally quoted and the type defaulting to transparent.
func validateZoneLine(value string) error {
	name, rest, err := splitQuotedToken(value)
	if err != nil {
		return err
	}
	if err := ValidateZoneName(name); err != nil {
		return fmt.Errorf("invalid zone name: %w", err)
	}
	typ := strings.TrimSpace(rest)
	if typ == "" {
		typ = "transparent"
	}
	if !zoneTypeWhitelist[typ] {
		return fmt.Errorf("unsupported zone type %q", typ)
	}
	return nil
}

func validateText(value string) error {
	return checkControl(value)
}

// checkControl rejects any control character (including tab, CR, LF and DEL).
func checkControl(value string) error {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("value contains control character %#U", r)
		}
	}
	return nil
}

// splitQuotedToken returns the first token of s (unquoted when quoted) and
// the remaining text. It is the validate-local twin of config.cutToken,
// duplicated to keep the packages lateral-free.
func splitQuotedToken(s string) (token, rest string, err error) {
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
