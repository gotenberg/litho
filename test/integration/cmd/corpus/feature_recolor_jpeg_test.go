package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// recolorJPEGPDF draws one 120 x 80 RGB JPEG of colour gradients over a
// 200 x 200 page.
func recolorJPEGPDF(t *testing.T) []byte {
	t.Helper()
	const w, h = 120, 80
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: uint8(255 - x*255/w), A: 255})
		}
	}
	var data bytes.Buffer
	if err := jpeg.Encode(&data, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	content := "q 120 0 0 80 40 60 cm /Im0 Do Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /XObject << /Im0 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream",
			w, h, data.Len(), data.String()),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, 0, len(objects))
	for i, obj := range objects {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// runRecolorPDF runs the feature on the PDF data with convert planted, nil
// for the real conversion, and scores it.
func runRecolorPDF(t *testing.T, data []byte, convert func(context.Context, *pdf.Writer, ...pdf.ColorOption) (*pdf.ColorReport, error)) Verdict {
	t.Helper()
	path := filepath.Join(t.TempDir(), "in.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f := newRecolorFeature()
	if convert != nil {
		f.convert = convert
	}
	out, err := f.Run(context.Background(), path, "in")
	if err != nil {
		t.Fatal(err)
	}
	return f.Compare("in", out)
}

// convertCMYK is the cell's own conversion.
func convertCMYK(ctx context.Context, w *pdf.Writer, opts ...pdf.ColorOption) (*pdf.ColorReport, error) {
	return w.ConvertColors(ctx, pdf.CMYKColorTarget(), opts...)
}

// TestRecolorFeature_JPEGRun pins the JPEG run end to end: a JPEG source
// re-encodes smaller and passes, a JPEG run that stamps, converts to RGB or
// fails fails its own metric and the PDF while the lossless metrics stay
// clean, and a PDF whose conversion converts no image has no JPEG run.
func TestRecolorFeature_JPEGRun(t *testing.T) {
	t.Parallel()
	src := recolorJPEGPDF(t)

	v := runRecolorPDF(t, src, nil)
	if !v.Match || v.Metrics[metricJPEGFiles] != 1 || v.Metrics[metricJPEGChecked] != 1 || v.Metrics[metricJPEGGrew] != 0 {
		t.Errorf("JPEG control: %+v; %s", v, mismatchText(v))
	}

	// Each plant changes the JPEG run alone: the lossless run is the real
	// conversion.
	plant := func(jpegRun func(ctx context.Context, w *pdf.Writer, opts ...pdf.ColorOption) (*pdf.ColorReport, error)) func(context.Context, *pdf.Writer, ...pdf.ColorOption) (*pdf.ColorReport, error) {
		return func(ctx context.Context, w *pdf.Writer, opts ...pdf.ColorOption) (*pdf.ColorReport, error) {
			if len(opts) == 0 {
				return convertCMYK(ctx, w)
			}
			return jpegRun(ctx, w, opts...)
		}
	}
	for _, tc := range []struct {
		name   string
		run    func(ctx context.Context, w *pdf.Writer, opts ...pdf.ColorOption) (*pdf.ColorReport, error)
		metric string
	}{
		{"stamp", func(ctx context.Context, w *pdf.Writer, opts ...pdf.ColorOption) (*pdf.ColorReport, error) {
			if err := w.StampText(pdf.TextStamp{Text: "PLANTED PLANTED", FontSize: 40, Opacity: 1}); err != nil {
				return nil, err
			}
			return convertCMYK(ctx, w, opts...)
		}, metricJPEGInkMoved},
		{"rgb", func(ctx context.Context, w *pdf.Writer, opts ...pdf.ColorOption) (*pdf.ColorReport, error) {
			return w.ConvertColors(ctx, pdf.RGBColorTarget(), opts...)
		}, metricJPEGOutOfTarget},
		{"failure", func(context.Context, *pdf.Writer, ...pdf.ColorOption) (*pdf.ColorReport, error) {
			return nil, errors.New("planted failure")
		}, metricJPEGFailed},
	} {
		v := runRecolorPDF(t, src, plant(tc.run))
		if v.Match || v.Metrics[tc.metric] != 1 {
			t.Errorf("%s: match %v, %s %d; %s", tc.name, v.Match, tc.metric, v.Metrics[tc.metric], mismatchText(v))
		}
		for _, m := range []string{metricRecolorFailed, metricOutOfTarget, metricInkMoved, metricColourDrift} {
			if v.Metrics[m] != 0 {
				t.Errorf("%s: the lossless %s is %d", tc.name, m, v.Metrics[m])
			}
		}
	}

	if v := runRecolor(t, "0 1 1 0 k 50 50 100 100 re f", convertCMYK); !v.Match || v.Metrics[metricJPEGFiles] != 0 || v.Metrics[metricJPEGChecked] != 0 {
		t.Errorf("no image: %+v", v)
	}
}

// TestCountJPEGRun_Grew pins that a JPEG output larger than the lossless one
// fails the run.
func TestCountJPEGRun_Grew(t *testing.T) {
	t.Parallel()
	m := map[string]int{}
	run := &recolorJPEGRun{Size: 2048, LosslessSize: 1024, Reencoded: true}
	countJPEGRun(run, m)
	if !run.failed() || m[metricJPEGGrew] != 1 || m[metricJPEGSavedKiB] != 0 {
		t.Errorf("grew: failed %v, metrics %v", run.failed(), m)
	}
	m = map[string]int{}
	run = &recolorJPEGRun{Size: 1024, LosslessSize: 4096, Reencoded: true}
	countJPEGRun(run, m)
	if run.failed() || m[metricJPEGGrew] != 0 || m[metricJPEGSavedKiB] != 3 || m[metricJPEGFiles] != 1 {
		t.Errorf("shrank: failed %v, metrics %v", run.failed(), m)
	}
}
