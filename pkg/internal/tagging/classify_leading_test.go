package tagging

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// qRestoresLeadingPDF sets the leading OUTSIDE a q…Q pair, lowers it INSIDE,
// and then advances a line with T* AFTER the Q.
//
// Per ISO 32000-1, 8.4.2 and Table 104 the leading TL is a text-state parameter
// and therefore part of the graphics state, so Q restores it: the T* after the
// Q advances by 40, not by 8.
//
// The geometry is chosen so the leak is observable as a role count rather than
// as an invisible coordinate. lineAdjacent merges two blocks when
// gap = a.lastY - b.y is at most max(fontSize)*lineHeightFactor*maxLineGapRatio,
// which at 12pt is 12*1.2*1.3 = 18.72:
//
//	leading restored (40): first block ends at 700-40 = 660, gap to 650 is 10 -> merges
//	leading leaked    (8): first block ends at 700-8  = 692, gap to 650 is 42 -> stays split
//
// So a correct restore yields ONE paragraph and the leak yields two.
func qRestoresLeadingPDF() []byte {
	cs := "BT /F1 12 Tf 40 TL 72 700 Td (First line of the paragraph here.) Tj\n" +
		"q 8 TL Q\n" +
		"T* (Second line of the same paragraph.) Tj ET\n" +
		"BT /F1 12 Tf 72 650 Td (Third line continuing the paragraph.) Tj ET\n"
	return buildTaggedTestPDF(cs)
}

// TestClassify_QRestoresTheTextLeading pins the second text-state parameter
// pageTextBlocks tracks.
//
// 734e5a43 gave the loop a q/Q stack for the font size but not for the leading,
// which the same loop sets from TL and TD and consumes as textY -= leading.
// textY becomes cur.lastY and cur.y, which feed lineAdjacent, so a leading
// raised inside q…Q and leaked past the Q moves a baseline and splits a
// paragraph that should merge.
//
// Reachability is narrow and worth stating: pageTextBlocks zeroes leading at
// every BT, which handleBeginText in the graphics machine does NOT do (it
// resets only Tm and Tlm), so only text after the Q inside the SAME text object
// can observe the leak. That is why the q…Q sits inside the first BT block.
func TestClassify_QRestoresTheTextLeading(t *testing.T) {
	ctx := context.Background()
	data := qRestoresLeadingPDF()

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	var paras int
	var got []Role
	for _, k := range root.Kids {
		got = append(got, k.Role)
		if k.Role == RoleP {
			paras++
		}
	}
	if paras != 1 {
		t.Errorf("got %d paragraphs, want 1: %v", paras, got)
		t.Log("two paragraphs mean the 8 TL set inside q…Q survived the Q, " +
			"so the T* advanced 8 instead of 40 and the blocks no longer merge")
	}
}
