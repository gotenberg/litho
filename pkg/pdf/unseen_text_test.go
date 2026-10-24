package pdf

import (
	"context"
	"errors"
	"fmt"
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
