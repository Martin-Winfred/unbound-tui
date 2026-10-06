package unbound

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// installListFake installs a fake unbound-control whose stdout is the
// fixture content (cat of a file, so the parser sees byte-realistic
// captured output per design doc §9.2) and returns a client bound to a
// dummy config.
func installListFake(t *testing.T, fixtureContent string) *Client {
	t.Helper()
	c := newTestClient(t)
	dir := t.TempDir()
	fixture := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(fixture, []byte(fixtureContent), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	fakeControl(t, dir, fmt.Sprintf("cat %q", fixture))
	return c
}

// TestListLocalZones pins the list_local_zones parser: fields[0]=name,
// fields[1]=type, blank and short lines skipped, extra columns tolerated.
func TestListLocalZones(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		want   []domain.LocalZone
	}{
		{
			name:   "two zones",
			stdout: "example.com. transparent\nlan.example. static\n",
			want: []domain.LocalZone{
				{Name: "example.com.", Type: "transparent"},
				{Name: "lan.example.", Type: "static"},
			},
		},
		{
			name:   "blank lines are skipped",
			stdout: "\nexample.com. transparent\n\n\nlan.example. static\n\n",
			want: []domain.LocalZone{
				{Name: "example.com.", Type: "transparent"},
				{Name: "lan.example.", Type: "static"},
			},
		},
		{
			name:   "lines with fewer than two fields are skipped",
			stdout: "example.com. transparent\nlonely\n\nexample.com.\n",
			want: []domain.LocalZone{
				{Name: "example.com.", Type: "transparent"},
			},
		},
		{
			name:   "extra columns are tolerated",
			stdout: "example.com. transparent some extra column\n",
			want: []domain.LocalZone{
				{Name: "example.com.", Type: "transparent"},
			},
		},
		{
			name:   "carriage returns do not leak into the type",
			stdout: "example.com. transparent\r\n",
			want: []domain.LocalZone{
				{Name: "example.com.", Type: "transparent"},
			},
		},
		{
			name:   "empty output yields no zones",
			stdout: "",
			want:   nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := installListFake(t, tc.stdout)
			got, err := c.ListLocalZones()
			if err != nil {
				t.Fatalf("ListLocalZones: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("zones = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestListLocalData pins the list_local_data parser: non-blank lines
// are returned verbatim (the exact RR text unbound reported, which a
// later local_data_remove hits per §4.4 (4)).
func TestListLocalData(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		want   []string
	}{
		{
			name:   "two rr lines",
			stdout: "web.example.com. 3600 IN A 192.168.1.1\nmail.example.com. 3600 IN AAAA 2001:db8::1\n",
			want: []string{
				"web.example.com. 3600 IN A 192.168.1.1",
				"mail.example.com. 3600 IN AAAA 2001:db8::1",
			},
		},
		{
			name:   "blank lines are skipped",
			stdout: "\nweb.example.com. 3600 IN A 192.168.1.1\n\n",
			want: []string{
				"web.example.com. 3600 IN A 192.168.1.1",
			},
		},
		{
			name:   "unbound spacing is preserved verbatim",
			stdout: "web.example.com.  3600   IN A 192.168.1.1\n",
			want: []string{
				"web.example.com.  3600   IN A 192.168.1.1",
			},
		},
		{
			name:   "empty output yields no lines",
			stdout: "",
			want:   nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := installListFake(t, tc.stdout)
			got, err := c.ListLocalData()
			if err != nil {
				t.Fatalf("ListLocalData: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("lines = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestListLocalDataIgnoresStderr pins stream separation: a warning or
// advisory that unbound-control writes to stderr must not masquerade as an RR
// line. Only stdout is the list contract; stderr belongs to error Detail.
func TestListLocalDataIgnoresStderr(t *testing.T) {
	c := newTestClient(t)
	fakeControl(t, t.TempDir(), `echo "warning: deprecated option" >&2
echo "web.example.com. 3600 IN A 192.168.1.1"`)

	got, err := c.ListLocalData()
	if err != nil {
		t.Fatalf("ListLocalData: %v", err)
	}
	want := []string{"web.example.com. 3600 IN A 192.168.1.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %q, want %q (stderr warning must not appear)", got, want)
	}
}

// TestListArgvShape verifies both list commands invoke unbound-control
// as "-c <confPath> <subcommand>" (§4.4 (5): -c is the only config
// mechanism; $3 is the subcommand).
func TestListArgvShape(t *testing.T) {
	cases := []struct {
		name string
		op   func(c *Client) error
		sub  string
	}{
		{"ListLocalZones", func(c *Client) error { _, err := c.ListLocalZones(); return err }, "list_local_zones"},
		{"ListLocalData", func(c *Client) error { _, err := c.ListLocalData(); return err }, "list_local_data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			dir := t.TempDir()
			argvFile := filepath.Join(dir, "argv.txt")
			body := fmt.Sprintf(`[ "$1" = "-c" ] || { echo "missing -c" >&2; exit 1; }
[ "$2" = %q ] || { echo "wrong conf path: $2" >&2; exit 1; }
[ "$3" = %q ] || { echo "wrong subcommand: $3" >&2; exit 1; }
printf '%%s\n' "$@" > %q
`, c.confPath, tc.sub, argvFile)
			fakeControl(t, dir, body)

			if err := tc.op(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			got := strings.Split(strings.TrimSuffix(readFile(t, argvFile), "\n"), "\n")
			want := []string{"-c", c.confPath, tc.sub}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("argv = %q, want %q", got, want)
			}
		})
	}
}

// TestListRunErrorPropagation pins that a failing unbound-control
// surfaces as the wrapped *UnboundError produced inside run() (the
// list wrappers must return it as-is, without double wrapping).
func TestListRunErrorPropagation(t *testing.T) {
	c := newTestClient(t)
	fakeControl(t, t.TempDir(), `echo "error: control certificate expired" >&2; exit 1`)

	ops := []struct {
		name string
		op   func() error
	}{
		{"ListLocalZones", func() error { _, err := c.ListLocalZones(); return err }},
		{"ListLocalData", func() error { _, err := c.ListLocalData(); return err }},
	}
	for _, tc := range ops {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.op()
			if err == nil {
				t.Fatalf("%s: expected an error, got nil", tc.name)
			}
			var uberr *UnboundError
			if !errors.As(err, &uberr) {
				t.Fatalf("%s: error is not *UnboundError: %T", tc.name, err)
			}
			if uberr.Code != ErrExit {
				t.Errorf("Code = %q, want %q", uberr.Code, ErrExit)
			}
			if !strings.Contains(err.Error(), "control certificate expired") {
				t.Errorf("error = %v, want it to carry the fake's stderr", err)
			}
		})
	}
}
