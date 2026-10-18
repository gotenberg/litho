package typography

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// koreaUseCMapToUnicode is the shape of pdfjs issue5010.pdf's /ToUnicode: it
// builds on Adobe-Korea1-UCS2 through usecmap and maps a few codes itself,
// here <0D18>, which the base maps to 홍, so the program's own value shows.
const koreaUseCMapToUnicode = `/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/Adobe-Korea1-UCS2 usecmap
/CMapName /Unidocs-Korea1-002 def
/CMapType 2 def
1 begincodespacerange
<0000> <ffff>
endcodespacerange
1 beginbfchar
<0D18> <0041>
endbfchar
endcmap
CMapName currentdict /CMap defineresource pop
end
end`

// TestResolveFont_ToUnicodeUseCMapReadsTheBase pins that a /ToUnicode CMap
// running usecmap holds the named CMap's mappings (ISO 32000-1, 9.7.5.4), as
// PDF.js reads it: the codes pdfjs issue5010.pdf shows under Identity-H read
// as Korean through Adobe-Korea1-UCS2, where litho read each code as its own
// value, and a code the program maps itself keeps the program's value.
func TestResolveFont_ToUnicodeUseCMapReadsTheBase(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
	buf.WriteString("5 0 obj\n<< /Type /Font /Subtype /CIDFontType0 /BaseFont /UNIDOCS " +
		"/CIDSystemInfo << /Registry (Unidocs) /Ordering (Korea1) /Supplement 2 >> /DW 1000 >>\nendobj\n")
	fmt.Fprintf(&buf, "7 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n",
		len(koreaUseCMapToUnicode)+1, koreaUseCMapToUnicode)
	buf.WriteString("trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")
	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	font, err := ResolveFont(ctx, doc, ast.PdfDict{
		"Type":            ast.PdfName("Font"),
		"Subtype":         ast.PdfName("Type0"),
		"BaseFont":        ast.PdfName("UNIDOCS-Identity-H"),
		"Encoding":        ast.PdfName("Identity-H"),
		"ToUnicode":       ast.PdfIndirectRef{ObjectNum: 7},
		"DescendantFonts": ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 5}},
	})
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}

	shown := []byte("\x0a\x4b\x0c\x20\x08\x2d\x00\xa0\x0d\x18")
	var got string
	for len(shown) > 0 {
		char, n, _ := decodeNextChar(font, shown)
		got += char
		shown = shown[n:]
	}
	if want := "인터뷰●A"; got != want {
		t.Errorf("decoded %q, want %q", got, want)
	}
}
