package validate

import (
	"strings"
	"testing"
)

func TestSchemaFor(t *testing.T) {
	tests := []struct {
		name string
		kind string
		key  string
		want Type
	}{
		{"local-data is an RR line", "server", "local-data", TypeRR},
		{"local-zone is a zone line", "server", "local-zone", TypeZone},
		{"unregistered key is text", "server", "forward-addr", TypeText},
		{"cross-kind isolation", "forward-zone", "local-data", TypeText},
		{"empty kind and key", "", "", TypeText},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SchemaFor(tt.kind, tt.key); got != tt.want {
				t.Errorf("SchemaFor(%q, %q) = %q, want %q", tt.kind, tt.key, got, tt.want)
			}
		})
	}
}

func TestValidateValue(t *testing.T) {
	tests := []struct {
		name    string
		typ     Type
		value   string
		wantErr string // substring; empty = want nil
	}{
		// bool
		{"bool yes", TypeBool, "yes", ""},
		{"bool no", TypeBool, "no", ""},
		{"bool true", TypeBool, "true", ""},
		{"bool false", TypeBool, "false", ""},
		{"bool on", TypeBool, "on", ""},
		{"bool off", TypeBool, "off", ""},
		{"bool zero", TypeBool, "0", ""},
		{"bool one", TypeBool, "1", ""},
		{"bool maybe", TypeBool, "maybe", "invalid boolean"},

		// int
		{"int zero", TypeInt, "0", ""},
		{"int 65535", TypeInt, "65535", ""},
		{"int negative", TypeInt, "-1", "must be >= 0"},
		{"int junk", TypeInt, "x", "invalid integer"},
		{"int exponent", TypeInt, "1e9", "invalid integer"},

		// path
		{"path ok", TypePath, "/etc/ssl/certs", ""},
		{"path empty", TypePath, "", "empty path"},
		{"path control", TypePath, "a\x00b", "control"},

		// address
		{"addr ipv4", TypeAddr, "1.1.1.1", ""},
		{"addr host", TypeAddr, "dns.example", ""},
		{"addr ipv6 port", TypeAddr, "[2001:db8::1]:53", ""},
		{"addr ipv4 port", TypeAddr, "1.1.1.1:5353", ""},
		{"addr port zero", TypeAddr, "1.1.1.1:0", "invalid port"},
		{"addr port too big", TypeAddr, "1.1.1.1:70000", "invalid port"},
		{"addr bad host", TypeAddr, "bad host", "invalid host"},
		{"addr empty", TypeAddr, "", "empty address"},

		// cidr
		{"cidr v4", TypeCIDR, "192.0.2.0/24", ""},
		{"cidr v6", TypeCIDR, "2001:db8::/32", ""},
		{"cidr host bits", TypeCIDR, "192.0.2.1/24", "host bits"},
		{"cidr bad prefix", TypeCIDR, "192.0.2.0/33", "invalid CIDR"},

		// rr
		{"rr valid", TypeRR, "host.example. 300 IN A 192.0.2.1", ""},
		{"rr quoted valid", TypeRR, `"host.example. 300 IN A 192.0.2.1"`, ""},
		{"rr invalid", TypeRR, "host.example. 300 IN A not-an-ip", "invalid A address"},

		// zone
		{"zone explicit type", TypeZone, `"example.com" static`, ""},
		{"zone unquoted", TypeZone, "example.com refuse", ""},
		{"zone default type", TypeZone, `"example.com"`, ""},
		{"zone bad type", TypeZone, `"example.com" bogus`, "unsupported zone type"},
		{"zone bad name", TypeZone, `"bad name" static`, "invalid zone name"},

		// text
		{"text plain", TypeText, "hello world", ""},
		{"text quotes and hash", TypeText, `a"b #c`, ""},
		{"text empty", TypeText, "", ""},
		{"text nul", TypeText, "a\x00b", "control"},
		{"text del", TypeText, "a\x7fb", "control"},
		{"empty type is text", Type(""), "hello world", ""},
		{"empty type rejects control", Type(""), "a\x01b", "control"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateValue(tt.typ, tt.value)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateValue(%q, %q) = %v, want nil", tt.typ, tt.value, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateValue(%q, %q) = nil, want error containing %q", tt.typ, tt.value, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateValue(%q, %q) = %v, want error containing %q", tt.typ, tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidateRRLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantErr string // substring; empty = want nil
	}{
		{"quoted full line", `"host.example. 300 IN A 192.0.2.1"`, ""},
		{"unquoted owner", "host.example. 300 IN A 192.0.2.1", ""},
		{"no class", "host.example. 300 A 192.0.2.1", ""},
		{"apex owner", "@ 300 A 192.0.2.1", ""},
		{"missing fields", "host.example. 300 A", "invalid RR line"},
		{"empty line", "", "invalid RR line"},
		{"bad ttl", "host.example. abc A 192.0.2.1", "invalid ttl"},
		{"ttl out of range", "host.example. 604801 A 192.0.2.1", "out of range"},
		{"unknown rtype", "host.example. 300 IN CAA x", "unsupported record type"},
		{"bad value", "host.example. 300 IN A not-an-ip", "invalid A address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRRLine(tt.line)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateRRLine(%q) = %v, want nil", tt.line, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateRRLine(%q) = nil, want error containing %q", tt.line, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateRRLine(%q) = %v, want error containing %q", tt.line, err, tt.wantErr)
			}
		})
	}
}
