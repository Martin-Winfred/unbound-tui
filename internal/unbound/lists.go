package unbound

import (
	"strings"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// ListLocalZones returns the runtime zone table via
// `unbound-control list_local_zones` — one half of the
// startup-reconcile diff source (§4.4 semantic 6). Output shape is one
// zone per line with the name in field 0 and the type in field 1.
// Blank lines and lines with fewer than two fields are skipped rather
// than fatal (a stray trailing line must not abort reconciliation);
// columns beyond the type are tolerated and ignored.
//
// Error handling decision: run() already maps every failure to a
// wrapped *UnboundError (client.go run + errors.go wrapError), so its
// error is returned as-is — no double wrapping. Parse errors cannot
// occur because the skip rule above makes malformed lines non-fatal.
func (c *Client) ListLocalZones() ([]domain.LocalZone, error) {
	out, err := c.run("", "list_local_zones")
	if err != nil {
		return nil, err
	}
	var zones []domain.LocalZone
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		zones = append(zones, domain.LocalZone{Name: fields[0], Type: fields[1]})
	}
	return zones, nil
}

// ListLocalData returns the runtime local-data set via
// `unbound-control list_local_data` — the record half of the
// startup-reconcile diff source (§4.4 semantic 6). Each non-blank
// stdout line is one RR in zonefile presentation form and is returned
// verbatim: §4.4 semantic 4 requires the exact text Unbound itself
// reported for a later local_data_remove to hit.
//
// Error handling mirrors ListLocalZones: run()'s error is already a
// wrapped *UnboundError and is returned as-is.
func (c *Client) ListLocalData() ([]string, error) {
	out, err := c.run("", "list_local_data")
	if err != nil {
		return nil, err
	}
	var rrs []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rrs = append(rrs, line)
	}
	return rrs, nil
}
