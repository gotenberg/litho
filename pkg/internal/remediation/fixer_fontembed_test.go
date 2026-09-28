package remediation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/models"
)

// unembeddedFontsPDF builds one page showing "AB" with each of the fonts, which
// the page's resources name F1, F2 and so on, as objects 5 onwards.
func unembeddedFontsPDF(fonts ...string) []byte {
	return unembeddedFontsPDFWith(fonts)
}

// unembeddedFontsPDFWith is [unembeddedFontsPDF] with extra objects after the
// fonts, which no resource names.
func unembeddedFontsPDFWith(fonts []string, extra ...string) []byte {
	var names, text strings.Builder
	for i := range fonts {
		fmt.Fprintf(&names, " /F%d %d 0 R", i+1, i+5)
		fmt.Fprintf(&text, "/F%d 12 Tf 0 %d Td (AB) Tj ", i+1, 20)
	}
	stream := "BT 72 700 Td " + text.String() + "ET"
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font <<" + names.String() + " >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	return buildStructPDF(append(append(bodies, fonts...), extra...)...)
}

// trueTypeFont returns a non-symbolic TrueType font over WinAnsiEncoding
// declaring widths a and b for "A" and "B", with no program.
func trueTypeFont(baseFont string, a, b int) string {
	return fmt.Sprintf("<< /Type /Font /Subtype /TrueType /BaseFont /%s /Encoding /WinAnsiEncoding /FirstChar 65 /LastChar 66"+
		" /Widths [%d %d] /FontDescriptor << /Type /FontDescriptor /FontName /%s /Flags 32 /FontBBox [0 0 1000 800]"+
		" /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >> >>", baseFont, a, b, baseFont)
}

// sansProgram returns a TrueType program drawing "A" and "B" at the given
// advances, per 1000 units, with a space besides.
func sansProgram(fsType uint16, advances ...uint16) []byte {
	glyphs := []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 278, Empty: true}}
	for i, adv := range advances {
		glyphs = append(glyphs, fontbuild.Glyph{Name: string(rune('A' + i)), Unicode: 'A' + rune(i), Advance: adv})
	}
	return fontbuild.TrueType{PostScriptName: "TestSans", Family: "Test Sans", Subfamily: "Regular", FSType: fsType, Glyphs: glyphs, NotdefAdvance: 500}.Build()
}

// embeddingFindings returns how often doc fails the embedding, glyph presence
// and glyph width rules of profile.
func embeddingFindings(t *testing.T, doc *document.Document, profile models.PDFAProfile) (embed, presence, width int) {
	t.Helper()
	errs, err := compliance.ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	p := string(profile)
	for _, v := range toViolations(errs) {
		switch v.Rule {
		case compliance.FontEmbeddingRule(p):
			embed++
		case compliance.GlyphPresenceRule(p):
			presence++
		case compliance.GlyphWidthRule(p):
			width++
		}
	}
	return embed, presence, width
}

