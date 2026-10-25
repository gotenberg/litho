package pdf

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// unseenPDF is a 400-point square page holding one run of each kind
// [Reader.ExtractUnseenText] tells apart, each a word no other contains:
//
//   - SHOWN, black on the page: seen, not reported;
//   - LAYER, rendering mode 3: render-mode;
//   - OUTSIDE, set past the right edge: outside-page;
//   - COVERED, black under a white rectangle drawn after it: covered;
//   - BLANKED, white on the page: paints-nothing;
//   - CLIPPED, black outside a clipping rectangle: paints-nothing.
func unseenPDF() []byte {
	const content = "BT /F1 14 Tf 20 360 Td (SHOWN) Tj ET\n" +
		"BT 3 Tr /F1 14 Tf 20 320 Td (LAYER) Tj ET\n" +
		"BT 0 Tr /F1 14 Tf 500 300 Td (OUTSIDE) Tj ET\n" +
		"BT 0 g /F1 14 Tf 20 260 Td (COVERED) Tj ET\n" +
		"1 g 10 250 200 30 re f\n" +
		"BT 1 g /F1 14 Tf 20 200 Td (BLANKED) Tj ET\n" +
		"q 300 0 100 100 re W n BT 0 g /F1 14 Tf 20 140 Td (CLIPPED) Tj ET Q\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

func TestExtractUnseenText(t *testing.T) {
	t.Parallel()
	r := openLimited(t, unseenPDF(), Limits{})

	got, err := r.ExtractUnseenText(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]models.UnseenReason{}
	for _, u := range got {
		reasons[u.Text] = u.Reason
		if u.BBox == [4]float64{} || u.FontSize != 14 {
			t.Errorf("%q: box %v, font size %v", u.Text, u.BBox, u.FontSize)
		}
	}
	want := map[string]models.UnseenReason{
		"LAYER":   models.UnseenRenderMode,
		"OUTSIDE": models.UnseenOutsidePage,
		"COVERED": models.UnseenCovered,
		"BLANKED": models.UnseenPaintsNothing,
		"CLIPPED": models.UnseenPaintsNothing,
	}
	for text, reason := range want {
		if reasons[text] != reason {
			t.Errorf("%s: reason %q, want %q (all: %v)", text, reasons[text], reason, reasons)
		}
	}
	if _, ok := reasons["SHOWN"]; ok {
		t.Errorf("SHOWN reported unseen: %v", reasons)
	}
	if len(got) != len(want) {
		t.Errorf("%d runs reported, want %d: %v", len(got), len(want), reasons)
	}
}

// neighbourBoxPDF is a 400-point square page whose large "L" holds words
// inside its glyph box and away from its ink:
//
//   - Hello, past the box's right edge, its "H" inside the box and seen, the
//     rest under a white rectangle drawn after it;
//   - Gone, wholly inside the box, under a white rectangle drawn after it;
//   - Grey, wholly inside the box, 0.6 grey on the white page;
//
// and a black Shade under a white Shade set a point up and to the left,
// the white seen only over the black.
func neighbourBoxPDF() []byte {
	const content = "BT 0 g /F1 300 Tf 20 40 Td (L) Tj ET\n" +
		"BT 0 g /F1 10 Tf 176 290 Td (Hello) Tj ET\n" +
		"1 g 184 280 100 30 re f\n" +
		"BT 0 g /F1 10 Tf 90 150 Td (Gone) Tj ET\n" +
		"1 g 85 140 50 25 re f\n" +
		"BT 0.6 g /F1 10 Tf 90 320 Td (Grey) Tj ET\n" +
		"BT 0 g /F1 24 Tf 220 100 Td (Shade) Tj ET\n" +
		"BT 1 g /F1 24 Tf 219 101 Td (Shade) Tj ET\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// TestExtractUnseenTextInsideNeighbourBox judges runs by their own ink: a
// word whose ink lies only inside other glyphs' boxes shows, and one covered
// there is reported, though no pixel of either is its alone.
func TestExtractUnseenTextInsideNeighbourBox(t *testing.T) {
	t.Parallel()
	r := openLimited(t, neighbourBoxPDF(), Limits{})

	got, err := r.ExtractUnseenText(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]models.UnseenReason{}
	for _, u := range got {
		reasons[u.Text] = u.Reason
	}
	if want := map[string]models.UnseenReason{"Gone": models.UnseenCovered}; !maps.Equal(reasons, want) {
		t.Errorf("reported %v, want %v", reasons, want)
	}
}

// TestExtractUnseenTextClipMode judges text in rendering mode 7 by what the
// page paints through its clip (ISO 32000-1, 9.3.6, Table 106): SHOWS clips
// a black rectangle drawn after it, and is seen as a viewer shows it; BARE
// clips nothing drawn after it, and is reported for its mode.
func TestExtractUnseenTextClipMode(t *testing.T) {
	t.Parallel()
	const content = "q BT 7 Tr /F1 24 Tf 20 300 Td (SHOWS) Tj ET 0 g 10 290 200 40 re f Q\n" +
		"q BT 7 Tr /F1 24 Tf 20 200 Td (BARE) Tj ET Q\n" +
		"0 g 10 100 200 40 re f\n"
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
	r := openLimited(t, data, Limits{})

	got, err := r.ExtractUnseenText(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]models.UnseenReason{}
	for _, u := range got {
		reasons[u.Text] = u.Reason
	}
	if want := map[string]models.UnseenReason{"BARE": models.UnseenRenderMode}; !maps.Equal(reasons, want) {
		t.Errorf("reported %v, want %v", reasons, want)
	}
}

func TestExtractUnseenTextErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	r := openLimited(t, unseenPDF(), Limits{})
	if _, err := r.ExtractUnseenText(ctx, 2); !errors.Is(err, ErrInvalidPageNumber) {
		t.Errorf("page 2: err %v, want ErrInvalidPageNumber", err)
	}
	small := openLimited(t, unseenPDF(), Limits{MaxRenderPixels: 100})
	if _, err := small.ExtractUnseenText(ctx, 1); !errors.Is(err, ErrRenderTooLarge) {
		t.Errorf("budget of 100 pixels: err %v, want ErrRenderTooLarge", err)
	}
}

// TestExtractUnseenTextConcurrent reads the page from several goroutines.
func TestExtractUnseenTextConcurrent(t *testing.T) {
	t.Parallel()
	r := openLimited(t, unseenPDF(), Limits{})
	ctx := context.Background()

	var wg sync.WaitGroup
	counts := make([]int, 8)
	errs := make([]error, 8)
	for i := range 8 {
		wg.Go(func() {
			got, err := r.ExtractUnseenText(ctx, 1)
			counts[i], errs[i] = len(got), err
		})
	}
	wg.Wait()
	for i := range 8 {
		if errs[i] != nil || counts[i] != 5 {
			t.Errorf("goroutine %d: %d runs, err %v; want 5", i, counts[i], errs[i])
		}
	}
}

// TestExtractUnseenTextClipCopy judges a filled run painted over and drawn
// again later at its place in rendering mode 7 by what that copy's clip lets
// through (ISO 32000-1, 9.3.6, Table 106), as InDesign draws a heading over
// an image clipped to its letters:
//
//   - HEADING, filled, painted over in white, then drawn again in mode 7 to
//     clip a black rectangle: seen in its copy, not reported;
//   - BURIED, filled, painted over in white, then drawn again in mode 7 over
//     nothing: reported covered, and its copy for its mode;
//   - EARLIER, drawn in mode 7 to clip a black rectangle, then filled and
//     painted over in white: the copy comes first and shows nothing of the
//     fill, so both are reported;
//   - COVERED, filled and painted over in white, with OTHER at its place in
//     mode 7 clipping a black rectangle: other text, so COVERED is reported;
//     it shows only if the drawing that leaves OTHER out of the clip lets the
//     whole rectangle through rather than nothing.
func TestExtractUnseenTextClipCopy(t *testing.T) {
	t.Parallel()
	const content = "BT 0 g /F1 24 Tf 20 340 Td (HEADING) Tj ET\n" +
		"1 g 10 330 200 40 re f\n" +
		"q BT 7 Tr /F1 24 Tf 20 340 Td (HEADING) Tj ET 0 g 10 330 200 40 re f Q\n" +
		"BT 0 g /F1 24 Tf 20 260 Td (BURIED) Tj ET\n" +
		"1 g 10 250 200 40 re f\n" +
		"q BT 7 Tr /F1 24 Tf 20 260 Td (BURIED) Tj ET Q\n" +
		"q BT 7 Tr /F1 24 Tf 20 180 Td (EARLIER) Tj ET 0 g 10 170 200 40 re f Q\n" +
		"BT 0 g /F1 24 Tf 20 180 Td (EARLIER) Tj ET\n" +
		"1 g 10 170 200 40 re f\n" +
		"BT 0 g /F1 24 Tf 20 100 Td (COVERED) Tj ET\n" +
		"1 g 10 90 200 40 re f\n" +
		"q BT 7 Tr /F1 24 Tf 20 100 Td (OTHER) Tj ET 0 g 10 90 200 40 re f Q\n"
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
	r := openLimited(t, data, Limits{})

	got, err := r.ExtractUnseenText(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string][]models.UnseenReason{}
	for _, u := range got {
		reasons[u.Text] = append(reasons[u.Text], u.Reason)
	}
	want := map[string][]models.UnseenReason{
		"BURIED":  {models.UnseenCovered, models.UnseenRenderMode},
		"EARLIER": {models.UnseenRenderMode, models.UnseenCovered},
		"COVERED": {models.UnseenCovered},
	}
	if !maps.EqualFunc(reasons, want, slices.Equal) {
		t.Errorf("reported %v, want %v", reasons, want)
	}
}
