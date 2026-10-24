package pdf

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// imageDoc builds a document whose pages all name object 7, a 4x4 DeviceGray
// image with every sample at 0x80, as /Im1 through one shared resources
// dictionary. Each entry of contents is one page's content stream; pages are
// objects 10, 12, ... and their streams 11, 13, .... catalogExtra and
// firstPageExtra are spliced into the catalog and the first page, and extra
// holds any further objects they name.
func imageDoc(contents []string, catalogExtra, firstPageExtra, extra string) []byte {
	var kids, pages strings.Builder
	for i, body := range contents {
		pageNum, contentNum := 10+2*i, 11+2*i
		kids.WriteString(strconv.Itoa(pageNum) + " 0 R ")
		pageExtra := ""
		if i == 0 {
			pageExtra = firstPageExtra
		}
		pages.WriteString(strconv.Itoa(pageNum) + " 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources 5 0 R /Contents " + strconv.Itoa(contentNum) + " 0 R " + pageExtra + ">>\nendobj\n")
		pages.WriteString(strconv.Itoa(contentNum) + " 0 obj\n<< /Length " + strconv.Itoa(len(body)) +
			" >>\nstream\n" + body + "\nendstream\nendobj\n")
	}
	samples := string(bytes.Repeat([]byte{0x80}, 16))
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R " + catalogExtra + ">>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [" + kids.String() + "] /Count " + strconv.Itoa(len(contents)) + " >>\nendobj\n" +
		"5 0 obj\n<< /XObject << /Im1 7 0 R >> >>\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray " +
		"/BitsPerComponent 8 /Length 16 >>\nstream\n" + samples + "\nendstream\nendobj\n" +
		pages.String() + extra +
		"trailer\n<< /Size 40 /Root 1 0 R >>\n%%EOF\n")
}

// Placements of the 4x4 image: a 100pt square at x=100 or x=300, y=500. Its
// sample centres sit at y 587.5, 562.5, 537.5 and 512.5, top row first.
const (
	placeLeft  = "q 100 0 0 100 100 500 cm /Im1 Do Q"
	placeRight = "q 100 0 0 100 300 500 cm /Im1 Do Q"
)

var (
	wholeLeft  = [4]float64{90, 490, 210, 610}
	wholeRight = [4]float64{290, 490, 410, 610}
	topLeft    = [4]float64{90, 555, 210, 610}
	bottomLeft = [4]float64{90, 490, 210, 545}
)

// imageSamples decodes /Im1 on page and returns its gray samples, row by row.
func imageSamples(t *testing.T, pdf []byte, page int) []uint8 {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	img, err := r.DecodeImage(ctx, page, "Im1")
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	b := img.Bounds()
	var out []uint8
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			gray, _, _, _ := img.At(x, y).RGBA()
			out = append(out, uint8(gray>>8))
		}
	}
	return out
}

// redactRegions opens src, runs one Redact over regions and writes the result.
// A refused redaction is returned as the error with the unchanged document.
func redactRegions(t *testing.T, src []byte, regions ...RedactRegion) ([]byte, error) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	_, redactErr := w.Redact(ctx, regions)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes(), redactErr
}

func allSamples(samples []uint8, want uint8) bool {
	for _, s := range samples {
		if s != want {
			return false
		}
	}
	return len(samples) > 0
}

// TestWriter_Redact_SecondCallKeepsImageBlanking pins that blanking builds on
// an earlier blanking of the same image. Each call decoded the source image,
// so the second one wrote the samples the first had cleared back.
func TestWriter_Redact_SecondCallKeepsImageBlanking(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := imageDoc([]string{placeLeft}, "", "", "")
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	for _, rect := range [][4]float64{topLeft, bottomLeft} {
		if _, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: rect}}); err != nil {
			t.Fatalf("Redact %v: %v", rect, err)
		}
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := imageSamples(t, out.Bytes(), 1); !allSamples(got, 0) {
		t.Fatalf("samples after two halves were blanked = %v, want all 0", got)
	}
}

