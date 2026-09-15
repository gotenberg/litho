package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestICCBasedProfileClassAndSpace pins ISO 19005-1, 6.2.3.2 t1 and ISO
// 19005-2, 6.2.4.2 t1 on the header of an ICCBased colour space's profile, as
// veraPDF's ICCInputProfile test reads it: the device class shall be prtr,
// mntr, scnr or spac, the colour space RGB, CMYK, GRAY or Lab, and the version
// below the part's ceiling (3 for PDF/A-1, 5 after). Every case was measured
// against veraPDF.
func TestICCBasedProfileClassAndSpace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name    string
		cls, cs string
		major   byte
		want1   int // PDF/A-1b, 6.2.3.2 t1
		want2   int // PDF/A-2b, 6.2.4.2 t1
	}{
		{"a monitor RGB profile", "mntr", "RGB ", 2, 0, 0},
		{"an abstract Lab profile", "abst", "Lab ", 2, 1, 1},
		{"a device link RGB profile", "link", "RGB ", 2, 1, 1},
		{"a monitor XYZ profile", "mntr", "XYZ ", 2, 1, 1},
		{"a version 4 monitor RGB profile", "mntr", "RGB ", 4, 1, 0},
		{"a colour space Lab profile", "spac", "Lab ", 2, 0, 0},
		{"a scanner RGB profile", "scnr", "RGB ", 2, 0, 0},
		{"a named colour RGB profile", "nmcl", "RGB ", 2, 1, 1},
	}
	for _, tt := range tests {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
			want    int
		}{{models.PDFA1b, Rule_19005_1_6_2_3_2_t01, tt.want1}, {models.PDFA2b, Rule_19005_2_6_2_4_2_t01, tt.want2}} {
			t.Run(fmt.Sprintf("%s, %s", tt.name, p.profile), func(t *testing.T) {
				t.Parallel()
				icc := []byte(iccHeaderOnly(tt.cls, tt.cs))
				icc[8] = tt.major
				body := "/CS0 cs 0.5 0.5 0.5 sc 0 0 10 10 re f"
				doc, _ := objsDocPDF(t,
					"<< /Type /Catalog /Pages 2 0 R >>",
					"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
					"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /ColorSpace << /CS0 [/ICCBased 5 0 R] >> >> >>",
					fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
					fmt.Sprintf("<< /N 3 /Length %d >>\nstream\n%s\nendstream", len(icc), icc),
				)
				viols, err := ValidatePDFA(ctx, doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != p.want {
					t.Errorf("%s t%d fired %d times, want %d", p.rule.Clause, p.rule.TestNumber, got, p.want)
				}
			})
		}
	}
}
