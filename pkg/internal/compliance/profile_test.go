package compliance

import "testing"

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

func TestIsPDFUA(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile string
		want    bool
	}{
		{"PDF/UA-1", true},
		{"PDF/UA-2", true},
		{"PDF/UA-99", true}, // prefix match; caller must check part separately
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

func TestPDFUAPart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile string
		want    int
	}{
		{"PDF/UA-1", 1},
		{"PDF/UA-2", 2},
		{"PDF/UA-99", 0}, // matches IsPDFUA prefix but not a real part
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
