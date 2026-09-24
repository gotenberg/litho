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

// TestWriter_Redact_RefusesToBlankBeyondTheRegions pins that an image painted
// in several places is blanked only when every place had the same samples
// covered.
//
// Blanking rewrites the image object, so every placement of it shows the
// result. A region over one placement blanked the others too, which is content
// outside the region, and a logo on every page lost its copy on pages the
// redaction never named.
func TestWriter_Redact_RefusesToBlankBeyondTheRegions(t *testing.T) {
	t.Parallel()

	annotAppearance := "8 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 50 50] " +
		"/Resources << /XObject << /Im1 7 0 R >> >> /Length 28 >>\nstream\n" +
		"q 50 0 0 50 0 0 cm /Im1 Do Q\nendstream\nendobj\n" +
		"9 0 obj\n<< /Type /Annot /Subtype /Stamp /Rect [400 100 450 150] /AP << /N 8 0 R >> >>\nendobj\n"
	outline := "20 0 obj\n<< /Type /Outlines /First 21 0 R /Last 21 0 R /Count 1 >>\nendobj\n" +
		"21 0 obj\n<< /Title (Page one) /Parent 20 0 R /Dest [10 0 R /Fit] >>\nendobj\n"

	for _, tc := range []struct {
		name    string
		src     []byte
		regions []RedactRegion
		refused bool
	}{
		{
			name:    "one of two placements on a page",
			src:     imageDoc([]string{placeLeft + " " + placeRight}, "", "", ""),
			regions: []RedactRegion{{Page: 1, Rect: wholeLeft}},
			refused: true,
		},
		{
			name:    "both placements on a page",
			src:     imageDoc([]string{placeLeft + " " + placeRight}, "", "", ""),
			regions: []RedactRegion{{Page: 1, Rect: wholeLeft}, {Page: 1, Rect: wholeRight}},
		},
		{
			name:    "different parts of two placements",
			src:     imageDoc([]string{placeLeft + " " + placeRight}, "", "", ""),
			regions: []RedactRegion{{Page: 1, Rect: topLeft}, {Page: 1, Rect: wholeRight}},
			refused: true,
		},
		{
			name:    "one of two pages",
			src:     imageDoc([]string{placeLeft, placeLeft}, "", "", ""),
			regions: []RedactRegion{{Page: 1, Rect: wholeLeft}},
			refused: true,
		},
		{
			name:    "every page",
			src:     imageDoc([]string{placeLeft, placeLeft}, "", "", ""),
			regions: []RedactRegion{{Page: 1, Rect: wholeLeft}, {Page: 2, Rect: wholeLeft}},
		},
		{
			name:    "also painted by an annotation appearance",
			src:     imageDoc([]string{placeLeft}, "", "/Annots [9 0 R] ", annotAppearance),
			regions: []RedactRegion{{Page: 1, Rect: wholeLeft}},
			refused: true,
		},
		{
			// An outline pointing at the page is not a use of the image: the
			// page is walked from the page tree, not from its destinations.
			name:    "page named by an outline",
			src:     imageDoc([]string{placeLeft}, "/Outlines 20 0 R ", "", outline),
			regions: []RedactRegion{{Page: 1, Rect: wholeLeft}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := redactRegions(t, tc.src, tc.regions...)
			if tc.refused {
				if !errors.Is(err, ErrRedactionRefused) || !errors.Is(err, mutations.ErrImageShared) {
					t.Fatalf("Redact = %v, want ErrRedactionRefused wrapping ErrImageShared", err)
				}
				// A refusal leaves the Writer as it was, so the image survives
				// everywhere it was painted.
				if got := imageSamples(t, out, 1); !allSamples(got, 0x80) {
					t.Fatalf("samples after a refused redaction = %v, want all 0x80", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if got := imageSamples(t, out, 1); !allSamples(got, 0) {
				t.Fatalf("samples = %v, want all 0", got)
			}
		})
	}
}
