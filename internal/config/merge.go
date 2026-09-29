package config

import (
	"fmt"
	"sort"

	"github.com/Martin-Winfred/unbound-tui/internal/domain"
)

// FragmentFromZones folds the edited zone model back into a base fragment,
// returning a new Fragment and leaving base untouched.
//
// It is the temporary shim between the v0.1 zone model and the generic model:
// every local-zone and local-data entry is stripped from every section, and
// the edited zones are regenerated into a single server section (created at
// the front when the base has none). All other entries and sections are
// preserved in place — including sections left empty by the strip.
//
// Records are regenerated in v0.1 layout: active local-zone entries, then
// active local-data entries, then disabled local-zone entries, then disabled
// local-data entries (records of a disabled zone included). Zones are stable
// sorted by name first.
func FragmentFromZones(base domain.Fragment, zones []domain.Zone) domain.Fragment {
	sections := make([]domain.Section, len(base.Sections))
	for i, s := range base.Sections {
		var entries []domain.Entry
		for _, e := range s.Entries {
			if e.Key == "local-zone" || e.Key == "local-data" {
				continue
			}
			entries = append(entries, e)
		}
		sections[i] = domain.Section{Kind: s.Kind, Entries: entries}
	}

	server := -1
	for i, s := range sections {
		if s.Kind == "server" {
			server = i
			break
		}
	}
	if server < 0 {
		sections = append([]domain.Section{{Kind: "server"}}, sections...)
		server = 0
	}

	sorted := make([]domain.Zone, len(zones))
	copy(sorted, zones)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	sections[server].Entries = append(sections[server].Entries, regeneratedEntries(sorted)...)
	return domain.Fragment{Sections: sections}
}

// regeneratedEntries emits the local-zone/local-data entries for zones in the
// v0.1 layout (active zones, active records, disabled zones, disabled
// records). A record of a disabled zone is always disabled, so a disabled
// zone never leaks as an implicit transparent zone.
func regeneratedEntries(zones []domain.Zone) []domain.Entry {
	var entries []domain.Entry
	for _, z := range zones {
		if z.Disabled {
			continue
		}
		entries = append(entries, zoneEntry(z, false))
	}
	for _, z := range zones {
		if z.Disabled {
			continue
		}
		for _, r := range z.Records {
			if r.Disabled {
				continue
			}
			entries = append(entries, recordEntry(z, r, false))
		}
	}
	for _, z := range zones {
		if !z.Disabled {
			continue
		}
		entries = append(entries, zoneEntry(z, true))
	}
	for _, z := range zones {
		for _, r := range z.Records {
			if z.Disabled || r.Disabled {
				entries = append(entries, recordEntry(z, r, true))
			}
		}
	}
	return entries
}

func zoneEntry(z domain.Zone, disabled bool) domain.Entry {
	return domain.Entry{
		Key:      "local-zone",
		Value:    fmt.Sprintf("%q %s", domain.FQDN(z.Name), z.Type),
		Disabled: disabled,
	}
}

func recordEntry(z domain.Zone, r domain.Record, disabled bool) domain.Entry {
	return domain.Entry{
		Key:      "local-data",
		Value:    fmt.Sprintf("%q", domain.RRString(z.Name, r)),
		Disabled: disabled,
	}
}
