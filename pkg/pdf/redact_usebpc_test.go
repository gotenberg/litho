package pdf

import (
	"bytes"
	"context"
	"image/color"
	"testing"
)

// TestRedact_BlanksUnderUseBlackPtCompOff redacts the centred quarter of the
// litho fixture usebpc_4's third page, whose images and fill are painted
// under /UseBlackPtComp /OFF (ISO 32000-2, Table 57) in profiles whose black
// reads L* 42. A blanked sample there draws the profile's own black, a mid
// grey no blank goes below, which the pixel check read as a picture left in
// the region and refused; it now reads the images-only rendering
// compensated, where the blank is black. The output draws the blank as the
// /OFF page draws the profile's black, (99, 99, 99).
func TestRedact_BlanksUnderUseBlackPtCompOff(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := readLithoFixture(t, "usebpc_4.pdf")
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	report, err := w.Redact(ctx, []RedactRegion{{Page: 3, Rect: [4]float64{25, 50, 75, 150}}})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if len(report.BlankedImages) != 2 {
		t.Fatalf("blanked %d images, want 2", len(report.BlankedImages))
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	after, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer after.Close()
	img, err := after.RenderPage(ctx, 3, WithRenderDPI(72))
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	// The RGB ramp spans y 80 to 130 on the 200-point page, its samples 10
	// points wide from x = 10: x 50 is the middle of a covered sample.
	got := color.NRGBAModel.Convert(img.At(50, 200-105)).(color.NRGBA)
	if got.R != got.G || got.G != got.B || got.R < 90 || got.R > 110 {
		t.Errorf("blanked sample draws %v, want the profile's own black under /OFF, about (99, 99, 99)", got)
	}
}
