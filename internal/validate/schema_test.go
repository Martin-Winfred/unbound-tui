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
		{"forward-zone name", "forward-zone", "name", TypeText},
		{"forward-zone forward-addr", "forward-zone", "forward-addr", TypeUpstream},
		{"forward-zone forward-host", "forward-zone", "forward-host", TypeHost},
		{"forward-zone forward-tls-upstream", "forward-zone", "forward-tls-upstream", TypeBool},
		{"forward-zone forward-first", "forward-zone", "forward-first", TypeBool},
		{"stub-zone name", "stub-zone", "name", TypeText},
		{"stub-zone stub-addr", "stub-zone", "stub-addr", TypeUpstream},
		{"stub-zone stub-host", "stub-zone", "stub-host", TypeHost},
		{"stub-zone stub-prime", "stub-zone", "stub-prime", TypeBool},
		{"stub-zone stub-first", "stub-zone", "stub-first", TypeBool},
		{"server interface", "server", "interface", TypeAddr},
		{"server port", "server", "port", TypePort},
		{"server verbosity", "server", "verbosity", TypeInt},
		{"server username", "server", "username", TypeText},
		{"server tls-cert-bundle", "server", "tls-cert-bundle", TypePath},
		{"server root-hints", "server", "root-hints", TypePath},
		{"server access-control", "server", "access-control", TypeAccessCtrl},
		{"remote-control control-enable", "remote-control", "control-enable", TypeBool},
		{"remote-control control-interface", "remote-control", "control-interface", TypeAddr},
		{"remote-control control-port", "remote-control", "control-port", TypePort},
		{"remote-control control-use-cert", "remote-control", "control-use-cert", TypeBool},
		{"remote-control server-key-file", "remote-control", "server-key-file", TypePath},
		{"remote-control server-cert-file", "remote-control", "server-cert-file", TypePath},
		{"remote-control control-key-file", "remote-control", "control-key-file", TypePath},
		{"remote-control control-cert-file", "remote-control", "control-cert-file", TypePath},
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
		{"addr port space", TypeAddr, "1.1.1.1: 853", "invalid port"},
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

		// upstream: IP[@port][#auth]
		{"upstream bare ipv4", TypeUpstream, "192.0.2.53", ""},
		{"upstream ipv4 port", TypeUpstream, "192.0.2.53@853", ""},
		{"upstream ipv4 port auth", TypeUpstream, "192.0.2.53@853#dns.example", ""},
		{"upstream bare ipv6", TypeUpstream, "2001:db8::1", ""},
		{"upstream ipv6 port auth", TypeUpstream, "2001:db8::1@853#auth.name", ""},
		{"upstream port zero", TypeUpstream, "192.0.2.53@0", "invalid port"},
		{"upstream port too big", TypeUpstream, "192.0.2.53@70000", "invalid port"},
		{"upstream port space", TypeUpstream, "192.0.2.53@ 853", "invalid port"},
		{"upstream empty auth", TypeUpstream, "192.0.2.53#", "invalid auth"},
		{"upstream auth whitespace", TypeUpstream, "192.0.2.53#au thor", "invalid auth"},
		{"upstream not an ip", TypeUpstream, "not a host", "invalid address"},
		{"upstream empty", TypeUpstream, "", "empty"},

		// host: hostname[@port]
		{"host bare", TypeHost, "dns.example", ""},
		{"host port", TypeHost, "dns.example@853", ""},
		{"host trailing dot", TypeHost, "a.b.example.", ""},
		{"host empty label", TypeHost, "dns..example", "invalid host"},
		{"host port zero", TypeHost, "dns.example@0", "invalid port"},
		{"host port space", TypeHost, "dns.example@ 853", "invalid port"},
		{"host leading hyphen", TypeHost, "-bad.example", "invalid host"},

		// access-control: "<CIDR> <action>"
		{"access-control allow", TypeAccessCtrl, "192.0.2.0/24 allow", ""},
		{"access-control deny v6", TypeAccessCtrl, "2001:db8::/32 deny", ""},
		{"access-control allow_snoop", TypeAccessCtrl, "10.0.0.0/8 allow_snoop", ""},
		{"access-control refuse", TypeAccessCtrl, "192.0.2.0/24 refuse", ""},
		{"access-control deny_non_local", TypeAccessCtrl, "192.0.2.0/24 deny_non_local", ""},
		{"access-control refuse_non_local", TypeAccessCtrl, "192.0.2.0/24 refuse_non_local", ""},
		{"access-control always_transparent", TypeAccessCtrl, "192.0.2.0/24 always_transparent", ""},
		{"access-control always_refuse", TypeAccessCtrl, "192.0.2.0/24 always_refuse", ""},
		{"access-control always_nxdomain", TypeAccessCtrl, "192.0.2.0/24 always_nxdomain", ""},
		{"access-control host bits", TypeAccessCtrl, "192.0.2.1/24 allow", "host bits"},
		{"access-control bad cidr", TypeAccessCtrl, "bad/24 allow", "invalid CIDR"},
		{"access-control bad action", TypeAccessCtrl, "192.0.2.0/24 maybe", "invalid action"},
		{"access-control missing action", TypeAccessCtrl, "192.0.2.0/24", `expected "<CIDR> <action>"`},
		{"access-control extra token", TypeAccessCtrl, "192.0.2.0/24 allow extra", `expected "<CIDR> <action>"`},
		{"access-control empty", TypeAccessCtrl, "", `expected "<CIDR> <action>"`},

		// port
		{"port one", TypePort, "1", ""},
		{"port dns", TypePort, "53", ""},
		{"port max", TypePort, "65535", ""},
		{"port zero", TypePort, "0", "out of range [1, 65535]"},
		{"port too big", TypePort, "65536", "out of range [1, 65535]"},
		{"port negative", TypePort, "-1", "out of range [1, 65535]"},
		{"port junk", TypePort, "53x", "out of range [1, 65535]"},
		{"port empty", TypePort, "", "out of range [1, 65535]"},
		{"port leading space", TypePort, " 53", "out of range [1, 65535]"},
		{"port trailing space", TypePort, "53 ", "out of range [1, 65535]"},

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
