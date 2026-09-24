package compliance

import "testing"

// TestPDFAPart pins the part number [PDFAPart] returns for each profile
// identifier.
func TestPDFAPart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile string
		want    int
	}{
		{"PDF/A-1a", 1},
		{"PDF/A-1b", 1},
		{"PDF/A-2a", 2},
		{"PDF/A-2b", 2},
		{"PDF/A-2u", 2},
		{"PDF/A-3a", 3},
		{"PDF/A-3b", 3},
		{"PDF/A-3u", 3},
		{"PDF/A-4", 4},
		{"PDF/A-4e", 4},
		{"PDF/A-4f", 4},
		{"PDF/UA-1", 0},
		{"PDF/UA-2", 0},
		{"", 0},
		{"random", 0},
		{"PDF/A-5", 0},  // future part, not yet in the switch
		{"PDF/A-2x", 0}, // unknown level
	}
	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			if got := PDFAPart(tc.profile); got != tc.want {
				t.Errorf("PDFAPart(%q) = %d, want %d", tc.profile, got, tc.want)
			}
		})
	}
}

// TestPDFALevel pins the level byte [PDFALevel] returns for each profile
// identifier.
func TestPDFALevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile string
		want    byte
	}{
		{"PDF/A-1a", 'a'},
		{"PDF/A-1b", 'b'},
		{"PDF/A-2u", 'u'},
		{"PDF/A-4", '4'}, // no level, returns the digit
		{"PDF/A-4e", 'e'},
		{"PDF/A-4f", 'f'},
		{"", 0},
	}
	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			if got := PDFALevel(tc.profile); got != tc.want {
				t.Errorf("PDFALevel(%q) = %c, want %c", tc.profile, got, tc.want)
			}
		})
	}
}

// TestPDFAConformance pins the conformance letter [PDFAConformance] returns
// for each profile identifier.
func TestPDFAConformance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile string
		want    string
	}{
		{"PDF/A-1a", "A"},
		{"PDF/A-1b", "B"},
		{"PDF/A-2a", "A"},
		{"PDF/A-2b", "B"},
		{"PDF/A-2u", "U"},
		{"PDF/A-3u", "U"},
		{"PDF/A-4", "B"}, // bare PDF/A-4 defaults to conformance B
		{"PDF/A-4e", "E"},
		{"PDF/A-4f", "F"},
		{"PDF/UA-1", ""},
		{"", ""},
		{"random", ""},
	}
	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			if got := PDFAConformance(tc.profile); got != tc.want {
				t.Errorf("PDFAConformance(%q) = %q, want %q", tc.profile, got, tc.want)
			}
		})
	}
}

// TestIsPDFA pins which profile identifiers [IsPDFA] recognises.
func TestIsPDFA(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile string
		want    bool
	}{
		{"PDF/A-1a", true},
		{"PDF/A-2b", true},
		{"PDF/A-4", true},
		{"PDF/A-4e", true},
		{"PDF/UA-1", false},
		{"PDF/UA-2", false},
		{"", false},
		{"random", false},
	}
	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			if got := IsPDFA(tc.profile); got != tc.want {
				t.Errorf("IsPDFA(%q) = %v, want %v", tc.profile, got, tc.want)
			}
		})
	}
}

// TestIsPDFUA pins which profile identifiers [IsPDFUA] recognises.
func TestIsPDFUA(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile string
		want    bool
	}{
		{"PDF/UA-1", true},
		{"PDF/UA-2", true},
		{"PDF/UA-99", true}, // prefix match; caller must check part separately
		// WTPDF adopts the ISO 14289-2 requirements, so it imposes PDF/UA
		// obligations. Remediation fixers gate on this: a bare "PDF/UA"
		// prefix test silently disabled the marked-content language fix
		// under a WTPDF run.
		{"WTPDF-1.0-Accessibility", true},
		{"WTPDF-1.0-Reuse", true},
		{"WTPDF-1.0", false}, // not a conformance level
		{"PDF/A-1a", false},
		{"PDF/A-4", false},
		{"", false},
		{"PDF/U", false},
		{"PDFUA", false},
	}
	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			if got := IsPDFUA(tc.profile); got != tc.want {
				t.Errorf("IsPDFUA(%q) = %v, want %v", tc.profile, got, tc.want)
			}
		})
	}
}

