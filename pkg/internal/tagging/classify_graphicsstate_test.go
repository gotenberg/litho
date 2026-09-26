package tagging

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// qRestoresFontSizePDF sets the body size OUTSIDE a q…Q pair, raises it to a
// heading size INSIDE, and then shows an ordinary body line AFTER the Q with no
// Tf of its own.
//
// Per ISO 32000-1, 8.4.2 and Table 104 the text state, including the font and
// its size, belongs to the graphics state, so Q restores it: the line after the
// Q renders at 12, not 24.
//
// The two trailing body lines exist to pin the weighted mode. bodyFontSize
// weights each size by the number of characters carrying it, so without them a
// mis-attributed line could move the body size itself and the test would be
// measuring its own fixture rather than the defect.
func qRestoresFontSizePDF() []byte {
	cs := "BT /F1 12 Tf 72 720 Td (Body line zero with several words here.) Tj ET\n" +
		"q\n" +
		"BT /F1 24 Tf 72 700 Td (Big Heading) Tj ET\n" +
		"Q\n" +
		"BT 72 660 Td (Body line one with several words here.) Tj ET\n" +
		"BT /F1 12 Tf 72 640 Td (Body line two with several words here.) Tj ET\n" +
		"BT /F1 12 Tf 72 620 Td (Body line three with several words here.) Tj ET\n"
	return buildTaggedTestPDF(cs)
}

// TestClassify_QRestoresTheTextFontSize pins that a font size set inside q…Q
// does not leak past the Q into later text.
//
// pageTextBlocks tracked curFont from Tf alone and had no q or Q case, so a size
// raised inside a saved graphics state stayed in effect after the restore. The
// consequence is not cosmetic: fontSize drives bodyFontSize, the
// fontSize >= body*headingSizeRatio heading test, and the line-height merge, so
// a leaked size can promote body text to a heading and reshape the structure
// tree that auto-tagging emits.
//
// Note that BT deliberately does NOT reset curFont: a font legitimately
// persists across text objects until another Tf changes it. That is correct,
// and it is why the line after the Q carries no Tf of its own. What is at issue
// here is only the restore that Q owes.
func TestClassify_QRestoresTheTextFontSize(t *testing.T) {
	ctx := context.Background()
	data := qRestoresFontSizePDF()

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	root, err := Classify(ctx, nil, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	// Exactly one heading is present in the content: the line inside q…Q.
	var headings int
	for _, k := range root.Kids {
		if k.Role == RoleH1 || k.Role == RoleH2 || k.Role == RoleH3 {
			headings++
		}
	}
	if headings != 1 {
		var got []Role
		for _, k := range root.Kids {
			got = append(got, k.Role)
		}
		t.Errorf("got %d headings, want 1: %v", headings, got)
		t.Log("a second heading means the 24 set inside q…Q leaked past the Q " +
			"and promoted the body line that follows it")
	}
}
