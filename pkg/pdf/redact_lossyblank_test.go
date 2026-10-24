package pdf

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	"github.com/gotenberg/litho/pkg/internal/imagetiles"
)

// lossyBlankSide is the side, in samples, of [jpegPhotoPDF]'s photograph:
// five cells of the tile grid, so a centred region leaves a ring of cells
// no region reaches.
const lossyBlankSide = 5 * imagetiles.Size

// lossyBlankRegion is the region the tests redact, in the default user space
// of [jpegPhotoPDF]'s 200 by 200 page, which the photograph fills.
var lossyBlankRegion = [4]float64{80, 80, 120, 120}

// photoSamples returns side x side RGB samples of smooth waves under a fine
// grain, which the tile grid reads as a photograph.
func photoSamples(side int) []byte {
	samples := make([]byte, 0, 3*side*side)
	state := uint32(1)
	grain := func() float64 {
		state = state*1664525 + 1013904223
		return float64(state>>28) - 8
	}
	for y := range side {
		for x := range side {
			fx, fy := float64(x), float64(y)
			samples = append(samples,
				byte(128+90*math.Sin(fx/17)+grain()),
				byte(128+90*math.Cos(fy/13)+grain()),
				byte(128+90*math.Sin((fx+fy)/23)+grain()))
		}
	}
	return samples
}

// jpegPhotoPDF builds a page a JPEG photograph fills, drawn by the page
// itself, with extra entries on the image dictionary.
func jpegPhotoPDF(t *testing.T, extra string, objs ...string) []byte {
	t.Helper()
	data, err := dct.Encode(context.Background(), photoSamples(lossyBlankSide), lossyBlankSide, lossyBlankSide, 3, dct.EncodeOptions{Quality: 90})
	if err != nil {
		t.Fatal(err)
	}
	const page = "q 200 0 0 200 0 0 cm /Im Do Q"
	return buildPDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /XObject << /Im 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode%s /Length %d >>\nstream\n%s\nendstream",
			lossyBlankSide, lossyBlankSide, extra, len(data), data),
	}, objs...))
}

// redactBytes redacts region on page 1 of data with opts and returns the
// output and the report.
func redactBytes(t *testing.T, data []byte, region [4]float64, opts ...RedactOption) ([]byte, *RedactReport) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	report, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: region}}, opts...)
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes(), report
}

// drawnPicture returns the samples the page's /Im draws in data, composed
// from the images of the form a lossy blanking wrote, or read from the image
// itself, and the pixels of each JPEG tile.
func drawnPicture(t *testing.T, data []byte) ([]byte, []image.Rectangle) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pages, err := r.resolvePages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	xobjects, err := document.ResolveDict(ctx, r.doc, pages[0].Resources["XObject"])
	if err != nil {
		t.Fatal(err)
	}
	stream, err := document.ResolveStream(ctx, r.doc, xobjects["Im"])
	if err != nil {
		t.Fatal(err)
	}
	decode := func(s ast.PdfStream) []byte {
		samples, err := r.doc.DecodeStream(ctx, s)
		if err != nil {
			t.Fatal(err)
		}
		return samples
	}
	if stream.Dict["Subtype"] == ast.PdfName("Image") {
		return decode(stream), nil
	}
	formRes, err := document.ResolveDict(ctx, r.doc, stream.Dict["Resources"])
	if err != nil {
		t.Fatal(err)
	}
	images, err := document.ResolveDict(ctx, r.doc, formRes["XObject"])
	if err != nil {
		t.Fatal(err)
	}
	under, err := document.ResolveStream(ctx, r.doc, images["U"])
	if err != nil {
		t.Fatal(err)
	}
	picture := decode(under)
	if len(picture) != 3*lossyBlankSide*lossyBlankSide {
		t.Fatalf("underlay holds %d bytes", len(picture))
	}
	var jpegs []image.Rectangle
	for name, ref := range images {
		if name == "U" {
			continue
		}
		place, err := document.ResolveStream(ctx, r.doc, ref)
		if err != nil {
			t.Fatal(err)
		}
		m, _ := place.Dict["Matrix"].(ast.PdfArray)
		num := func(i int) float64 {
			return document.Float(m[i])
		}
		placeRes, err := document.ResolveDict(ctx, r.doc, place.Dict["Resources"])
		if err != nil {
			t.Fatal(err)
		}
		inner, err := document.ResolveDict(ctx, r.doc, placeRes["XObject"])
		if err != nil {
			t.Fatal(err)
		}
		tile, err := document.ResolveStream(ctx, r.doc, inner["I"])
		if err != nil {
			t.Fatal(err)
		}
		tw, _ := tile.Dict["Width"].(ast.PdfInt)
		th, _ := tile.Dict["Height"].(ast.PdfInt)
		x0 := int(math.Round(num(4) * lossyBlankSide))
		y1 := lossyBlankSide - int(math.Round(num(5)*lossyBlankSide))
		px := image.Rect(x0, y1-int(th), x0+int(tw), y1)
		if tile.Dict["Filter"] == ast.PdfName("DCTDecode") {
			jpegs = append(jpegs, px)
		}
		samples := decode(tile)
		for y := px.Min.Y; y < px.Max.Y; y++ {
			copy(picture[3*(y*lossyBlankSide+px.Min.X):], samples[3*(y-px.Min.Y)*int(tw):3*(y-px.Min.Y+1)*int(tw)])
		}
	}
	return picture, jpegs
}

