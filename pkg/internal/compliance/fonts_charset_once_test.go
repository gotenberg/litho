package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestCharSetCompletenessJudgedOncePerFont pins that the parts 2/3 and PDF/UA
// /CharSet rule is stated once per font object, not once per page that selects
// it.
//
// The rule compares /CharSet against the whole embedded program, which no
// page's codes enter into, so it belongs with the dictionary rules for the same
// reason the glyph-width comparison does.
//
// # Corpus evidence
//
//   - govdocs thread_300_300511.pdf: selects NSNPEK+CMR12 from 31 pages, and
//     litho reported its missing glyph "O" 31 times, where veraPDF states it
//     once against the font.
func TestCharSetCompletenessJudgedOncePerFont(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// A program defining two glyphs, against a /CharSet naming only one.
	program := type1Program("/Encoding StandardEncoding def", "A", "B")
	const content = "BT /F1 12 Tf 10 20 Td (A) Tj ET"

	page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R" +
		" /Resources << /Font << /F1 4 0 R >> >> >>"
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 7 0 R 8 0 R] /Count 3 >>",
		page,
		"<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+LithoT1 /FirstChar 65 /LastChar 65"+
			" /Widths [600] /FontDescriptor 5 0 R >>",
		"<< /Type /FontDescriptor /FontName /ABCDEF+LithoT1 /Flags 32 /CharSet (/A)"+
			" /FontFile 9 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		page, page,
		fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream",
			len(program), len(program), program),
	)

	pages := pageCtxsFor(ctx, doc, cat)
	for i := range pages {
		pages[i].content = []byte(content)
	}

	got := checkFontGroup(ctx, &validationCtx{
		doc: doc, catalog: cat, profile: "PDF/A-2b", part: 2, pages: pages,
	})

	n := 0
	for _, v := range got {
		if v.Rule == Rule_19005_2_6_2_11_4_2_t01 {
			n++
		}
	}
	if n != 1 {
		t.Errorf("an incomplete /CharSet must report once per font object, got %d (%v)", n, got)
	}
}
