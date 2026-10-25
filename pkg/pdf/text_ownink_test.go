package pdf

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// layeredPDF is a 400-point square page drawing content in Helvetica, /F1.
func layeredPDF(content string) []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// TestOwnInkLayeredContrast judges a white sign drawn at the origin of a dark
// glyph, a symbol built of two glyphs, against the dark glyph it lies on: the
// page drawn without the sign keeps the dark glyph, so the sign shows white
// on black and passes. Leaving out every glyph at the sign's origin removed
// the dark glyph too, and the sign read white on the white page.
func TestOwnInkLayeredContrast(t *testing.T) {
	t.Parallel()
	r := openLimited(t, layeredPDF("BT 0 g 0 G 16 w 2 Tr /F1 40 Tf 100 200 Td (I) Tj ET\n"+
		"BT 1 g 0 Tr /F1 12 Tf 100 200 Td (l) Tj ET\n"), Limits{})
	rep, err := r.CheckTextContrast(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Judged != 2 || len(rep.Failures) != 0 {
		t.Errorf("judged %d, failures %+v; want both glyphs judged and none short", rep.Judged, rep.Failures)
	}
}

// TestOwnInkLayeredUnseen reports a word hidden under a rectangle while a
// visible glyph is drawn over the rectangle at the origin of its first
// letter: the page drawn without the word alone is the page drawn whole.
// Leaving out every glyph at that origin removed the visible "W" too, and
// the hidden word read as seen.
func TestOwnInkLayeredUnseen(t *testing.T) {
	t.Parallel()
	r := openLimited(t, layeredPDF("BT 0 g /F1 12 Tf 100 300 Td (HIDE) Tj ET\n"+
		"1 g 95 295 50 20 re f\n"+
		"0 g BT /F1 12 Tf 0 1 -1 0 100 300 Tm (W) Tj ET\n"), Limits{})
	unseen, err := r.ExtractUnseenText(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	var hide bool
	for _, u := range unseen {
		switch {
		case strings.Contains(u.Text, "W"):
			t.Errorf("visible W reported unseen: %+v", u)
		case u.Text == "HIDE":
			hide = true
			if u.Reason != models.UnseenCovered {
				t.Errorf("HIDE reason %q, want %q", u.Reason, models.UnseenCovered)
			}
		}
	}
	if !hide {
		t.Errorf("unseen %+v, want HIDE covered", unseen)
	}
}

// TestOwnInkLayeredWhiteText leaves out of default text extraction a white
// word on a white rectangle, though a black glyph is drawn after it at the
// origin of its first letter: the word's own ink marks nothing.
func TestOwnInkLayeredWhiteText(t *testing.T) {
	t.Parallel()
	r := openLimited(t, layeredPDF("1 g 90 290 120 30 re f\n"+
		"BT 1 g /F1 12 Tf 100 300 Td (GHOST) Tj ET\n"+
		"BT 0 g /F1 12 Tf 100 300 Td (V) Tj ET\n"), Limits{})
	text, err := r.ExtractText(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "GHOST") || !strings.Contains(text, "V") {
		t.Errorf("text %q, want V without GHOST", text)
	}
}
