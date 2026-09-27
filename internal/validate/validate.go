package validate

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// label 规则：RFC 1035 字母/数字/连字符，1-63 字符，首尾不为连字符
var labelRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// 记录相对名：label、点分隔，允许通配符 "*." 前缀与 "@"
var nameRe = regexp.MustCompile(`^(@|\*(\.[a-zA-Z0-9-]+)*|[a-zA-Z0-9_]([a-zA-Z0-9-_.]{0,61}[a-zA-Z0-9_])?)$`)

var rtypeWhitelist = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "PTR": true,
	"MX": true, "TXT": true, "SRV": true, "NS": true,
	// 注：CAA 值含引号，与注入防御冲突，暂不支持（后续如需再评估）
}

const maxTTL = 604800 // 7 天

// ValidateRecord 所有写入路径的唯一入口
func ValidateRecord(r domain.Record) error {
	if err := ValidateZoneName(r.Zone); err != nil {
		return fmt.Errorf("zone: %w", err)
	}
	if !nameRe.MatchString(r.Name) {
		return fmt.Errorf("invalid record name %q", r.Name)
	}
	if injectable(r.Zone) || injectable(r.Name) || injectable(r.Value) {
		return fmt.Errorf("record contains forbidden characters (quote/backslash/newline/;/#)")
	}
	if !rtypeWhitelist[r.RType] {
		return fmt.Errorf("unsupported record type %q", r.RType)
	}
	if r.TTL < 0 || r.TTL > maxTTL {
		return fmt.Errorf("ttl %d out of range [0, %d]", r.TTL, maxTTL)
	}
	return validateValue(r.RType, r.Value)
}

// ValidateZoneName 校验区域名（可带尾部点）
func ValidateZoneName(name string) error {
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 253 {
		return fmt.Errorf("invalid zone name length: %q", name)
	}
	for _, label := range strings.Split(name, ".") {
		if !labelRe.MatchString(label) {
			return fmt.Errorf("invalid label %q", label)
		}
	}
	return nil
}

func injectable(s string) bool {
	return strings.ContainsAny(s, "\"'\\\n\r;#")
}

// validateValue 按记录类型校验值格式
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
