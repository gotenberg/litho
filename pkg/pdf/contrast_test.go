package pdf

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// contrastPDF is a 400-point square page of runs, each a word no other
// contains, with the ratio each shows against what lies under it:
//
//   - BLACK, black on white: 21:1;
//   - GREYED, 0.6 grey at 12 points: about 2.85:1, short of 4.5:1;
//   - BOLDISH, 0.6 grey, bold at 14 points, large-scale: about 2.85:1, short
//     of 3:1;
//   - LARGE, 0.55 grey at 24 points: about 3.4:1, past 3:1, short of AAA's
//     4.5:1;
//   - MIDDLE, 0.45 grey at 12 points: about 4.8:1, past 4.5:1, short of AAA's
//     7:1;
//   - NAVY, white on a navy bar: about 16:1;
//   - FADED, at constant alpha 0.5: undetermined.
func contrastPDF() []byte {
	const content = "BT /F1 12 Tf 20 360 Td (BLACK) Tj ET\n" +
		"BT 0.6 g /F1 12 Tf 20 320 Td (GREYED) Tj ET\n" +
		"BT 0.6 g /F2 14 Tf 20 280 Td (BOLDISH) Tj ET\n" +
		"BT 0.55 g /F1 24 Tf 20 230 Td (LARGE) Tj ET\n" +
		"BT 0.45 g /F1 12 Tf 20 190 Td (MIDDLE) Tj ET\n" +
		"0 0 0.5 rg 10 140 300 30 re f\n" +
		"BT 1 g /F1 12 Tf 20 150 Td (NAVY) Tj ET\n" +
		"q /Half gs BT 0 g /F1 12 Tf 20 100 Td (FADED) Tj ET Q\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Resources << /Font << /F1 5 0 R /F2 6 0 R >> /ExtGState << /Half << /ca 0.5 >> >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
	})
}

func failuresByText(rep models.ContrastReport) map[string]models.TextContrast {
	out := map[string]models.TextContrast{}
	for _, f := range rep.Failures {
		out[f.Text] = f
	}
	return out
}

