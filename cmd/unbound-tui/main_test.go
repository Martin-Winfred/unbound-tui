package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestBootMissingConfig(t *testing.T) {
	_, _, err := boot(filepath.Join(t.TempDir(), "absent.conf"), "", &bytes.Buffer{})
	if err == nil {
		t.Fatal("boot(absent config) = nil error, want error")
	}
}

func TestBootWarnsWhenUnboundUnreachable(t *testing.T) {
	dir := t.TempDir()
	frag := filepath.Join(dir, "frag.conf")
	conf := writeFile(t, dir, "unbound.conf", "include: "+frag+"\n")

	var stderr bytes.Buffer
	ctl, cfg, err := boot(conf, frag, &stderr)
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	if ctl == nil || cfg == nil {
		t.Fatal("boot returned nil collaborators")
	}
	if !strings.Contains(stderr.String(), "cannot reach unbound-control") {
		t.Errorf("stderr = %q, want an unbound-control warning", stderr.String())
	}
}

func TestBootWarnsWhenIncludeMissing(t *testing.T) {
	dir := t.TempDir()
	frag := filepath.Join(dir, "frag.conf")
	conf := writeFile(t, dir, "unbound.conf", "server:\n  verbosity: 1\n")

	var stderr bytes.Buffer
	if _, _, err := boot(conf, frag, &stderr); err != nil {
		t.Fatalf("boot: %v", err)
	}
	if !strings.Contains(stderr.String(), "does not include our fragment") {
		t.Errorf("stderr = %q, want a missing-include warning", stderr.String())
	}
}

func TestRunRejectsBadFlag(t *testing.T) {
	if err := run([]string{"-not-a-flag"}, io.Discard, io.Discard); err == nil {
		t.Fatal("run with a bad flag = nil error, want error")
	}
}

func TestRunVersionFlag(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-version"}, &out, io.Discard); err != nil {
		t.Fatalf("run -version: %v", err)
	}
	if !strings.Contains(out.String(), "unbound-tui") {
		t.Errorf("version output = %q, want it to contain unbound-tui", out.String())
	}
}

// TestRunBootError pins that a -config path that does not exist fails boot and
// is returned by run (main prints it and exits 1) instead of starting the TUI
// against an unreadable config.
func TestRunBootError(t *testing.T) {
	err := run([]string{"-config", filepath.Join(t.TempDir(), "absent.conf")}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("run with a missing -config = nil error, want error")
	}
	if !strings.Contains(err.Error(), "unbound config not found") {
		t.Errorf("error = %v, want it to name the missing config", err)
	}
}
