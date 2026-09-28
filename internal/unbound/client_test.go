package unbound

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeControl writes an executable fake unbound-control shell script into
// dir and puts dir first on PATH. body is the shell snippet the fake runs.
// Tests must not talk to a real unbound-control; everything goes through
// these PATH-injected fakes (design doc §9.2).
func fakeControl(t *testing.T, dir, body string) {
	t.Helper()
	script := filepath.Join(dir, "unbound-control")
	content := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake unbound-control: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

// newTestClient returns a Client whose confPath points at a dummy config
// file inside a temp directory (never /etc/unbound/unbound.conf).
func newTestClient(t *testing.T) *Client {
	t.Helper()
	conf := filepath.Join(t.TempDir(), "unbound.conf")
	if err := os.WriteFile(conf, []byte("server:\n  interface: 127.0.0.1\n"), 0o600); err != nil {
		t.Fatalf("write dummy conf: %v", err)
	}
	c, err := NewClient(conf)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// readFile returns the full contents of path, failing the test on any I/O error.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestNewClient(t *testing.T) {
	t.Run("existing config", func(t *testing.T) {
		conf := filepath.Join(t.TempDir(), "unbound.conf")
		if err := os.WriteFile(conf, []byte("server:\n"), 0o600); err != nil {
			t.Fatalf("write conf: %v", err)
		}
		c, err := NewClient(conf)
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if c.confPath != conf {
			t.Errorf("confPath = %q, want %q", c.confPath, conf)
		}
		if c.timeout != 30*time.Second {
			t.Errorf("timeout = %s, want 30s", c.timeout)
		}
	})

	t.Run("missing config", func(t *testing.T) {
		_, err := NewClient(filepath.Join(t.TempDir(), "missing.conf"))
		if err == nil {
			t.Fatal("NewClient: expected error for missing config, got nil")
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("error does not wrap fs.ErrNotExist: %v", err)
		}
		if !strings.Contains(err.Error(), "unbound config not found") {
			t.Errorf("error message = %q, want it to contain \"unbound config not found\"", err.Error())
		}
	})
}

func TestRunBinaryMissing(t *testing.T) {
	c := newTestClient(t)
	t.Setenv("PATH", t.TempDir()) // empty dir: unbound-control is not resolvable
	_, err := c.run("", "status")
	if err == nil {
		t.Fatal("run: expected error when unbound-control is missing, got nil")
	}
	if !strings.Contains(err.Error(), "start unbound-control") {
		t.Errorf("error message = %q, want it to contain \"start unbound-control\"", err.Error())
	}
}

func TestRunTimeout(t *testing.T) {
	start := time.Now()
	c := newTestClient(t)
	c.timeout = 50 * time.Millisecond
	fakeControl(t, t.TempDir(), "sleep 5")
	_, err := c.run("", "status")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("run took %s, want it to abort well under 2s", elapsed)
	}
	if err == nil {
		t.Fatal("run: expected timeout error, got nil")
	}
	var uberr *UnboundError
	if !errors.As(err, &uberr) {
		t.Fatalf("error is not *UnboundError: %T", err)
	}
	if uberr.Code != ErrControlTimeout {
		t.Errorf("Code = %q, want %q", uberr.Code, ErrControlTimeout)
	}
	if !strings.Contains(uberr.Message, "timed out") {
		t.Errorf("Message = %q, want it to mention the timeout", uberr.Message)
	}
}