func TestFixFontEmbed_EmbedsSuppliedPrograms(t *testing.T) {
	t.Parallel()
	data := unembeddedFontsPDF(
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		trueTypeFont("ArialMT", 667, 667),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Times-Roman >>",
	)
	var requests []FontRequest
	provider := func(_ context.Context, req FontRequest) ([]byte, error) {
		requests = append(requests, req)
		switch req.BaseFont {
		case "Helvetica", "ArialMT":
			return sansProgram(0, 667, 667), nil
		case "Times-Roman":
			return fontbuild.CFF{FontName: "TestSerif", Glyphs: []fontbuild.Glyph{
				{Name: "A", Advance: 722}, {Name: "B", Advance: 667},
			}}.Build(), nil
		}
		return nil, nil
	}

	for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b} {
		requests = nil
		doc, alloc := newTestDoc(t, data)
		if embed, _, _ := embeddingFindings(t, doc, profile); embed != 3 {
			t.Fatalf("%s precondition: %d embedding findings, want 3", profile, embed)
		}
		report, err := RemediatePDFA(context.Background(), doc, alloc, profile, Options{PreserveContent: true, Fonts: provider})
		if err != nil {
			t.Fatalf("%s: RemediatePDFA: %v", profile, err)
		}
		if len(report.FontRefusals) != 0 {
			t.Errorf("%s: refusals %v", profile, report.FontRefusals)
		}
		embedded := 0
		for _, f := range report.Fixed {
			if strings.HasPrefix(f, "embedded ") {
				embedded++
			}
		}
		if embedded != 3 {
			t.Errorf("%s: %d fonts embedded, want 3: %v", profile, embedded, report.Fixed)
		}
		if len(requests) != 3 || requests[0].Codes[0].Glyph != "A" || requests[0].Codes[0].Width != 667 {
			t.Errorf("%s: requests %+v", profile, requests)
		}
		var out bytes.Buffer
		if err := mutations.FullRewrite(context.Background(), &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
			t.Fatalf("FullRewrite: %v", err)
		}
		reread, err := document.NewDocument(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
		if err != nil {
			t.Fatalf("reopening the output: %v", err)
		}
		if embed, presence, width := embeddingFindings(t, reread, profile); embed+presence+width != 0 {
			t.Errorf("%s: output fails embedding %d, presence %d, width %d times", profile, embed, presence, width)
		}
		helvetica := pageFont(t, reread, "F1")
		if helvetica["Subtype"] != ast.PdfName("TrueType") || helvetica["Widths"] == nil {
			t.Errorf("%s: Helvetica over a TrueType program = %v", profile, helvetica)
		}
	}
}

func TestFixFontEmbed_Refusals(t *testing.T) {
	t.Parallel()
	data := unembeddedFontsPDF(
		trueTypeFont("Narrow", 500, 500),
		trueTypeFont("Partial", 667, 667),
		trueTypeFont("Locked", 667, 667),
		trueTypeFont("Unknown", 667, 667),
		"<< /Type /Font /Subtype /TrueType /BaseFont /Serif /Encoding /WinAnsiEncoding /FirstChar 65 /LastChar 66 /Widths [667 667]"+
			" /FontDescriptor << /Type /FontDescriptor /FontName /Serif /Flags 32 /FontBBox [0 0 1000 800] /ItalicAngle 0"+
			" /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >> >>",
		trueTypeFont("Garbled", 667, 667),
	)
	provider := func(_ context.Context, req FontRequest) ([]byte, error) {
		switch req.BaseFont {
		case "Narrow":
			return sansProgram(0, 667, 667), nil
		case "Partial":
			return sansProgram(0, 667), nil
		case "Locked":
			return sansProgram(0x0002, 667, 667), nil
		case "Serif":
			return fontbuild.CFF{FontName: "TestSerif", Glyphs: []fontbuild.Glyph{{Name: "A", Advance: 667}, {Name: "B", Advance: 667}}}.Build(), nil
		case "Garbled":
			return []byte("not a font"), nil
		}
		return nil, nil
	}
	doc, alloc := newTestDoc(t, data)
	report, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{PreserveContent: true, Fonts: provider})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	want := map[string]error{
		"Narrow":  ErrFontWidths,
		"Partial": ErrFontCoverage,
		"Locked":  ErrFontEmbeddingRestricted,
		"Unknown": ErrFontNotSupplied,
		"Serif":   ErrFontKind,
		"Garbled": ErrFontProgramInvalid,
	}
	if len(report.FontRefusals) != len(want) {
		t.Fatalf("refusals %v, want one per font", report.FontRefusals)
	}
	for _, r := range report.FontRefusals {
		if !errors.Is(r.Err, want[r.BaseFont]) {
			t.Errorf("%s (object %d): refused with %v, want %v", r.BaseFont, r.Object, r.Err, want[r.BaseFont])
		}
	}
	for _, f := range report.Fixed {
		if strings.HasPrefix(f, "embedded ") {
			t.Errorf("a refused font was embedded: %s", f)
		}
	}
}

