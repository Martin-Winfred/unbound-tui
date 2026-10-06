package domain

import (
	"fmt"
	"strings"
)

// FQDN normalizes a name to a fully-qualified form with a trailing dot.
func FQDN(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		return "."
	}
	return name + "."
}

// RRString renders a record as unbound zonefile RR text qualified by its
// zone. Apex records (Name "" or "@") render as the zone name itself, never
// as the invalid "@.<zone>." form. The input must have passed validate.
func RRString(zone string, r Record) string {
	z := FQDN(zone)
	name := strings.TrimSuffix(r.Name, ".")
	fqdn := z
	if name != "" && name != "@" {
		if z == "." {
			// The root zone contributes no suffix of its own; the relative
			// owner is already absolute once it carries a trailing dot.
			fqdn = name + "."
		} else {
			fqdn = name + "." + z
		}
	}
	return fmt.Sprintf("%s %d IN %s %s", fqdn, r.TTL, r.RType, r.Value)
}
