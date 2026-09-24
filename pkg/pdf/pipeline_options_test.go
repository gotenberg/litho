package pdf

import (
	"bytes"
	"context"
	"strconv"
	"testing"
)

// indexedCMYKPDF builds a one-page document showing a 2x1 indexed image over
// DeviceCMYK, both pixels full cyan, drawn over (50,600)-(250,700).
func indexedCMYKPDF() []byte {
	body := "q 200 0 0 100 50 600 cm /Im1 Do Q"
	data := string([]byte{0, 0})
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"5 0 obj\n<< /Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace [/Indexed /DeviceCMYK 0 <FF000000>]" +
		" /BitsPerComponent 8 /Length 2 >>\nstream\n" + data + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 6 /Root 1 0 R >>\n%%EOF\n")
}

// TestPipeline_ReopenKeepsTheReadingOptions pins that a stage after a
// pipeline reopens its intermediate file reads the document with the options
// the caller opened it with. Redaction re-encodes an indexed image as RGB, so
// the pixel it leaves shows which CMYK profile it was read in: the caller's
// profile paints it white, the built-in one cyan.
func TestPipeline_ReopenKeepsTheReadingOptions(t *testing.T) {
	t.Parallel()

	leftPixel := RedactRegion{Page: 1, Rect: [4]float64{60, 610, 140, 690}}
	redact := func(ctx context.Context, w *Writer) error {
		_, err := w.Redact(ctx, []RedactRegion{leftPixel})
		return err
	}
	for _, tc := range []struct {
		name  string
		split bool
	}{
		{"the reader the caller opened", false},
		{"the reader a split reopened", true},
	} {
		ctx := context.Background()
		src := indexedCMYKPDF()
		r, err := Open(ctx, bytes.NewReader(src), int64(len(src)), WithDefaultCMYKProfile(whiteProfile(t)))
		if err != nil {
			t.Fatalf("%s: Open: %v", tc.name, err)
		}
		p := NewPipeline().Open(r)
		if tc.split {
			p = p.Split([]int{1})
		}
		var out bytes.Buffer
		if _, err := p.Apply(redact).WriteTo(ctx, &out); err != nil {
			t.Fatalf("%s: WriteTo: %v", tc.name, err)
		}
		if err := r.Close(); err != nil {
			t.Errorf("%s: Close: %v", tc.name, err)
		}

		got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			t.Fatalf("%s: reopening the output: %v", tc.name, err)
		}
		img, err := got.DecodeImage(ctx, 1, "Im1")
		if err != nil {
			t.Fatalf("%s: DecodeImage: %v", tc.name, err)
		}
		if px := levels(img)[1]; px != [3]uint32{255, 255, 255} {
			t.Errorf("%s: the pixel left is %v, want white from the caller's profile", tc.name, px)
		}
	}
}
