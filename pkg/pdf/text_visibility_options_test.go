package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// visibilityPDF builds a one-page document carrying three text runs that the
// extractor classifies differently.
//
// The three tokens are deliberately not substrings of one another. An earlier
// draft used VISIBLE/INVISIBLE/CLIPPED, where strings.Contains(got, "VISIBLE")
// was satisfied by the word INVISIBLE, so the precondition that plain text
// still extracts could pass on a document whose plain run had vanished.
//
// ALPHA is plain black text and is the precondition for every assertion below.
//
// BETA uses text rendering mode 3, which typography.textIsInvisible matches on
// Tmode == 3 alone (ISO 32000-1, 9.3.6: mode 3 paints nothing). It is wrapped
// in q/Q, and the run after it sets 0 Tr explicitly, because the text
// rendering mode lives in the GRAPHICS state (ISO 32000-1, 8.4.2, Table 104):
// ET does not reset it. Without both, GAMMA inherited mode 3 and came back
// Invisible as well as Hidden, which made it impossible to attribute its
// suppression to one stripper.
//
// GAMMA is painted outside a clipping rectangle installed with "re W n".
// typography.textIsClipped marks a glyph Hidden when a clip is in force and
// the glyph origin falls outside its bounds, so the run sits at y=100 while
// the clip admits only y>=600. Note the naming: this is the W/W*
// graphics-state path, which the extractor calls Hidden and which
// [WithClipPathText] reaches, NOT the CropBox clipping of [WithClippedText].
func visibilityPDF() []byte {
	const content = "BT /F1 12 Tf 100 700 Td (ALPHA) Tj ET\n" +
		"q BT 3 Tr /F1 12 Tf 100 650 Td (BETA) Tj ET Q\n" +
		"q 0 600 612 192 re W n BT 0 Tr /F1 12 Tf 100 100 Td (GAMMA) Tj ET Q\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 4 0 R /Contents 5 0 R >>",
		"<< /Font << /F1 6 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	return buf.Bytes()
}

// TestExtractText_VisibilityFixtureIsolatesOneStripperPerRun is the fixture's
// own control, and it has to pass before the wiring test below means anything.
//
// Each run must carry exactly the flag it is meant to exercise. A run that is
// both Invisible and Hidden is suppressed by two strippers at once, so no
// option can be shown to reveal it on its own and the wiring test silently
// stops discriminating.
func TestExtractText_VisibilityFixtureIsolatesOneStripperPerRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := visibilityPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	els, err := r.ExtractTextElements(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}

	want := map[string]struct{ hidden, invisible, clipped bool }{
		"ALPHA": {false, false, false},
		"BETA":  {false, true, false},
		"GAMMA": {true, false, false},
	}
	seen := map[string]bool{}
	for _, el := range els {
		exp, ok := want[el.Text]
		if !ok {
			continue
		}
		seen[el.Text] = true
		if el.Hidden != exp.hidden || el.Invisible != exp.invisible || el.IsClipped != exp.clipped {
			t.Errorf("%s: Hidden=%v Invisible=%v IsClipped=%v, want %v/%v/%v",
				el.Text, el.Hidden, el.Invisible, el.IsClipped, exp.hidden, exp.invisible, exp.clipped)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("run %q never reached ExtractTextElements", name)
		}
	}
}

// TestExtractText_VisibilityOptionsWiring pins which option un-strips which
// category, one option per category.
//
// [Reader.ExtractText] builds a typography.ExtractionConfig from four strip
// flags, and each now has an option of its own: StripInvisible is cleared by
// [WithInvisibleText], StripHidden (the W/W* graphics-state clip) by
// [WithClipPathText], StripClipped (the CropBox) by [WithClippedText], and
// StripArtifacts by [WithArtifacts]. [WithHiddenText] clears all four.
//
// Before [WithClipPathText] existed there were four flags and three options,
// so StripHidden rode [WithInvisibleText]: asking for invisible text silently
// also returned path-clipped text, and no caller could ask for one without the
// other. The separation is what these subtests pin, in both directions.
//
// Each category is asserted present under its own option AND absent under its
// neighbour. Asserting only the positive half would still pass if two options
// were wired to the same flag, which is the exact defect this replaced.
func TestExtractText_VisibilityOptionsWiring(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := visibilityPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	extract := func(t *testing.T, opts ...ExtractTextOption) string {
		t.Helper()
		got, err := r.ExtractText(ctx, 1, opts...)
		if err != nil {
			t.Fatalf("ExtractText: %v", err)
		}
		return got
	}

	base := extract(t)
	if !strings.Contains(base, "ALPHA") {
		t.Fatalf("precondition: plain text did not extract, so no assertion here is meaningful: %q", base)
	}
	if strings.Contains(base, "BETA") {
		t.Fatalf("precondition: Tr 3 text survived a default extraction, so the fixture does not exercise StripInvisible: %q", base)
	}
	if strings.Contains(base, "GAMMA") {
		t.Fatalf("precondition: path-clipped text survived a default extraction, so the fixture does not exercise StripHidden: %q", base)
	}

	t.Run("WithInvisibleText returns the mode-3 run only", func(t *testing.T) {
		got := extract(t, WithInvisibleText())
		if !strings.Contains(got, "BETA") {
			t.Errorf("WithInvisibleText did not return the Tr 3 run: %q", got)
		}
		if strings.Contains(got, "GAMMA") {
			t.Errorf("WithInvisibleText returned the W/W* path-clipped run: %q\n"+
				"That category belongs to WithClipPathText; the two options must not share a flag", got)
		}
	})

	t.Run("WithClipPathText returns the path-clipped run only", func(t *testing.T) {
		got := extract(t, WithClipPathText())
		if !strings.Contains(got, "GAMMA") {
			t.Errorf("WithClipPathText did not return the W/W* path-clipped run: %q", got)
		}
		if strings.Contains(got, "BETA") {
			t.Errorf("WithClipPathText returned the Tr 3 run: %q", got)
		}
	})

	t.Run("WithClippedText returns neither", func(t *testing.T) {
		// CropBox clipping is a third category, and this fixture contains
		// none of it: the option must move nothing here.
		got := extract(t, WithClippedText())
		if strings.Contains(got, "GAMMA") {
			t.Errorf("WithClippedText returned the W/W* path-clipped run: %q\n"+
				"It covers CropBox clipping (StripClipped) only", got)
		}
		if strings.Contains(got, "BETA") {
			t.Errorf("WithClippedText returned the Tr 3 run: %q", got)
		}
	})

	t.Run("WithHiddenText returns every category", func(t *testing.T) {
		got := extract(t, WithHiddenText())
		for _, want := range []string{"ALPHA", "BETA", "GAMMA"} {
			if !strings.Contains(got, want) {
				t.Errorf("WithHiddenText did not return %q: %q", want, got)
			}
		}
	})
}
