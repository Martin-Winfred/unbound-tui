package unbound

import (
	"fmt"
	"strings"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// Status parses the output of `unbound-control status`. Actual output shape
// (verified against do_status in daemon/remote.c):
//
//	version: 1.26.0
//	verbosity: 1
//	threads: 1
//	modules: 4 [ subnetcache validator iterator respip ]
//	uptime: 1234 seconds
//	options: reuseport control(ssl)        <- control interface type is on this line
//	unbound (pid 567) is running...
func (c *Client) Status() (domain.StatusInfo, error) {
	out, err := c.run("", "status")
	if err != nil {
		return domain.StatusInfo{}, fmt.Errorf("query status: %w", err)
	}
	info := domain.StatusInfo{Raw: out}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "version:"):
			info.Version = strings.TrimSpace(strings.TrimPrefix(line, "version:"))
		case strings.HasPrefix(line, "options:"):
			switch {
			case strings.Contains(line, "control(namedpipe)"):
				info.ControlType = "namedpipe"
			case strings.Contains(line, "control(ssl)"):
				info.ControlType = "ssl"
			}
		}
	}
	return info, nil
}
