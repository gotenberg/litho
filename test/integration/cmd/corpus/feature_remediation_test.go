package main

import (
	"errors"
	"testing"
)

// veraPDFHeader is the start of a veraPDF JSON report whose build header
// names core.
func veraPDFHeader(core string) []byte {
	return []byte(`{"report":{"buildInformation":{"releaseDetails":[{
  "id" : "core",
  "version" : "` + core + `",
  "buildDate" : 1791360060000
}]},"jobs":[]}}`)
}

// TestCheckLiveVeraPDF refuses a report of a veraPDF other than the pinned
// one: a host veraPDF upgraded to 1.30.3 between two gate runs moved both
// remediation cells on outputs whose bytes had not changed.
func TestCheckLiveVeraPDF(t *testing.T) {
	t.Parallel()
	if err := checkLiveVeraPDF(veraPDFHeader("1.30.2"), "1.30.2"); err != nil {
		t.Errorf("pinned core: %v", err)
	}
	if err := checkLiveVeraPDF(veraPDFHeader("1.30.3"), "1.30.2"); !errors.Is(err, errVeraPDFVersion) {
		t.Errorf("core 1.30.3 against pin 1.30.2: %v, want %v", err, errVeraPDFVersion)
	}
	if err := checkLiveVeraPDF([]byte(`{"report":{"jobs":[]}}`), "1.30.2"); !errors.Is(err, errVeraPDFVersion) {
		t.Errorf("no build header: %v, want %v", err, errVeraPDFVersion)
	}
	if err := checkLiveVeraPDF(veraPDFHeader("1.30.3"), ""); err != nil {
		t.Errorf("no pin: %v", err)
	}
}
