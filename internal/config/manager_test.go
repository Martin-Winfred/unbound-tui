package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

func testManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	conf := filepath.Join(dir, "unbound.conf")
	if err := os.WriteFile(conf, []byte("include: "+filepath.Join(dir, "frag.conf")+"\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	frag := filepath.Join(dir, "frag.conf")
	m.SetFragmentPath(frag)
	return m, frag
}

func TestManagerReadWrite(t *testing.T) {
	m, frag := testManager(t)

	if zones, err := m.Read(); err != nil || len(zones) != 0 {
		t.Fatalf("Read on missing fragment = %v, %v; want empty, nil", zones, err)
	}

	want := []domain.Zone{{
		Name: "example.com.", Type: "transparent",
		Records: []domain.Record{{Name: "www", RType: "A", Value: "192.0.2.1", TTL: 300}},
	}}
	if err := m.Write(want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(frag); err != nil {
		t.Fatalf("fragment not created: %v", err)
	}
	got, err := m.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read = %+v, want %+v", got, want)
	}
}

func TestCheckInclude(t *testing.T) {
	m, _ := testManager(t)
	if err := m.CheckInclude(); err != nil {
		t.Fatalf("CheckInclude (included) = %v, want nil", err)
	}

	dir := t.TempDir()
	conf := filepath.Join(dir, "unbound.conf")
	if err := os.WriteFile(conf, []byte("server:\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	m2, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	err = m2.CheckInclude()
	var ce *ConfigError
	if !errors.As(err, &ce) || ce.Type != ErrMissingInclude {
		t.Errorf("CheckInclude (missing) = %v, want ConfigError %s", err, ErrMissingInclude)
	}
}

func TestCheckIncludeIgnoresComments(t *testing.T) {
	dir := t.TempDir()
	frag := filepath.Join(dir, "frag.conf")
	// The fragment path appears only in a comment: this is NOT an include.
	conf := writeTemp(t, "# include: "+frag+"\nserver:\n")
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetFragmentPath(frag)
	var ce *ConfigError
	if err := m.CheckInclude(); !errors.As(err, &ce) {
		t.Errorf("CheckInclude with only a commented include = %v, want ConfigError", err)
	}
}

func TestCheckIncludeGlob(t *testing.T) {
	dir := t.TempDir()
	confD := filepath.Join(dir, "unbound.conf.d")
	if err := os.MkdirAll(confD, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	conf := writeTemp(t, "include-toplevel: \""+confD+"/*.conf\"\n")
	frag := filepath.Join(confD, "unbound-tui.conf") // not created yet
	m, err := NewManager(conf)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetFragmentPath(frag)
	if err := m.CheckInclude(); err != nil {
		t.Errorf("CheckInclude with a matching glob = %v, want nil", err)
	}
}
