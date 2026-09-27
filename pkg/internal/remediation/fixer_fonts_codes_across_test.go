package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// sharedFont is the simple font every case of
// [TestFixFontIssuesAccumulatesShownCodesAcrossUses] shows text with, object 9:
// its /Encoding states what each code means, and it carries no /ToUnicode.
const sharedFont = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"

// fontUsesPDF builds a document from objects 1 to 8, with [sharedFont] as
// object 9. Object 1 is the catalog and object 2 the page tree root.
func fontUsesPDF(objs ...string) []byte {
	for len(objs) < 8 {
		objs = append(objs, "null")
	}
	return buildSimplePDF(append(objs, sharedFont))
}

// TestFixFontIssuesAccumulatesShownCodesAcrossUses pins that the /ToUnicode
// decision for a font reads the codes every use of it shows.
//
// The repair attaches a generated /ToUnicode only when it maps every code the
// font shows, the set ISO 19005-2, 6.2.11.7.2 holds it to. It read the codes
// of the first page whose /Resources named the font and marked the font done
// there: a page naming it without showing a code with it left every later page
// that does without a mapping, and the codes a form, a tiling pattern, a Type 3
// glyph procedure or an annotation appearance shows never counted. A font such
// a stream alone names was never reached at all.
func TestFixFontIssuesAccumulatesShownCodesAcrossUses(t *testing.T) {
	t.Parallel()

	const (
		catalog = "<< /Type /Catalog /Pages 2 0 R >>"
		onePage = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		show    = "BT /F1 12 Tf (AB) Tj ET"
	)
	page := func(resources, rest string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources " + resources + " " + rest + " >>"
	}

	tests := []struct {
		name string
		data []byte
		// want reports whether the font should take a /ToUnicode.
		want bool
	}{
		{
			name: "a later page shows the codes",
			data: fontUsesPDF(
				catalog,
				"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
				page("<< /Font << /F1 9 0 R >> >>", "/Contents 5 0 R"),
				page("<< /Font << /F1 9 0 R >> >>", "/Contents 6 0 R"),
				contentObj("q Q"),
				contentObj(show),
			),
			want: true,
		},
		{
			name: "a form the page draws shows the codes",
			data: fontUsesPDF(
				catalog, onePage,
				page("<< /Font << /F1 9 0 R >> /XObject << /Fm1 4 0 R >> >>", "/Contents 5 0 R"),
				formStream("<< /Font << /F1 9 0 R >> >>", show),
				contentObj("q /Fm1 Do Q"),
			),
			want: true,
		},
		{
			name: "only a form the page draws names the font",
			data: fontUsesPDF(
				catalog, onePage,
				page("<< /XObject << /Fm1 4 0 R >> >>", "/Contents 5 0 R"),
				formStream("<< /Font << /F1 9 0 R >> >>", show),
				contentObj("q /Fm1 Do Q"),
			),
			want: true,
		},
		{
			name: "a tiling pattern cell shows the codes",
			data: fontUsesPDF(
				catalog, onePage,
				page("<< /Font << /F1 9 0 R >> /Pattern << /P1 4 0 R >> >>", "/Contents 5 0 R"),
				streamBody("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10]"+
					" /XStep 10 /YStep 10 /Resources << /Font << /F1 9 0 R >> >>", show),
				contentObj("/Pattern cs /P1 scn 0 0 10 10 re f"),
			),
			want: true,
		},
		{
			name: "a Type 3 glyph procedure shows the codes",
			data: fontUsesPDF(
				catalog, onePage,
				page("<< /Font << /T3 4 0 R >> >>", "/Contents 5 0 R"),
				"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.001 0 0 0.001 0 0]"+
					" /CharProcs << /a 6 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >>"+
					" /FirstChar 97 /LastChar 97 /Widths [1000] /Resources << /Font << /F1 9 0 R >> >> >>",
				contentObj("BT /T3 12 Tf (a) Tj ET"),
				streamBody("", "1000 0 d0 "+show),
			),
			want: true,
		},
		{
			name: "an annotation appearance shows the codes",
			data: fontUsesPDF(
				catalog, onePage,
				page("<< /Font << /F1 9 0 R >> >>", "/Contents 5 0 R /Annots [4 0 R]"),
				"<< /Type /Annot /Subtype /FreeText /Rect [0 0 10 10] /F 4 /DA (/F1 12 Tf) /AP << /N 6 0 R >> >>",
				contentObj("q Q"),
				formStream("<< /Font << /F1 9 0 R >> >>", show),
			),
			want: true,
		},
		{
			// 0x81 is a code WinAnsiEncoding leaves undefined, so no mapping
			// can cover it, and a CMap covering the page's codes alone is
			// partial.
			name: "a form shows a code no mapping covers",
			data: fontUsesPDF(
				catalog, onePage,
				page("<< /Font << /F1 9 0 R >> /XObject << /Fm1 4 0 R >> >>", "/Contents 5 0 R"),
				formStream("<< /Font << /F1 9 0 R >> >>", "BT /F1 12 Tf <81> Tj ET"),
				contentObj("BT /F1 12 Tf (A) Tj ET q /Fm1 Do Q"),
			),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, tc.data)
			fixes, err := fixFontIssues(ctx, alloc, doc, Options{}, nil, "PDF/A-2u")
			if err != nil {
				t.Fatalf("fixFontIssues: %v", err)
			}

			font, _ := alloc.Pending()[9].(ast.PdfDict)
			_, got := font["ToUnicode"]
			if got != tc.want {
				t.Errorf("the font takes a /ToUnicode: %v, want %v (fixes %q)", got, tc.want, fixes)
			}
		})
	}
}
