package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCheckType3FontWidths_UsageScoped pins that the Type3 /Widths-vs-CharProc
// width check is scoped to rendered codes, matching veraPDF (which resolves and
// checks only shown codes). A subset Type3 font routinely maps every unused code
// to a shared .notdef placeholder (e.g. /g0) with /Widths 0 while that CharProc
// still declares a non-zero d0/d1 advance; comparing those unrendered entries
// fired a spurious §6.2.11.5 / §7.21.5 width mismatch veraPDF passes.
func TestCheckType3FontWidths_UsageScoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	streamObj := func(body string) string {
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body)
	}
	// obj1 catalog, obj2 page, obj3 Type3 font, obj4 /g0 CharProc, obj5 /gA CharProc.
	// code 1 -> /g0 (declared width 0, CharProc d1 wx 1245 => mismatch)
	// code 2 -> /gA (declared width 1000, CharProc d1 wx 1000 => match)
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Type /Page >>",
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 100 100] /FontMatrix [0.001 0 0 0.001 0 0] "+
			"/CharProcs << /g0 4 0 R /gA 5 0 R >> /Encoding << /Differences [1 /g0 /gA] >> "+
			"/FirstChar 1 /LastChar 2 /Widths [0 1000] >>",
		streamObj("1245 0 0 0 100 100 d1"),
		streamObj("1000 0 0 0 100 100 d1"),
	)
	font, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("resolve font: %v", err)
	}
	rule := Rule_19005_2_6_2_11_5_t01

	// Only code 2 is rendered: the unrendered code-1 mismatch must be skipped.
	usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{2: true}}
	if v := checkType3FontWidths(ctx, doc, font, "F1", 1, rule, usage); len(v) != 0 {
		t.Errorf("unrendered code 1 mismatch must not fire, got %v", v)
	}

	// Once code 1 is rendered, the same mismatch fires (rendered codes ARE checked).
	rendered := &fontUsageInfo{visible: true, charCodes: map[byte]bool{1: true, 2: true}}
	if v := checkType3FontWidths(ctx, doc, font, "F1", 1, rule, rendered); len(v) == 0 {
		t.Error("rendered code 1 width mismatch must fire")
	}

	// Nil usage is fail-safe: check every code.
	if v := checkType3FontWidths(ctx, doc, font, "F1", 1, rule, nil); len(v) == 0 {
		t.Error("nil usage must check all codes and fire on code 1")
	}
}
