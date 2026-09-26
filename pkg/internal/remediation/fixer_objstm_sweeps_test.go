package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixTrueTypeEncoding_FontInsideAnObjectStream pins that a TrueType font
// stored in an object stream (ISO 32000-1, 7.5.7) gets its encoding repaired.
// Its entry is type 2, which the sweep skipped.
func TestFixTrueTypeEncoding_FontInsideAnObjectStream(t *testing.T) {
	t.Parallel()

	_, doc, alloc := objStmDoc(t, "<< /Type /Font /Subtype /TrueType /BaseFont /Probe /Encoding /Bogus >>")
	fixes, err := fixTrueTypeEncoding(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixTrueTypeEncoding: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("the font inside the object stream must be repaired, got %v", fixes)
	}
	if enc := pendingDict(t, alloc, 5)["Encoding"]; enc != ast.PdfName("WinAnsiEncoding") {
		t.Errorf("/Encoding is %v, want /WinAnsiEncoding", enc)
	}
}

// TestFixFileSpecFUF_SpecInsideAnObjectStream pins that an embedded-file
// specification stored in an object stream gets its missing /UF filled in.
func TestFixFileSpecFUF_SpecInsideAnObjectStream(t *testing.T) {
	t.Parallel()

	_, doc, alloc := objStmDoc(t, "<< /Type /Filespec /F (a.txt) /EF << /F 3 0 R >> >>")
	if _, err := fixFileSpecFUF(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixFileSpecFUF: %v", err)
	}
	if uf := pendingDict(t, alloc, 5)["UF"]; uf != ast.PdfString("a.txt") {
		t.Errorf("/UF is %v, want (a.txt)", uf)
	}
}

// TestStripEFFromFileSpecs_SpecInsideAnObjectStream pins that ISO 19005-1,
// 6.1.11 t1 reaches a file specification stored in an object stream.
func TestStripEFFromFileSpecs_SpecInsideAnObjectStream(t *testing.T) {
	t.Parallel()

	_, doc, alloc := objStmDoc(t, "<< /Type /Filespec /F (a.txt) /EF << /F 3 0 R >> >>")
	if n := stripEFFromFileSpecs(context.Background(), alloc, doc); n != 1 {
		t.Fatalf("stripped %d specifications, want 1", n)
	}
	if _, has := pendingDict(t, alloc, 5)["EF"]; has {
		t.Error("the specification still carries /EF")
	}
}
