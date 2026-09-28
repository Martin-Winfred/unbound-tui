package unbound

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusParsing(t *testing.T) {
	cases := []struct {
		name     string
		output   string
		wantVer  string
		wantCtrl string
	}{
		{
			// Real do_status shape (daemon/remote.c) with TLS control.
			"ssl control",
			"version: 1.26.0\n" +
				"verbosity: 1\n" +
				"threads: 1\n" +
				"modules: 4 [ subnetcache validator iterator respip ]\n" +
				"uptime: 1234 seconds\n" +
				"options: reuseport control(ssl)\n" +
				"unbound (pid 567) is running...\n",
			"1.26.0", "ssl",
		},
		{
			"namedpipe control",
			"version: 1.19.0\n" +
				"verbosity: 1\n" +
				"options: reuseport control(namedpipe)\n" +
				"unbound (pid 9) is running...\n",
			"1.19.0", "namedpipe",
		},
		{
			// Neither marker: remote-control is not enabled (§4.4 (7)).
			"no remote-control",
			"version: 1.26.0\n" +
				"verbosity: 1\n" +
				"options: reuseport\n" +
				"unbound (pid 5) is running...\n",
			"1.26.0", "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			dir := t.TempDir()
			outFile := filepath.Join(dir, "status.txt")
			if err := os.WriteFile(outFile, []byte(tc.output), 0o600); err != nil {
				t.Fatalf("write status fixture: %v", err)
			}
			fakeControl(t, dir, fmt.Sprintf("cat %q", outFile))

			info, err := c.Status()
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if info.Version != tc.wantVer {
				t.Errorf("Version = %q, want %q", info.Version, tc.wantVer)
			}
			if info.ControlType != tc.wantCtrl {
				t.Errorf("ControlType = %q, want %q", info.ControlType, tc.wantCtrl)
			}
			if info.Raw != tc.output {
				t.Errorf("Raw = %q, want verbatim %q", info.Raw, tc.output)
			}
		})
	}
}

func TestStatusQueryError(t *testing.T) {
	c := newTestClient(t)
	fakeControl(t, t.TempDir(), `echo "connect: permission denied" >&2
exit 1`)

	_, err := c.Status()
	if err == nil {
		t.Fatal("Status: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "query status") {
		t.Errorf("error message = %q, want it to contain \"query status\"", err.Error())
	}
	var uberr *UnboundError
	if !errors.As(err, &uberr) {
		t.Fatalf("error does not wrap *UnboundError: %T", err)
	}
	if uberr.Code != ErrPermission {
		t.Errorf("Code = %q, want %q", uberr.Code, ErrPermission)
	}
}
