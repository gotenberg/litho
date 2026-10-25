package pdf

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// stretchedSamplePDF returns a page drawing a one-sample grey image twice,
// each placement 200 points square, at (0,0) and (300,0): one sample stretched
// over each placement, its centre at (100,100) and (400,100).
func stretchedSamplePDF() []byte {
	const page = "q 200 0 0 200 0 0 cm /Im Do Q q 200 0 0 200 300 0 cm /Im Do Q"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 500 200] /Contents 4 0 R /Resources << /XObject << /Im 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x80\nendstream",
	})
}

// stretchedInlineSamplePDF returns the page of [stretchedSamplePDF] with each
// placement an inline image of its own: one grey sample stretched over each
// 200-point square, its centre at (100,100) and (400,100).
func stretchedInlineSamplePDF() []byte {
	const image = "BI /W 1 /H 1 /CS /G /BPC 8 ID \x80\nEI"
	const page = "q 200 0 0 200 0 0 cm " + image + " Q q 200 0 0 200 300 0 cm " + image + " Q"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 500 200] /Contents 4 0 R /Resources << >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
	})
}

// TestWriterRedactClearsARegionOverAnAlreadyBlankedInlineSample pins for
// inline images what [TestWriterRedactClearsARegionOverAnAlreadyBlankedSample]
// pins for an image XObject: a second redaction on the output of a first
// succeeds where the region meets only the inline image's sample the first
// blanked, and the inline image the first left as it was still refuses a
// region missing its centre.
func TestWriterRedactClearsARegionOverAnAlreadyBlankedInlineSample(t *testing.T) {
	t.Parallel()
	src := stretchedInlineSamplePDF()

	first, report, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: [4]float64{90, 90, 110, 110}}})
	if err != nil {
		t.Fatalf("first Redact: %v", err)
	}
	if len(report.BlankedImages) != 1 {
		t.Fatalf("first BlankedImages = %v, want the first inline image", report.BlankedImages)
	}

	second, report, err := redactWith(t, first, []RedactRegion{{Page: 1, Rect: [4]float64{10, 10, 40, 40}}})
	if err != nil {
		t.Fatalf("second Redact over the blanked inline sample: %v", err)
	}
	if len(report.BlankedImages) != 0 {
		t.Errorf("second Redact blanked %v, want nothing: the sample was blank", report.BlankedImages)
	}
	if got := renderedGray(t, second, 1, [2]float64{25, 25}); got > 8 {
		t.Errorf("covered point = %d, want the blank", got)
	}
	if got, want := renderedGray(t, second, 1, [2]float64{325, 25}), renderedGray(t, src, 1, [2]float64{325, 25}); got != want {
		t.Errorf("second placement = %d, want %d: no region met it", got, want)
	}

	_, _, err = redactWith(t, first, []RedactRegion{{Page: 1, Rect: [4]float64{310, 10, 340, 40}}})
	if !errors.Is(err, mutations.ErrImageRegionCoversNoSample) {
		t.Errorf("Redact over the grey inline image's corner = %v, want ErrImageRegionCoversNoSample", err)
	}
}

