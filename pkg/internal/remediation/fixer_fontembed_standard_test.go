package remediation

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/models"
)

// pageText extracts the text of the first page of doc.
func pageText(t *testing.T, doc *document.Document) string {
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
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("parsing the content: %v", err)
	}
	text, err := typography.ExtractText(ctx, doc, graphics.NewMachine([4]float64{0, 0, 612, 792}), pages[0], ops, typography.ExtractionConfig{})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	return strings.TrimSpace(text)
}

// TestFixFontEmbed_TrueTypeWithoutEncodingKeepsStandardGlyphs embeds a program
// into a non-embedded non-symbolic TrueType font with no /Encoding, whose
// codes name StandardEncoding glyphs while the program is absent. The codes
// 0x27, 0x60, 0xF1 and 0xF9 name quoteright, quoteleft, ae and oslash there,
// and quotesingle, grave, ntilde and ugrave under WinAnsiEncoding, so the
// embedded font must carry an /Encoding that keeps the StandardEncoding
// names over a base ISO 19005-2, 6.2.11.6 admits.
func TestFixFontEmbed_TrueTypeWithoutEncodingKeepsStandardGlyphs(t *testing.T) {
	t.Parallel()
	shown := []byte{'A', 0x27, 0x60, 0xF1, 0xF9}
	stream := fmt.Sprintf("BT /F1 12 Tf 72 700 Td <%X> Tj ET", shown)
	font := "<< /Type /Font /Subtype /TrueType /BaseFont /ArialMT /FirstChar 39 /LastChar 249" +
		" /Widths [" + widthsFor(39, 249, map[int]int{39: 222, 65: 667, 96: 222, 241: 889, 249: 611}) + "]" +
		" /FontDescriptor << /Type /FontDescriptor /FontName /ArialMT /Flags 32 /FontBBox [0 0 1000 800]" +
		" /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >> >>"
	data := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		font,
	)
	program := fontbuild.TrueType{PostScriptName: "TestSans", Family: "Test Sans", Subfamily: "Regular", Glyphs: []fontbuild.Glyph{
		{Name: "space", Unicode: ' ', Advance: 278, Empty: true},
		{Name: "A", Unicode: 'A', Advance: 667},
		{Name: "quoteright", Unicode: '’', Advance: 222},
		{Name: "quoteleft", Unicode: '‘', Advance: 222},
		{Name: "ae", Unicode: 'æ', Advance: 889},
		{Name: "oslash", Unicode: 'ø', Advance: 611},
		{Name: "quotesingle", Unicode: '\'', Advance: 191},
		{Name: "grave", Unicode: '`', Advance: 333},
		{Name: "ntilde", Unicode: 'ñ', Advance: 556},
		{Name: "ugrave", Unicode: 'ù', Advance: 556},
	}, NotdefAdvance: 500}.Build()
	provider := func(_ context.Context, _ FontRequest) ([]byte, error) { return program, nil }

	doc, alloc := newTestDoc(t, data)
	before := pageText(t, doc)
	if before != "A’‘æø" {
		t.Fatalf("precondition: source text %q, want %q", before, "A’‘æø")
	}
	report, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{PreserveContent: true, Fonts: oneProgram(provider)})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if len(report.FontRefusals) != 0 {
		t.Fatalf("refusals %v", report.FontRefusals)
	}
	var out bytes.Buffer
	if err := mutations.FullRewrite(context.Background(), &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	reread, err := document.NewDocument(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	if after := pageText(t, reread); after != before {
		t.Errorf("text after embedding %q, want the source's %q", after, before)
	}
	if embed, presence, width := embeddingFindings(t, reread, models.PDFA2b); embed+presence+width != 0 {
		t.Errorf("output fails embedding %d, presence %d, width %d times", embed, presence, width)
	}
	enc, err := document.ResolveDict(context.Background(), reread, pageFont(t, reread, "F1")["Encoding"])
	if err != nil || enc["BaseEncoding"] != ast.PdfName("WinAnsiEncoding") {
		t.Fatalf("/Encoding = %v (%v), want a dictionary over WinAnsiEncoding", enc, err)
	}
	want := fmt.Sprint(ast.PdfArray{ast.PdfInt(39), ast.PdfName("quoteright"), ast.PdfInt(96), ast.PdfName("quoteleft"), ast.PdfInt(241), ast.PdfName("ae"), ast.PdfInt(249), ast.PdfName("oslash")})
	if got := fmt.Sprint(enc["Differences"]); got != want {
		t.Errorf("/Differences = %s, want %s", got, want)
	}
}

// widthsFor returns a /Widths body from first to last, 0 for every code w does
// not name.
func widthsFor(first, last int, w map[int]int) string {
	var b strings.Builder
	for code := first; code <= last; code++ {
		fmt.Fprintf(&b, "%d ", w[code])
	}
	return strings.TrimSpace(b.String())
}
