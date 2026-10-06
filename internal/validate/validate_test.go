package validate

import (
	"strings"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

func TestValidateZoneName(t *testing.T) {
	valid := []string{"example.com", "example.com.", "a", "a-b.c", "sub.lan"}
	invalid := []string{"", "a..b", "-bad", "bad-", "has space", "ok;bad", "日本語"}
	for _, name := range valid {
		if err := ValidateZoneName(name); err != nil {
			t.Errorf("ValidateZoneName(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range invalid {
		if err := ValidateZoneName(name); err == nil {
			t.Errorf("ValidateZoneName(%q) = nil, want error", name)
		}
	}
}

func TestValidateZoneNameRoot(t *testing.T) {
	if err := ValidateZoneName("."); err != nil {
		t.Errorf(`ValidateZoneName(".") = %v, want nil (root zone)`, err)
	}
}

func TestValidateRecord(t *testing.T) {
	zone := "example.com."
	tests := []struct {
		name    string
		rec     domain.Record
		wantErr string // substring; empty = want nil
	}{
		{"A record", domain.Record{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}, ""},
		{"apex at-sign", domain.Record{Name: "@", RType: "A", Value: "192.0.2.1", TTL: 300}, ""},
		{"wildcard", domain.Record{Name: "*.svc", RType: "A", Value: "192.0.2.1", TTL: 300}, ""},
		{"underscore label", domain.Record{Name: "_dmarc", RType: "TXT", Value: "v=DMARC1", TTL: 60}, ""},
		{"empty label rejected", domain.Record{Name: "a..b", RType: "A", Value: "192.0.2.1", TTL: 300}, "invalid record name"},
		{"trailing dot rejected", domain.Record{Name: "www.", RType: "A", Value: "192.0.2.1", TTL: 300}, "invalid record name"},
		{"tab in value rejected", domain.Record{Name: "txt", RType: "TXT", Value: "a\tb", TTL: 300}, "forbidden"},
		{"nul in value rejected", domain.Record{Name: "txt", RType: "TXT", Value: "a\x00b", TTL: 300}, "forbidden"},
		{"quote rejected", domain.Record{Name: "txt", RType: "TXT", Value: `a"b`, TTL: 300}, "forbidden"},
		{"bad rtype", domain.Record{Name: "www", RType: "CAA", Value: "x", TTL: 300}, "unsupported record type"},
		{"bad A value", domain.Record{Name: "www", RType: "A", Value: "not-an-ip", TTL: 300}, "invalid A address"},
		{"AAAA requires v6", domain.Record{Name: "www", RType: "AAAA", Value: "192.0.2.1", TTL: 300}, "invalid AAAA"},
		{"ttl too large", domain.Record{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 604801}, "out of range"},
		{"ttl negative", domain.Record{Name: "www", RType: "A", Value: "192.0.2.1", TTL: -1}, "out of range"},
		{"bad mx", domain.Record{Name: "@", RType: "MX", Value: "10", TTL: 300}, "invalid MX"},
		{"good mx", domain.Record{Name: "@", RType: "MX", Value: "10 mail.example.com.", TTL: 300}, ""},
		{"bad srv", domain.Record{Name: "_sip._tcp", RType: "SRV", Value: "1 2 x", TTL: 300}, "invalid SRV"},
		{"good srv", domain.Record{Name: "_sip._tcp", RType: "SRV", Value: "1 2 5060 sip.example.com.", TTL: 300}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRecord(zone, tt.rec)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateRecord = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateRecord = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateRecord = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestValidateValueMXSRVStrict pins the strict MX/SRV rdata parser: exact
// token counts (trailing or glued tokens are rejected), integer fields, and
// numeric ranges (MX pref 0-65535; SRV prio/weight 0-65535, port 1-65535).
// The previous fmt.Sscanf implementation silently accepted trailing tokens
// and out-of-range ports.
func TestValidateValueMXSRVStrict(t *testing.T) {
	tests := []struct {
		name    string
		rtype   string
		value   string
		wantErr string // substring; empty = want nil
	}{
		{"mx ok", "MX", "10 mail.example.com", ""},
		{"mx ok lower boundary", "MX", "0 mail.example.com", ""},
		{"mx ok upper boundary", "MX", "65535 mail.example.com", ""},
		{"mx trailing token", "MX", "10 mail.example.com extra", "invalid MX value"},
		{"mx glued", "MX", "10mail.example.com", "invalid MX value"},
		{"mx missing host", "MX", "10", "invalid MX value"},
		{"mx non-numeric pref", "MX", "mail.example.com", "invalid MX value"},
		{"mx pref too large", "MX", "65536 mail.example.com", "out of range"},
		{"mx pref negative", "MX", "-1 mail.example.com", "out of range"},
		{"mx bad host", "MX", "10 bad_host!", "invalid MX host"},

		{"srv ok", "SRV", "10 60 5060 sip.example.com", ""},
		{"srv ok lower boundary", "SRV", "0 0 1 sip.example.com", ""},
		{"srv ok upper boundary", "SRV", "65535 65535 65535 sip.example.com", ""},
		{"srv trailing token", "SRV", "10 60 5060 sip.example.com extra", "invalid SRV value"},
		{"srv missing target", "SRV", "1 2 3", "invalid SRV value"},
		{"srv non-numeric prio", "SRV", "a 60 5060 sip.example.com", "invalid SRV value"},
		{"srv port zero", "SRV", "10 60 0 sip.example.com", "out of range"},
		{"srv port too large", "SRV", "10 60 99999 sip.example.com", "out of range"},
		{"srv prio too large", "SRV", "70000 60 5060 sip.example.com", "out of range"},
		{"srv weight too large", "SRV", "10 70000 5060 sip.example.com", "out of range"},
		{"srv bad target", "SRV", "10 60 5060 bad_target!", "invalid SRV target"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateValue(tt.rtype, tt.value)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("validateValue(%q, %q) = %v, want nil", tt.rtype, tt.value, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateValue(%q, %q) = nil, want error containing %q", tt.rtype, tt.value, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("validateValue(%q, %q) = %v, want error containing %q", tt.rtype, tt.value, err, tt.wantErr)
			}
		})
	}
}

// The canonical ValidateRRLine accept/reject table is TestValidateRRLine in
// schema_test.go; the two tests below add only the injection-focused cases on
// top of it.

// TestValidateRRLineInjection pins that ValidateRRLine applies the same
// injectable() rule as ValidateRecord: an embedded directive must not slip
// through the rdata of a local-data RR line. Before this gate existed the
// first row below was accepted, breaking the surrounding quotes on write.
func TestValidateRRLineInjection(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantErr string // substring; empty = want nil
	}{
		{"TXT embedded directive", `www.example.com. 60 TXT "a" local-zone: "evil" static`, "forbidden characters"},
		{"TXT double quote", `www.example.com. 60 TXT a"b`, "forbidden characters"},
		{"TXT semicolon", `www.example.com. 60 TXT a;b`, "forbidden characters"},
		{"TXT hash", `www.example.com. 60 TXT a#b`, "forbidden characters"},
		{"TXT backslash", `www.example.com. 60 TXT a\b`, "forbidden characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRRLine(tt.line)
			if err == nil {
				t.Fatalf("ValidateRRLine(%q) = nil, want error containing %q", tt.line, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateRRLine(%q) = %v, want error containing %q", tt.line, err, tt.wantErr)
			}
		})
	}
}

// TestRRLineRecordInjectionConsistency pins the documented promise that
// ValidateRecord and ValidateRRLine accept the same values: every rdata the
// RR-line gate rejects for injection must be rejected by the record gate too.
func TestRRLineRecordInjectionConsistency(t *testing.T) {
	rdata := []string{
		`"a" local-zone: "evil" static`,
		`a"b`,
		`a;b`,
		`a#b`,
		`a\b`,
	}
	for i, v := range rdata {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			if err := ValidateRRLine("www.example.com. 60 TXT " + v); err == nil {
				t.Errorf("ValidateRRLine accepted injectable rdata %q", v)
			}
			rec := domain.Record{Name: "txt", RType: "TXT", Value: v, TTL: 60}
			if err := ValidateRecord("example.com.", rec); err == nil {
				t.Errorf("ValidateRecord accepted injectable value %q", v)
			}
		})
	}
}
