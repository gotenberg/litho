package compliance

import "testing"

func TestPdfaPart(t *testing.T) {
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
	}
	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			if got := pdfaPart(tc.profile); got != tc.want {
				t.Errorf("pdfaPart(%q) = %d, want %d", tc.profile, got, tc.want)
			}
		})
	}
}

func TestPdfaLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile string
		want    byte
	}{
		{"PDF/A-1a", 'a'},
		{"PDF/A-1b", 'b'},
		{"PDF/A-2u", 'u'},
		{"PDF/A-4", '4'},
		{"PDF/A-4e", 'e'},
		{"PDF/A-4f", 'f'},
		{"", 0},
	}
	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			if got := pdfaLevel(tc.profile); got != tc.want {
				t.Errorf("pdfaLevel(%q) = %c, want %c", tc.profile, got, tc.want)
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
		{"PDF/UA-99", true},
		{"PDF/A-1a", false},
		{"PDF/A-4", false},
		{"", false},
		{"PDF/U", false},
		{"PDFUA", false},
	}
	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			if got := isPDFUA(tc.profile); got != tc.want {
				t.Errorf("isPDFUA(%q) = %v, want %v", tc.profile, got, tc.want)
			}
		})
	}
}

func TestRuleByPart(t *testing.T) {
	t.Parallel()

	r1 := Rule{Specification: "r1"}
	r2 := Rule{Specification: "r2"}
	r3 := Rule{Specification: "r3"}
	r4 := Rule{Specification: "r4"}

	tests := []struct {
		name string
		part int
		want string
	}{
		{"part 1", 1, "r1"},
		{"part 2", 2, "r2"},
		{"part 3", 3, "r3"},
		{"part 4", 4, "r4"},
		{"part 0", 0, ""},
		{"part 5", 5, ""},
		{"part -1", -1, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ruleByPart(tc.part, r1, r2, r3, r4)
			if got.Specification != tc.want {
				t.Errorf("ruleByPart(%d, ...) = %q, want %q", tc.part, got.Specification, tc.want)
			}
		})
	}
}

func TestViolationError(t *testing.T) {
	t.Parallel()

	t.Run("with context", func(t *testing.T) {
		t.Parallel()
		v := Violation{
			Rule:    Rule{Clause: "6.1", TestNumber: 1, Description: "test desc"},
			Context: "page 1",
		}
		got := v.Error()
		if got != "clause 6.1 test 1: test desc [page 1]" {
			t.Errorf("unexpected Error(): %s", got)
		}
	})

	t.Run("without context", func(t *testing.T) {
		t.Parallel()
		v := Violation{
			Rule: Rule{Clause: "6.1", TestNumber: 1, Description: "test desc"},
		}
		got := v.Error()
		if got != "clause 6.1 test 1: test desc" {
			t.Errorf("unexpected Error(): %s", got)
		}
	})
}

func TestViolationsToErrors(t *testing.T) {
	t.Parallel()

	t.Run("nil on empty", func(t *testing.T) {
		t.Parallel()
		if got := violationsToErrors(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("converts violations", func(t *testing.T) {
		t.Parallel()
		vs := []Violation{
			{Rule: Rule{Clause: "1"}, Context: "a"},
			{Rule: Rule{Clause: "2"}, Context: "b"},
		}
		errs := violationsToErrors(vs)
		if len(errs) != 2 {
			t.Fatalf("expected 2 errors, got %d", len(errs))
		}
	})
}
