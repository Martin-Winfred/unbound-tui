package validate

import (
	"strings"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

func TestValidateZoneName(t *testing.T) {
	valid := []string{"example.com", "example.com.", "a", "a-b.c", "sub.lan"}
	invalid := []string{"", ".", "a..b", "-bad", "bad-", "has space", "ok;bad", "日本語"}
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
