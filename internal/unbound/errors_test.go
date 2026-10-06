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

// TestWrapErrorViaRun drives wrapError through run() with fake binaries and
// pins the separated-stream contract: Detail is the stderr text, falling back
// to stdout only when stderr is empty, and never a mix of the two.
func TestWrapErrorViaRun(t *testing.T) {
	cases := []struct {
		name       string
		script     string
		wantCode   string
		wantMsg    string
		wantDetail string
	}{
		{
			"syntax error output",
			`echo "error: syntax error in RR" >&2
exit 1`,
			ErrSyntax, "invalid RR data", "error: syntax error in RR\n",
		},
		{
			"permission denied output",
			`echo "connect: permission denied" >&2
exit 1`,
			ErrPermission, "check control interface permissions", "connect: permission denied\n",
		},
		{
			"other nonzero output falls back to stdout",
			`echo "unbound is very unhappy"
exit 1`,
			ErrExit, "command exited with error", "unbound is very unhappy\n",
		},
		{
			"stderr wins over stdout",
			`echo "stdout noise"
echo "real failure" >&2
exit 1`,
			ErrExit, "command exited with error", "real failure\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			fakeControl(t, t.TempDir(), tc.script)

			_, err := c.run("", "local_data", "bogus")
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
			if uberr.Detail != tc.wantDetail {
				t.Errorf("Detail = %q, want %q", uberr.Detail, tc.wantDetail)
			}
		})
	}
}

// TestWrapErrorPreservesCause pins that the original exec error stays reachable
// through errors.Is/As from the returned *UnboundError, in both the ExitError
// and the non-ExitError branches.
func TestWrapErrorPreservesCause(t *testing.T) {
	t.Run("non-exit error", func(t *testing.T) {
		sentinel := errors.New("i/o crash")
		err := wrapError(sentinel, "")
		if !errors.Is(err, sentinel) {
			t.Errorf("errors.Is(err, sentinel) = false, want true; err = %v", err)
		}
	})

	t.Run("exit error", func(t *testing.T) {
		exitErr := &exec.ExitError{}
		err := wrapError(exitErr, "syntax error in RR")
		var got *exec.ExitError
		if !errors.As(err, &got) || got != exitErr {
			t.Errorf("errors.As(err, *exec.ExitError) did not reach the original cause; err = %v", err)
		}
	})
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
