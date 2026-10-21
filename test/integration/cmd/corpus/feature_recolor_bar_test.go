package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// recolorGridPDF paints a levels³ grid of sRGB colours, DeviceRGB patches of
// patch points side by side, each colour written through order, which maps
// (r, g, b) to the operands of rg.
func recolorGridPDF(levels, patch int, order func(r, g, b float64) (float64, float64, float64)) []byte {
	var body strings.Builder
	cols := levels * levels
	i := 0
	for b := range levels {
		for g := range levels {
			for r := range levels {
				step := float64(levels - 1)
				x, y, z := order(float64(r)/step, float64(g)/step, float64(b)/step)
				fmt.Fprintf(&body, "%.4f %.4f %.4f rg %d %d %d %d re f\n", x, y, z, (i%cols)*patch, (i/cols)*patch, patch, patch)
				i++
			}
		}
	}
	page := fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Contents 4 0 R >>", cols*patch, levels*patch)
	return recolorTestPDF(page, body.String())
}

// recolorTestPDF writes a one-page PDF of the page dictionary and content.
func recolorTestPDF(page, content string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		page,
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
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
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// renderConverted converts src to the built-in CMYK and renders page 1 of
// the source and the output at [renderDPI].
func renderConverted(t *testing.T, src []byte) (image.Image, image.Image) {
	t.Helper()
	ctx := context.Background()
	r, err := pdf.Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := pdf.NewWriter(r)
	defer w.Close()
	if _, err := w.ConvertColors(ctx, pdf.CMYKColorTarget()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatal(err)
	}
	after, err := pdf.Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	before, err := r.RenderPage(ctx, 1, pdf.WithRenderDPI(renderDPI))
	if err != nil {
		t.Fatal(err)
	}
	converted, err := after.RenderPage(ctx, 1, pdf.WithRenderDPI(renderDPI))
	if err != nil {
		t.Fatal(err)
	}
	return before, converted
}

func sameOrder(r, g, b float64) (float64, float64, float64) { return r, g, b }

// TestRecolorDeltaE_Derivation is the measurement [recolorDeltaE] rests on:
// a correct conversion of every colour of a 17-level sRGB grid to the
// built-in CMYK, drawn back by litho's renderer, moves none past the bar,
// sRGB blue the furthest, by 71.7.
func TestRecolorDeltaE_Derivation(t *testing.T) {
	t.Parallel()
	before, after := renderConverted(t, recolorGridPDF(17, 4, sameOrder))
	pixels, moved, drift, maxDE, size := compareRecolored(before, after)
	if size || moved != 0 || drift != 0 {
		t.Errorf("a correct conversion: %d of %d pixels moved, %d drift (max %.1f) past %d", moved, pixels, drift, maxDE, recolorDeltaE)
	}
}

// TestRecolorDeltaE_CatchesASwappedChannel plants a conversion that reads
// DeviceRGB as BGR: the page fails the colour check.
func TestRecolorDeltaE_CatchesASwappedChannel(t *testing.T) {
	t.Parallel()
	before, _ := renderConverted(t, recolorGridPDF(9, 4, sameOrder))
	_, swapped := renderConverted(t, recolorGridPDF(9, 4, func(r, g, b float64) (float64, float64, float64) { return b, g, r }))
	pixels, _, drift, maxDE, _ := compareRecolored(before, swapped)
	if !overShare(drift, pixels) {
		t.Errorf("a swapped channel drifts on %d of %d pixels (max %.1f), within the page's allowance", drift, pixels, maxDE)
	}
}
