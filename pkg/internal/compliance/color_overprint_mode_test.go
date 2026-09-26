package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestICCBasedCMYKCountsEachOverprintMode pins that veraPDF judges a
// four-component ICCBased space once for every overprint mode and flag a
// painting operator or a text show links it under, the direct spaces of the
// document sharing one identity per mode (see [checkICCBasedProfiles]), and
// fails ISO 19005-2, 6.2.4.2 t2 once for each judged under overprint mode 1
// with overprinting on. Every count was measured with veraPDF 1.30 at PDF/A-1b
// and PDF/A-2b on the same fixture, over an abstract profile.
func TestICCBasedCMYKCountsEachOverprintMode(t *testing.T) {
	t.Parallel()

	profile := func(pad int) string {
		data := []byte(iccHeaderOnly("abst", "CMYK"))
		data = append(data, make([]byte, pad)...)
		return fmt.Sprintf("<< /N 4 /Length %d >>\nstream\n%s\nendstream", len(data), data)
	}
	cmyk := profile(0)
	const (
		fill    = "0 0 10 10 re f"
		select0 = "/CS0 cs 0 0 0 1 scn "
		ext     = "/ExtGState << /GS1 << /OP true /op true /OPM 1 >> /GS2 << /op true >> /GS3 << /OPM 1 >> /GS4 << /OP true /OPM 1 >> >>"
		direct  = "<< /ColorSpace << /CS0 [/ICCBased 5 0 R] >> " + ext + " >>"
	)
	twoPages := func(cs string, extra ...string) []byte {
		res := "<< /ColorSpace << /CS0 " + cs + " >> " + ext + " >>"
		content := linkStreamObj("", select0+"/GS1 gs "+fill)
		return cyclePDF(append([]string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + res + " /Contents 5 0 R >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + res + " /Contents 6 0 R >>",
			content, content + " ",
		}, extra...)...)
	}
	tests := []struct {
		name            string
		data            []byte
		profile, opmOne int
	}{
		{"one mode", linkPagePDF(direct, select0+fill, cmyk), 1, 0},
		{"a fill under OPM 1 and op", linkPagePDF(direct, select0+fill+" /GS1 gs "+fill, cmyk), 2, 1},
		{"four modes", linkPagePDF(direct, select0+fill+" q /GS1 gs "+fill+" Q q /GS2 gs "+fill+" Q q /GS3 gs "+fill+" Q", cmyk), 4, 1},
		{"an indirect space", linkPagePDF("<< /ColorSpace << /CS0 6 0 R >> "+ext+" >>", select0+fill+" /GS1 gs "+fill, cmyk, "[/ICCBased 5 0 R]"), 2, 1},
		{"a stroke under OPM 1 and OP", linkPagePDF(direct, "/CS0 CS 0 0 0 1 SCN 0 0 10 10 re S /GS1 gs 0 0 10 10 re S", cmyk), 2, 1},
		{"text under OPM 1 and op", linkPagePDF("<< /ColorSpace << /CS0 [/ICCBased 5 0 R] >> "+ext+
			" /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >>", select0+"/GS1 gs BT /F1 12 Tf (a) Tj ET", cmyk), 2, 1},
		{"an indexed space's base", linkPagePDF("<< /ColorSpace << /CS0 [/Indexed [/ICCBased 5 0 R] 1 <0000000000000000>] >> "+ext+" >>",
			"/CS0 cs 0 scn "+fill+" /GS1 gs "+fill, cmyk), 1, 0},
		{"an image before a fill", linkPagePDF("<< /ColorSpace << /CS0 [/ICCBased 5 0 R] >> /XObject << /Im0 6 0 R >> "+ext+" >>",
			"/Im0 Do "+select0+fill+" /GS1 gs /Im0 Do "+fill, cmyk,
			linkStreamObj("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace [/ICCBased 5 0 R] /BitsPerComponent 8", "abcd")), 2, 1},
		{"a form filling under the page's mode", linkPagePDF("<< /ColorSpace << /CS0 [/ICCBased 5 0 R] >> /XObject << /Fm0 6 0 R >> "+ext+" >>",
			select0+fill+" /GS1 gs /Fm0 Do", cmyk, linkStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >>", "0 0 1 1 re f")), 2, 1},
		{"two pages sharing no object", twoPages("[/ICCBased 7 0 R]", cmyk), 2, 1},
		{"two pages sharing an indirect space", twoPages("8 0 R", cmyk, "[/ICCBased 7 0 R]"), 2, 1},
		{"two direct spaces", linkPagePDF("<< /ColorSpace << /CS0 [/ICCBased 5 0 R] /CS1 [/ICCBased 6 0 R] >> "+ext+" >>",
			select0+"/GS1 gs "+fill+" /CS1 cs 0 0 0 1 scn "+fill, cmyk, profile(4)), 2, 1},
		{"op falling back to OP", linkPagePDF(direct, select0+"/GS4 gs "+fill, cmyk), 2, 1},
		{"a mode the Q undoes", linkPagePDF(direct, select0+"q /GS1 gs Q "+fill, cmyk), 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, p := range []struct {
				profile models.PDFAProfile
				clause  string
			}{{models.PDFA1b, "6.2.3.2"}, {models.PDFA2b, "6.2.4.2"}} {
				if got := ruleContexts(t, tt.data, p.profile, p.clause, 1); len(got) != tt.profile {
					t.Errorf("%s: %s t1 fired %d times, want %d", p.profile, p.clause, len(got), tt.profile)
				}
			}
			if got := ruleContexts(t, tt.data, models.PDFA2b, "6.2.4.2", 2); len(got) != tt.opmOne {
				t.Errorf("6.2.4.2 t2 fired %d times, want %d: %q", len(got), tt.opmOne, got)
			}
		})
	}
}