// TestWriterRedactClearsARegionOverAnAlreadyBlankedSample pins that a second
// redaction on the output of a first succeeds where the region meets only a
// sample the first blanked, stretched past its centre: the region shows the
// blank already. The placement the first left as it was still refuses a
// region missing its centre, since its sample shows picture there.
//
// # Corpus evidence
//
//   - govdocs thread_300_300873, thread_400_400390: a second redaction after a
//     p1-quarter refused on a 1 by 9004 background copy, LithoRedacted1, over
//     samples the first redaction had blanked: 52 of the 120 and 112 cells of
//     an 8 by 8 grid of page 1.
func TestWriterRedactClearsARegionOverAnAlreadyBlankedSample(t *testing.T) {
	t.Parallel()
	src := stretchedSamplePDF()

	// Over the first placement's centre: blanks its sample in a copy, the
	// image being painted at (300,0) too.
	first, report, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: [4]float64{90, 90, 110, 110}}})
	if err != nil {
		t.Fatalf("first Redact: %v", err)
	}
	if !slices.Equal(report.ImagesBlankedInCopy[1], []string{"Im"}) {
		t.Fatalf("first ImagesBlankedInCopy = %v, want [Im]", report.ImagesBlankedInCopy)
	}

	// The first placement's corner, far from its centre, on the blank.
	second, report, err := redactWith(t, first, []RedactRegion{{Page: 1, Rect: [4]float64{10, 10, 40, 40}}})
	if err != nil {
		t.Fatalf("second Redact over the blanked sample: %v", err)
	}
	if len(report.BlankedImages) != 0 || len(report.ImagesBlankedInCopy) != 0 {
		t.Errorf("second Redact blanked %v (in copy %v), want nothing: the sample was blank",
			report.BlankedImages, report.ImagesBlankedInCopy)
	}
	if got := renderedGray(t, second, 1, [2]float64{25, 25}); got > 8 {
		t.Errorf("covered point = %d, want the blank", got)
	}
	if got, want := renderedGray(t, second, 1, [2]float64{325, 25}), renderedGray(t, src, 1, [2]float64{325, 25}); got != want {
		t.Errorf("second placement = %d, want %d: no region met it", got, want)
	}

	// The second placement's corner still shows the grey picture.
	_, _, err = redactWith(t, first, []RedactRegion{{Page: 1, Rect: [4]float64{310, 10, 340, 40}}})
	if !errors.Is(err, mutations.ErrImageRegionCoversNoSample) {
		t.Errorf("Redact over the grey placement's corner = %v, want ErrImageRegionCoversNoSample", err)
	}
}

// halfBlankSamplePDF returns a page drawing a 1 by 2 grey image twice, each
// placement 200 points square, at (0,0) and (300,0): its top sample black,
// stretched over each placement's upper half, its bottom sample white over
// the lower half.
func halfBlankSamplePDF() []byte {
	const page = "q 200 0 0 200 0 0 cm /Im Do Q q 200 0 0 200 300 0 cm /Im Do Q"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 500 200] /Contents 4 0 R /Resources << /XObject << /Im 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 2 >>\nstream\n\x00\xff\nendstream",
	})
}

// TestWriterRedactLeavesAnAlreadyBlankPlacementOfAnImageBlankedInACopy pins
// that the check [Writer.WriteTo] runs for an image blanked in a copy reads a
// placement as Redact does. One region covers the first placement's white
// sample, which is blanked in a copy since the second placement keeps it; a
// second region meets the second placement over its black sample only, away
// from its centre, which Redact leaves painting the image as it is. The write
// check used to count that placement as the image painted again under a
// region, and refused a redaction Redact had found exact.
//
// # Corpus evidence
//
//   - pdfjs bug857031 p1-band: Im162 (object 11) is blanked in a copy at one
//     placement, and the band meets two others 0.012 points deep over samples
//     already black; Redact succeeded and its write refused.
func TestWriterRedactLeavesAnAlreadyBlankPlacementOfAnImageBlankedInACopy(t *testing.T) {
	t.Parallel()
	src := halfBlankSamplePDF()
	regions := []RedactRegion{
		{Page: 1, Rect: [4]float64{90, 40, 110, 60}},
		{Page: 1, Rect: [4]float64{310, 160, 340, 190}},
	}
	for _, opts := range [][]RedactOption{nil, {WithoutRedactionPixelCheck()}} {
		out, report, err := redactWith(t, src, regions, opts...)
		if err != nil {
			t.Fatalf("Redact(%d options): %v", len(opts), err)
		}
		if !slices.Equal(report.ImagesBlankedInCopy[1], []string{"Im"}) {
			t.Fatalf("ImagesBlankedInCopy = %v, want [Im]", report.ImagesBlankedInCopy)
		}
		if got := renderedGray(t, out, 1, [2]float64{100, 50}); got > 8 {
			t.Errorf("first placement's covered point = %d, want the blank", got)
		}
		if got := renderedGray(t, out, 1, [2]float64{325, 175}); got > 8 {
			t.Errorf("second placement's covered point = %d, want its black sample", got)
		}
		if got, want := renderedGray(t, out, 1, [2]float64{400, 50}), renderedGray(t, src, 1, [2]float64{400, 50}); got != want {
			t.Errorf("second placement's white sample = %d, want %d: no region met it", got, want)
		}
	}
}