// TestWriter_Redact_NeverBlanksBeyondTheRegions pins that an image painted in
// several places is blanked in place only when every place had the same
// samples covered, and in a copy the covered placement alone paints
// otherwise. Under WithStrictRedaction a shared image refuses.
//
// Blanking rewrites the image object, so every placement of it shows the
// result. A region over one placement blanked the others too, which is content
// outside the region, and a logo on every page lost its copy on pages the
// redaction never named. Refusing instead blocked the redaction of every page
// carrying a logo the document repeats.
func TestWriter_Redact_NeverBlanksBeyondTheRegions(t *testing.T) {
	t.Parallel()

	annotAppearance := "8 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 50 50] " +
		"/Resources << /XObject << /Im1 7 0 R >> >> /Length 28 >>\nstream\n" +
		"q 50 0 0 50 0 0 cm /Im1 Do Q\nendstream\nendobj\n" +
		"9 0 obj\n<< /Type /Annot /Subtype /Stamp /Rect [400 100 450 150] /AP << /N 8 0 R >> >>\nendobj\n"
	outline := "20 0 obj\n<< /Type /Outlines /First 21 0 R /Last 21 0 R /Count 1 >>\nendobj\n" +
		"21 0 obj\n<< /Title (Page one) /Parent 20 0 R /Dest [10 0 R /Fit] >>\nendobj\n"

	// Sample points, page space, each the centre of one sample of a placement:
	// top and bottom rows of the left placement, and the right one.
	leftTop, leftBottom, right := [2]float64{112.5, 587.5}, [2]float64{112.5, 512.5}, [2]float64{312.5, 587.5}
	annot := [2]float64{425, 125}
	// A placement a region covers whole is removed, and the paper shows
	// where it was.
	const blank, kept, gone = 0, 0x80, 0xff

	type probe struct {
		page int
		at   [2]float64
		want uint8
	}
	for _, tc := range []struct {
		name    string
		src     []byte
		regions []RedactRegion
		// shared says the image is painted where the regions do not cover
		// the same samples: blanked in a copy, refused under strict.
		shared bool
		probes []probe
	}{
		{
			name:    "one of two placements on a page",
			src:     imageDoc([]string{placeLeft + " " + placeRight}, "", "", ""),
			regions: []RedactRegion{{Page: 1, Rect: wholeLeft}},
			shared:  true,
			probes:  []probe{{1, leftTop, gone}, {1, leftBottom, gone}, {1, right, kept}},
		},
		{
			name:    "both placements on a page",
			src:     imageDoc([]string{placeLeft + " " + placeRight}, "", "", ""),
			regions: []RedactRegion{{Page: 1, Rect: wholeLeft}, {Page: 1, Rect: wholeRight}},
			probes:  []probe{{1, leftTop, gone}, {1, right, gone}},
		},
		{
			name:    "different parts of two placements",
			src:     imageDoc([]string{placeLeft + " " + placeRight}, "", "", ""),
			regions: []RedactRegion{{Page: 1, Rect: topLeft}, {Page: 1, Rect: wholeRight}},
			shared:  true,
			probes:  []probe{{1, leftTop, blank}, {1, leftBottom, kept}, {1, right, gone}},
		},
		{
			name:    "one of two pages",
			src:     imageDoc([]string{placeLeft, placeLeft}, "", "", ""),
			regions: []RedactRegion{{Page: 1, Rect: wholeLeft}},
			shared:  true,
			probes:  []probe{{1, leftTop, gone}, {2, leftTop, kept}},
		},
		{
			name:    "every page",
			src:     imageDoc([]string{placeLeft, placeLeft}, "", "", ""),
			regions: []RedactRegion{{Page: 1, Rect: wholeLeft}, {Page: 2, Rect: wholeLeft}},
			probes:  []probe{{1, leftTop, gone}, {2, leftTop, gone}},
		},
		{
			name:    "also painted by an annotation appearance",
			src:     imageDoc([]string{placeLeft}, "", "/Annots [9 0 R] ", annotAppearance),
			regions: []RedactRegion{{Page: 1, Rect: wholeLeft}},
			shared:  true,
			probes:  []probe{{1, leftTop, gone}, {1, annot, kept}},
		},
		{
			// An outline pointing at the page is not a use of the image: the
			// page is walked from the page tree, not from its destinations.
			name:    "page named by an outline",
			src:     imageDoc([]string{placeLeft}, "/Outlines 20 0 R ", "", outline),
			regions: []RedactRegion{{Page: 1, Rect: wholeLeft}},
			probes:  []probe{{1, leftTop, gone}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, report, err := redactWith(t, tc.src, tc.regions)
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			for _, p := range tc.probes {
				if got := renderedGray(t, out, p.page, p.at); !nearGray(got, p.want) {
					t.Errorf("page %d at %v = %d, want %d", p.page, p.at, got, p.want)
				}
			}
			copied := report.ImagesBlankedInCopy[1]
			if tc.shared != (len(copied) > 0) {
				t.Errorf("ImagesBlankedInCopy = %v, shared %v", report.ImagesBlankedInCopy, tc.shared)
			}
			if tc.shared {
				// The image itself keeps its samples for its other placements.
				if got := imageSamples(t, out, 1); !allSamples(got, kept) {
					t.Errorf("/Im1 samples = %v, want all 0x80", got)
				}
			} else if got := imageSamples(t, out, 1); !allSamples(got, blank) {
				t.Errorf("/Im1 samples = %v, want all 0", got)
			}

			_, _, strictErr := redactWith(t, tc.src, tc.regions, WithStrictRedaction())
			if tc.shared != errors.Is(strictErr, mutations.ErrImageShared) {
				t.Fatalf("strict Redact = %v, shared %v", strictErr, tc.shared)
			}
			if tc.shared && !errors.Is(strictErr, ErrRedactionRefused) {
				t.Fatalf("strict Redact = %v, want ErrRedactionRefused", strictErr)
			}
		})
	}
}

// redactWith opens src, runs one Redact over regions with opts and writes the
// result. A refused redaction is returned as the error with the unchanged
// document.
func redactWith(t *testing.T, src []byte, regions []RedactRegion, opts ...RedactOption) ([]byte, *RedactReport, error) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	report, redactErr := w.Redact(ctx, regions, opts...)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes(), report, redactErr
}

// renderedGray renders page of pdf at 72 dpi and returns the red channel at
// the page-space point at, which on these gray images is the gray level.
func renderedGray(t *testing.T, pdf []byte, page int, at [2]float64) uint8 {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	img, err := r.RenderPage(ctx, page, WithRenderDPI(72))
	if err != nil {
		t.Fatalf("RenderPage %d: %v", page, err)
	}
	b := img.Bounds()
	red, _, _, _ := img.At(b.Min.X+int(at[0]), b.Min.Y+b.Dy()-1-int(at[1])).RGBA()
	return uint8(red >> 8)
}

// nearGray reports whether got is want, give or take what anti-aliasing and
// colour conversion move a flat area by.
func nearGray(got, want uint8) bool {
	const tolerance = 8
	return int(got) >= int(want)-tolerance && int(got) <= int(want)+tolerance
}