// TestPDFUAPart pins the part number [PDFUAPart] returns for each profile
// identifier.
func TestPDFUAPart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile string
		want    int
	}{
		{"PDF/UA-1", 1},
		{"PDF/UA-2", 2},
		{"PDF/UA-99", 0}, // matches IsPDFUA prefix but not a real part
		// Both WTPDF levels build on ISO 14289-2 and so report part 2.
		{"WTPDF-1.0-Accessibility", 2},
		{"WTPDF-1.0-Reuse", 2},
		{"WTPDF-1.0", 0},
		{"PDF/A-1a", 0},
		{"", 0},
	}
	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			if got := PDFUAPart(tc.profile); got != tc.want {
				t.Errorf("PDFUAPart(%q) = %d, want %d", tc.profile, got, tc.want)
			}
		})
	}
}

// TestRequiresUnicodeMapping pins which PDF/A profiles
// [RequiresUnicodeMapping] holds to Unicode mapping.
func TestRequiresUnicodeMapping(t *testing.T) {
	tests := []struct {
		profile string
		want    bool
	}{
		// Part 1: only Level A requires Unicode mapping.
		{"PDF/A-1a", true},
		{"PDF/A-1b", false},
		// Parts 2 and 3: Levels U and A.
		{"PDF/A-2a", true},
		{"PDF/A-2u", true},
		{"PDF/A-2b", false},
		{"PDF/A-3a", true},
		{"PDF/A-3u", true},
		{"PDF/A-3b", false},
		// Part 4 always requires Unicode mapping.
		{"PDF/A-4", true},
		{"PDF/A-4e", true},
		{"PDF/A-4f", true},
		// Non-PDF/A profiles fall back to false; the gate only fires
		// for callers that have already filtered to PDF/A.
		{"PDF/UA-1", false},
		{"PDF/UA-2", false},
		{"", false},
		{"garbage", false},
	}
	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			if got := RequiresUnicodeMapping(tc.profile); got != tc.want {
				t.Errorf("RequiresUnicodeMapping(%q) = %v, want %v", tc.profile, got, tc.want)
			}
		})
	}
}

// TestConformanceSatisfies pins which XMP conformance claims
// [ConformanceSatisfies] accepts for each profile level.
func TestConformanceSatisfies(t *testing.T) {
	tests := []struct {
		name    string
		xmpConf string
		level   byte
		want    bool
	}{
		// Profile -a accepts only A.
		{"A covers a", "A", 'a', true},
		{"U does not cover a", "U", 'a', false},
		{"B does not cover a", "B", 'a', false},
		// Profile -u accepts A or U.
		{"A covers u", "A", 'u', true},
		{"U covers u", "U", 'u', true},
		{"B does not cover u", "B", 'u', false},
		// Profile -b accepts A, U, or B.
		{"A covers b", "A", 'b', true},
		{"U covers b", "U", 'b', true},
		{"B covers b", "B", 'b', true},
		// Case-sensitive on the XMP claim; ISO 19005 requires the value
		// to be uppercase.
		{"lowercase a claim does not cover a", "a", 'a', false},
		{"lowercase u claim does not cover b", "u", 'b', false},
		{"lowercase b claim does not cover b", "b", 'b', false},
		// Profile letter is matched case-insensitively so callers can
		// pass either form of [PDFALevel].
		{"uppercase profile letter", "A", 'A', true},
		// Degenerate input.
		{"empty xmp", "", 'b', false},
		{"two-char xmp", "AB", 'b', false},
		{"unknown letter", "X", 'b', false},
		{"unknown profile letter", "A", 'z', false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ConformanceSatisfies(tc.xmpConf, tc.level); got != tc.want {
				t.Errorf("ConformanceSatisfies(%q, %q) = %v, want %v",
					tc.xmpConf, string(tc.level), got, tc.want)
			}
		})
	}
}
