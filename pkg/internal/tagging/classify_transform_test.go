package tagging

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// scaledHeadingPDF draws three body lines at 12pt and one line that is also
// 12pt in the font dictionary but painted under a doubling CTM.
//
// Per ISO 32000-1, 9.4.4 a glyph reaches the page through the text space to
// user space transform, so the size a reader sees is the Tf size scaled by the
// text matrix and then by the CTM. `2 0 0 2 0 0 cm` doubles both axes, so that
// line renders at an effective 24pt against a 12pt body.
//
// The cm sits inside a q…Q pair, which is how a real producer scopes a
// transform and keeps the rest of the page at unit scale (ISO 32000-1, 8.4.2).
func scaledHeadingPDF() []byte {
	cs := "BT /F1 12 Tf 72 700 Td (Body text line one goes here.) Tj ET\n" +
		"q 2 0 0 2 0 0 cm BT /F1 12 Tf 36 330 Td (Scaled Heading) Tj ET Q\n" +
		"BT /F1 12 Tf 72 620 Td (Body text line two goes here.) Tj ET\n" +
		"BT /F1 12 Tf 72 600 Td (Body text line three goes here.) Tj ET\n"
	return buildTaggedTestPDF(cs)
}

// TestClassify_CMScaleSetsTheEffectiveFontSize pins that a doubling CTM makes
// 12pt text classify as a heading.
//
// pageTextBlocks records fontSize from the Tf operand alone. headingLevels is a
// pure function over those recorded sizes, so a size that never accounts for
// the transform cannot be recovered downstream: every block reports 12, the
// body size is 12, and `fontSize >= body*headingSizeRatio` is false everywhere,
// so the page emits no heading at all.
//
// This is the gap the autotag record calls "font size from Tf only". It is a
// SEPARATE defect from the q/Q restores fixed in 734e5a43 and bfe26e5e: those
// leaked a correctly-read size past a Q, this one never reads the size right in
// the first place. A limitation note that named the wrong mechanism hid the
// graphics-state bug for months, so this test names the mechanism it covers.
func TestClassify_CMScaleSetsTheEffectiveFontSize(t *testing.T) {
	ctx := context.Background()
	data := scaledHeadingPDF()

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	var headings int
	var got []Role
	for _, k := range root.Kids {
		got = append(got, k.Role)
		if k.Role == RoleH1 {
			headings++
		}
	}
	if headings != 1 {
		t.Errorf("got %d H1, want 1: %v", headings, got)
		t.Log("no heading means the 2 0 0 2 0 0 cm was ignored, so the scaled line " +
			"was recorded at its Tf size of 12 and never cleared body*headingSizeRatio")
	}
}

// TestClassify_TmScaleSetsTheEffectiveFontSize is the same defect reached
// through the text matrix rather than the CTM.
//
// Per ISO 32000-1, 9.4.2 Tm sets the text matrix directly, and its d operand
// scales the vertical axis. `12 0 0 12 72 330 Tm` with a 1pt font is the
// idiomatic way a producer expresses 12pt text, so a classifier reading Tf
// alone sees 1 and a classifier reading the transform sees 12. Here the scaled
// line uses Tm to reach 24pt while the body stays at 12.
//
// Tm and cm are tested apart because they are separate operands on separate
// matrices: a fix that handles one does not imply the other, and the autotag
// record names both.
func TestClassify_TmScaleSetsTheEffectiveFontSize(t *testing.T) {
	ctx := context.Background()
	cs := "BT /F1 12 Tf 72 700 Td (Body text line one goes here.) Tj ET\n" +
		"BT /F1 1 Tf 24 0 0 24 72 660 Tm (Scaled Heading) Tj ET\n" +
		"BT /F1 12 Tf 72 620 Td (Body text line two goes here.) Tj ET\n" +
		"BT /F1 12 Tf 72 600 Td (Body text line three goes here.) Tj ET\n"
	data := buildTaggedTestPDF(cs)

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	var headings int
	var got []Role
	for _, k := range root.Kids {
		got = append(got, k.Role)
		if k.Role == RoleH1 {
			headings++
		}
	}
	if headings != 1 {
		t.Errorf("got %d H1, want 1: %v", headings, got)
		t.Log("no heading means the Tm d operand was ignored, so the line was " +
			"recorded at its Tf size of 1 and ranked below the 12pt body")
	}
}

// TestClassify_ScaledBlocksDoNotMergeAcrossACM pins the guard that licenses
// tracking sizes in device space while leaving baselines in text space.
//
// pageTextBlocks records a transform-aware font size but an untransformed
// baseline. lineAdjacent compares a baseline gap against a size-derived line
// height, so the two must never meet across a CTM change. Two things keep that
// true, and this test pins the second:
//
//   - q, Q and cm may not appear inside a text object (ISO 32000-1, 8.2 and
//     Table 31), so every glyph of one BT…ET block shares one CTM and the scale
//     factor cancels on both sides of the comparison.
//   - across blocks, classifyBlocks merges only when blankGap holds, and the
//     bytes of an intervening cm are not blank.
//
// Remove either and a 12pt line under a doubling cm would be measured with a
// 24pt line height against an unscaled gap.
func TestClassify_ScaledBlocksDoNotMergeAcrossACM(t *testing.T) {
	// Two lines 10 apart, which at 12pt would merge (10 <= 12*1.2*1.3 = 18.72),
	// but the second is drawn under a doubling cm.
	cs := "BT /F1 12 Tf 72 700 Td (First line of the paragraph.) Tj ET\n" +
		"q 2 0 0 2 0 0 cm BT /F1 12 Tf 36 345 Td (Second line, scaled.) Tj ET Q\n"
	data := buildTaggedTestPDF(cs)

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	var got []Role
	for _, k := range root.Kids {
		got = append(got, k.Role)
	}
	if len(got) < 2 {
		t.Errorf("got %d elements %v, want the cm to keep the two blocks apart", len(got), got)
		t.Log("one element means the blocks merged across an intervening cm, so a " +
			"device-space line height was compared against a text-space gap")
	}
}
