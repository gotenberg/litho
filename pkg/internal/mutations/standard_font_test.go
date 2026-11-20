package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// twoFieldDoc builds a one-page document with two text fields whose /DA names
// /Helv and whose form has no /DR.
func twoFieldDoc(t *testing.T) (context.Context, *document.Document, *writer.ObjAllocator) {
	t.Helper()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 6 0 R] /DA (/Helv 9 Tf 0 g) >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R 6 0 R] /Contents 5 0 R >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (a) /V (First) /Rect [10 10 130 30] /P 3 0 R >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (b) /V (Second) /Rect [10 40 130 60] /P 3 0 R >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return ctx, doc, writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
}

// pendingType1Fonts returns every Type 1 font dictionary alloc holds.
func pendingType1Fonts(alloc *writer.ObjAllocator) []ast.PdfDict {
	var fonts []ast.PdfDict
	for _, obj := range alloc.Pending() {
		if d, ok := obj.(ast.PdfDict); ok && d["Type"] == ast.PdfName("Font") && d["Subtype"] == ast.PdfName("Type1") {
			fonts = append(fonts, d)
		}
	}
	return fonts
}

// checkCompleteStandardFont fails unless font states /FirstChar, /LastChar,
// /Widths and a /FontDescriptor that alloc holds, which ISO 32000-2, 9.6.2.2
// requires of a standard font and ISO 32000-1, 9.6.2.2 expects from PDF 1.5
// on.
func checkCompleteStandardFont(t *testing.T, alloc *writer.ObjAllocator, font ast.PdfDict) {
	t.Helper()
	for _, key := range []ast.PdfName{"FirstChar", "LastChar", "Widths"} {
		if _, ok := font[key]; !ok {
			t.Errorf("%v lacks %v", font["BaseFont"], key)
		}
	}
	ref, ok := font["FontDescriptor"].(ast.PdfIndirectRef)
	if !ok {
		t.Errorf("%v: /FontDescriptor %v is no indirect reference", font["BaseFont"], font["FontDescriptor"])
		return
	}
	if fd, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict); !ok || fd["Type"] != ast.PdfName("FontDescriptor") {
		t.Errorf("%v: descriptor %d is %v", font["BaseFont"], ref.ObjectNum, alloc.Pending()[ref.ObjectNum])
	}
}

// TestStampTextWritesACompleteStandardFont pins that a text stamp in a
// standard font writes the font with its widths and descriptor, for a font of
// WinAnsiEncoding and for one of a built-in encoding.
func TestStampTextWritesACompleteStandardFont(t *testing.T) {
	t.Parallel()

	for _, name := range []ast.PdfName{"Helvetica", "Times-Italic", "ZapfDingbats"} {
		ctx, doc, pages, alloc := stampDoc(t)
		if err := StampText(ctx, alloc, doc, pages, TextStampOptions{Text: "Stamp", FontSize: 12, StandardFont: name}, nil); err != nil {
			t.Fatalf("%s: StampText: %v", name, err)
		}
		fonts := pendingType1Fonts(alloc)
		if len(fonts) != 1 || fonts[0]["BaseFont"] != name {
			t.Fatalf("%s: the stamp wrote %d Type 1 fonts: %v", name, len(fonts), fonts)
		}
		checkCompleteStandardFont(t, alloc, fonts[0])
	}
}

// TestFlattenWritesOneCompleteFallbackFont pins that a flatten drawing fields
// whose /DA font the form's /DR does not hold writes one standard Helvetica
// for all of them, with its widths and descriptor.
func TestFlattenWritesOneCompleteFallbackFont(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := twoFieldDoc(t)
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}
	_, drawn := flattenedPage(t, alloc)
	if len(drawn) != 2 {
		t.Fatalf("the flatten drew %d appearances, want 2", len(drawn))
	}
	fonts := pendingType1Fonts(alloc)
	if len(fonts) != 1 {
		t.Fatalf("the flatten wrote %d Type 1 fonts, want 1", len(fonts))
	}
	if fonts[0]["BaseFont"] != ast.PdfName("Helvetica") {
		t.Errorf("fallback font = %v", fonts[0]["BaseFont"])
	}
	checkCompleteStandardFont(t, alloc, fonts[0])
}
