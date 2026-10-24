package pdf

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"testing"
)

// photoFormPDF builds a page a form fills with a 208 by 208 RGB image of
// smooth waves under a fine grain, which the fallback's composite holds as a
// photograph.
func photoFormPDF() []byte {
	const side = 208
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
	data := hex.EncodeToString(samples) + ">"
	const form = "q 200 0 0 200 0 0 cm /Im Do Q"
	const page = "/Fm0 Do"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /XObject << /Fm0 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 200] /Resources << /XObject << /Im 6 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(form), form),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /ASCIIHexDecode /Length %d >>\nstream\n%s\nendstream", side, side, len(data), data),
	})
}

// redactPhotoForm redacts the centre of [photoFormPDF] with opts and returns
// the output.
func redactPhotoForm(t *testing.T, opts ...RedactOption) ([]byte, error) {
	t.Helper()
	ctx := context.Background()
	data := photoFormPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if _, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{80, 80, 120, 120}}}, opts...); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes(), nil
}

// TestWriterRedactJPEGQuality pins [WithRedactionJPEGQuality]: the fallback's
// images of a photograph a form draws are encoded as JPEG in part, smaller
// than the lossless default, which writes none.
func TestWriterRedactJPEGQuality(t *testing.T) {
	t.Parallel()
	lossless, err := redactPhotoForm(t, WithRedactionRasterFallback())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(lossless, []byte("/DCTDecode")) {
		t.Error("the default fallback wrote a JPEG")
	}
	jpeg, err := redactPhotoForm(t, WithRedactionRasterFallback(), WithRedactionJPEGQuality(75))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(jpeg, []byte("/DCTDecode")) {
		t.Error("no image was encoded as JPEG")
	}
	if len(jpeg) >= len(lossless) {
		t.Errorf("the JPEG output takes %d bytes, the lossless one %d", len(jpeg), len(lossless))
	}
}

// TestWriterRedactJPEGQualityOutOfRange pins [ErrInvalidRedactOptions] for a
// quality outside 1 to 100.
func TestWriterRedactJPEGQualityOutOfRange(t *testing.T) {
	t.Parallel()
	for _, q := range []int{0, -1, 101} {
		if _, err := redactPhotoForm(t, WithRedactionRasterFallback(), WithRedactionJPEGQuality(q)); !errors.Is(err, ErrInvalidRedactOptions) {
			t.Errorf("quality %d: err = %v, want ErrInvalidRedactOptions", q, err)
		}
	}
}
