package domain

import "testing"

func TestRRString(t *testing.T) {
	tests := []struct {
		name string
		r    Record
		want string
	}{
		{
			// Pinned verbatim behavior: "@" is not rewritten to the zone apex,
			// the formula concatenates it like any other relative name.
			name: "at-sign name is kept verbatim by the pinned formula",
			r:    Record{Name: "@", Zone: "example.com", RType: "A", Value: "192.0.2.1", TTL: 300},
			want: "@.example.com. 300 IN A 192.0.2.1",
		},
		{
			name: "wildcard name",
			r:    Record{Name: "*.svc", Zone: "example.com", RType: "A", Value: "192.0.2.1", TTL: 300},
			want: "*.svc.example.com. 300 IN A 192.0.2.1",
		},
		{
			name: "trailing dot on zone is trimmed to identical output",
			r:    Record{Name: "www", Zone: "example.com.", RType: "A", Value: "192.0.2.1", TTL: 300},
			want: "www.example.com. 300 IN A 192.0.2.1",
		},
		{
			name: "trailing dot on name is trimmed",
			r:    Record{Name: "www.", Zone: "example.com", RType: "A", Value: "192.0.2.1", TTL: 300},
			want: "www.example.com. 300 IN A 192.0.2.1",
		},
		{
			name: "week-long TTL is formatted as a bare integer",
			r:    Record{Name: "www", Zone: "example.com", RType: "A", Value: "192.0.2.1", TTL: 604800},
			want: "www.example.com. 604800 IN A 192.0.2.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RRString(tt.r); got != tt.want {
				t.Errorf("RRString(%+v) = %q, want %q", tt.r, got, tt.want)
			}
		})
	}
}
