package validate

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

func TestValidateRecordHappyPerType(t *testing.T) {
	tests := []struct {
		name  string
		rtype string
		value string
	}{
		{"A record", "A", "192.0.2.1"},
		{"AAAA record", "AAAA", "2001:db8::1"},
		{"CNAME record with trailing dot target", "CNAME", "backend.example.com."},
		{"PTR record", "PTR", "10.0.0.10.in-addr.arpa."},
		{"NS record", "NS", "ns1.example.com."},
		{"TXT record under 255 bytes", "TXT", "hello world"},
		{"MX record with trailing dot host", "MX", "10 mail.example.com."},
		{"SRV record with trailing dot target", "SRV", "10 20 443 sip.example.com."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := domain.Record{Name: "www", Zone: "example.com", RType: tt.rtype, Value: tt.value, TTL: 300}
			if err := ValidateRecord(r); err != nil {
				t.Errorf("ValidateRecord(%+v) unexpected error: %v", r, err)
			}
		})
	}
}

func TestValidateRecordInjection(t *testing.T) {
	forbidden := []string{`"`, `'`, `\`, "\n", "\r", ";", "#"}

	for _, ch := range forbidden {
		// Pinned check order in ValidateRecord: nameRe runs before the
		// injectable() scan, so a forbidden character planted in Name is
		// rejected as an invalid record name first (defense in depth).
		t.Run("forbidden char "+strconv.Quote(ch)+" in name is rejected", func(t *testing.T) {
			r := domain.Record{Name: "www" + ch, Zone: "example.com", RType: "A", Value: "192.0.2.1", TTL: 300}
			err := ValidateRecord(r)
			if err == nil {
				t.Fatalf("ValidateRecord(%+v) = nil, want error", r)
			}
			if !strings.Contains(err.Error(), "invalid record name") {
				t.Errorf("error %q does not contain %q", err, "invalid record name")
			}
		})
		t.Run("forbidden char "+strconv.Quote(ch)+" in TXT value is rejected", func(t *testing.T) {
			r := domain.Record{Name: "www", Zone: "example.com", RType: "TXT", Value: "hello" + ch + "world", TTL: 300}
			err := ValidateRecord(r)
			if err == nil {
				t.Fatalf("ValidateRecord(%+v) = nil, want error", r)
			}
			if !strings.Contains(err.Error(), "forbidden characters") {
				t.Errorf("error %q does not contain %q", err, "forbidden characters")
			}
		})
	}
}

func TestValidateRecordTTL(t *testing.T) {
	tests := []struct {
		name    string
		ttl     int
		wantErr string
	}{
		{"negative ttl rejected", -1, "out of range"},
		{"ttl above seven days rejected", 604801, "out of range"},
		{"zero ttl accepted", 0, ""},
		{"seven-day ttl accepted", 604800, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := domain.Record{Name: "www", Zone: "example.com", RType: "A", Value: "192.0.2.1", TTL: tt.ttl}
			err := ValidateRecord(r)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateRecord(%+v) unexpected error: %v", r, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateRecord(%+v) = nil, want error containing %q", r, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRecordUnsupportedType(t *testing.T) {
	for _, rtype := range []string{"CAA", "SPF", "a"} {
		t.Run("unsupported rtype "+strconv.Quote(rtype), func(t *testing.T) {
			r := domain.Record{Name: "www", Zone: "example.com", RType: rtype, Value: "192.0.2.1", TTL: 300}
			err := ValidateRecord(r)
			if err == nil {
				t.Fatalf("ValidateRecord(%+v) = nil, want error", r)
			}
			if !strings.Contains(err.Error(), "unsupported record type") {
				t.Errorf("error %q does not contain %q", err, "unsupported record type")
			}
		})
	}
}

func TestValidateRecordNames(t *testing.T) {
	tests := []struct {
		name    string
		recName string
		wantErr string
	}{
		{"at-sign with suffix rejected", "@x", "invalid record name"},
		{"leading hyphen rejected", "-lead", "invalid record name"},
		// Pinned nameRe quirk: the middle character class [a-zA-Z0-9-_.]
		// includes '.', so consecutive dots inside a relative name are
		// accepted. Asserting as-is; zone-level "a..b" is rejected in
		// TestValidateZoneName instead.
		{"consecutive dots accepted as pinned nameRe quirk", "a..b", ""},
		{"wildcard name accepted", "*.svc", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := domain.Record{Name: tt.recName, Zone: "example.com", RType: "A", Value: "192.0.2.1", TTL: 300}
			err := ValidateRecord(r)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateRecord(%+v) unexpected error: %v", r, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateRecord(%+v) = nil, want error containing %q", r, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRecordBadValues(t *testing.T) {
	tests := []struct {
		name    string
		rtype   string
		value   string
		wantErr string
	}{
		{"A rejects IPv6 literal", "A", "2001:db8::1", "invalid A address"},
		{"A rejects octet 999", "A", "999.1.1.1", "invalid A address"},
		{"A rejects octet 256", "A", "192.0.2.256", "invalid A address"},
		{"AAAA rejects IPv4 literal", "AAAA", "192.0.2.1", "invalid AAAA address"},
		{"MX rejects missing preference", "MX", "mail.example.com", "invalid MX value"},
		{"MX rejects missing host", "MX", "10", "invalid MX value"},
		{"MX rejects invalid host label", "MX", "10 -bad.example.com", "invalid MX host"},
		{"MX rejects non-numeric preference", "MX", "x mail.example.com", "invalid MX value"},
		{"SRV rejects missing target", "SRV", "10 20 443", "invalid SRV value"},
		{"SRV rejects invalid target label", "SRV", "10 20 443 -bad.example.com", "invalid SRV target"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := domain.Record{Name: "www", Zone: "example.com", RType: tt.rtype, Value: tt.value, TTL: 300}
			err := ValidateRecord(r)
			if err == nil {
				t.Fatalf("ValidateRecord(%+v) = nil, want error containing %q", r, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRecordZoneErrorIsWrapped(t *testing.T) {
	r := domain.Record{Name: "www", Zone: "-a.example.com", RType: "A", Value: "192.0.2.1", TTL: 300}
	err := ValidateRecord(r)
	if err == nil {
		t.Fatalf("ValidateRecord(%+v) = nil, want error containing %q", r, "zone: invalid label")
	}
	if !strings.Contains(err.Error(), "zone: invalid label") {
		t.Errorf("error %q does not contain %q", err, "zone: invalid label")
	}
}

func TestValidateZoneName(t *testing.T) {
	tests := []struct {
		name    string
		zone    string
		wantErr string
	}{
		{"valid zone", "example.com", ""},
		{"valid zone with trailing dot", "example.com.", ""},
		{"leading hyphen label", "-a.example.com", "invalid label"},
		{"trailing hyphen label", "a-.example.com", "invalid label"},
		{"empty middle label", "a..b", "invalid label"},
		{"label of 64 chars", strings.Repeat("a", 64), "invalid label"},
		{"empty zone", "", "invalid zone name length"},
		{"zone over 253 chars", strings.Repeat("a.", 130), "invalid zone name length"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateZoneName(tt.zone)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateZoneName(%q) unexpected error: %v", tt.zone, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateZoneName(%q) = nil, want error containing %q", tt.zone, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}
