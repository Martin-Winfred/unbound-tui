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
		{"forward-zone name", "forward-zone", "name", TypeZoneName},
		{"forward-zone forward-addr", "forward-zone", "forward-addr", TypeUpstream},
		{"forward-zone forward-host", "forward-zone", "forward-host", TypeHost},
		{"forward-zone forward-tls-upstream", "forward-zone", "forward-tls-upstream", TypeBool},
		{"forward-zone forward-first", "forward-zone", "forward-first", TypeBool},
		{"stub-zone name", "stub-zone", "name", TypeZoneName},
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
		{"remote-control control-interface", "remote-control", "control-interface", TypeControlAddr},
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
		{"path quote", TypePath, `/tmp/a"b.conf`, "forbidden character"},
		{"path semicolon", TypePath, "/tmp/a;b.conf", "forbidden character"},
		{"path hash", TypePath, "/tmp/a#b.conf", "forbidden character"},

		// address: an IP literal or interface name, optional `@port`.
		// `host:port` and `[v6]:port` are not unbound syntax.
		{"addr ipv4", TypeAddr, "192.0.2.1", ""},
		{"addr ipv6", TypeAddr, "2001:db8::1", ""},
		{"addr interface name", TypeAddr, "eth0", ""},
		{"addr dotted name", TypeAddr, "eth0.100", ""},
		{"addr ipv4 port", TypeAddr, "192.0.2.1@5353", ""},
		{"addr ipv6 port", TypeAddr, "2001:db8::1@53", ""},
		{"addr port one", TypeAddr, "192.0.2.1@1", ""},
		{"addr port max", TypeAddr, "192.0.2.1@65535", ""},
		{"addr port zero", TypeAddr, "192.0.2.1@0", "invalid port"},
		{"addr port too big", TypeAddr, "192.0.2.1@65536", "invalid port"},
		{"addr port space", TypeAddr, "192.0.2.1@ 853", "invalid port"},
		{"addr host port rejected", TypeAddr, "192.0.2.1:5353", "invalid address"},
		{"addr bracketed v6 port rejected", TypeAddr, "[2001:db8::1]:5353", "invalid address"},
		{"addr socket path rejected", TypeAddr, "/run/unbound.ctl", "invalid host"},
		{"addr bad host", TypeAddr, "bad host", "invalid host"},
		{"addr empty", TypeAddr, "", "empty address"},

		// control-address: interface/IP[@port] plus an absolute Unix socket
		// path (the Debian default control-interface).
		{"control ipv4", TypeControlAddr, "192.0.2.1", ""},
		{"control ipv4 port", TypeControlAddr, "192.0.2.1@5353", ""},
		{"control ipv6 port", TypeControlAddr, "2001:db8::1@53", ""},
		{"control interface name", TypeControlAddr, "eth0", ""},
		{"control socket path", TypeControlAddr, "/run/unbound.ctl", ""},
		{"control port zero", TypeControlAddr, "192.0.2.1@0", "invalid port"},
		{"control port too big", TypeControlAddr, "192.0.2.1@65536", "invalid port"},
		{"control host port rejected", TypeControlAddr, "192.0.2.1:5353", "invalid address"},
		{"control bracketed v6 port rejected", TypeControlAddr, "[2001:db8::1]:5353", "invalid address"},
		{"control relative path rejected", TypeControlAddr, "run/unbound.ctl", "invalid host"},
		{"control empty", TypeControlAddr, "", "empty address"},

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

		// zone name: the forward-zone/stub-zone `name` value, a single
		// domain name with an optional surrounding quote pair, root "."
		{"zone-name root", TypeZoneName, ".", ""},
		{"zone-name quoted", TypeZoneName, `"example.com"`, ""},
		{"zone-name quoted root", TypeZoneName, `"."`, ""},
		{"zone-name unquoted", TypeZoneName, "example.com", ""},
		{"zone-name empty", TypeZoneName, "", ""},
		{"zone-name whitespace", TypeZoneName, "   ", ""},
		{"zone-name lone quote", TypeZoneName, `"`, "invalid zone name"},
		{"zone-name injection", TypeZoneName, `foo bar".`, "invalid label"},
		{"zone-name directive", TypeZoneName, "x include: /tmp/e.conf", "invalid label"},
		{"zone-name empty label", TypeZoneName, "a..b", "invalid label"},

		// upstream: IP[@port][#auth]
		{"upstream bare ipv4", TypeUpstream, "192.0.2.53", ""},
		{"upstream ipv4 port", TypeUpstream, "192.0.2.53@853", ""},
		{"upstream ipv4 port auth", TypeUpstream, "192.0.2.53@853#dns.example", ""},
		{"upstream bare ipv6", TypeUpstream, "2001:db8::1", ""},
		{"upstream ipv6 port auth", TypeUpstream, "2001:db8::1@853#auth.name", ""},
		{"upstream port zero", TypeUpstream, "192.0.2.53@0", "invalid port"},
		{"upstream port too big", TypeUpstream, "192.0.2.53@70000", "invalid port"},
		{"upstream port space", TypeUpstream, "192.0.2.53@ 853", "invalid port"},
		{"upstream port names address", TypeUpstream, "192.0.2.53@0", `in address "192.0.2.53@0"`},
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
		{"host port names host", TypeHost, "dns.example@0", `in host "dns.example@0"`},
		{"host leading hyphen", TypeHost, "-bad.example", "invalid host"},

		// access-control: "<CIDR> <action>"
		{"access-control allow", TypeAccessCtrl, "192.0.2.0/24 allow", ""},
		{"access-control deny v6", TypeAccessCtrl, "2001:db8::/32 deny", ""},
		{"access-control allow_snoop", TypeAccessCtrl, "10.0.0.0/8 allow_snoop", ""},
		{"access-control refuse", TypeAccessCtrl, "192.0.2.0/24 refuse", ""},
		{"access-control deny_non_local", TypeAccessCtrl, "192.0.2.0/24 deny_non_local", ""},
		{"access-control refuse_non_local", TypeAccessCtrl, "192.0.2.0/24 refuse_non_local", ""},
		{"access-control always_transparent rejected", TypeAccessCtrl, "192.0.2.0/24 always_transparent", "invalid action"},
		{"access-control always_refuse rejected", TypeAccessCtrl, "192.0.2.0/24 always_refuse", "invalid action"},
		{"access-control always_nxdomain rejected", TypeAccessCtrl, "192.0.2.0/24 always_nxdomain", "invalid action"},
		{"access-control allow_setrd", TypeAccessCtrl, "192.0.2.0/24 allow_setrd", ""},
		{"access-control allow_cookie", TypeAccessCtrl, "192.0.2.0/24 allow_cookie", ""},
		{"access-control host bits", TypeAccessCtrl, "192.0.2.1/24 allow", "host bits"},
		{"access-control bad cidr", TypeAccessCtrl, "bad/24 allow", "invalid CIDR"},
		{"access-control bad cidr names directive", TypeAccessCtrl, "bad/24 allow", "access-control: invalid CIDR"},
		{"access-control bad action", TypeAccessCtrl, "192.0.2.0/24 maybe", "invalid action"},
		{"access-control bad action names directive", TypeAccessCtrl, "192.0.2.0/24 maybe", "access-control: invalid action"},
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
		{"text quotes and hash", TypeText, `a"b #c`, "forbidden character"},
		{"text double quote", TypeText, `a"b`, "forbidden character"},
		{"text single quote", TypeText, "a'b", "forbidden character"},
		{"text semicolon", TypeText, "a;b", "forbidden character"},
		{"text hash", TypeText, "a#b", "forbidden character"},
		{"text backslash", TypeText, `a\b`, "forbidden character"},
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

