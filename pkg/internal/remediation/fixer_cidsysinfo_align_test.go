package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// cidFontPDF builds a Type 0 font whose /Encoding is an embedded CMap stream,
// so the encoding cannot be relabelled /Identity-H and the CIDSystemInfo has to
// be brought into agreement instead.
//
// Object 5 is the font, 6 the CMap stream, 7 the CIDFont. cidSysInfo and
// cmapSysInfo are the two dictionaries under test; dictWMode is what the CMap
// dictionary claims and programWMode what its body declares.
func cidFontPDF(cidSysInfo, cmapSysInfo string, dictWMode, programWMode int) []byte {
	program := fmt.Sprintf("%%!PS-Adobe-3.0 Resource-CMap\n/WMode %d def\n/CMapName /Test-H def\n", programWMode)

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /Font << /C0_0 5 0 R >> >> >>",
		"<< /Type /Nothing >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding 6 0 R /DescendantFonts [7 0 R] >>",
		fmt.Sprintf("<< /Type /CMap /CMapName /Test-H /WMode %d /CIDSystemInfo %s /Length %d >>\nstream\n%s\nendstream",
			dictWMode, cmapSysInfo, len(program), program),
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /Test /CIDSystemInfo " + cidSysInfo + " >>",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// runCIDFix applies the fixer and returns the allocator.
func runCIDFix(t *testing.T, data []byte) *writer.ObjAllocator {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if _, err := fixCIDSystemInfo(ctx, alloc, doc, Options{}, nil, "PDF/A-4"); err != nil {
		t.Fatalf("fixCIDSystemInfo: %v", err)
	}
	return alloc
}

// patchedCIDSystemInfo returns the CIDFont's rewritten /CIDSystemInfo.
func patchedCIDSystemInfo(t *testing.T, alloc *writer.ObjAllocator) ast.PdfDict {
	t.Helper()

	cidFont, ok := alloc.Pending()[7].(ast.PdfDict)
	if !ok {
		return nil
	}
	sys, ok := cidFont["CIDSystemInfo"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/CIDSystemInfo is %T, want a dict", cidFont["CIDSystemInfo"])
	}
	return sys
}

// TestAlignCIDSystemInfoFixesLetterCase pins that a Registry differing only in
// letter case is settled in the CMap's favour.
//
// ISO 19005-4, 6.2.10.3.1 test 1 wants the values identical, and "adobe"
// against the CMap's "Adobe" is a producer typo naming the same character
// collection. The fixer could only relabel /Encoding to /Identity-H, which a
// real CMap forbids, so it left the file failing.
//
// # Corpus evidence
//
//   - verapdf 6-2-10-3-1-t01-fail-a: writes "adobe" against the CMap's
//     "Adobe".
func TestAlignCIDSystemInfoFixesLetterCase(t *testing.T) {
	t.Parallel()

	alloc := runCIDFix(t, cidFontPDF(
		"<< /Registry (adobe) /Ordering (Japan1) /Supplement 2 >>",
		"<< /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >>", 0, 0))

	sys := patchedCIDSystemInfo(t, alloc)
	if sys == nil {
		t.Fatal("the CIDFont was not rewritten")
	}
	if reg, _ := sys["Registry"].(ast.PdfString); string(reg) != "Adobe" {
		t.Errorf("/Registry = %q, want %q", reg, "Adobe")
	}
}

// TestAlignCIDSystemInfoClampsTheSupplement pins that a /Supplement above the
// CMap's is lowered to it, which 6.2.10.3.1 test 1 requires.
func TestAlignCIDSystemInfoClampsTheSupplement(t *testing.T) {
	t.Parallel()

	alloc := runCIDFix(t, cidFontPDF(
		"<< /Registry (Adobe) /Ordering (Japan1) /Supplement 3 >>",
		"<< /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >>", 0, 0))

	sys := patchedCIDSystemInfo(t, alloc)
	if sys == nil {
		t.Fatal("the CIDFont was not rewritten")
	}
	if sup, _ := sys["Supplement"].(ast.PdfInt); int(sup) != 2 {
		t.Errorf("/Supplement = %v, want 2", sys["Supplement"])
	}
}

// TestAlignCIDSystemInfoLeavesARealMismatch is the control that matters most:
// a genuinely different character collection must be left alone.
//
// When the font declares Ordering "China1" against a Japan1 CMap, the font and
// the CMap really do disagree, so overwriting the declaration would assert a
// collection the glyphs do not come from. The violation stays as an honest
// residual.
//
// # Corpus evidence
//
//   - verapdf 6-2-10-3-1-t01-fail-b: declares Ordering "China1" against a
//     Japan1 CMap.
func TestAlignCIDSystemInfoLeavesARealMismatch(t *testing.T) {
	t.Parallel()

	alloc := runCIDFix(t, cidFontPDF(
		"<< /Registry (Adobe) /Ordering (China1) /Supplement 2 >>",
		"<< /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >>", 0, 0))

	if sys := patchedCIDSystemInfo(t, alloc); sys != nil {
		if ord, _ := sys["Ordering"].(ast.PdfString); string(ord) != "China1" {
			t.Errorf("/Ordering = %q, want it left as %q", ord, "China1")
		}
	}
}

// TestAlignCMapWModeFollowsTheProgram pins that a CMap dictionary's /WMode is
// corrected to the value its own program declares.
//
// ISO 19005-4, 6.2.10.3.3 test 2 requires the two to be identical, and the
// program is what a viewer executes.
//
// # Corpus evidence
//
//   - verapdf 6-2-10-3-3-t02-fail-a: claims 1 over a program that says 0.
func TestAlignCMapWModeFollowsTheProgram(t *testing.T) {
	t.Parallel()

	alloc := runCIDFix(t, cidFontPDF(
		"<< /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >>",
		"<< /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >>", 1, 0))

	stream, ok := alloc.Pending()[6].(ast.PdfStream)
	if !ok {
		t.Fatalf("the CMap stream was not rewritten: %T", alloc.Pending()[6])
	}
	if wmode, _ := stream.Dict["WMode"].(ast.PdfInt); int(wmode) != 0 {
		t.Errorf("/WMode = %v, want 0 to match the program", stream.Dict["WMode"])
	}
	// The program must survive the dictionary edit.
	body, err := stream.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading the CMap body: %v", err)
	}
	if !bytes.Contains(body, []byte("/CMapName /Test-H def")) {
		t.Errorf("the CMap program was lost: %q", body)
	}
}

