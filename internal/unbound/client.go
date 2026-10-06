// Package unbound wraps the unbound-control CLI. It is the only component
// that talks to the running Unbound daemon; every invocation goes through
// Client.run so that the -c flag, the timeout, and error mapping are uniform.
package unbound

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Client executes unbound-control against a fixed Unbound config file.
type Client struct {
	confPath string
	timeout  time.Duration
}

// NewClient verifies that confPath exists and returns a Client with a 30s
// per-command timeout. The path must be the same config the running unbound
// daemon uses (see run).
func NewClient(confPath string) (*Client, error) {
	if _, err := os.Stat(confPath); err != nil {
		return nil, fmt.Errorf("unbound config not found: %w", err)
	}
	return &Client{confPath: confPath, timeout: 30 * time.Second}, nil
}

// run is the single entry point for every unbound-control invocation.
//
// Verified facts (checked against the unbound-control source):
//   - the config is passed only via -c <file>; there is no UNBOUND_CONF
//     environment variable (the getopt string is just "c:s:qh", and the
//     source never calls getenv)
//   - unbound-control reads control-interface / control-port / certificate
//     paths from the remote-control section of the -c config to build its
//     connection
//   - therefore -c must point at the very config the running unbound uses
func (c *Client) run(stdin string, args ...string) (string, error) {
	full := append([]string{"-c", c.confPath}, args...)
	cmd := exec.Command("unbound-control", full...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start unbound-control: %w", err)
	}
	// Buffered (capacity 1) so the waiter goroutine never blocks when this
	// select has already taken the timeout branch and killed the process;
	// verbatim from design doc §4.1, prevents a goroutine leak.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			// Only stderr is the error detail: stdout may carry partial list
			// rows, and mixing the streams would let a warning masquerade as
			// data. Fall back to stdout when the command failed silently.
			detail := stderr.String()
			if detail == "" {
				detail = stdout.String()
			}
			return stdout.String(), wrapError(err, detail)
		}
		return stdout.String(), nil
	case <-time.After(c.timeout):
		// Sanctioned suppression: after Kill there is nothing left to do;
		// the waiter goroutine drains cmd.Wait's result into the buffered
		// channel above.
		_ = cmd.Process.Kill()
		return "", &UnboundError{
			Code:    ErrControlTimeout,
			Message: fmt.Sprintf("unbound-control %v timed out after %s", args, c.timeout),
		}
	}
}
