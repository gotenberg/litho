package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// realContentStream renders a content stream object whose dictionary holds
// dict and whose data is body.
func realContentStream(dict, body string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(body), body)
}

// realContentType3 renders a Type 3 font whose glyphs g1 and g2, codes A and
// B, are the procedures 6 0 R and 7 0 R and carry names no Unicode value
// derives from, against resources res.
func realContentType3(res string) string {
	return "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0]" +
		" /CharProcs << /g1 6 0 R /g2 7 0 R >> /Encoding << /Type /Encoding /Differences [65 /g1 /g2] >>" +
		" /FirstChar 65 /LastChar 66 /Widths [1000 1000] /Resources " + res + " >>"
}

// realContentObjs returns a one-page document: the page (3) shows content
// (4) against the resources pageRes, font A (5) is a Type 3 font with the
// procedures g1 (6) and g2 (7) and the resources aRes, object 8 is a tiling
// pattern whose cell runs cell, object 9 a form running form, and object 10 a
// Type 3 font B whose one glyph h1, code A, is the procedure 11 running proc.
// annots, when not empty, is the page's /Annots.
func realContentObjs(pageRes, content, aRes, g1, cell, form, proc, annots string) []string {
	page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources " + pageRes + " /Contents 4 0 R"
	if annots != "" {
		page += " /Annots " + annots
	}
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		page + " >>",
		realContentStream("", content),
		realContentType3(aRes),
		realContentStream("", "1000 0 d0 "+g1),
		realContentStream("", "1000 0 d0 0 0 10 10 re f"),
		realContentStream("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 20 20] /XStep 20 /YStep 20"+
			" /Resources << /Font << /TA 5 0 R >> /XObject << /Fm 9 0 R >> >>", cell),
		realContentStream("/Type /XObject /Subtype /Form /BBox [0 0 20 20] /Resources << /Font << /TA 5 0 R >> /XObject << /In 12 0 R >> >>", form),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0]" +
			" /CharProcs << /h1 11 0 R >> /Encoding << /Type /Encoding /Differences [65 /h1] >>" +
			" /FirstChar 65 /LastChar 65 /Widths [1000] /Resources << /Font << /TA 5 0 R >> /XObject << /Fm 9 0 R >> >> >>",
		realContentStream("", "1000 0 d0 "+proc),
		realContentStream("/Type /XObject /Subtype /Form /BBox [0 0 20 20] /Resources << /Font << /TA 5 0 R >> >>", "BT /TA 4 Tf (A) Tj ET"),
	}
}

