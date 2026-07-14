package compliance

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestSimpleFontGlyphMetrics_CFFBuiltinEncodingFallback guards the WinAnsi
// NO-BREAK SPACE case. A Type1C (CFF) subset names its 0xA0 slot "space" and
// maps code 160 to it through the program's built-in encoding, but reverse
// Adobe Glyph List resolution of WinAnsi's U+00A0 yields the synthetic
// "uni00A0", which the charset does not hold. Without the built-in-encoding
// fallback the code resolves to GID 0 (.notdef) and misreports both a glyph
// width mismatch (§6.2.11.5) and an absent glyph (§6.2.11.4.1); veraPDF, which
// honours the program's own encoding, reports neither. The extracted fixture
// is Lato-Bold with FontMatrix 0.0005 (2000 units/em): its "space" glyph is
// 486 program units → 486×1000/2000 = 243, matching the declared /Widths[160].
func TestSimpleFontGlyphMetrics_CFFBuiltinEncodingFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cff, err := os.ReadFile("testdata/lato_bold_subset.cff")
	if err != nil {
		t.Fatalf("read CFF fixture: %v", err)
	}

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /T1_0 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /TEEUUH+Lato-Bold /FirstChar 160 /LastChar 160 /Widths [243] /Encoding /WinAnsiEncoding /FontDescriptor 5 0 R >>",
		"<< /Type /FontDescriptor /FontName /TEEUUH+Lato-Bold /Flags 34 /FontFile3 6 0 R >>",
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
		t.Fatalf("code 160 resolved to GID %d (mapped=%v); want the built-in-encoding \"space\" glyph, not .notdef", gid, mapped)
	}
	if got := float64(binaryWidths[gid]) * 1000.0 / float64(unitsPerEm); got != 243 {
		t.Errorf("space program advance = %.1f, want 243 (matching declared /Widths[160])", got)
	}

	usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{160: true}}
	if v := checkSimpleFontWidths(ctx, doc, fontDict, binaryWidths, unitsPerEm, "T1_0", 8, Rule_19005_2_6_2_11_5_t01, charCodeToGID, usage); len(v) != 0 {
		t.Errorf("width check fired on correctly-resolved nbsp: %v", v)
	}
	if v := checkSimpleGlyphPresence(ctx, doc, fontDict, "T1_0", 8, "PDF/A-2b", usage); len(v) != 0 {
		t.Errorf("glyph-presence check fired on correctly-resolved nbsp: %v", v)
	}
}