func TestCheckTextContrast(t *testing.T) {
	t.Parallel()
	r := openLimited(t, contrastPDF(), Limits{})
	ctx := context.Background()

	rep, err := r.CheckTextContrast(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Judged != 6 || rep.Undetermined != 1 {
		t.Errorf("judged %d, undetermined %d; want 6 and 1", rep.Judged, rep.Undetermined)
	}
	got := failuresByText(rep)
	if len(got) != 2 {
		t.Errorf("failures %v, want GREYED and BOLDISH", got)
	}
	greyed, ok := got["GREYED"]
	if !ok {
		t.Fatalf("GREYED not reported: %v", got)
	}
	if math.Abs(greyed.Ratio-2.85) > 0.05 || greyed.Required != 4.5 || greyed.Large || greyed.Bold {
		t.Errorf("GREYED = %+v, want ratio about 2.85 against 4.5, not large", greyed)
	}
	if greyed.TextColor != [3]uint8{153, 153, 153} || greyed.BackgroundColor != [3]uint8{255, 255, 255} || greyed.BackgroundVaries {
		t.Errorf("GREYED colours %v on %v (varies %v), want 0.6 grey on white", greyed.TextColor, greyed.BackgroundColor, greyed.BackgroundVaries)
	}
	bold, ok := got["BOLDISH"]
	if !ok {
		t.Fatalf("BOLDISH not reported: %v", got)
	}
	if !bold.Bold || !bold.Large || bold.Required != 3 {
		t.Errorf("BOLDISH = %+v, want bold, large, against 3", bold)
	}

	enhanced, err := r.CheckTextContrast(ctx, 1, WithEnhancedContrast())
	if err != nil {
		t.Fatal(err)
	}
	got = failuresByText(enhanced)
	for word, required := range map[string]float64{"GREYED": 7, "BOLDISH": 4.5, "LARGE": 4.5, "MIDDLE": 7} {
		f, ok := got[word]
		if !ok {
			t.Errorf("enhanced: %s not reported: %v", word, got)
			continue
		}
		if f.Required != required {
			t.Errorf("enhanced: %s required %v, want %v", word, f.Required, required)
		}
	}
	for _, word := range []string{"BLACK", "NAVY", "FADED"} {
		if _, ok := got[word]; ok {
			t.Errorf("enhanced: %s reported: %v", word, got[word])
		}
	}
}

// TestCheckTextContrastInsideNeighbourBox judges the runs of
// [neighbourBoxPDF] that show by their own ink, though no pixel of Hello or
// Grey is theirs alone: Grey against the white page under it. The two Shades
// are one word drawn twice, a shadow, judged as one by its black copy
// against the white page.
func TestCheckTextContrastInsideNeighbourBox(t *testing.T) {
	t.Parallel()
	r := openLimited(t, neighbourBoxPDF(), Limits{})

	rep, err := r.CheckTextContrast(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// L, Hello, Grey and the Shade stack; Gone is covered.
	if rep.Judged != 4 || rep.Undetermined != 0 {
		t.Errorf("judged %d, undetermined %d; want 4 and 0", rep.Judged, rep.Undetermined)
	}
	got := failuresByText(rep)
	grey, ok := got["Grey"]
	if !ok || len(got) != 1 {
		t.Fatalf("failures %v, want Grey alone", got)
	}
	if grey.BackgroundColor != [3]uint8{255, 255, 255} || math.Abs(grey.Ratio-2.85) > 0.05 {
		t.Errorf("Grey = %+v, want about 2.85 against white", grey)
	}
}

// TestCheckTextContrastManyApart judges more runs set inside other glyphs'
// boxes than [maxSceneOwnDrawings] allows drawings: twenty grey words, each
// inside the box of its own large "L" and apart from the others, share one
// drawing, and each is judged and reported short of 4.5:1.
func TestCheckTextContrastManyApart(t *testing.T) {
	t.Parallel()
	var content strings.Builder
	const words = 20
	for n := range words {
		x, y := 10+70*(n%5), 10+90*(n/5)
		fmt.Fprintf(&content, "BT 0 g /F1 60 Tf %d %d Td (L) Tj ET\n", x, y)
		fmt.Fprintf(&content, "BT 0.6 g /F1 6 Tf %d %d Td (w%c) Tj ET\n", x+3, y+48, 'a'+n)
	}
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", content.Len(), content.String()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
	r := openLimited(t, data, Limits{})

	rep, err := r.CheckTextContrast(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Judged != 2*words || len(rep.Failures) != words {
		t.Errorf("judged %d, %d failures; want %d and %d", rep.Judged, len(rep.Failures), 2*words, words)
	}
}

func TestCheckTextContrastErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	r := openLimited(t, contrastPDF(), Limits{})
	if _, err := r.CheckTextContrast(ctx, 0); !errors.Is(err, ErrInvalidPageNumber) {
		t.Errorf("page 0: err %v, want ErrInvalidPageNumber", err)
	}
	small := openLimited(t, contrastPDF(), Limits{MaxRenderPixels: 100})
	if _, err := small.CheckTextContrast(ctx, 1); !errors.Is(err, ErrRenderTooLarge) {
		t.Errorf("budget of 100 pixels: err %v, want ErrRenderTooLarge", err)
	}
}

// TestCheckTextContrastConcurrent measures the page from several goroutines.
func TestCheckTextContrastConcurrent(t *testing.T) {
	t.Parallel()
	r := openLimited(t, contrastPDF(), Limits{})
	ctx := context.Background()

	var wg sync.WaitGroup
	counts := make([]int, 8)
	errs := make([]error, 8)
	for i := range 8 {
		wg.Go(func() {
			rep, err := r.CheckTextContrast(ctx, 1)
			counts[i], errs[i] = len(rep.Failures), err
		})
	}
	wg.Wait()
	for i := range 8 {
		if errs[i] != nil || counts[i] != 2 {
			t.Errorf("goroutine %d: %d failures, err %v; want 2", i, counts[i], errs[i])
		}
	}
}

// TestCheckTextContrastClipCopy counts undetermined a heading painted over
// and drawn again later at its place in rendering mode 7 to clip a grey
// rectangle: what shows there is the rectangle's grey in the heading's
// letters, not the text's colour. BURIED, painted over with no clipping copy
// showing, is not seen and not counted; SHOWN, black on white, is judged.
func TestCheckTextContrastClipCopy(t *testing.T) {
	t.Parallel()
	const content = "BT 0 g /F1 24 Tf 20 340 Td (HEADING) Tj ET\n" +
		"1 g 10 330 200 40 re f\n" +
		"q BT 7 Tr /F1 24 Tf 20 340 Td (HEADING) Tj ET 0.9 g 10 330 200 40 re f Q\n" +
		"BT 0 g /F1 24 Tf 20 260 Td (BURIED) Tj ET\n" +
		"1 g 10 250 200 40 re f\n" +
		"q BT 7 Tr /F1 24 Tf 20 260 Td (BURIED) Tj ET Q\n" +
		"BT 0 g /F1 24 Tf 20 180 Td (SHOWN) Tj ET\n"
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
	r := openLimited(t, data, Limits{})

	rep, err := r.CheckTextContrast(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Judged != 1 || rep.Undetermined != 1 || len(rep.Failures) != 0 {
		t.Errorf("judged %d, undetermined %d, failures %v; want 1, 1, none", rep.Judged, rep.Undetermined, rep.Failures)
	}
}