// TestGlyphIdentityRealContent pins that veraPDF tells a glyph shown in a
// page's content, or a form it draws, apart from the same glyph shown in a
// tiling cell, a Type 3 glyph procedure or an annotation appearance at PDF/UA
// and WTPDF, and not at PDF/A; see [glyphContext]. It also pins that a cell
// starts in the text rendering mode of the stream selecting its pattern, and
// that a Type 3 font's glyphs a cell its own procedure selects shows are
// judged as any other.
//
// Every count was measured with veraPDF 1.30.2 at PDF/A-2u, PDF/UA-1, PDF/UA-2
// and WTPDF 1.0 Accessibility on Type 3 glyphs with no Unicode value.
func TestGlyphIdentityRealContent(t *testing.T) {
	t.Parallel()

	const (
		fontA     = "<< /Font << /TA 5 0 R >> >>"
		fontsAB   = "<< /Font << /TA 5 0 R /TB 10 0 R >> >>"
		patternP1 = "<< /Font << /TA 5 0 R >> /Pattern << /P1 8 0 R >> >>"
		showA     = "BT /TA 10 Tf 1 1 Td (A) Tj ET"
		selectP1  = " q /Pattern cs /P1 scn 0 0 100 100 re f Q"
		plain     = "0 0 10 10 re f"
		cellShowA = "BT /TA 4 Tf (A) Tj ET"
		selfCell  = "<< /Pattern << /P1 8 0 R >> >>"
		annotRef  = "[13 0 R]"
	)
	appearance := func(objs []string) []string {
		return append(objs, "<< /Type /Annot /Subtype /Square /Rect [10 10 30 30] /F 4 /AP << /N 12 0 R >> >>")
	}
	for _, tc := range []struct {
		name    string
		objs    []string
		pdfa    int
		uaCount int
	}{
		{"the page and a cell", realContentObjs(patternP1, showA+selectP1, "<< >>", plain, cellShowA, "", "", ""), 1, 2},
		{"the page in mode 2 and a cell", realContentObjs(patternP1, "BT 2 Tr /TA 10 Tf (A) Tj ET"+selectP1, "<< >>", plain, cellShowA, "", "", ""), 2, 2},
		{"the page and a form", realContentObjs("<< /Font << /TA 5 0 R >> /XObject << /Fm 12 0 R >> >>", showA+" /Fm Do", "<< >>", plain, "", "", "", ""), 1, 1},
		{"the page and a form a form draws", realContentObjs("<< /Font << /TA 5 0 R >> /XObject << /Fm 9 0 R >> >>", showA+" /Fm Do", "<< >>", plain, "", "/In Do", "", ""), 1, 1},
		{"the page and a form a cell draws", realContentObjs(patternP1, showA+selectP1, "<< >>", plain, "/Fm Do", "/In Do", "", ""), 1, 2},
		{"the page and a Type 3 procedure", realContentObjs(fontsAB, showA+" BT /TB 10 Tf (A) Tj ET", "<< >>", plain, "", "", cellShowA, ""), 2, 3},
		{"the page and an annotation appearance", appearance(realContentObjs(fontA, showA, "<< >>", plain, "", "", "", annotRef)), 1, 2},
		{"a cell its own procedure selects, the other glyph", realContentObjs(fontA, showA, selfCell, "/Pattern cs /P1 scn "+plain, "BT /TA 4 Tf (B) Tj ET", "", "", ""), 2, 2},
		{"a cell its own procedure selects, both glyphs", realContentObjs(fontA, showA, selfCell, "/Pattern cs /P1 scn "+plain, "BT /TA 4 Tf (AB) Tj ET", "", "", ""), 2, 3},
		{"a cell its own procedure selects, in the page's mode 2", realContentObjs(fontA, "BT 2 Tr /TA 10 Tf (A) Tj ET", selfCell, "/Pattern cs /P1 scn "+plain, cellShowA, "", "", ""), 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			doc, _ := objsDocPDF(t, tc.objs...)
			viols, err := ValidatePDFA(ctx, doc, models.PDFA2u)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_2_6_2_11_7_2_t01); got != tc.pdfa {
				t.Errorf("PDF/A-2u: 6.2.11.7.2 t1 fired %d times, want %d", got, tc.pdfa)
			}
			for _, p := range []struct {
				profile models.PDFUAProfile
				rule    Rule
			}{
				{models.PDFUA1, Rule_14289_1_7_21_7_t01},
				{models.PDFUA2, Rule_14289_2_8_4_5_8_t01},
			} {
				doc, _ := objsDocPDF(t, tc.objs...)
				viols, err := ValidatePDFUA(ctx, doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFUA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.uaCount {
					t.Errorf("%s: %s t%d fired %d times, want %d", p.profile, p.rule.Clause, p.rule.TestNumber, got, tc.uaCount)
				}
			}
			doc, _ = objsDocPDF(t, tc.objs...)
			viols, err = ValidateWTPDF(ctx, doc, models.WTPDF1Accessibility)
			if err != nil {
				t.Fatalf("ValidateWTPDF: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_2_8_4_5_8_t01); got != tc.uaCount {
				t.Errorf("WTPDF: 8.4.5.8 t1 fired %d times, want %d", got, tc.uaCount)
			}
		})
	}
}
