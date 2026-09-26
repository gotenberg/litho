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

// TestType3WidthsAsVeraPDFReadsThem pins the two widths [checkType3FontWidths]
// compares for a shown code: the /Widths entry, or the /MissingWidth outside
// /FirstChar to /LastChar, against the wx a glyph procedure opens with, or the
// /MissingWidth where the code has no procedure or the procedure does not open
// with d0 or d1. Both are compared in glyph space, whatever the /FontMatrix.
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b. The
// font's encoding names codes 65 and 66 /a and /b, and only /a has a procedure,
// so a code other than 65 also fails the presence rule.
func TestType3WidthsAsVeraPDFReadsThem(t *testing.T) {
	t.Parallel()

	const std = "[0.001 0 0 0.001 0 0]"
	font := func(matrix, differences, widths, extra, proc string) []string {
		return []string{
			"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix " + matrix +
				" /CharProcs << /a 6 0 R >> /Encoding << /Type /Encoding /Differences " + differences + " >>" +
				" /FirstChar 65 /LastChar 67 /Widths " + widths + extra + " >>",
			exponentStream("", proc),
			"<< /Type /FontDescriptor /FontName /T3 /Flags 4 /FontBBox [0 0 1000 1000] /ItalicAngle 0 /Ascent 700" +
				" /Descent -200 /CapHeight 700 /StemV 80 /MissingWidth 500 >>",
		}
	}
	undecodable := func(widths string) []string {
		f := font(std, "[65 /a]", widths, "", "")
		f[1] = exponentStream("/Filter /FlateDecode", "not a zlib stream at all")
		return f
	}
	for _, tc := range []struct {
		name  string
		font  []string
		shows string
		want  glyphRuleCounts
	}{
		{"a procedure at its width", font(std, "[65 /a /b]", "[500 500 500]", "", "500 0 d0"), "(A) Tj", glyphRuleCounts{0, 0, -1}},
		{"a procedure that does not decode, /Widths 500", undecodable("[500 500 500]"), "(A) Tj", glyphRuleCounts{0, 1, -1}},
		{"a procedure that does not decode, /Widths 0", undecodable("[0 500 500]"), "(A) Tj", glyphRuleCounts{0, 0, -1}},
		{"a procedure off its width", font(std, "[65 /a /b]", "[502 500 500]", "", "500 0 d0"), "(A) Tj", glyphRuleCounts{0, 1, -1}},
		{"a d1 procedure off its width", font(std, "[65 /a /b]", "[400 500 500]", "", "500 0 0 0 100 100 d1"), "(A) Tj", glyphRuleCounts{0, 1, -1}},
		{"no procedure, /Widths 500", font(std, "[65 /a /b]", "[500 500 0]", "", "500 0 d0"), "(B) Tj", glyphRuleCounts{1, 1, -1}},
		{"no procedure, /Widths 2", font(std, "[65 /a /b]", "[500 2 0]", "", "500 0 d0"), "(B) Tj", glyphRuleCounts{1, 1, -1}},
		{"no procedure, /Widths 1", font(std, "[65 /a /b]", "[500 1 0]", "", "500 0 d0"), "(B) Tj", glyphRuleCounts{1, 0, -1}},
		{"no procedure, /Widths 0", font(std, "[65 /a /b]", "[500 0 0]", "", "500 0 d0"), "(B) Tj", glyphRuleCounts{1, 0, -1}},
		{"no procedure, /MissingWidth 500", font(std, "[65 /a /b]", "[500 500 0]", " /FontDescriptor 7 0 R", "500 0 d0"), "(B) Tj", glyphRuleCounts{1, 0, -1}},
		{"a code the encoding does not name", font(std, "[65 /a /b]", "[500 500 500]", "", "500 0 d0"), "(C) Tj", glyphRuleCounts{1, 1, -1}},
		{"no procedure, past /LastChar", font(std, "[65 /a /b]", "[500 500 500]", "", "500 0 d0"), "(D) Tj", glyphRuleCounts{1, 0, -1}},
		{"a procedure past /LastChar", font(std, "[65 /a /b 68 /a]", "[500 500 500]", "", "500 0 d0"), "(D) Tj", glyphRuleCounts{0, 1, -1}},
		{"a zero-width procedure past /LastChar", font(std, "[65 /a /b 68 /a]", "[500 500 500]", "", "0 0 d0"), "(D) Tj", glyphRuleCounts{0, 0, -1}},
		{"a procedure past a short /Widths", font(std, "[65 /a 66 /a]", "[500]", "", "500 0 d0"), "(B) Tj", glyphRuleCounts{0, 1, -1}},
		{"a procedure opening with q, /Widths 500", font(std, "[65 /a]", "[500 500 500]", "", "q Q 500 0 d0"), "(A) Tj", glyphRuleCounts{0, 1, -1}},
		{"a procedure opening with q, /Widths 0", font(std, "[65 /a]", "[0 500 500]", "", "q Q 500 0 d0"), "(A) Tj", glyphRuleCounts{0, 0, -1}},
		{"a matrix of 0.01, half a unit apart", font("[0.01 0 0 0.01 0 0]", "[65 /a /b]", "[50.5 0 0]", "", "50 0 d0"), "(A) Tj", glyphRuleCounts{0, 0, -1}},
		{"a matrix of 0.01, two units apart", font("[0.01 0 0 0.01 0 0]", "[65 /a /b]", "[52 0 0]", "", "50 0 d0"), "(A) Tj", glyphRuleCounts{0, 1, -1}},
		{"a matrix of 0.01, no procedure, /Widths 0.5", font("[0.01 0 0 0.01 0 0]", "[65 /a /b]", "[50 0.5 0]", "", "50 0 d0"), "(B) Tj", glyphRuleCounts{1, 0, -1}},
		{"a matrix of 0.01, no procedure, /Widths 2", font("[0.01 0 0 0.01 0 0]", "[65 /a /b]", "[50 2 0]", "", "50 0 d0"), "(B) Tj", glyphRuleCounts{1, 1, -1}},
		{"no procedure, only in rendering mode 3", font(std, "[65 /a /b]", "[500 500 500]", "", "500 0 d0"), "3 Tr (B) Tj", glyphRuleCounts{0, 0, -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkGlyphRuleCounts(t, glyphPagesObjs(tc.font, glyphText(tc.shows)), tc.want)
		})
	}
}
