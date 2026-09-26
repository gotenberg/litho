package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// langStream renders body as an indirect stream object for [cycleDoc].
func langStream(dict, body string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(body), body)
}

// pdfaLangCount returns how many ISO 19005 Lang violations the PDF/A level-a
// check reports on doc at part.
func pdfaLangCount(t *testing.T, doc *document.Document, part int) int {
	t.Helper()
	cat, err := document.ResolveCatalog(context.Background(), doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	return len(checkPDFALangValid(context.Background(), doc, cat.Dict, "", part))
}

// TestCheckPDFALangValid_MarkedContent pins which marked-content /Lang entries
// the PDF/A level-a Lang check and the PDF/UA one judge, each shape measured on
// veraPDF 1.30.2 at PDF/A-1a and PDF/A-2a with a one-page file carrying the
// invalid tag "1x", and at PDF/UA-1 and PDF/UA-2 with the litho fixtures
// mclang_2a.pdf and mcform_2a.pdf.
//
// veraPDF judges the property list of every BDC operator its content walk
// reaches, inline or named through /Properties, in the page, in a Form XObject
// it draws (once however often it is drawn) and in an annotation appearance,
// whether or not the file has a structure tree. It judges neither a DP
// operator, nor a property list no operator names, nor "/Lang" spelled inside a
// string operand.
func TestCheckPDFALangValid_MarkedContent(t *testing.T) {
	t.Parallel()

	page := func(extra string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Contents 4 0 R " + extra + " >>"
	}
	cases := []struct {
		name   string
		tagged bool
		objs   []string
		want   int
	}{
		{"untagged inline BDC", false, []string{
			page("/Resources << >>"),
			langStream("", "/Span << /Lang (1x) >> BDC EMC"),
		}, 1},
		{"untagged two inline BDC", false, []string{
			page("/Resources << >>"),
			langStream("", "/Span << /Lang (1x) >> BDC EMC /Span << /Lang (2y) >> BDC EMC"),
		}, 2},
		{"untagged named BDC", false, []string{
			page("/Resources << /Properties << /P0 5 0 R >> >>"),
			langStream("", "/Span /P0 BDC EMC"),
			"<< /Lang (1x) >>",
		}, 1},
		{"untagged valid tag", false, []string{
			page("/Resources << >>"),
			langStream("", "/Span << /Lang (en-US) >> BDC EMC"),
		}, 0},
		{"untagged inline DP", false, []string{
			page("/Resources << >>"),
			langStream("", "/Span << /Lang (1x) >> DP"),
		}, 0},
		{"tagged inline DP", true, []string{
			page("/Resources << >>"),
			langStream("", "/Span << /Lang (1x) >> DP"),
		}, 0},
		{"tagged property list no operator names", true, []string{
			page("/Resources << /Properties << /P0 5 0 R >> >>"),
			langStream("", ""),
			"<< /Lang (1x) >>",
		}, 0},
		{"tagged /Lang inside a string operand", true, []string{
			page("/Resources << >>"),
			langStream("", "BT ET (/Lang \\(1x\\)) pop"),
		}, 0},
		{"untagged Form XObject drawn twice", false, []string{
			page("/Resources << /XObject << /Fm0 5 0 R >> >>"),
			langStream("", "/Fm0 Do /Fm0 Do"),
			langStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >>", "/Span << /Lang (1x) >> BDC EMC"),
		}, 1},
		{"untagged Form XObject naming a page property list", false, []string{
			page("/Resources << /XObject << /Fm0 5 0 R >> /Properties << /P0 6 0 R >> >>"),
			langStream("", "/Fm0 Do"),
			langStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "/Span /P0 BDC EMC"),
			"<< /Lang (1x) >>",
		}, 1},
		{"untagged annotation appearance", false, []string{
			page("/Resources << >> /Annots [5 0 R]"),
			langStream("", ""),
			"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 6 0 R >> >>",
			langStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >>", "/Span << /Lang (1x) >> BDC EMC"),
		}, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			catalog := "<< /Type /Catalog /Pages 2 0 R >>"
			objs := append([]string{catalog, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"}, tc.objs...)
			if tc.tagged {
				objs[0] = fmt.Sprintf("<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot %d 0 R >>", len(objs)+1)
				objs = append(objs, "<< /Type /StructTreeRoot >>")
			}
			doc := cycleDoc(t, objs...)
			for _, part := range []int{1, 2, 3} {
				if got := pdfaLangCount(t, doc, part); got != tc.want {
					t.Errorf("part %d: got %d Lang violations, want %d", part, got, tc.want)
				}
			}
			cat, err := document.ResolveCatalog(context.Background(), doc)
			if err != nil {
				t.Fatalf("ResolveCatalog: %v", err)
			}
			for _, profile := range []string{"PDF/UA-1", "PDF/UA-2"} {
				if got := len(checkMarkedContentLang(context.Background(), doc, cat.Dict, profile)); got != tc.want {
					t.Errorf("%s: got %d marked-content Lang violations, want %d", profile, got, tc.want)
				}
			}
		})
	}
}
