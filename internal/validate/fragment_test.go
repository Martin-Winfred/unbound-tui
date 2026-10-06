package validate

import (
	"strings"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// validFragment is the serializer golden model: a top-level include, a server
// section holding active and disabled entries, and a named forward-zone.
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
			name:    "section kind with space rejected",
			f:       fragOf(domain.Section{Kind: "foo bar", Entries: []domain.Entry{{Key: "x", Value: "y"}}}),
			wantErr: `invalid section kind "foo bar"`,
		},
		{
			name:    "section kind with punctuation rejected",
			f:       fragOf(domain.Section{Kind: "server:", Entries: []domain.Entry{{Key: "x", Value: "y"}}}),
			wantErr: `invalid section kind "server:"`,
		},
		{
			name: "empty synthetic section kind allowed",
			f:    fragOf(domain.Section{Kind: "", Entries: []domain.Entry{{Key: "include", Value: `"/x.conf"`}}}),
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
			name:    "empty value rejected",
			f:       fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "foo", Value: ""}}}),
			wantErr: `section server: entry 1: invalid value "": empty value`,
		},
		{
			name:    "whitespace-only value rejected",
			f:       fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "foo", Value: "   "}}}),
			wantErr: `section server: entry 1: invalid value "   ": empty value`,
		},
		{
			name: "quoted empty string still allowed",
			f:    fragOf(domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "foo", Value: `""`}}}),
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
			name: "embedded local-zone",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "username", Value: `bob local-zone: "evil.org" static`}}}),
			wantErr: "embedded directive",
		},
		{
			name: "embedded include",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "root-hints", Value: `x include: /tmp/e.conf`}}}),
			wantErr: "embedded directive",
		},
		{
			name: "embedded include no space",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "root-hints", Value: `x include:"/tmp/e.conf"`}}}),
			wantErr: "embedded directive",
		},
		{
			name: "embedded include slash no space",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "root-hints", Value: `x include:/tmp/e.conf`}}}),
			wantErr: "embedded directive",
		},
		{
			name: "embedded local-zone no space",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "username", Value: `x local-zone:evil.org static`}}}),
			wantErr: "embedded directive",
		},
		{
			name: "embedded local-zone quote no space",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "username", Value: `x local-zone:'evil.org' static`}}}),
			wantErr: "embedded directive",
		},
		{
			name: "embedded forward-addr",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "name", Value: `example.com. forward-addr: 6.6.6.6`}}}),
			wantErr: "embedded directive",
		},
		{
			name: "trailing directive token",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "x", Value: "x local-zone:"}}}),
			wantErr: "embedded directive",
		},
		{
			name: "unbalanced quote",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "x", Value: `"a`}}}),
			wantErr: "unbalanced quote",
		},
		{
			name: "quoted rr line",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "local-data", Value: `"www.example.com. 300 IN A 1.2.3.4"`}}}),
		},
		{
			name: "quoted zone line",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "local-zone", Value: `"x." refuse`}}}),
		},
		{
			name: "ipv6 literal",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "interface", Value: "2001:db8::1"}}}),
		},
		{
			name: "ipv6 hex word",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "interface", Value: "dead:beef::1"}}}),
		},
		{
			name: "ip literal",
			f: fragOf(domain.Section{Kind: "server",
				Entries: []domain.Entry{{Key: "interface", Value: "192.0.2.53"}}}),
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
			name: "duplicate forward-zone trailing-dot variant",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"example.com"`}}},
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"example.com."`}}},
			),
			wantErr: `duplicate forward-zone "example.com"`,
		},
		{
			name: "duplicate forward-zone case and trailing-dot variant",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"Example.COM"`}}},
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"example.com."`}}},
			),
			wantErr: `duplicate forward-zone "Example.COM"`,
		},
		{
			name: "non-ASCII long s does not fold to s",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: "\"\u017f.example\""}}},
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"s.example"`}}},
			),
		},
		{
			name: "Kelvin sign does not fold to k",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: "\"\u212a\""}}},
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{{Key: "name", Value: `"k"`}}},
			),
		},
		{
			name: "active root name allowed",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
					{Key: "name", Value: `"."`},
					{Key: "forward-addr", Value: "192.0.2.53"},
				}},
			),
		},
		{
			name: "active quoted empty name rejected",
			f: fragOf(domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
				{Key: "name", Value: `""`},
				{Key: "forward-addr", Value: "192.0.2.53"},
			}}),
			wantErr: `section forward-zone: missing name`,
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
			name: "active forward-zone and disabled duplicate allowed",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
					{Key: "name", Value: `"example.com"`},
					{Key: "forward-addr", Value: "192.0.2.53"},
				}},
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
					{Key: "name", Value: `"example.com"`, Disabled: true},
					{Key: "forward-addr", Value: "192.0.2.54", Disabled: true},
				}},
			),
		},
		{
			name: "disabled forward-zone and active duplicate allowed",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
					{Key: "name", Value: `"example.com"`, Disabled: true},
					{Key: "forward-addr", Value: "192.0.2.53", Disabled: true},
				}},
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
					{Key: "name", Value: `"example.com"`},
					{Key: "forward-addr", Value: "192.0.2.54"},
				}},
			),
		},
		{
			name: "two disabled forward-zones with same name allowed",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
					{Key: "name", Value: `"example.com"`, Disabled: true},
				}},
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
					{Key: "name", Value: `"example.com"`, Disabled: true},
				}},
			),
		},
		{
			name: "active forward-zone with disabled name entry rejected as missing",
			f: fragOf(
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
					{Key: "name", Value: `"example.com"`, Disabled: true},
					{Key: "forward-addr", Value: "192.0.2.53"},
				}},
				domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
					{Key: "name", Value: `"example.com"`},
					{Key: "forward-addr", Value: "192.0.2.54"},
				}},
			),
			wantErr: `section forward-zone: missing name`,
		},
		{
			name: "two unnamed server sections allowed",
			f: fragOf(
				domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "local-zone", Value: `"a" static`}}},
				domain.Section{Kind: "server", Entries: []domain.Entry{{Key: "local-zone", Value: `"b" static`}}},
			),
		},
		{
			name: "active forward-zone without name rejected",
			f: fragOf(domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
				{Key: "forward-addr", Value: "192.0.2.53"},
			}}),
			wantErr: `section forward-zone: missing name`,
		},
		{
			name: "active stub-zone without name rejected",
			f: fragOf(domain.Section{Kind: "stub-zone", Entries: []domain.Entry{
				{Key: "stub-addr", Value: "192.0.2.53"},
			}}),
			wantErr: `section stub-zone: missing name`,
		},
		{
			name: "active view without name rejected",
			f: fragOf(domain.Section{Kind: "view", Entries: []domain.Entry{
				{Key: "view-first", Value: "yes"},
			}}),
			wantErr: `section view: missing name`,
		},
		{
			name: "disabled-only forward-zone without name allowed",
			f: fragOf(domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
				{Key: "forward-addr", Value: "192.0.2.53", Disabled: true},
			}}),
		},
		{
			name: "active forward-zone with disabled-only name rejected",
			f: fragOf(domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
				{Key: "name", Value: `"example.com"`, Disabled: true},
				{Key: "forward-addr", Value: "192.0.2.53"},
			}}),
			wantErr: `section forward-zone: missing name`,
		},
		{
			name: "active forward-zone with active name and disabled duplicate name allowed",
			f: fragOf(domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
				{Key: "name", Value: `"example.com"`},
				{Key: "name", Value: `"example.com"`, Disabled: true},
				{Key: "forward-addr", Value: "192.0.2.53"},
			}}),
		},
		{
			name: "disabled-only forward-zone with disabled name allowed",
			f: fragOf(domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
				{Key: "name", Value: `"example.com"`, Disabled: true},
				{Key: "forward-addr", Value: "192.0.2.53", Disabled: true},
			}}),
		},
		{
			name: "zero-entry forward-zone allowed",
			f:    fragOf(domain.Section{Kind: "forward-zone"}),
		},
		{
			name: "whitespace-only name active rejected",
			f: fragOf(domain.Section{Kind: "forward-zone", Entries: []domain.Entry{
				{Key: "name", Value: `"   "`},
				{Key: "forward-addr", Value: "192.0.2.53"},
			}}),
			wantErr: `section forward-zone: missing name`,
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
