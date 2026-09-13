package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestMediaClipAltShape pins ISO 14289-1, 7.18.6.2 t2 as veraPDF tests it
// (GFPDMediaClip hasCorrectAlt): the media clip's /Alt shall be an array of
// even length whose every element is a text string (ISO 32000-1, Table 274:
// pairs of a language and a text), and every text shall be non-empty once
// decoded, untrimmed. An empty array passes. Every case was measured against
// veraPDF.
func TestMediaClipAltShape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name string
		alt  string // the clip's /Alt entry, empty for absent
		want int
	}{
		{"a plain string", " /Alt (a movie)", 1},
		{"an empty array", " /Alt []", 0},
		{"an odd-length array", " /Alt [(en) (a movie) (fr)]", 1},
		{"one empty text among good ones", " /Alt [(en) (a movie) (fr) ()]", 1},
		{"a name as the language", " /Alt [/en (a movie)]", 1},
		{"a text of spaces", " /Alt [(en) (   )]", 0},
		{"one language and text pair", " /Alt [(en) (a movie)]", 0},
		{"a text holding only a byte order mark", " /Alt [(en) <FEFF>]", 1},
		{"no /Alt", "", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R] >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 6 0 R] >> >>",
				"<< /S /Document /P 4 0 R /K [6 0 R] >>",
				"<< /S /Annot /P 5 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>] >>",
				"<< /Type /Annot /Subtype /Screen /Rect [10 10 100 100] /F 4 /Contents (a movie) /StructParent 0"+
					" /A << /S /Rendition /OP 0 /R << /Type /Rendition /S /MR /C << /Type /MediaClip /S /MCD"+
					" /CT (video/mp4) /D (movie.mp4)"+tt.alt+" >> >> >> >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_18_6_2_t02); got != tt.want {
				t.Errorf("7.18.6.2 t2 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
