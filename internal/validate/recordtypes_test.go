package validate

import (
	"slices"
	"testing"
)

// TestRecordTypeNames pins the exported picker list to the canonical record
// type set and checks every listed type passes the RR gate with minimal rdata.
func TestRecordTypeNames(t *testing.T) {
	want := []string{"A", "AAAA", "CNAME", "MX", "NS", "PTR", "SRV", "TXT"}
	got := RecordTypeNames()
	if !slices.Equal(got, want) {
		t.Fatalf("RecordTypeNames() = %v, want %v", got, want)
	}

	rdata := map[string]string{
		"A":     "192.0.2.1",
		"AAAA":  "2001:db8::1",
		"CNAME": "target.example.com",
		"MX":    "10 mail.example.com",
		"NS":    "ns.example.com",
		"PTR":   "target.example.com",
		"SRV":   "10 60 5060 sip.example.com",
		"TXT":   "hello",
	}
	for _, rt := range got {
		if err := ValidateRRLine("example.com. 300 IN " + rt + " " + rdata[rt]); err != nil {
			t.Errorf("%s: minimal RR line rejected: %v", rt, err)
		}
	}
}
