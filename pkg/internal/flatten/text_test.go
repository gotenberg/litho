package flatten

import (
	"bytes"
	"testing"
)

func TestFlattenKeepsAlphaZeroTextInvisible(t *testing.T) {
	t.Parallel()
	// Text under a fill alpha of 0, as OCR layers write it, shows nothing
	// (ISO 32000-2, 11.3.7.2). Once the alpha is gone the rendering mode has
	// to keep it so: 3, neither fill nor stroke.
	out, report := checkFlattened(t, fixture{
		objs:      []string{"<< /Type /ExtGState /ca 0 >>"},
		resources: "/ExtGState << /GS1 6 0 R /Clear 7 0 R >>",
		bare:      true,
		content:   "0 0 1 rg 10 10 50 50 re f q /Clear gs BT /F1 12 Tf 10 20 Td (Hidden) Tj ET Q",
	})
	if pr := report.Pages[0]; pr.TransparentText != 0 {
		t.Errorf("report %+v: text kept invisible exactly is no approximation", pr)
	}
	if !bytes.Contains(out, []byte("3 Tr")) {
		t.Error("the invisible text is not drawn in mode 3")
	}
}

func TestFlattenDrawsAWatermarkOnThePaperAsItWas(t *testing.T) {
	t.Parallel()
	// A half-transparent red watermark on the paper, beside a transparent
	// square: its colour composited over white draws it exactly.
	_, report := checkFlattened(t, fixture{
		content: "q /GS1 gs 0 0 1 rg 0 0 30 30 re f Q q /GS1 gs 1 0 0 rg BT /F1 24 Tf 10 60 Td (DRAFT) Tj ET Q",
	})
	if pr := report.Pages[0]; pr.TransparentText != 0 {
		t.Errorf("report %+v: the watermark on the paper is drawn exactly", pr)
	}
}

func TestFlattenKeepsCoveredTextHidden(t *testing.T) {
	t.Parallel()
	// ISO 32000-1, 8.2: the blue rectangle drawn after the text hides it,
	// and the transparent square over both puts them in one region. The
	// text stays in the content, above the image, and the image is drawn
	// again over the part of it the source did not show.
	for _, tc := range []struct {
		name, cover string
	}{
		{"wholly", "0 0 1 rg 0 0 90 50 re f"},
		{"partly", "0 0 1 rg 0 0 30 50 re f"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, report := checkFlattened(t, fixture{
				content: "BT /F1 12 Tf 10 20 Td (Hidden) Tj ET " + tc.cover + " q /GS1 gs 1 0 0 rg 5 5 20 40 re f Q",
			})
			if pr := report.Pages[0]; pr.TextUnderTransparency != 0 {
				t.Errorf("report %+v: covered text is no approximation", pr)
			}
			if n := bytes.Count(out, []byte("/FlatIm1 Do")); n != 2 {
				t.Errorf("the image is drawn %d times, want 2: once under the text, once over its covered part", n)
			}
			if !bytes.Contains(out, []byte("(Hidden) Tj")) {
				t.Error("the covered text left the content")
			}
		})
	}
}

func TestFlattenCountsTextVisibleUnderTransparency(t *testing.T) {
	t.Parallel()
	// A half-transparent square over the text lets it show tinted: the
	// text stays above the image untinted, which the report counts.
	in := fixture{content: "BT /F1 24 Tf 10 20 Td (Shown) Tj ET q /GS1 gs 1 0 0 rg 0 0 100 60 re f Q"}.bytes()
	_, report := flattenBytes(t, in, Options{})
	if pr := report.Pages[0]; pr.TextUnderTransparency != 1 {
		t.Errorf("report %+v, want the tinted text counted", pr)
	}
}

func TestFlattenSkipsAPageWhoseContentDoesNotRead(t *testing.T) {
	t.Parallel()
	in := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Contents [4 0 R 9 0 R] /MediaBox [0 0 100 100] /Resources << /ExtGState << /GS1 5 0 R >> >> >>",
		stream("", "/GS1 gs 0 0 50 50 re f"),
		"<< /Type /ExtGState /ca 0.5 >>",
	})
	out, report := flattenBytes(t, in, Options{})
	if len(report.Skipped) != 1 || report.Skipped[0] != 0 || len(report.Pages) != 0 {
		t.Errorf("report %+v, want page 0 skipped", report)
	}
	if !bytes.Equal(out[:len(in)], in) || len(out) > len(in)+200 {
		t.Error("the skipped page was edited")
	}
}

func TestFlattenRemovesTransparencyThatPaintsNothing(t *testing.T) {
	t.Parallel()
	// The square lies off the page: removing it changes nothing a viewer
	// shows, and leaves no transparency behind.
	_, report := checkFlattened(t, fixture{content: "/GS1 gs 1 0 0 rg 200 200 20 20 re f"})
	if pr := report.Pages[0]; pr.Regions != 0 || pr.Removed != 1 {
		t.Errorf("report %+v, want the square removed and no region", pr)
	}
}