// TestAlignCMapWModeLeavesAnAgreeingDictionary is the control: when the two
// already agree, nothing is rewritten.
func TestAlignCMapWModeLeavesAnAgreeingDictionary(t *testing.T) {
	t.Parallel()

	alloc := runCIDFix(t, cidFontPDF(
		"<< /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >>",
		"<< /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >>", 1, 1))

	if _, rewritten := alloc.Pending()[6]; rewritten {
		t.Error("a CMap whose /WMode already matches its program was rewritten")
	}
}

// TestMatchCIDSystemInfoClaimsEverySpelling pins that the alignment repair is
// offered under every specification that states the requirement.
//
// ISO 14289-1, 7.21.3.1 t1 and ISO 14289-2, 8.4.5.3.1 t1 restate the
// CIDSystemInfo relationship word for word, and 7.21.3.3 t2 / 8.4.5.4 t2 do the
// same for the CMap /WMode. Claiming only the PDF/A clause numbers left the
// fixer idle on every PDF/UA document that needed it.
//
// ISO 19005-1 states both at 6.3.3.1 t1 and 6.3.3.3 t2 and was omitted in the
// same pass that added the PDF/UA rows, so the fixer sat idle on PDF/A-1 too:
// the same file was repaired as PDF/A-2b ("aligned /CIDSystemInfo with the
// CMap") and left reporting 6.3.3.1 t1 as PDF/A-1b.
//
// A matcher-level assertion is sufficient HERE, and only because
// [fixCIDSystemInfo] ignores its violations argument and this file holds no
// internal clause gate: claiming therefore implies repairing. That is NOT true
// of every fixer. fixFontIssues gates its rewrite on a second clause list, so
// its matcher test passed for three months while the repair was dead.
func TestMatchCIDSystemInfoClaimsEverySpelling(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		clause  string
		context string
	}{
		{"6.3.3.1", "font C0_0 on page 1 (CIDFont Registry=(adobe) ...)"},
		{"6.2.11.3.1", "font C0_0 on page 1 (CIDFont Registry=(adobe) ...)"},
		{"6.2.10.3.1", "font C0_0 on page 1 (CIDFont Registry=(adobe) ...)"},
		{"7.21.3.1", "font C0_0 on page 1 (CIDFont Registry=(adobe) ...)"},
		{"8.4.5.3.1", "font C0_0 on page 1 (CIDFont Registry=(adobe) ...)"},
		{"6.3.3.3", "font C0_0 on page 1: CMap dict WMode=1 but stream WMode=0"},
		{"6.2.11.3.3", "font C0_0 on page 1: CMap dict WMode=1 but stream WMode=0"},
		{"7.21.3.3", "font C0_0 on page 1: CMap dict WMode=1 but stream WMode=0"},
		{"8.4.5.4", "font C0_0 on page 1: CMap dict WMode=1 but stream WMode=0"},
	} {
		v := compliance.Violation{
			Rule:    compliance.Rule{Clause: tc.clause, TestNumber: 1},
			Context: tc.context,
		}
		if !matchCIDSystemInfo(v) {
			t.Errorf("clause %s is not claimed by the CIDSystemInfo fixer", tc.clause)
		}
	}
}

// TestMatchCIDSystemInfoDeclinesWhatItCannotRepair is the control against the
// fixer claiming work it cannot do.
//
// The CMap clauses carry three tests and this fixer repairs only t2. Embedding
// a missing CMap (t1) and rewriting a /UseCMap chain (t3) are capabilities
// litho does not have, so the clause arm returns contextContains "WMode" rather
// than true. The contexts below are the ones [cmapEmbeddingRules] actually
// emits (compliance/fonts_cmap.go:403, :440, :468), not invented strings: a
// control written against an imagined message proves nothing about the gate.
func TestMatchCIDSystemInfoDeclinesWhatItCannotRepair(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		clause  string
		test    int
		context string
	}{
		{
			"an unrelated width clause", "6.2.11.5", 1,
			"font C0_0 on page 1: .notdef glyph width mismatch",
		},
		{
			"CMap embedding, which litho cannot do", "6.3.3.3", 1,
			`font C0_0 on page 1: CMap "Adobe-Japan1-UCS2" is not predefined and not embedded`,
		},
		{
			"a non-embedded /UseCMap base", "6.3.3.3", 1,
			`font C0_0 on page 1: CMap uses non-embedded CMap "Adobe-Korea1-2" via /UseCMap`,
		},
		{
			"a /UseCMap chain, part 2's t3", "6.2.11.3.3", 3,
			`font C0_0 on page 1: CMap chain references non-predefined CMap "Adobe-Japan1-2"`,
		},
	} {
		v := compliance.Violation{
			Rule:    compliance.Rule{Clause: tc.clause, TestNumber: tc.test},
			Context: tc.context,
		}
		if matchCIDSystemInfo(v) {
			t.Errorf("%s: %s-t%02d was claimed by the CIDSystemInfo fixer, which cannot repair it",
				tc.name, tc.clause, tc.test)
		}
	}
}
