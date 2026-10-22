package image

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// maskDoc builds a document whose object 1 is the mask stream dict over data,
// read under an image budget of maxPixels.
func maskDoc(t *testing.T, dict string, data []byte, maxPixels int64) (context.Context, *document.Document, ast.PdfStream) {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	off := buf.Len()
	fmt.Fprintf(&buf, "1 0 obj\n<< %s /Length %d >>\nstream\n%s\nendstream\nendobj\n", dict, len(data), data)
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 2\n0000000000 65535 f\r\n%010d 00000 n\r\n", off)
	fmt.Fprintf(&buf, "trailer\n<< /Size 2 >>\nstartxref\n%d\n%%%%EOF\n", xref)
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil,
		document.WithLimits(limits.Limits{MaxImagePixels: maxPixels}))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: 1})
	if err != nil {
		t.Fatalf("resolving the mask: %v", err)
	}
	return ctx, doc, stream
}

// TestDecodeMaskAt_WithinBudgetIsDecodeMask pins that a mask the image budget
// holds decodes exactly as DecodeMask decodes it, whatever grid is asked.
func TestDecodeMaskAt_WithinBudgetIsDecodeMask(t *testing.T) {
	t.Parallel()

	const dict = "/Type /XObject /Subtype /Image /Width 16 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 1"
	data := []byte{0xF0, 0x0F, 0xAA, 0x55}
	ctx, doc, stream := maskDoc(t, dict, data, 0)
	want, err := DecodeMask(ctx, doc, "M", stream)
	if err != nil {
		t.Fatalf("DecodeMask: %v", err)
	}
	got, err := DecodeMaskAt(ctx, doc, "M", stream, 2, 1)
	if err != nil {
		t.Fatalf("DecodeMaskAt: %v", err)
	}
	if got.Bounds() != want.Bounds() {
		t.Fatalf("bounds = %v, want %v", got.Bounds(), want.Bounds())
	}
	for y := range 2 {
		for x := range 16 {
			if g, w := grey(t, got, x, y), grey(t, want, x, y); g != w {
				t.Errorf("pixel (%d,%d) = %d, want %d", x, y, g, w)
			}
		}
	}
}

// TestDecodeMaskAt_PastBudgetAverages pins the reduced reading: a mask past
// the image budget is box-averaged onto the grid asked for rather than
// refused, so the shape it masks survives (ISO 32000-1, 11.6.5.3).
func TestDecodeMaskAt_PastBudgetAverages(t *testing.T) {
	t.Parallel()

	// 16x4 at one bit: left half opaque (1), right half transparent (0) on
	// the top two rows, and one 1 sample in eight on the bottom two rows.
	data := []byte{0xFF, 0x00, 0xFF, 0x00, 0x80, 0x80, 0x80, 0x80}
	const base = "/Type /XObject /Subtype /Image /Width 16 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 1"

	for _, tc := range []struct {
		name  string
		extra string
		want  [2][2]uint8
	}{
		{"as stored", "", [2][2]uint8{{255, 0}, {32, 32}}},
		{"under /Decode [1 0]", " /Decode [1 0]", [2][2]uint8{{0, 255}, {223, 223}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// A budget of 32 pixels refuses the 64-pixel mask whole.
			ctx, doc, stream := maskDoc(t, base+tc.extra, data, 32)
			if _, err := DecodeMask(ctx, doc, "M", stream); !errors.Is(err, ErrImageTooLarge) {
				t.Fatalf("control: DecodeMask = %v, want ErrImageTooLarge", err)
			}
			img, err := DecodeMaskAt(ctx, doc, "M", stream, 2, 2)
			if err != nil {
				t.Fatalf("DecodeMaskAt: %v", err)
			}
			if b := img.Bounds(); b != image.Rect(0, 0, 2, 2) {
				t.Fatalf("bounds = %v, want a 2x2 grid", b)
			}
			for y := range 2 {
				for x := range 2 {
					if v := grey(t, img, x, y); v != tc.want[y][x] {
						t.Errorf("box (%d,%d) = %d, want %d", x, y, v, tc.want[y][x])
					}
				}
			}
		})
	}
}

// TestDecodeMaskAt_GridStaysUnderBudget pins that the grid asked for is
// shrunk to the budget: a caller asking for the source size still gets a
// mask the budget holds.
func TestDecodeMaskAt_GridStaysUnderBudget(t *testing.T) {
	t.Parallel()

	const dict = "/Type /XObject /Subtype /Image /Width 16 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8"
	data := bytes.Repeat([]byte{200}, 64)
	ctx, doc, stream := maskDoc(t, dict, data, 20)
	img, err := DecodeMaskAt(ctx, doc, "M", stream, 16, 4)
	if err != nil {
		t.Fatalf("DecodeMaskAt: %v", err)
	}
	b := img.Bounds()
	if b.Dx()*b.Dy() > 20 {
		t.Fatalf("grid %v holds %d samples, past the budget of 20", b, b.Dx()*b.Dy())
	}
	if v := grey(t, img, 0, 0); v != 200 {
		t.Errorf("sample = %d, want 200", v)
	}
}

// TestDecodeMaskAt_ShortDataPaintsBlack pins that rows the data lacks read
// black, as [DecodeMask] paints them, and that data short of one row is
// refused.
func TestDecodeMaskAt_ShortDataPaintsBlack(t *testing.T) {
	t.Parallel()

	const dict = "/Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceGray /BitsPerComponent 8"
	ctx, doc, stream := maskDoc(t, dict, bytes.Repeat([]byte{255}, 16), 32)
	img, err := DecodeMaskAt(ctx, doc, "M", stream, 1, 4)
	if err != nil {
		t.Fatalf("DecodeMaskAt: %v", err)
	}
	for y, want := range []uint8{255, 0, 0, 0} {
		if v := grey(t, img, 0, y); v != want {
			t.Errorf("band %d = %d, want %d", y, v, want)
		}
	}

	ctx, doc, stream = maskDoc(t, dict, []byte{1, 2, 3}, 32)
	if _, err := DecodeMaskAt(ctx, doc, "M", stream, 1, 4); err == nil {
		t.Error("a mask short of one row decoded")
	}
}

// TestDecodeMaskAt_RefusesCodestreamMasks pins that a JPEG mask past the
// budget is refused, its codestream shaping samples the reduced reading
// cannot stride.
func TestDecodeMaskAt_RefusesCodestreamMasks(t *testing.T) {
	t.Parallel()

	const dict = "/Type /XObject /Subtype /Image /Width 16 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode"
	ctx, doc, stream := maskDoc(t, dict, []byte{0xFF, 0xD8}, 32)
	if _, err := DecodeMaskAt(ctx, doc, "M", stream, 2, 2); !errors.Is(err, ErrImageTooLarge) {
		t.Errorf("DecodeMaskAt = %v, want ErrImageTooLarge", err)
	}
}
