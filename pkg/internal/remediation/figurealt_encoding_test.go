package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixFigureAltEncodesTheSuppliedText pins that the alternate text a caller
// supplies reaches the file in an encoding a reader can decode.
//
// /Alt is a text string (ISO 32000-1, 14.9.3), so it takes PDFDocEncoding or
// UTF-16BE with a byte order mark. WithFigureAltText accepts anything, and
// casting the Go string left raw UTF-8 in the file, which a reader decodes one
// byte per character.
func TestFixFigureAltEncodesTheSuppliedText(t *testing.T) {
	t.Parallel()

	pdf := figurePDF(
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixFigureAlt(context.Background(), alloc, doc,
		Options{PreserveContent: true, FigureAlt: "Изображение"}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}

	got, ok := pendingDict(t, alloc, 5)["Alt"].(ast.PdfString)
	if !ok {
		t.Fatalf("/Alt is %T, want a string", pendingDict(t, alloc, 5)["Alt"])
	}
	if len(got) < 2 || got[0] != 0xFE || got[1] != 0xFF {
		t.Errorf("/Alt = % x, want it to start with the UTF-16BE byte order mark", got)
	}
	// The first letter of the Cyrillic word is two bytes in UTF-8.
	if strings.Contains(string(got), string([]byte{0xD0, 0x98})) {
		t.Errorf("/Alt reached the file as raw UTF-8: % x", got)
	}
}

// TestFixFigureAltKeepsASCIIUnchanged is the control: the ordinary case must
// produce exactly the bytes it always did.
func TestFixFigureAltKeepsASCIIUnchanged(t *testing.T) {
	t.Parallel()

	pdf := figurePDF(
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixFigureAlt(context.Background(), alloc, doc,
		Options{PreserveContent: true, FigureAlt: "A quarterly revenue bar chart"}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if got := pendingDict(t, alloc, 5)["Alt"]; got != ast.PdfString("A quarterly revenue bar chart") {
		t.Errorf("/Alt = %v, want the supplied string unchanged", got)
	}
}
