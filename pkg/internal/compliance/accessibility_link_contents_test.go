package compliance

import (
	"context"
	"testing"
)

// TestCheckLinkContents_VeraPDFScope pins ISO 14289-1 §7.18.5 t2 against the
// terms veraPDF's test names: a /Contents present and not empty, or a
// rectangle outside the crop box (isOutsideCropBox), or the hidden flag
// ((F & 2) == 2).
//
// A hidden Link and one whose rectangle lies off the page's crop box are exempt;
// pdfjs issue3694_reduced.pdf has only an off-page Link without /Contents and
// veraPDF passes it. /Contents is a text string judged by the text it decodes
// to, so a byte order mark alone does not describe the link (ISO 32000-1,
// 7.9.2.2).
func TestCheckLinkContents_VeraPDFScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		link  string
		fires bool
	}{
		{"a Link with no /Contents", "<< /Type /Annot /Subtype /Link /Rect [10 10 50 30] >>", true},
		{"a /Contents that is only a byte order mark", "<< /Type /Annot /Subtype /Link /Rect [10 10 50 30] /Contents <FEFF> >>", true},
		{"a hidden Link", "<< /Type /Annot /Subtype /Link /Rect [10 10 50 30] /F 2 >>", false},
		{"a Link off the crop box", "<< /Type /Annot /Subtype /Link /Rect [700 10 750 30] >>", false},
		{"a Link with a description", "<< /Type /Annot /Subtype /Link /Rect [10 10 50 30] /Contents (Home page) >>", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
				tt.link,
			)
			got := checkLinkContents(context.Background(), doc, cat, "PDF/UA-1")
			if fired := hasRule(got, Rule_14289_1_7_18_5_t02); fired != tt.fires {
				t.Errorf("7.18.5 t2 fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
