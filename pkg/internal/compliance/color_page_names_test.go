package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestColorScans_JudgePageNamesOnlyFormsSelect pins that the device colour
// and ICCBased scans judge a page's colour space that only a form the page
// paints selects, through the page's resources (ISO 32000-1, 7.8.3), and leave
// alone one that nothing selects. Measured with veraPDF 1.30 at PDF/A-1b and
// PDF/A-2b on [pageNamesPDF].
func TestColorScans_JudgePageNamesOnlyFormsSelect(t *testing.T) {
	t.Parallel()

	abstract := iccHeaderOnly("abst", "RGB ")
	icc := fmt.Sprintf("<< /N 3 /Length %d >>\nstream\n%s\nendstream", len(abstract), abstract)
	tests := []struct {
		name   string
		cs     string
		extra  []string
		rules  map[models.PDFAProfile][2]any
		fillOp string
	}{
		{
			name:   "an indexed space over DeviceGray",
			cs:     "/CS0 [/Indexed /DeviceGray 1 <00FF>]",
			fillOp: "0 scn",
			rules: map[models.PDFAProfile][2]any{
				models.PDFA1b: {"6.2.3.3", 3},
				models.PDFA2b: {"6.2.4.3", 4},
			},
		},
		{
			name:   "an ICCBased space over an abstract profile",
			cs:     "/CS0 [/ICCBased 7 0 R]",
			extra:  []string{icc},
			fillOp: "0 0 0 scn",
			rules: map[models.PDFAProfile][2]any{
				models.PDFA1b: {"6.2.3.2", 1},
				models.PDFA2b: {"6.2.4.2", 1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			formOnly := pageNamesPDF(tt.cs, "/Fm0 Do", "/CS0 cs "+tt.fillOp+" 0 0 1 1 re f", tt.extra...)
			unselected := pageNamesPDF(tt.cs, "/Fm0 Do", "0 0 1 1 re n", tt.extra...)
			for profile, rule := range tt.rules {
				clause, test := rule[0].(string), rule[1].(int)
				if got := ruleContexts(t, formOnly, profile, clause, test); len(got) == 0 {
					t.Errorf("%s: %s t%d did not fire when a form selects the page's space", profile, clause, test)
				}
				if got := ruleContexts(t, unselected, profile, clause, test); len(got) != 0 {
					t.Errorf("%s: %s t%d fired %d times when nothing selects the space: %q", profile, clause, test, len(got), got)
				}
			}
		})
	}
}
