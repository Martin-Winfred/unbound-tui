package main

import (
	"bytes"
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
	if err := run([]string{"-not-a-flag"}, &bytes.Buffer{}); err == nil {
		t.Fatal("run with a bad flag = nil error, want error")
	}
}
