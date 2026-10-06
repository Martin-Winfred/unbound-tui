package domain

import "testing"

func TestFQDN(t *testing.T) {
	cases := map[string]string{
		"example.com":  "example.com.",
		"example.com.": "example.com.",
		"":             ".",
		"  a.b  ":      "a.b.",
	}
	for in, want := range cases {
		if got := FQDN(in); got != want {
			t.Errorf("FQDN(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRRStringRootZone pins rendering of records owned by the root zone ".":
// a relative owner is FQDN-qualified directly (never through the zone), and the
// apex renders as the root itself. Before the fix the concatenation produced
// the doubled dot "example.com.." for a root-owned relative name.
func TestRRStringRootZone(t *testing.T) {
	tests := []struct {
		name string
		zone string
		rec  Record
		want string
	}{
		{
			name: "relative name under root",
			zone: ".",
			rec:  Record{Name: "example.com", RType: "A", Value: "1.2.3.4", TTL: 300},
			want: "example.com. 300 IN A 1.2.3.4",
		},
		{
			name: "root apex via empty name",
			zone: ".",
			rec:  Record{Name: "", RType: "A", Value: "1.2.3.4", TTL: 300},
			want: ". 300 IN A 1.2.3.4",
		},
		{
			name: "root apex via at-sign",
			zone: ".",
			rec:  Record{Name: "@", RType: "A", Value: "1.2.3.4", TTL: 300},
			want: ". 300 IN A 1.2.3.4",
		},
		{
			name: "normal zone still concatenates",
			zone: "example.com.",
			rec:  Record{Name: "www", RType: "A", Value: "1.2.3.4", TTL: 300},
			want: "www.example.com. 300 IN A 1.2.3.4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RRString(tt.zone, tt.rec); got != tt.want {
				t.Errorf("RRString(%q, %+v) = %q, want %q", tt.zone, tt.rec, got, tt.want)
			}
		})
	}
}

func TestRRString(t *testing.T) {
	tests := []struct {
		name string
		zone string
		rec  Record
		want string
	}{
		{
			name: "apex via at-sign renders as the zone",
			zone: "example.com.",
			rec:  Record{Name: "@", RType: "A", Value: "192.0.2.1", TTL: 300},
			want: "example.com. 300 IN A 192.0.2.1",
		},
		{
			name: "apex via empty name renders as the zone",
			zone: "example.com",
			rec:  Record{Name: "", RType: "A", Value: "192.0.2.1", TTL: 300},
			want: "example.com. 300 IN A 192.0.2.1",
		},
		{
			name: "relative name",
			zone: "example.com.",
			rec:  Record{Name: "www", RType: "A", Value: "192.0.2.2", TTL: 300},
			want: "www.example.com. 300 IN A 192.0.2.2",
		},
		{
			name: "wildcard name",
			zone: "example.com.",
			rec:  Record{Name: "*.svc", RType: "A", Value: "192.0.2.3", TTL: 60},
			want: "*.svc.example.com. 60 IN A 192.0.2.3",
		},
		{
			name: "zone without trailing dot is normalized",
			zone: "example.com",
			rec:  Record{Name: "www", RType: "A", Value: "192.0.2.4", TTL: 300},
			want: "www.example.com. 300 IN A 192.0.2.4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RRString(tt.zone, tt.rec); got != tt.want {
				t.Errorf("RRString(%q, %+v) = %q, want %q", tt.zone, tt.rec, got, tt.want)
			}
		})
	}
}
