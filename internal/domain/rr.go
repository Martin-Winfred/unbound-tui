package domain

import (
	"fmt"
	"strings"
)

// RRString 构造与 SQLite/片段一致的 RR 文本。输入必须已通过 validate 校验。
func RRString(r Record) string {
	fqdn := strings.TrimSuffix(r.Name, ".") + "." + strings.TrimSuffix(r.Zone, ".") + "."
	return fmt.Sprintf("%s %d IN %s %s", fqdn, r.TTL, r.RType, r.Value)
}
