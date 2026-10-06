package validate

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
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
	// TypeControlAddr is a control-interface value: an IP address or interface
	// name with an optional `@port`, or an absolute Unix socket path.
	TypeControlAddr Type = "control-address"
	TypeCIDR        Type = "cidr"
	TypeRR          Type = "rr"
	// TypeZone is a `name [zone-type]` line, the raw value of a local-zone
	// entry (for example `"example.com" static`).
	TypeZone Type = "zone"
	// TypeUpstream is an `ip[@port][#auth]` entry, the value of a
	// forward-addr or stub-addr upstream.
	TypeUpstream Type = "upstream"
	// TypeHost is a `hostname[@port]` entry, the value of a forward-host or
	// stub-host upstream.
	TypeHost Type = "host"
	// TypeAccessCtrl is an access-control value, `"<CIDR> <action>"` (for
	// example `"192.0.2.0/24 allow"`).
	TypeAccessCtrl Type = "access-control"
	// TypePort is a bare TCP/UDP port number in [1, 65535].
	TypePort Type = "port"
	// TypeZoneName is a single domain name with an optional surrounding
	// quote pair; root `"."` (quoted or not) is accepted. It is the value of
	// a forward-zone or stub-zone `name` entry.
	TypeZoneName Type = "zone-name"
	TypeText     Type = "text"
)

// accessCtrlActions is the set of actions unbound accepts in an
// access-control value. It mirrors unbound.conf(5) exactly and is pinned here
// because validate must not import config. Note that the local-zone types
// (always_transparent, always_refuse, always_nxdomain, ...) are NOT valid
// access-control actions and must not be added here.
var accessCtrlActions = map[string]bool{
	"allow":            true,
	"deny":             true,
	"refuse":           true,
	"allow_snoop":      true,
	"allow_setrd":      true,
	"allow_cookie":     true,
	"deny_non_local":   true,
	"refuse_non_local": true,
}

// schemaRegistry seeds the (section kind, entry key) -> Type map. It covers
// the local-* directives, the forward-zone and stub-zone upstream directives,
// and the server/remote-control options with a meaningful schema type;
// everything else is free text.
var schemaRegistry = map[[2]string]Type{
	{"server", "local-data"}: TypeRR,
	{"server", "local-zone"}: TypeZone,

	{"server", "interface"}:       TypeAddr,
	{"server", "port"}:            TypePort,
	{"server", "verbosity"}:       TypeInt,
	{"server", "username"}:        TypeText,
	{"server", "tls-cert-bundle"}: TypePath,
	{"server", "root-hints"}:      TypePath,
	{"server", "access-control"}:  TypeAccessCtrl,

	{"remote-control", "control-enable"}:    TypeBool,
	{"remote-control", "control-interface"}: TypeControlAddr,
	{"remote-control", "control-port"}:      TypePort,
	{"remote-control", "control-use-cert"}:  TypeBool,
	{"remote-control", "server-key-file"}:   TypePath,
	{"remote-control", "server-cert-file"}:  TypePath,
	{"remote-control", "control-key-file"}:  TypePath,
	{"remote-control", "control-cert-file"}: TypePath,

	{"forward-zone", "name"}:                 TypeZoneName,
	{"forward-zone", "forward-addr"}:         TypeUpstream,
	{"forward-zone", "forward-host"}:         TypeHost,
	{"forward-zone", "forward-tls-upstream"}: TypeBool,
	{"forward-zone", "forward-first"}:        TypeBool,

	{"stub-zone", "name"}:       TypeZoneName,
	{"stub-zone", "stub-addr"}:  TypeUpstream,
	{"stub-zone", "stub-host"}:  TypeHost,
	{"stub-zone", "stub-prime"}: TypeBool,
	{"stub-zone", "stub-first"}: TypeBool,
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
// unregistered Type is treated as TypeText, which rejects control characters
// and the config delimiters (quote, backslash, ';' and '#'); a value can
// therefore never smuggle a second directive past an unknown key.
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
	case TypeControlAddr:
		return validateControlAddr(value)
	case TypeCIDR:
		return validateCIDR(value)
	case TypeRR:
		return ValidateRRLine(value)
	case TypeZone:
		return validateZoneLine(value)
	case TypeUpstream:
		return validateUpstream(value)
	case TypeHost:
		return validateHostValue(value)
	case TypeAccessCtrl:
		return validateAccessCtrl(value)
	case TypePort:
		return validatePortValue(value)
	case TypeZoneName:
		return validateZoneNameValue(value)
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
	if err := rejectDelimiters(value); err != nil {
		return err
	}
	return checkControl(value)
}

// validateAddress accepts an `interface` value: an IP literal or an interface
// name, with an optional `@port` suffix (port in [1, 65535]). The `host:port`
// and `[v6]:port` spellings are not unbound syntax and are rejected: an IPv6
// literal is matched whole (it contains colons), while any other colon or
// bracket is refused.
func validateAddress(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("empty address")
	}
	host := value
	if i := strings.LastIndexByte(value, '@'); i >= 0 {
		host = value[:i]
		if err := validatePort(value[i+1:], value, "address"); err != nil {
			return err
		}
	}
	if host == "" {
		return fmt.Errorf("invalid address %q: empty host", value)
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil
	}
	if strings.ContainsAny(host, ":[]") {
		return fmt.Errorf("invalid address %q", value)
	}
	return validateHost(host, value)
}

