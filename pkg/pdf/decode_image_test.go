package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"sync"
	"testing"
)

// imagePDF builds a one-page document whose page shows /Im0, a 4x2 grey image
// with samples 0, 40, ..., 280 mod 256 read row by row, and draws the form /Fm0,
// which holds /Im1, a 2x1 RGB image (red, blue), and its own /Im0, a 1x1 grey
// image of value 7, then the form /Fm1, which holds another /Im1, a 1x1 grey
// image of value 99. /Bad is an image behind a filter no decoder knows.
func imagePDF() []byte {
	grey := []byte{0, 40, 80, 120, 160, 200, 240, 24}
	rgb := []byte{255, 0, 0, 0, 0, 255}
	image := func(w, h int, cs, extra string, data []byte) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /%s /BitsPerComponent 8 %s /Length %d >>\nstream\n%s\nendstream",
			w, h, cs, extra, len(data), data)
	}
	const content = "q 8 0 0 4 0 0 cm /Im0 Do Q /Fm0 Do /Fm1 Do"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /XObject << /Im0 5 0 R /Fm0 6 0 R /Fm1 10 0 R /Bad 9 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		image(4, 2, "DeviceGray", "", grey),
		"<< /Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources << /XObject << /Im1 7 0 R /Im0 8 0 R >> >> /Length 0 >>\nstream\n\nendstream",
		image(2, 1, "DeviceRGB", "", rgb),
		image(1, 1, "DeviceGray", "", []byte{7}),
		image(1, 1, "DeviceGray", "/Filter /NoSuchDecode", []byte{7}),
		"<< /Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources << /XObject << /Im1 11 0 R >> >> /Length 0 >>\nstream\n\nendstream",
		image(1, 1, "DeviceGray", "", []byte{99}),
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

// openImagePDF opens imagePDF.
func openImagePDF(t *testing.T) *Reader {
	t.Helper()
	data := imagePDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Logf("closing reader: %v", err)
		}
	})
	return r
}

// levels returns the 8-bit red, green and blue of every pixel, row by row.
func levels(img image.Image) [][3]uint32 {
	var out [][3]uint32
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			out = append(out, [3]uint32{r >> 8, g >> 8, bl >> 8})
		}
	}
	return out
}

// TestDecodeImage decodes the page's own image and one drawn through a form,
// each by the name ExtractImages reports. The page's /Im0 wins over the
// form's: a name the page's own resources hold names that image. Of the two
// forms' /Im1, the first the listing reaches, /Fm0's, is decoded.
func TestDecodeImage(t *testing.T) {
	t.Parallel()

	r := openImagePDF(t)
	ctx := context.Background()
	metas, err := r.ExtractImages(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractImages: %v", err)
	}
	listed := map[string]bool{}
	for _, m := range metas {
		listed[m.Filename] = true
	}
	for _, name := range []string{"Im0", "Im1"} {
		if !listed[name] {
			t.Errorf("ExtractImages does not list %s", name)
		}
	}

	img, err := r.DecodeImage(ctx, 1, "Im0")
	if err != nil {
		t.Fatalf("DecodeImage(Im0): %v", err)
	}
	var want [][3]uint32
	for _, v := range []uint32{0, 40, 80, 120, 160, 200, 240, 24} {
		want = append(want, [3]uint32{v, v, v})
	}
	if got := levels(img); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Im0 = %v, want %v", got, want)
	}

	img, err = r.DecodeImage(ctx, 1, "Im1")
	if err != nil {
		t.Fatalf("DecodeImage(Im1): %v", err)
	}
	if got, want := levels(img), [][3]uint32{{255, 0, 0}, {0, 0, 255}}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Im1 = %v, want %v", got, want)
	}
}

// TestImageMetaDecode decodes every entry ExtractImages lists through its own
// Decode function. The page shows two images named /Im0 and two named /Im1,
// and each entry decodes to its own pixels, not to the first image of its
// name.
func TestImageMetaDecode(t *testing.T) {
	t.Parallel()

	r := openImagePDF(t)
	ctx := context.Background()
	metas, err := r.ExtractImages(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractImages: %v", err)
	}
	got := map[string]string{}
	for _, m := range metas {
		img, err := m.Decode(ctx)
		if m.Filename == "Bad" {
			if !errors.Is(err, ErrImageNotDecodable) {
				t.Errorf("Bad: Decode = %v, want %v", err, ErrImageNotDecodable)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: Decode: %v", m.Filename, err)
			continue
		}
		if b := img.Bounds(); b.Dx() != m.Width || b.Dy() != m.Height {
			t.Errorf("%s: decoded %dx%d, listed %dx%d", m.Filename, b.Dx(), b.Dy(), m.Width, m.Height)
		}
		got[fmt.Sprintf("%s %dx%d", m.Filename, m.Width, m.Height)] = fmt.Sprint(levels(img)[0])
	}
	want := map[string]string{
		"Im0 4x2": "[0 0 0]",
		"Im0 1x1": "[7 7 7]",
		"Im1 2x1": "[255 0 0]",
		"Im1 1x1": "[99 99 99]",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("decoded first pixels = %v, want %v", got, want)
	}
}

// TestDecodeImage_Errors names each failure with its sentinel: a page out of
// range, a name the page does not show, and an image behind a filter no
// decoder knows.
func TestDecodeImage_Errors(t *testing.T) {
	t.Parallel()

	r := openImagePDF(t)
	for _, tc := range []struct {
		page int
		name string
		want error
	}{
		{2, "Im0", ErrInvalidPageNumber},
		{1, "Nope", ErrImageNotFound},
		{1, "Bad", ErrImageNotDecodable},
	} {
		if _, err := r.DecodeImage(context.Background(), tc.page, tc.name); !errors.Is(err, tc.want) {
			t.Errorf("DecodeImage(%d, %s) = %v, want %v", tc.page, tc.name, err, tc.want)
		}
	}
}

// TestDecodeImage_Concurrent decodes the same images from many goroutines at
// once, which -race checks against the Reader's concurrency contract.
func TestDecodeImage_Concurrent(t *testing.T) {
	t.Parallel()

	r := openImagePDF(t)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for range 16 {
		for _, name := range []string{"Im0", "Im1"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := r.DecodeImage(context.Background(), 1, name); err != nil {
					errs <- err
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
