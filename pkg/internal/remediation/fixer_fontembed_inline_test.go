package remediation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// inlineFontsPDF builds one page showing "AB" with fonts written inline in
// three places: /F1 and /F3 in the page's direct /Resources, /F2 in the
// indirect /Font dictionary object 6, and /F1 of form X1, object 5, in the
// form's direct /Resources.
func inlineFontsPDF() []byte {
	page := "BT /F1 12 Tf 72 700 Td (AB) Tj /F2 12 Tf 0 20 Td (AB) Tj /F3 12 Tf 0 20 Td (AB) Tj ET /X1 Do"
	form := "BT /F1 12 Tf 72 600 Td (AB) Tj ET"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font 6 0 R"+
			" /XObject << /X1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /Font << /F1 %s >> >> /Length %d >>\nstream\n%s\nendstream",
			trueTypeFont("ArialMT", 667, 667), len(form), form),
		"<< /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> /F2 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>"+
			" /F3 << /Type /Font /Subtype /Type1 /BaseFont /Times-Roman >> >>",
	)
}

// directResourcesPDF builds one page showing "AB" with Helvetica written
// inline in the page's direct /Resources and direct /Font dictionary.
func directResourcesPDF() []byte {
	page := "BT /F1 12 Tf 72 700 Td (AB) Tj ET"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1"+
			" << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
	)
}

// inlineProvider supplies a program for every font of [inlineFontsPDF] but
// Times-Roman.
func inlineProvider(_ context.Context, req FontRequest) ([]byte, error) {
	switch req.BaseFont {
	case "Helvetica", "ArialMT":
		return sansProgram(0, 667, 667), nil
	case "Helvetica-Bold":
		return sansProgram(0, 722, 722), nil
	}
	return nil, nil
}

// TestFixFontEmbed_InlineFonts pins a font dictionary written inline in a
// resource dictionary (ISO 32000-1, 7.3.7): it is embedded as an indirect
// font written at a new object number, which the resource dictionary names in
// its place, through the first link of the chain with an object number (the
// /Font dictionary, the page, the form), and one refused is listed with object
// number 0.
func TestFixFontEmbed_InlineFonts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, data := range [][]byte{inlineFontsPDF(), directResourcesPDF()} {
		doc, alloc := newTestDoc(t, data)
		before, _, _ := embeddingFindings(t, doc, models.PDFA2b)
		report, err := RemediatePDFA(ctx, doc, alloc, models.PDFA2b, Options{PreserveContent: true, Fonts: inlineProvider})
		if err != nil {
			t.Fatalf("RemediatePDFA: %v", err)
		}
		var embedded []string
		for _, f := range report.Fixed {
			if strings.HasPrefix(f, "embedded ") {
				embedded = append(embedded, f)
			}
		}
		wantRefused := 0
		if before == 4 {
			wantRefused = 1
		}
		if len(embedded) != before-wantRefused {
			t.Errorf("embedded %v, want %d of the %d unembedded fonts", embedded, before-wantRefused, before)
		}
		if len(report.FontRefusals) != wantRefused {
			t.Fatalf("refusals %v, want %d", report.FontRefusals, wantRefused)
		}
		if wantRefused == 1 {
			r := report.FontRefusals[0]
			if r.BaseFont != "Times-Roman" || r.Object != 0 || !errors.Is(r.Err, ErrFontNotSupplied) {
				t.Errorf("refusal %+v, want Times-Roman written inline, not supplied", r)
			}
		}
		if len(report.FontsEmbedded) != len(embedded) {
			t.Errorf("FontsEmbedded %v, want one per embedding", report.FontsEmbedded)
		}
		for _, f := range report.FontsEmbedded {
			if f.Object != 0 {
				t.Errorf("embedded %+v reports an object number the document did not give it", f)
			}
		}

		var out bytes.Buffer
		if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
			t.Fatalf("FullRewrite: %v", err)
		}
		reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
		if err != nil {
			t.Fatalf("reopening the output: %v", err)
		}
		if embed, presence, width := embeddingFindings(t, reread, models.PDFA2b); embed != wantRefused || presence+width != 0 {
			t.Errorf("output fails embedding %d, presence %d, width %d times, want %d embedding findings", embed, presence, width, wantRefused)
		}
		if _, ok := pageFontEntry(t, reread, "F1").(ast.PdfIndirectRef); !ok {
			t.Errorf("the page's /F1 is still written inline")
		}
		if wantRefused == 1 {
			if _, ok := pageFontEntry(t, reread, "F3").(ast.PdfDict); !ok {
				t.Errorf("the refused /F3 was moved")
			}
		}
	}
}