// TestFixFontEmbed_SharedAndUnreadableDescriptors pins the two descriptors a
// font may not be embedded through as others are: one it shares with a font
// embedded before it, which it takes the same program through, and one holding
// a program validation cannot read, a /FontFile3 naming no /Subtype (ISO
// 32000-1, Table 126), which is left as the file wrote it and reported.
func TestFixFontEmbed_SharedAndUnreadableDescriptors(t *testing.T) {
	t.Parallel()
	const descriptor = "<< /Type /FontDescriptor /FontName /ArialMT /Flags 32 /FontBBox [0 0 1000 800] /ItalicAngle 0" +
		" /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >>"
	sharing := func(name string) string {
		return "<< /Type /Font /Subtype /TrueType /BaseFont /" + name + " /Encoding /WinAnsiEncoding /FirstChar 65 /LastChar 66" +
			" /Widths [667 667] /FontDescriptor 9 0 R >>"
	}
	data := unembeddedFontsPDFWith([]string{
		sharing("ArialMT"),
		sharing("ArialMT"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Opaque /FirstChar 65 /LastChar 66 /Widths [667 667] /FontDescriptor 10 0 R >>",
	},
		"<< /Type /FontDescriptor /FontName /Unused /Flags 32 >>",
		descriptor,
		"<< /Type /FontDescriptor /FontName /Opaque /Flags 32 /FontBBox [0 0 1000 800] /ItalicAngle 0 /Ascent 800 /Descent -200"+
			" /CapHeight 700 /StemV 80 /FontFile3 11 0 R >>",
		"<< /Length 4 >>\nstream\nnone\nendstream",
	)
	provider := func(context.Context, FontRequest) ([]byte, error) { return sansProgram(0, 667, 667), nil }
	doc, alloc := newTestDoc(t, data)
	report, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{PreserveContent: true, Fonts: provider})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if len(report.FontRefusals) != 1 || report.FontRefusals[0].BaseFont != "Opaque" || !errors.Is(report.FontRefusals[0].Err, ErrFontKind) {
		t.Errorf("refusals %v, want Opaque's alone", report.FontRefusals)
	}
	embedded := 0
	for _, f := range report.Fixed {
		if strings.HasPrefix(f, "embedded ") {
			embedded++
		}
	}
	if embedded != 2 {
		t.Errorf("%d fonts embedded, want the two sharing a descriptor: %v", embedded, report.Fixed)
	}
	if _, rewritten := alloc.Pending()[10]; rewritten {
		t.Errorf("the descriptor holding an unreadable program was rewritten")
	}
}

func TestFixFontEmbed_ProviderError(t *testing.T) {
	t.Parallel()
	failure := errors.New("font store unreachable")
	doc, alloc := newTestDoc(t, unembeddedFontsPDF(trueTypeFont("ArialMT", 667, 667)))
	_, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{
		PreserveContent: true,
		Fonts:           func(context.Context, FontRequest) ([]byte, error) { return nil, failure },
	})
	if !errors.Is(err, failure) {
		t.Errorf("RemediatePDFA error = %v, want the provider's", err)
	}
}

func TestFixFontEmbed_NoProviderLeavesFonts(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, unembeddedFontsPDF(trueTypeFont("ArialMT", 667, 667)))
	report, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{PreserveContent: true})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if len(report.FontRefusals) != 0 {
		t.Errorf("refusals without a provider: %v", report.FontRefusals)
	}
	if _, pending := alloc.Pending()[5]; pending {
		t.Errorf("the font was rewritten without a provider")
	}
}

