package validate

import (
	"strings"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// validFragment mirrors the Task 4 serializer golden model: a top-level
// include, a server section holding active and disabled entries, and a named
// forward-zone.
func validFragment() domain.Fragment {
	return domain.Fragment{Sections: []domain.Section{
		{Kind: "", Entries: []domain.Entry{
			{Key: "include", Value: `"/x.conf"`},
		}},
		{Kind: "server", Entries: []domain.Entry{
			{Key: "local-zone", Value: `"example.com" refuse`},
			{Key: "local-data", Value: `"example.com. 300 IN A 192.0.2.1"`},
			{Key: "local-data", Value: `"old.example. 300 IN A 192.0.2.2"`, Disabled: true},
		}},
		{Kind: "forward-zone", Entries: []domain.Entry{
			{Key: "name", Value: `"."`},
			{Key: "forward-addr", Value: "192.0.2.53"},
		}},
	}}
}

func TestValidateFragment(t *testing.T) {
	tests := []struct {
		name    string
		f       domain.Fragment
		wantErr string // substring; empty = want nil
	}{
		{
			name: "valid golden model",
			f:    validFragment(),
		},
		{
			name: "empty key names section and entry index",
			f: fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{
				{Key: "local-zone", Value: `"a" static`},
				{Key: "", Value: "x"},
			}}),
			wantErr: `section server: entry 2: invalid key ""`,
		},
		{
			name:    "key with space names the key",
			f:       fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "foo bar", Value: "x"}}}),
			wantErr: `section server: entry 1: invalid key "foo bar"`,
		},
		{
			name:    "key with punctuation rejected",
			f:       fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "foo:bar", Value: "x"}}}),
			wantErr: `invalid key "foo:bar"`,
		},
		{
			name:    "value newline rejected",
			f:       fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "x", Value: "a\nb"}}}),
			wantErr: `section server: entry 1: invalid value`,
		},
		{
			name:    "value carriage return rejected",
			f:       fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "x", Value: "a\rb"}}}),
			wantErr: `section server: entry 1: invalid value`,
		},
		{
			name:    "value nul rejected",
			f:       fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "x", Value: "a\x00b"}}}),
			wantErr: `section server: entry 1: invalid value`,
		},
		{
			name:    "value tab rejected",
			f:       fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "x", Value: "a\tb"}}}),
			wantErr: `section server: entry 1: invalid value`,
		},
		{
			name:    "value del rejected",
			f:       fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "x", Value: "a\x7fb"}}}),
			wantErr: `section server: entry 1: invalid value`,
		},
		{
			name:    "disabled entry still validated",
			f:       fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "x", Value: "a\tb", Disabled: true}}}),
			wantErr: `section server: entry 1: invalid value`,
		},
		{
			name: "value quotes spaces hash allowed",
			f:    fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "local-data", Value: `"a b" # not a comment`}}}),
		},
		{
			name: "duplicate forward-zone",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"."`}, {Key: "forward-addr", Value: "192.0.2.53"}}},
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"."`}, {Key: "forward-addr", Value: "192.0.2.54"}}},
			),
			wantErr: `duplicate forward-zone "."`,
		},
		{
			name: "duplicate forward-zone quote vs bare",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"example.com"`}}},
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `example.com`}}},
			),
			wantErr: `duplicate forward-zone "example.com"`,
		},
		{
			name: "duplicate forward-zone case variant",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"Example.COM"`}}},
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"example.com"`}}},
			),
			wantErr: `duplicate forward-zone "Example.COM"`,
		},
		{
			name: "duplicate stub-zone",
			f: fragOf(
				domain.Section{Kind: "stub-zone", Entries: []domain.Entry{{Key: "name", Value: `"lan."`}}},
				domain.Section{Kind: "stub-zone", Entries: []domain.Entry{{Key: "name", Value: `"LAN."`}}},
			),
			wantErr: `duplicate stub-zone "lan."`,
		},
		{
			name: "duplicate view",
			f: fragOf(
				domain.Section{Kind: "view", Entries: []domain.Entry{{Key: "name", Value: `"internal"`}}},
				domain.Section{Kind: "view", Entries: []domain.Entry{{Key: "name", Value: `"Internal"`}}},
			),
			wantErr: `duplicate view "internal"`,
		},
		{
			name: "two unnamed server sections allowed",
			f: fragOf(
				domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "local-zone", Value: `"a" static`}}},
				domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "local-zone", Value: `"b" static`}}},
			),
		},
		{
			name: "two forward-zones without name allowed",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "forward-addr", Value: "192.0.2.53"}}},
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "forward-addr", Value: "192.0.2.54"}}},
			),
		},
		{
			name: "same name across kinds allowed",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"."`}}},
				domain.Section{Kind: "stub-zone", Entries: []domain.Entry{{Key: "name", Value: `"."`}}},
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFragment(tt.f)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateFragment = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateFragment = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateFragment = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func fragOf(sections ...domain.Section) domain.Fragment {
	return domain.Fragment{Sections: sections}
}
