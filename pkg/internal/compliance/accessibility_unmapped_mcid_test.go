package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestUnmappedMCIDShapes pins three shapes the retired orphan-MCID check
// reported under ISO 14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1, and veraPDF
// 1.30.2 does not: an /MCID the ParentTree does not map inside one it maps, one
// wrapping no painting, and one wrapping only an artifact. Content is tagged
// when the innermost mapped /MCID around it says so, and a sequence that
// paints nothing, or paints only an artifact, is no untagged content. The
// control, an unmapped /MCID around painting, fails at both levels.
func TestUnmappedMCIDShapes(t *testing.T) {
	t.Parallel()

	tagged := "/P <</MCID 0>> BDC " + namedListText + " EMC "
	for _, tc := range []struct {
		name string
		page string
		want int
	}{
		{"an unmapped MCID inside a mapped one", "/P <</MCID 0>> BDC /Span <</MCID 7>> BDC " + namedListText + " EMC EMC", 0},
		{"an unmapped MCID wrapping no painting", tagged + "/P <</MCID 7>> BDC EMC", 0},
		{"an unmapped MCID wrapping only an artifact", tagged + "/P <</MCID 7>> BDC /Artifact BMC " + namedListText + " EMC EMC", 0},
		{"an unmapped MCID wrapping painting", "/P <</MCID 7>> BDC " + namedListText + " EMC", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, namedListDoc{page: tc.page, lang: true}.objects()...)
			if got := uaCount(t, doc, models.PDFUA1, Rule_14289_1_7_1_t03); got != tc.want {
				t.Errorf("PDF/UA-1 7.1 t3 = %d, want %d", got, tc.want)
			}
			if got := uaCount(t, doc, models.PDFUA2, Rule_14289_2_8_2_2_t01); got != tc.want {
				t.Errorf("PDF/UA-2 8.2.2 t1 = %d, want %d", got, tc.want)
			}
		})
	}
}
