package unbound

import (
	"errors"
	"os/exec"
	"testing"
)

func TestWrapErrorMapping(t *testing.T) {
	t.Run("nil error maps to nil", func(t *testing.T) {
		if err := wrapError(nil, "whatever"); err != nil {
			t.Errorf("wrapError(nil, _) = %v, want nil", err)
		}
	})

	t.Run("non-exit error", func(t *testing.T) {
		err := wrapError(errors.New("i/o crash"), "")
		var uberr *UnboundError
		if !errors.As(err, &uberr) {
			t.Fatalf("wrapError did not return *UnboundError: %T", err)
		}
		if uberr.Code != ErrExit {
			t.Errorf("Code = %q, want %q", uberr.Code, ErrExit)
		}
		if uberr.Message != "i/o crash" {
			t.Errorf("Message = %q, want %q", uberr.Message, "i/o crash")
		}
		if uberr.Detail != "" {
			t.Errorf("Detail = %q, want empty", uberr.Detail)
		}
	})

	t.Run("direct exit error construction", func(t *testing.T) {
		err := wrapError(&exec.ExitError{}, "syntax error in RR")
		var uberr *UnboundError
		if !errors.As(err, &uberr) {
			t.Fatalf("wrapError did not return *UnboundError: %T", err)
		}
		if uberr.Code != ErrSyntax {
			t.Errorf("Code = %q, want %q", uberr.Code, ErrSyntax)
		}
	})
}

// TestWrapErrorViaRun drives wrapError through run() with fake binaries so
// that Detail is verified against the actually captured combined output.
func TestWrapErrorViaRun(t *testing.T) {
	cases := []struct {
		name     string
		script   string
		wantCode string
		wantMsg  string
	}{
		{
			"syntax error output",
			`echo "error: syntax error in RR" >&2
exit 1`,
			ErrSyntax, "invalid RR data",
		},
		{
			"permission denied output",
			`echo "connect: permission denied" >&2
exit 1`,
			ErrPermission, "check control interface permissions",
		},
		{
			"other nonzero output",
			`echo "unbound is very unhappy"
exit 1`,
			ErrExit, "command exited with error",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			fakeControl(t, t.TempDir(), tc.script)

			out, err := c.run("", "local_data", "bogus")
			if err == nil {
				t.Fatal("run: expected error, got nil")
			}
			var uberr *UnboundError
			if !errors.As(err, &uberr) {
				t.Fatalf("error is not *UnboundError: %T", err)
			}
			if uberr.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", uberr.Code, tc.wantCode)
			}
			if uberr.Message != tc.wantMsg {
				t.Errorf("Message = %q, want %q", uberr.Message, tc.wantMsg)
			}
			if uberr.Detail != out {
				t.Errorf("Detail = %q, want it to equal the captured output %q", uberr.Detail, out)
			}
		})
	}
}

func TestUnboundErrorFormatting(t *testing.T) {
	t.Run("with detail", func(t *testing.T) {
		e := &UnboundError{Code: "EXIT_ERROR", Message: "command exited with error", Detail: "boom"}
		want := "[EXIT_ERROR] command exited with error: boom"
		if got := e.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("without detail", func(t *testing.T) {
		e := &UnboundError{Code: "CONTROL_TIMEOUT", Message: "unbound-control timed out"}
		want := "[CONTROL_TIMEOUT] unbound-control timed out"
		if got := e.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})
}
