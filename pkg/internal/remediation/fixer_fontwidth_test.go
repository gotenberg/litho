package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestMatchFontWidth(t *testing.T) {
	t.Parallel()
	hit := compliance.Violation{
		Rule:    compliance.Rule_19005_2_6_2_11_5_t01,
		Context: "font ABC on page 1: char 65 width mismatch (PDF=500.0, font=600.0)",
	}
	if !matchFontWidth(hit) {
		t.Error("must match a glyph-width-mismatch violation")
	}
	if matchFontWidth(compliance.Violation{Rule: compliance.Rule_19005_2_6_2_11_5_t01, Context: "font ABC: missing /Widths array"}) {
		t.Error("must not match the missing-/Widths context (a different repair)")
	}
}

// TestFixFontWidth_SkipsSimpleFont confirms simple fonts (/Widths) are NOT
// width-repaired: their char-code to glyph mapping is unreliable on real fonts
// (it produces spurious width "mismatches" for non-standard codes), so rewriting
// /Widths from it would corrupt correct widths. Only CIDFontType2+Identity, where
// CID==GID and no mapping is involved, is repaired.
func TestFixFontWidth_SkipsSimpleFont(t *testing.T) {
	pdf := fontPDF("<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Helvetica /FirstChar 65 /Widths [500 600] >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFontWidth(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixFontWidth: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a simple font must not be width-repaired: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixFontWidth_NoopNonSimpleFont(t *testing.T) {
	pdf := fontPDF("<< /Type /Font /Subtype /Type0 /BaseFont /ABCDEF+Sans /Encoding /Identity-H >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFontWidth(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixFontWidth: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a Type0 font is not a simple-font width target: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixFontWidth_SkipsCIDFontType0(t *testing.T) {
	// CIDFontType0 (CFF) has a non-identity CID→GID mapping, so the CID==GID
	// width computation does not apply and the font must be left untouched.
	pdf := fontPDF("<< /Type /Font /Subtype /CIDFontType0 /BaseFont /ABCDEF+Sans /W [3 [600]] >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFontWidth(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixFontWidth: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("CIDFontType0 must be skipped: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

// TestPatchCIDWidthArray exercises the /W surgery directly with synthetic arrays
// (direct elements need no document), covering the part most prone to off-by-one:
// individual entries are patched per glyph, range entries are preserved verbatim.
func TestPatchCIDWidthArray(t *testing.T) {
	t.Parallel()
	// [3 [600 700]] then range [10 12 1000]. Expected: cid3=600 (already right),
	// cid4=500 (700 is wrong → fix); the range is preserved untouched.
	wArr := ast.PdfArray{
		ast.PdfInt(3), ast.PdfArray{ast.PdfInt(600), ast.PdfInt(700)},
		ast.PdfInt(10), ast.PdfInt(12), ast.PdfInt(1000),
	}
	expected := map[int]int{3: 600, 4: 500, 10: 900}

	out, changed := patchCIDWidthArray(context.Background(), nil, wArr, expected)
	if !changed {
		t.Fatal("expected a change (cid 4 width 700 -> 500)")
	}
	if len(out) != 5 {
		t.Fatalf("array length changed: got %d, want 5: %v", len(out), out)
	}
	sub, ok := out[1].(ast.PdfArray)
	if !ok || len(sub) != 2 || sub[0] != ast.PdfInt(600) || sub[1] != ast.PdfInt(500) {
		t.Errorf("individual entry: got %v, want [600 500]", out[1])
	}
	if out[2] != ast.PdfInt(10) || out[3] != ast.PdfInt(12) || out[4] != ast.PdfInt(1000) {
		t.Errorf("range entry must be preserved: got %v", out[2:])
	}
}

func TestPatchCIDWidthArray_NoChangeWhenCorrect(t *testing.T) {
	t.Parallel()
	wArr := ast.PdfArray{ast.PdfInt(3), ast.PdfArray{ast.PdfInt(600), ast.PdfInt(500)}}
	expected := map[int]int{3: 600, 4: 500}

	_, changed := patchCIDWidthArray(context.Background(), nil, wArr, expected)
	if changed {
		t.Error("widths already correct: expected no change")
	}
}

func TestPatchCIDWidthArray_UnknownCidKept(t *testing.T) {
	t.Parallel()
	// cid 4 has no entry in expected (glyph absent from program): keep its width.
	wArr := ast.PdfArray{ast.PdfInt(3), ast.PdfArray{ast.PdfInt(600), ast.PdfInt(700)}}
	expected := map[int]int{3: 600}

	out, changed := patchCIDWidthArray(context.Background(), nil, wArr, expected)
	if changed {
		t.Error("only computable widths change: expected no change")
	}
	sub := out[1].(ast.PdfArray)
	if sub[1] != ast.PdfInt(700) {
		t.Errorf("unknown-cid width must be kept: got %v", sub[1])
	}
}
