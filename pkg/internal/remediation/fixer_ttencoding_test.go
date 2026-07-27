package remediation

import (
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// trueTypePDF builds one page carrying a single TrueType font (object 4) whose
// descriptor (object 5) declares flags. The font has no /Encoding, which is the
// state the fixer normalises.
func trueTypePDF(flags int) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /TrueType /BaseFont /Probe /FirstChar 65 /LastChar 66 "+
			"/Widths [600 600] /FontDescriptor 5 0 R >>",
		"<< /Type /FontDescriptor /FontName /Probe /Flags "+strconv.Itoa(flags)+" >>",
	)
}

// A non-symbolic TrueType font with no /Encoding is exactly what the fixer is
// for: ISO 19005-2, 6.2.11.6 t2 requires it to name MacRoman or WinAnsi.
func TestFixTrueTypeEncoding_AddsEncodingToNonSymbolic(t *testing.T) {
	doc, alloc := newTestDoc(t, trueTypePDF(32))

	descs, err := fixTrueTypeEncoding(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixTrueTypeEncoding: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if enc := pendingDict(t, alloc, 4)["Encoding"]; enc != ast.PdfName("WinAnsiEncoding") {
		t.Errorf("non-symbolic /Encoding = %v, want WinAnsiEncoding", enc)
	}
}

// ISO 19005-2, 6.2.11.6 t3 forbids a symbolic TrueType font an /Encoding entry,
// so normalising one onto it would introduce a violation. The fixer previously
// ignored the symbolic flag and did exactly that.
func TestFixTrueTypeEncoding_LeavesSymbolicAlone(t *testing.T) {
	doc, alloc := newTestDoc(t, trueTypePDF(4))

	descs, err := fixTrueTypeEncoding(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixTrueTypeEncoding: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("expected no change for a symbolic font, got %v", descs)
	}
	if _, pending := alloc.Pending()[4]; pending {
		if enc, ok := pendingDict(t, alloc, 4)["Encoding"]; ok {
			t.Errorf("symbolic font gained /Encoding = %v", enc)
		}
	}
}

// A font with no readable descriptor cannot be established as symbolic, so it
// keeps the non-symbolic treatment rather than being skipped silently.
func TestFixTrueTypeEncoding_NoDescriptorIsTreatedNonSymbolic(t *testing.T) {
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /TrueType /BaseFont /Probe /FirstChar 65 /LastChar 66 /Widths [600 600] >>",
	))

	if _, err := fixTrueTypeEncoding(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixTrueTypeEncoding: %v", err)
	}
	if enc := pendingDict(t, alloc, 4)["Encoding"]; enc != ast.PdfName("WinAnsiEncoding") {
		t.Errorf("/Encoding = %v, want WinAnsiEncoding", enc)
	}
}