// coveredSample reports whether the sample at (x, y) of [jpegPhotoPDF]'s
// photograph has its centre inside [lossyBlankRegion].
func coveredSample(x, y int) bool {
	ux := 200 * (float64(x) + 0.5) / lossyBlankSide
	uy := 200 * (1 - (float64(y)+0.5)/lossyBlankSide)
	r := lossyBlankRegion
	return ux >= r[0] && ux <= r[2] && uy >= r[1] && uy <= r[3]
}

// TestWriterRedactJPEGQualityBlanksAPhotographLossily pins
// [WithRedactionJPEGQuality] on a blanked JPEG photograph: the picture is
// redrawn with JPEG tiles, smaller than the lossless blanking, every covered
// sample is the blank exactly as the page draws the picture, no JPEG tile
// holds a sample within two of a covered one, and the samples outside the
// JPEG tiles are those of the lossless blanking.
func TestWriterRedactJPEGQualityBlanksAPhotographLossily(t *testing.T) {
	t.Parallel()
	in := jpegPhotoPDF(t, "")
	lossless, plain := redactBytes(t, in, lossyBlankRegion)
	if plain.LossyBlankedImages != 0 || bytes.Contains(lossless, []byte("/DCTDecode")) {
		t.Fatalf("the default blanking wrote a JPEG (%d lossy)", plain.LossyBlankedImages)
	}
	lossy, report := redactBytes(t, in, lossyBlankRegion, WithRedactionJPEGQuality(75))
	if report.LossyBlankedImages != 1 {
		t.Fatalf("LossyBlankedImages = %d, want 1", report.LossyBlankedImages)
	}
	if len(lossy) >= len(lossless) {
		t.Errorf("lossy output %d bytes, lossless %d", len(lossy), len(lossless))
	}

	want, _ := drawnPicture(t, lossless)
	got, jpegs := drawnPicture(t, lossy)
	if len(jpegs) == 0 {
		t.Fatal("no JPEG tile")
	}
	covered := 0
	for y := range lossyBlankSide {
		for x := range lossyBlankSide {
			i := 3 * (y*lossyBlankSide + x)
			inJPEG := false
			for _, j := range jpegs {
				if (image.Point{X: x, Y: y}).In(j) {
					inJPEG = true
				}
			}
			if coveredSample(x, y) {
				covered++
				if !bytes.Equal(got[i:i+3], []byte{0, 0, 0}) {
					t.Fatalf("covered sample (%d, %d) draws %v, want the blank", x, y, got[i:i+3])
				}
				for _, j := range jpegs {
					if (image.Point{X: x, Y: y}).In(j.Inset(-imagetiles.UnderlayBand)) {
						t.Fatalf("covered sample (%d, %d) lies within %d samples of the JPEG tile %v", x, y, imagetiles.UnderlayBand, j)
					}
				}
				continue
			}
			if !inJPEG && !bytes.Equal(got[i:i+3], want[i:i+3]) {
				t.Fatalf("sample (%d, %d) outside every JPEG tile draws %v, the lossless blanking %v", x, y, got[i:i+3], want[i:i+3])
			}
		}
	}
	if covered == 0 {
		t.Fatal("the region covers no sample")
	}
}