// TestZoneTypeWhitelistAlignment pins that every zone type the canonical
// domain set accepts also passes the TypeZone value check. The list is the
// full unbound.conf(5) set; a type accepted by domain but rejected here would
// make the editor refuse a value the tool elsewhere considers valid.
func TestZoneTypeWhitelistAlignment(t *testing.T) {
	canonical := []string{
		"deny", "refuse", "static", "transparent",
		"typetransparent", "redirect", "nodefault",
		"inform", "inform_deny", "inform_redirect",
		"always_transparent", "always_refuse", "always_nxdomain",
		"always_nodata", "always_deny", "always_null", "noview",
		"block_a", "block_aaaa", "block_a_wdata", "block_aaaa_wdata",
	}
	for _, typ := range canonical {
		t.Run(typ, func(t *testing.T) {
			if err := ValidateValue(TypeZone, `"example.com" `+typ); err != nil {
				t.Errorf("ValidateValue(TypeZone, %q) = %v, want nil", typ, err)
			}
		})
	}
}

// TestAccessCtrlActionWhitelistAlignment pins that the accepted access-control
// action set matches the canonical unbound.conf(5) set exactly, in both
// directions. A canonical action the registry rejects would make the editor
// refuse a value unbound loads; a registry entry outside the canonical set
// (for example a local-zone type such as always_nxdomain) would let the editor
// accept a value unbound rejects at reload.
func TestAccessCtrlActionWhitelistAlignment(t *testing.T) {
	canonical := []string{
		"deny", "refuse", "allow", "allow_setrd",
		"allow_snoop", "allow_cookie", "deny_non_local", "refuse_non_local",
	}
	inCanonical := make(map[string]bool, len(canonical))
	for _, action := range canonical {
		inCanonical[action] = true
		t.Run(action, func(t *testing.T) {
			if err := ValidateValue(TypeAccessCtrl, "192.0.2.0/24 "+action); err != nil {
				t.Errorf("ValidateValue(TypeAccessCtrl, %q) = %v, want nil", action, err)
			}
		})
	}
	if len(accessCtrlActions) != len(canonical) {
		t.Errorf("accessCtrlActions has %d entries, canonical unbound set has %d", len(accessCtrlActions), len(canonical))
	}
	for action := range accessCtrlActions {
		if !inCanonical[action] {
			t.Errorf("accessCtrlActions contains %q, which is not in the canonical unbound set", action)
		}
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
