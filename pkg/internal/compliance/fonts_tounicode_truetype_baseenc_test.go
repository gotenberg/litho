package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestToUnicodeTrueTypeBaseEncodingNamesTheRest pins that an embedded
// non-symbolic TrueType earns the Unicode exemption at the PDF/A levels, not
// only under PDF/UA, when its /Differences name some codes and the base
// encoding names the rest.
//
// ISO 19005-2, 6.2.11.7.2, ISO 14289-1, 7.21.7 and ISO 14289-2, 8.4.5.8 ask the
// same question, and veraPDF answers it the same way in every flavour
// (Glyph.toUnicode != null).
//
// # Corpus evidence
//
//   - pdfjs bug1727053.pdf: an embedded Arial,Bold showing 27 codes, 23 of
//     them named by the implicit StandardEncoding base rather than by its
//     88-entry /Differences. veraPDF passes all 27; litho reported the font at
//     PDF/A-2u, 2a, 3a and 3u while staying silent at PDF/UA-1 and UA-2.
//   - govdocs: the unembedded control has the shape of the 16 files the
//     /Encoding dictionary restriction protects.
func TestToUnicodeTrueTypeBaseEncodingNamesTheRest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Code 65 is named by /Differences, code 66 is not and falls to the base
	// encoding, which names it "B" for an embedded non-symbolic TrueType.
	build := func(t *testing.T, profile, fontFile string, usage *fontUsageInfo) []Violation {
		t.Helper()
		objs := []string{
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /TrueType /Name /F1 /BaseFont /LithoSans" +
				" /FirstChar 65 /LastChar 66 /Encoding 3 0 R /FontDescriptor 4 0 R >>",
			"<< /Type /Encoding /Differences [ 65 /A ] >>",
			"<< /Type /FontDescriptor /FontName /LithoSans /Flags 32" + fontFile + " >>",
		}
		if fontFile != "" {
			objs = append(objs, "<< /Length 4 /Length1 4 >>\nstream\n\x00\x01\x00\x00\nendstream")
		}
		doc, _ := objsDocPDF(t, objs...)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		return checkToUnicode(ctx, doc, fontDict, "F1", 1, profile, usage)
	}
	used := func(codes ...byte) *fontUsageInfo {
		u := &fontUsageInfo{visible: true, charCodes: map[byte]bool{}}
		for _, c := range codes {
			u.charCodes[c] = true
		}
		return u
	}

	for _, profile := range []string{"PDF/A-2u", "PDF/A-2a", "PDF/A-3u", "PDF/UA-1", "PDF/UA-2"} {
		t.Run(profile, func(t *testing.T) {
			t.Parallel()

			// The embedded font earns the base encoding, so the code its
			// /Differences never names still carries a Unicode value.
			if got := build(t, profile, " /FontFile2 5 0 R", used(65, 66)); len(got) != 0 {
				t.Errorf("an embedded TrueType whose base encoding names the rest needs no /ToUnicode, got %v", got)
			}
			// Without a program there is no base encoding to earn, which is the
			// shape the restriction protects.
			if got := build(t, profile, "", used(65, 66)); len(got) == 0 {
				t.Error("an unembedded TrueType must still require /ToUnicode for a code /Differences does not name")
			}
		})
	}
}
