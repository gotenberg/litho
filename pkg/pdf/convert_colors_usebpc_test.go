package pdf

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"testing"
)

// TestConvertColors_FollowsUseBlackPtComp converts the litho fixture
// usebpc_4, whose three pages paint one pair of ICC images and an ICC fill
// under /UseBlackPtComp /Default, /ON and /OFF (ISO 32000-2, Table 57), in
// profiles whose black reads L* 42. /ON and /OFF override the conversion's
// choice for what they govern and /Default follows it. The renderer reads
// the same rule, with its own choice, to compensate, under /Default: so by
// default every page converts to what its source draws, and under
// [WithoutBlackPointCompensation] the /ON and /OFF pages still do, while the
// /Default page converts uncompensated, its black 99 levels lighter than the
// source draws it.
func TestConvertColors_FollowsUseBlackPtComp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := readLithoFixture(t, "usebpc_4.pdf")
	src := openColorPDF(t, data)
	for _, bpc := range []bool{false, true} {
		var opts []ColorOption
		if !bpc {
			opts = append(opts, WithoutBlackPointCompensation())
		}
		w := NewWriter(openColorPDF(t, data))
		report, err := w.ConvertColors(ctx, RGBColorTarget(), opts...)
		if err != nil {
			t.Fatalf("ConvertColors: %v", err)
		}
		// The page whose state the option does not match gets copies of the
		// two images, and its fill converted under its own state.
		if report.BlackPointCopies != 2 || report.BlackPointOperators == 0 || report.BlackPointUnhonoured != 0 {
			t.Errorf("bpc %t: report %+v", bpc, report)
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		converted := openColorPDF(t, out.Bytes())
		for page := 1; page <= 3; page++ {
			if page == 1 && !bpc {
				continue
			}
			before, err := src.RenderPage(ctx, page, WithRenderDPI(72))
			if err != nil {
				t.Fatalf("RenderPage: %v", err)
			}
			after, err := converted.RenderPage(ctx, page, WithRenderDPI(72))
			if err != nil {
				t.Fatalf("RenderPage: %v", err)
			}
			if d := worstChannel(before, after); d > 3 {
				t.Errorf("bpc %t, page %d: the converted page draws up to %d levels from its source", bpc, page, d)
			}
		}
	}
}

// worstChannel returns the largest difference of one channel between two
// renderings of one size.
func worstChannel(a, b image.Image) int {
	worst := 0
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			p := color.NRGBAModel.Convert(a.At(x, y)).(color.NRGBA)
			q := color.NRGBAModel.Convert(b.At(x, y)).(color.NRGBA)
			for _, d := range []int{int(p.R) - int(q.R), int(p.G) - int(q.G), int(p.B) - int(q.B)} {
				worst = max(worst, d, -d)
			}
		}
	}
	return worst
}
