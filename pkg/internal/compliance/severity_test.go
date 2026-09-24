package compliance

import "testing"

// TestSeverityOf pins the severity [SeverityOf] assigns the CII schematron
// rules and a PDF/A rule.
func TestSeverityOf(t *testing.T) {
	cases := []struct {
		name string
		rule Rule
		want string
	}{
		{"cii warning rule maps to warning", Rule_cii_sch_Warning, SeverityWarning},
		{"cii fatal rule maps to fatal", Rule_cii_sch_Fatal, SeverityFatal},
		{"a PDF/A rule is fatal (binary conformance)", Rule{Specification: "ISO_19005_1", Clause: "6.1.2", TestNumber: 1}, SeverityFatal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SeverityOf(Violation{Rule: tc.rule}); got != tc.want {
				t.Errorf("SeverityOf = %q, want %q", got, tc.want)
			}
		})
	}
}
