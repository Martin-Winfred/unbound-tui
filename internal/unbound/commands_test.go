package unbound

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestReloadArgvShape pins that Reload drives unbound-control as
// "-c <conf> reload": reload is the one runtime write the tool performs, and
// -c is the only config mechanism (there is no environment variable).
func TestReloadArgvShape(t *testing.T) {
	c := newTestClient(t)
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	body := fmt.Sprintf(`[ "$1" = "-c" ] || { echo "missing -c" >&2; exit 1; }
[ "$2" = %q ] || { echo "wrong conf path: $2" >&2; exit 1; }
[ "$3" = "reload" ] || { echo "wrong subcommand: $3" >&2; exit 1; }
printf '%%s\n' "$@" > %q
`, c.confPath, argvFile)
	fakeControl(t, dir, body)

	if err := c.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(readFile(t, argvFile), "\n"), "\n")
	want := []string{"-c", c.confPath, "reload"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

// TestReloadErrorPropagation pins that a failing unbound-control surfaces as
// the *UnboundError run builds: apply turns it into a reload error, so the
// error code and the daemon's stderr must survive.
func TestReloadErrorPropagation(t *testing.T) {
	c := newTestClient(t)
	fakeControl(t, t.TempDir(), `echo "error: cannot reload" >&2; exit 1`)

	err := c.Reload()
	if err == nil {
		t.Fatal("Reload: expected an error, got nil")
	}
	var uberr *UnboundError
	if !errors.As(err, &uberr) {
		t.Fatalf("error is not *UnboundError: %T", err)
	}
	if uberr.Code != ErrExit {
		t.Errorf("Code = %q, want %q", uberr.Code, ErrExit)
	}
	if !strings.Contains(err.Error(), "cannot reload") {
		t.Errorf("error = %v, want it to carry the fake's stderr", err)
	}
}
