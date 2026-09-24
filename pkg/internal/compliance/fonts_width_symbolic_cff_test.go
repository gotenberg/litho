package compliance

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestSimpleFontGlyphMetrics_SymbolicCFFResolvesByName pins that a symbolic
// Type 1 or CFF font with no PDF /Encoding still resolves its codes.
//
// The symbolic short-circuit exists for TrueType, whose codes go through the
// embedded (3,0)/(1,0) cmap (ISO 32000-1 §9.6.6.4) and are unresolvable when it
// is absent. A CFF program has no cmap to be missing: its codes resolve through
// the program's own encoding (ISO 32000-1 §9.6.6.2). Applying the sfnt
// short-circuit to it returned an empty mapping, so such a font went unchecked.
//
// # Corpus evidence
//
//   - govdocs thread_300_300492.pdf: every TeX Computer Modern subset went
//     unchecked, and litho reported none of the 22 §6.2.11.5 width mismatches
//     veraPDF and fontTools both compute.
func TestSimpleFontGlyphMetrics_SymbolicCFFResolvesByName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cff, err := os.ReadFile("testdata/lato_bold_subset.cff")
	if err != nil {
		t.Fatalf("read CFF fixture: %v", err)
	}

	// Flags 4 marks the font symbolic and the dictionary declares no /Encoding,
	// which is the shape the short-circuit used to swallow.
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /TEEUUH+Lato-Bold /FirstChar 160 /LastChar 160"+
			" /Widths [243] /FontDescriptor 5 0 R >>",
		"<< /Type /FontDescriptor /FontName /TEEUUH+Lato-Bold /Flags 4 /FontFile3 6 0 R >>",
		fmt.Sprintf("<< /Length %d /Subtype /Type1C >>\nstream\n%s\nendstream", len(cff), string(cff)),
	)

	fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolve font dict: %v", err)
	}

	binaryWidths, unitsPerEm, charCodeToGID, ok := simpleFontGlyphMetrics(ctx, doc, fontDict)
	if !ok {
		t.Fatal("simpleFontGlyphMetrics: not ok")
	}
	gid, mapped := charCodeToGID[160]
	if !mapped || gid == 0 {
		t.Fatalf("code 160 resolved to GID %d (mapped=%v); the program's own encoding names it \"space\"", gid, mapped)
	}
	if got := binaryWidths[gid] * 1000.0 / float64(unitsPerEm); got != 243 {
		t.Errorf("space program advance = %g, want 243", got)
	}
}