// compositePDF builds a page showing U+3042 through a Type0 font whose
// descendant, object 6, is an unembedded CIDFontType0 of the given collection.
func compositePDF(encoding, ordering string) []byte {
	stream := "BT /F1 12 Tf <3042> Tj ET"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type0 /BaseFont /KozMinPro-Regular /Encoding /"+encoding+" /DescendantFonts [6 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /KozMinPro-Regular /CIDSystemInfo << /Registry (Adobe) /Ordering ("+ordering+
			") /Supplement 2 >> /DW 1000 /FontDescriptor 7 0 R >>",
		"<< /Type /FontDescriptor /FontName /KozMinPro-Regular /Flags 4 /FontBBox [0 -120 1000 880] /ItalicAngle 0"+
			" /Ascent 880 /Descent -120 /CapHeight 740 /StemV 80 >>",
	)
}

func TestFixFontEmbed_CompositeFont(t *testing.T) {
	t.Parallel()
	program := fontbuild.TrueType{
		PostScriptName: "TestMincho", Family: "Test Mincho", Subfamily: "Regular",
		Glyphs: []fontbuild.Glyph{{Name: "uni3042", Unicode: 0x3042, Advance: 1000}}, NotdefAdvance: 1000,
	}.Build()
	var got FontRequest
	provider := func(_ context.Context, req FontRequest) ([]byte, error) {
		got = req
		return program, nil
	}

	doc, alloc := newTestDoc(t, compositePDF("UniJIS-UCS2-H", "Japan1"))
	report, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{PreserveContent: true, Fonts: provider})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if len(report.FontRefusals) != 0 {
		t.Fatalf("refusals %v", report.FontRefusals)
	}
	if got.Ordering != "Adobe-Japan1" || len(got.Codes) != 1 || got.Codes[0].Unicode != 0x3042 {
		t.Errorf("request %+v", got)
	}
	var out bytes.Buffer
	if err := mutations.FullRewrite(context.Background(), &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	reread, err := document.NewDocument(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	if embed, presence, width := embeddingFindings(t, reread, models.PDFA2b); embed+presence+width != 0 {
		t.Errorf("output fails embedding %d, presence %d, width %d times", embed, presence, width)
	}
	descendants, err := document.ResolveArray(context.Background(), reread, pageFont(t, reread, "F1")["DescendantFonts"])
	if err != nil || len(descendants) != 1 {
		t.Fatalf("resolving the descendants: %v", err)
	}
	desc, err := document.ResolveDict(context.Background(), reread, descendants[0])
	if err != nil {
		t.Fatalf("resolving the descendant: %v", err)
	}
	if desc["Subtype"] != ast.PdfName("CIDFontType2") {
		t.Errorf("descendant %v, want a CIDFontType2", desc)
	}
	if _, err := document.ResolveStream(context.Background(), reread, desc["CIDToGIDMap"]); err != nil {
		t.Errorf("descendant has no /CIDToGIDMap stream: %v", err)
	}

	// A descendant written inline in /DescendantFonts moves to an object of
	// its own (ISO 32000-1, Table 121).
	stream := "BT /F1 12 Tf <3042> Tj ET"
	inline := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type0 /BaseFont /KozMinPro-Regular /Encoding /UniJIS-UCS2-H /DescendantFonts [<< /Type /Font"+
			" /Subtype /CIDFontType0 /BaseFont /KozMinPro-Regular /CIDSystemInfo << /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >>"+
			" /DW 1000 /FontDescriptor << /Type /FontDescriptor /FontName /KozMinPro-Regular /Flags 4 /FontBBox [0 -120 1000 880]"+
			" /ItalicAngle 0 /Ascent 880 /Descent -120 /CapHeight 740 /StemV 80 >> >>] >>",
	)
	doc, alloc = newTestDoc(t, inline)
	report, err = RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{PreserveContent: true, Fonts: provider})
	if err != nil {
		t.Fatalf("inline descendant: RemediatePDFA: %v", err)
	}
	if len(report.FontRefusals) != 0 {
		t.Fatalf("inline descendant: refusals %v", report.FontRefusals)
	}
	if font, ok := alloc.Pending()[5].(ast.PdfDict); !ok {
		t.Errorf("inline descendant: the font was not rewritten")
	} else if arr, _ := font["DescendantFonts"].(ast.PdfArray); len(arr) != 1 {
		t.Errorf("inline descendant: /DescendantFonts = %v", font["DescendantFonts"])
	} else if _, isRef := arr[0].(ast.PdfIndirectRef); !isRef {
		t.Errorf("inline descendant: /DescendantFonts holds %T, want a reference", arr[0])
	}

	doc, alloc = newTestDoc(t, compositePDF("Identity-H", "Identity"))
	report, err = RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{PreserveContent: true, Fonts: provider})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if len(report.FontRefusals) != 1 || !errors.Is(report.FontRefusals[0].Err, ErrFontKind) {
		t.Errorf("Identity ordering: refusals %v, want one ErrFontKind", report.FontRefusals)
	}
}

// pageFont returns the font the first page's resources name name.
func pageFont(t *testing.T, doc *document.Document, name ast.PdfName) ast.PdfDict {
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
	font, err := document.ResolveDict(ctx, doc, fonts[name])
	if err != nil {
		t.Fatalf("resolving font %s: %v", name, err)
	}
	return font
}