// TestFixFontEmbed_InlineFontUnwritable pins a font written inline in the
// resources of a Type 3 font that is itself written inline: no object number
// reaches it, so it is refused with [ErrFontUnwritable] rather than skipped.
func TestFixFontEmbed_InlineFontUnwritable(t *testing.T) {
	t.Parallel()
	page := "BT /T3 12 Tf 72 700 Td (A) Tj ET"
	glyph := "1000 0 d0 BT /G1 12 Tf (AB) Tj ET"
	data := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /T3 << /Type /Font"+
			" /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] /CharProcs << /a 5 0 R >>"+
			" /Encoding << /Type /Encoding /Differences [65 /a] >> /FirstChar 65 /LastChar 65 /Widths [1000]"+
			" /Resources << /Font << /G1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> >> >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(glyph), glyph),
	)
	doc, alloc := newTestDoc(t, data)
	if embed, _, _ := embeddingFindings(t, doc, models.PDFA2b); embed != 1 {
		t.Fatalf("precondition: %d embedding findings, want Helvetica's", embed)
	}
	report, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{PreserveContent: true, Fonts: inlineProvider})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if len(report.FontRefusals) != 1 || report.FontRefusals[0].BaseFont != "Helvetica" || !errors.Is(report.FontRefusals[0].Err, ErrFontUnwritable) {
		t.Errorf("refusals %v, want Helvetica's, unwritable", report.FontRefusals)
	}
}

// TestFixFontEmbed_ReportsFontsJudged pins [Report.FontsJudged]: set with a
// provider, even on a document with nothing to repair, and unset without one
// or with font-embed disabled.
func TestFixFontEmbed_ReportsFontsJudged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		name string
		opts Options
		want bool
	}{
		{"provider", Options{PreserveContent: true, Fonts: inlineProvider}, true},
		{"none", Options{PreserveContent: true}, false},
		{"disabled", Options{PreserveContent: true, Fonts: inlineProvider, Disabled: []string{"font-embed"}}, false},
	}
	for _, tc := range cases {
		doc, alloc := newTestDoc(t, directResourcesPDF())
		report, err := RemediatePDFA(ctx, doc, alloc, models.PDFA2b, tc.opts)
		if err != nil {
			t.Fatalf("%s: RemediatePDFA: %v", tc.name, err)
		}
		if report.FontsJudged != tc.want {
			t.Errorf("%s: FontsJudged = %v, want %v", tc.name, report.FontsJudged, tc.want)
		}
	}
}

// pageFontEntry returns the entry name holds in the /Font dictionary of the
// first page's resources, unresolved.
func pageFontEntry(t *testing.T, doc *document.Document, name ast.PdfName) ast.PdfObject {
	t.Helper()
	ctx := context.Background()
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("resolving the catalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("resolving the pages: %v", err)
	}
	res, err := document.ResolveDict(ctx, doc, pages[0].Dict["Resources"])
	if err != nil {
		t.Fatalf("resolving the resources: %v", err)
	}
	fonts, err := document.ResolveDict(ctx, doc, res["Font"])
	if err != nil {
		t.Fatalf("resolving the fonts: %v", err)
	}
	return fonts[name]
}
