package pdf

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// strokedWhitePDF is a letter page of runs on the white page, each a word no
// other contains, painted with the text rendering modes of ISO 32000-1,
// 9.3.6, Table 106:
//
//   - OUTLINED, filled white and stroked black (mode 2): its outline shows;
//   - HAIRLINE, as OUTLINED at line width 0, the thinnest line the device
//     can render (8.4.3.2): it shows;
//   - KEYCAPS, filled black and stroked white (mode 2): its fill shows;
//   - HALOED, stroked white alone (mode 1), its black fill unused: white on
//     white, dropped;
//   - NOSTROKE, as OUTLINED with the stroke at CA 0 (11.6.4.4): only the
//     white fill marks the page, dropped;
//   - THROUGHCLIP, added to the clipping path (mode 7) and filled with a
//     navy rectangle after it: what the page paints next shows through it.
func strokedWhitePDF() []byte {
	const content = "BT 2 Tr 1 g 0 G /F1 14 Tf 60 700 Td (OUTLINED) Tj ET\n" +
		"BT 2 Tr 1 g 0 G 0 w /F1 14 Tf 60 650 Td (HAIRLINE) Tj ET\n" +
		"BT 2 Tr 0 g 1 G /F1 14 Tf 60 600 Td (KEYCAPS) Tj ET\n" +
		"BT 1 Tr 0 g 1 G /F1 14 Tf 60 550 Td (HALOED) Tj ET\n" +
		"q /NoStroke gs BT 2 Tr 1 g 0 G /F1 14 Tf 60 500 Td (NOSTROKE) Tj ET Q\n" +
		"q BT 7 Tr /F1 14 Tf 60 450 Td (THROUGHCLIP) Tj ET 0 0 0.5 rg 50 440 300 30 re f Q\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> /ExtGState << /NoStroke << /CA 0 >> >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

func TestExtractText_JudgesTextByEveryPaintItsModeApplies(t *testing.T) {
	t.Parallel()
	r := openLimited(t, strokedWhitePDF(), Limits{})
	ctx := context.Background()

	got, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"OUTLINED", "HAIRLINE", "KEYCAPS", "THROUGHCLIP"} {
		if !strings.Contains(got, want) {
			t.Errorf("ExtractText = %q, want %s", got, want)
		}
	}
	for _, gone := range []string{"HALOED", "NOSTROKE"} {
		if strings.Contains(got, gone) {
			t.Errorf("ExtractText = %q, want no %s", got, gone)
		}
	}

	elements, err := r.ExtractTextElements(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Each run sits on its own baseline; an element's Invisible is the
	// judgement ExtractText reads.
	wantInvisible := map[int]bool{700: false, 650: false, 600: false, 550: true, 500: true, 450: false}
	for _, el := range elements {
		want, ok := wantInvisible[int(el.Y+0.5)]
		if !ok || strings.TrimSpace(el.Text) == "" {
			continue
		}
		if el.Invisible != want {
			t.Errorf("glyph %q at y=%v: Invisible = %v, want %v", el.Text, el.Y, el.Invisible, want)
		}
	}
}

// strokedContrastPDF is a 400-point square page of runs filled and stroked
// (mode 2) on white, each a word no other contains:
//
//   - OUTLINED, filled white and stroked black: its black outline gives 21:1;
//   - PALE, filled white and stroked 0.9 grey: about 1.3:1, short of 4.5:1;
//   - HALFLINE, filled black and stroked at CA 0.5: undetermined.
func strokedContrastPDF() []byte {
	const content = "BT 2 Tr 1 g 0 G /F1 12 Tf 20 360 Td (OUTLINED) Tj ET\n" +
		"BT 2 Tr 1 g 0.9 G /F1 12 Tf 20 320 Td (PALE) Tj ET\n" +
		"q /Half gs BT 2 Tr 0 g 0 G /F1 12 Tf 20 280 Td (HALFLINE) Tj ET Q\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Resources << /Font << /F1 5 0 R >> /ExtGState << /Half << /CA 0.5 >> >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

func TestCheckTextContrast_JudgesAnOutlineByItsStroke(t *testing.T) {
	t.Parallel()
	r := openLimited(t, strokedContrastPDF(), Limits{})

	rep, err := r.CheckTextContrast(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Judged != 2 || rep.Undetermined != 1 {
		t.Errorf("judged %d, undetermined %d; want 2 and 1", rep.Judged, rep.Undetermined)
	}
	got := failuresByText(rep)
	if _, ok := got["OUTLINED"]; ok {
		t.Errorf("OUTLINED reported: %+v", got["OUTLINED"])
	}
	pale, ok := got["PALE"]
	if !ok {
		t.Fatalf("PALE not reported: %v", got)
	}
	if pale.TextColor != [3]uint8{230, 230, 230} {
		t.Errorf("PALE judged in %v, want its 0.9 grey stroke", pale.TextColor)
	}
}