// TestWriterRedactJPEGQualityBlanksACopyLossily pins a photograph also
// painted where no region reaches: its covered placement draws a blanked
// copy, which is re-encoded, and the image itself keeps its JPEG data.
func TestWriterRedactJPEGQualityBlanksACopyLossily(t *testing.T) {
	t.Parallel()
	in := jpegPhotoPDF(t, "")
	// A second, small placement in the page's corner, which the region
	// misses.
	in = bytes.Replace(in, []byte("q 200 0 0 200 0 0 cm /Im Do Q"), []byte("q 200 0 0 200 0 0 cm /Im Do Q q 9 0 0 9 0 0 cm /Im Do Q"), 1)
	in = bytes.Replace(in, []byte("/Length 29 >>"), []byte("/Length 56 >>"), 1)
	out, report := redactBytes(t, in, lossyBlankRegion, WithRedactionJPEGQuality(75))
	if report.LossyBlankedImages != 1 || len(report.ImagesBlankedInCopy[1]) != 1 {
		t.Fatalf("LossyBlankedImages = %d, ImagesBlankedInCopy = %v, want one lossy copy", report.LossyBlankedImages, report.ImagesBlankedInCopy)
	}
	picture, jpegs := drawnPicture(t, out)
	if jpegs != nil || len(picture) != 3*lossyBlankSide*lossyBlankSide {
		t.Errorf("the page's /Im is no longer the source image")
	}
}

// TestWriterRedactJPEGQualityKeepsSomeBlankedImagesLossless pins the
// pictures [WithRedactionJPEGQuality] leaves one lossless image: one the
// source held losslessly, and one with a soft mask.
func TestWriterRedactJPEGQualityKeepsSomeBlankedImagesLossless(t *testing.T) {
	t.Parallel()
	mask := bytes.Repeat([]byte{0xc0}, 16)
	cases := map[string][]byte{
		"soft mask": jpegPhotoPDF(t, " /SMask 6 0 R",
			fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length %d >>\nstream\n%s\nendstream", len(mask), mask)),
		"lossless source": photoFormPDF(),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, report := redactBytes(t, in, lossyBlankRegion, WithRedactionJPEGQuality(75))
			if report.LossyBlankedImages != 0 || len(report.BlankedImages) == 0 {
				t.Errorf("LossyBlankedImages = %d of %d blanked, want 0", report.LossyBlankedImages, len(report.BlankedImages))
			}
			if bytes.Contains(out, []byte("/DCTDecode")) {
				t.Error("the output holds a JPEG")
			}
		})
	}
}

// TestWriterRedactAgainOverALossyBlanking pins that a second redaction, on
// the Writer and on the written file, blanks a lossily blanked picture again
// where its JPEG tiles lie, as a page draws it: their samples are images the
// form draws, which the redaction blanks as any.
func TestWriterRedactAgainOverALossyBlanking(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := jpegPhotoPDF(t, "")
	corner := [4]float64{10, 10, 30, 30}
	check := func(t *testing.T, out []byte) {
		t.Helper()
		r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		img, err := r.RenderPage(ctx, 1, WithRenderDPI(72))
		if err != nil {
			t.Fatal(err)
		}
		for _, region := range [][4]float64{lossyBlankRegion, corner} {
			// The page is 200 by 200 at 72 dpi, its y axis down; two pixels in
			// from each edge, where no sample the region does not cover
			// reaches.
			for y := int(200-region[3]) + 2; y < int(200-region[1])-2; y++ {
				for x := int(region[0]) + 2; x < int(region[2])-2; x++ {
					cr, cg, cb, _ := img.At(x, y).RGBA()
					if max(cr, cg, cb) > 8<<8 {
						t.Fatalf("pixel (%d, %d) inside %v draws %v, want the blank", x, y, region, img.At(x, y))
					}
				}
			}
		}
	}

	t.Run("same writer", func(t *testing.T) {
		t.Parallel()
		r, err := Open(ctx, bytes.NewReader(in), int64(len(in)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		w := NewWriter(r)
		defer w.Close()
		for _, region := range [][4]float64{lossyBlankRegion, corner} {
			if _, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: region}}, WithRedactionJPEGQuality(75)); err != nil {
				t.Fatalf("Redact %v: %v", region, err)
			}
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatal(err)
		}
		check(t, out.Bytes())
	})
	t.Run("written file", func(t *testing.T) {
		t.Parallel()
		first, _ := redactBytes(t, in, lossyBlankRegion, WithRedactionJPEGQuality(75))
		second, _ := redactBytes(t, first, corner, WithRedactionJPEGQuality(75))
		check(t, second)
	})
}
