package remediation

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// notdefTTF is the minimal CIDFontType2 program the compliance package already
// keeps for the .notdef width check. It is read across packages rather than
// duplicated so both sides measure the same 2.5 KB of font.
//
// Its glyph 0 advance is 908 at 2048 units per em, which is 443.36 in the
// 1000-unit space a PDF width uses.
const (
	notdefTTFPath     = "../compliance/testdata/cidfont_notdef_min.ttf"
	notdefScaledWidth = 443.36
)

// cidFontDWPDF builds a Type0 font over that program whose descendant CIDFont
// declares the given /DW (omitted when empty).
func cidFontDWPDF(t *testing.T, dw string) []byte {
	t.Helper()

	ttf, err := os.ReadFile(notdefTTFPath)
	if err != nil {
		t.Fatalf("reading the font fixture: %v", err)
	}
	if dw != "" {
		dw = " /DW " + dw
	}

	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /X /Encoding /Identity-H /DescendantFonts [5 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /X /CIDToGIDMap /Identity" + dw +
			" /W [1 [500]] /FontDescriptor 6 0 R" +
			" /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
		"<< /Type /FontDescriptor /FontName /X /Flags 4 /FontFile2 7 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(ttf), string(ttf)),
	})
}

// repairedCIDFont runs the Type0 font repair and returns the rewritten CIDFont.
func repairedCIDFont(t *testing.T, data []byte) (ast.PdfDict, []string) {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolving the Type0 font: %v", err)
	}
	fixes := fixType0Font(ctx, doc, alloc, fontDict, ast.PdfIndirectRef{ObjectNum: 4}, true, false, false)
	cid, _ := alloc.Pending()[5].(ast.PdfDict)
	return cid, fixes
}

// TestFixType0FontCorrectsTheDefaultWidth pins that /DW is brought into
// agreement with the font program.
//
// /DW is the width every CID outside /W is drawn with, which is exactly the set
// a viewer renders as .notdef, so ISO 19005-2, 6.2.11.5 holds it to the
// program's glyph 0 advance. The repair only ever rewrote /W, so a .notdef
// width mismatch was reported with "Fixed: 0" on every pass.
//
// # Corpus evidence
//
//   - verapdf 6-2-11-4-1-t02-fail-e: reported ".notdef glyph width mismatch
//     (declared=1000.0, font=545.4)" with "Fixed: 0" on every pass.
func TestFixType0FontCorrectsTheDefaultWidth(t *testing.T) {
	t.Parallel()

	cid, fixes := repairedCIDFont(t, cidFontDWPDF(t, "1000"))
	if cid == nil {
		t.Fatalf("the CIDFont was not rewritten, fixes=%v", fixes)
	}
	got := document.Float(cid["DW"])
	if math.Abs(got-notdefScaledWidth) > 0.01 {
		t.Errorf("/DW = %v, want %v (the program's glyph 0 advance)", got, notdefScaledWidth)
	}
}

// TestFixType0FontLeavesAnAgreeingDefaultWidth is the control: a /DW that
// already matches the program is no reason to rewrite the font.
func TestFixType0FontLeavesAnAgreeingDefaultWidth(t *testing.T) {
	t.Parallel()

	cid, _ := repairedCIDFont(t, cidFontDWPDF(t, "443.36"))
	if cid != nil {
		if got := document.Float(cid["DW"]); math.Abs(got-notdefScaledWidth) > 0.01 {
			t.Errorf("/DW = %v, want it left at %v", got, notdefScaledWidth)
		}
	}
}

// TestFixType0FontCorrectsAnAbsentDefaultWidth pins the same repair when the
// CIDFont declares no /DW at all: ISO 32000-1, Table 117 makes that 1000, which
// disagrees with this program just as an explicit 1000 does.
func TestFixType0FontCorrectsAnAbsentDefaultWidth(t *testing.T) {
	t.Parallel()

	cid, fixes := repairedCIDFont(t, cidFontDWPDF(t, ""))
	if cid == nil {
		t.Fatalf("the CIDFont was not rewritten, fixes=%v", fixes)
	}
	if got := document.Float(cid["DW"]); math.Abs(got-notdefScaledWidth) > 0.01 {
		t.Errorf("/DW = %v, want %v", got, notdefScaledWidth)
	}
}