// validateControlAddr accepts a `control-interface` value: the same
// interface/IP[@port] syntax as validateAddress, or an absolute Unix socket
// path (Debian's default is `/run/unbound.ctl`). Paths stay exclusive to this
// type so the generic address validator never lets a `server interface` be a
// socket path.
func validateControlAddr(value string) error {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "/") {
		return validatePath(value)
	}
	return validateAddress(value)
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

func validatePort(port, whole, noun string) error {
	if _, err := parsePort(port); err != nil {
		return fmt.Errorf("invalid port %q in %s %q", port, noun, whole)
	}
	return nil
}

// parsePort parses a decimal port number and enforces [1, 65535]. Parsing is
// strict: surrounding whitespace is rejected, since a caller splitting
// `host:port` or `ip@port` must not let a stray space through. Its error names
// the accepted range and is shared by the address and bare-port validators.
func parsePort(value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q, out of range [1, 65535]", value)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("port %d out of range [1, 65535]", n)
	}
	return n, nil
}

// validatePortValue validates a bare port number, the value of a `port` or
// `control-port` directive.
func validatePortValue(value string) error {
	_, err := parsePort(value)
	return err
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

// validateAccessCtrl validates an access-control value: exactly the two
// whitespace-separated tokens `<CIDR> <action>`. The CIDR follows the same
// rules as TypeCIDR (host bits must be clear) and the action must be one of
// the pinned unbound actions.
func validateAccessCtrl(value string) error {
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return errors.New(`expected "<CIDR> <action>"`)
	}
	if err := validateCIDR(fields[0]); err != nil {
		return fmt.Errorf("access-control: %w", err)
	}
	if !accessCtrlActions[fields[1]] {
		return fmt.Errorf("access-control: invalid action %q", fields[1])
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
	if !domain.IsZoneTypeName(typ) {
		return fmt.Errorf("unsupported zone type %q", typ)
	}
	return nil
}

// validateZoneNameValue validates a single domain name, the value of a
// forward-zone or stub-zone `name` entry. The name may be wrapped in one
// surrounding quote pair (legacy and hand-written spellings preserve those
// bytes), which is trimmed with domain.NormalizeName — the single quote-trim
// authority — before ValidateZoneName runs; the root zone "." (quoted or not)
// is accepted. Empty and whitespace-only values pass here so the caller keeps
// owning the required-name gate; a lone quote normalizes to empty and is
// rejected rather than silently accepted.
func validateZoneNameValue(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	norm := domain.NormalizeName(value)
	if norm == "" {
		return fmt.Errorf("invalid zone name %q", value)
	}
	return ValidateZoneName(norm)
}

// validateUpstream validates an `ip[@port][#auth]` upstream address, the
// value of a forward-addr or stub-addr entry. IPv6 literals contain colons,
// so `#auth` is split off first and the optional port at the LAST `@`; the
// remainder must be an IP literal.
func validateUpstream(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("empty upstream address")
	}
	addr := value
	if i := strings.IndexByte(value, '#'); i >= 0 {
		if err := validateAuth(value[i+1:]); err != nil {
			return err
		}
		addr = value[:i]
	}
	host := addr
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		host = addr[:i]
		if err := validatePort(addr[i+1:], value, "address"); err != nil {
			return err
		}
	}
	if net.ParseIP(host) == nil {
		return fmt.Errorf("invalid address %q", host)
	}
	return nil
}

// validateAuth validates the `#auth` part of an upstream: non-empty, all
// characters printable and no whitespace, `@` or `#`.
func validateAuth(auth string) error {
	if auth == "" {
		return fmt.Errorf("invalid auth %q: empty", auth)
	}
	for _, r := range auth {
		if r <= ' ' || r == 0x7f || r == '@' || r == '#' {
			return fmt.Errorf("invalid auth %q", auth)
		}
	}
	return nil
}

// validateHostValue validates a `hostname[@port]` upstream name, the value of
// a forward-host or stub-host entry. The optional port follows the same rules
// as an address port; the host must be a valid zone name.
func validateHostValue(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("empty host")
	}
	host := value
	if i := strings.LastIndexByte(value, '@'); i >= 0 {
		host = value[:i]
		if err := validatePort(value[i+1:], value, "host"); err != nil {
			return err
		}
	}
	if err := ValidateZoneName(host); err != nil {
		return fmt.Errorf("invalid host %q: %w", host, err)
	}
	return nil
}

func validateText(value string) error {
	if err := rejectDelimiters(value); err != nil {
		return err
	}
	return checkControl(value)
}

// rejectDelimiters rejects the characters that would let a value escape its
// position and start a second directive: quotes, backslash, ';' and '#'.
// unbound's lexer returns to the initial state after one value token, so a
// value like `bob local-zone: "evil.org" static` would otherwise parse as
// two directives.
func rejectDelimiters(value string) error {
	if strings.ContainsAny(value, "\"'\\;#") {
		return fmt.Errorf("value contains a forbidden character (quote/backslash/;/#)")
	}
	return nil
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
