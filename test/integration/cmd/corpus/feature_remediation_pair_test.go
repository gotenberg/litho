package main

import (
	"encoding/json"
	"testing"
)

// TestLithoPairsCoverEveryHomeProfile pins that every home profile a litho
// fixture can have pairs with a PDF/A and a PDF/UA profile, and that each pair
// is one [pdf.WithPDFUA] accepts: PDF/A-4 with PDF/UA-2, every other PDF/A
// part with PDF/UA-1. A home profile without a pair would leave its fixtures
// out of the cell unnoticed.
func TestLithoPairsCoverEveryHomeProfile(t *testing.T) {
	t.Parallel()

	for suffix, home := range lithoHomeProfiles {
		pdfaDir, pdfuaDir, ok := lithoPairOf("fixture_" + suffix)
		if !ok {
			t.Errorf("home profile %s (suffix %q) has no pair", home, suffix)
			continue
		}
		if _, ok := dirToPDFAProfile[pdfaDir]; !ok {
			t.Errorf("%s pairs with %s, no PDF/A profile", home, pdfaDir)
		}
		want := "PDF_UA-1"
		if pdfaDir == "PDF_A-4" || pdfaDir == "PDF_A-4e" || pdfaDir == "PDF_A-4f" {
			want = "PDF_UA-2"
		}
		if pdfuaDir != want {
			t.Errorf("%s pairs %s with %s, want %s", home, pdfaDir, pdfuaDir, want)
		}
	}
}

// TestRemediationPairCompare pins the scoring: fully fixed only when the
// remediation ran and neither litho nor veraPDF finds a violation of either
// profile, and the disagreement summed over the two profiles.
func TestRemediationPairCompare(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		out          remediationPairOutput
		match        bool
		disagreement int
	}{
		{name: "clean", out: remediationPairOutput{}, match: true},
		{name: "a PDF/A rule veraPDF alone finds", out: remediationPairOutput{VeraPDFA: []string{"6.6.2.3.1-t01"}}, disagreement: 1},
		{name: "a PDF/UA rule both find", out: remediationPairOutput{LithoPDFUA: []string{"5-t01"}, VeraPDFUA: []string{"5-t01"}}},
		{name: "a remediation error", out: remediationPairOutput{RemediationError: "broke"}},
		{name: "an output veraPDF cannot parse", out: remediationPairOutput{ParseFailed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.out)
			if err != nil {
				t.Fatal(err)
			}
			v := newRemediationPairFeature().Compare("fixture_2b", data)
			if v.Match != tc.match {
				t.Errorf("Match = %v, want %v", v.Match, tc.match)
			}
			if got := v.Metrics["disagreement"]; got != tc.disagreement {
				t.Errorf("disagreement = %d, want %d", got, tc.disagreement)
			}
			if got := v.Metrics["fully_fixed"]; got != boolToInt(tc.match) {
				t.Errorf("fully_fixed = %d, want %d", got, boolToInt(tc.match))
			}
		})
	}
}
