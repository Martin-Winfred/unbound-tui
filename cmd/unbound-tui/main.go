package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Martin-Winfred/unbound-tui/internal/config"
	"github.com/Martin-Winfred/unbound-tui/internal/domain"
	"github.com/Martin-Winfred/unbound-tui/internal/model"
	"github.com/Martin-Winfred/unbound-tui/internal/unbound"
)

// version is overridden at build time with
// -ldflags "-X main.version=<tag>" (see .goreleaser.yaml).
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run parses flags, boots the collaborators and starts the TUI.
func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("unbound-tui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "/etc/unbound/unbound.conf", "path to the unbound config file")
	fragmentPath := fs.String("fragment", "", "override the fragment path (default: "+config.DefaultFragmentPath+")")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintln(stdout, "unbound-tui", version)
		return nil
	}

	ctl, cfg, err := boot(*configPath, *fragmentPath, stderr)
	if err != nil {
		return err
	}

	m := model.NewRootModel(ctl, cfg, version)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}
	return nil
}

// boot wires the config manager and the unbound client and runs the startup
// checks. The connectivity probe and the read-only include check are warnings
// only: the tool can still edit the fragment, and apply surfaces a hard error
// if unbound-control is unreachable.
func boot(configPath, fragmentPath string, stderr io.Writer) (domain.Controller, *config.Manager, error) {
	cfg, err := config.NewManager(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("config: %w", err)
	}
	if fragmentPath != "" {
		cfg.SetFragmentPath(fragmentPath)
	}
	ctl, err := unbound.NewClient(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("unbound client: %w", err)
	}
	if _, err := ctl.Status(); err != nil {
		fmt.Fprintln(stderr, "warning: cannot reach unbound-control:", err)
	}
	if err := cfg.CheckInclude(); err != nil {
		fmt.Fprintln(stderr, "warning:", err)
	}
	return ctl, cfg, nil
}
