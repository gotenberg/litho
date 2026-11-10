package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// TestStampPositionNamesRoundTrip pins that every position reads back from
// its name, and that the public positions list the internal ones in the same
// order, which [StampPosition.toMutations] relies on.
func TestStampPositionNamesRoundTrip(t *testing.T) {
	t.Parallel()

	internal := []mutations.StampPosition{
		mutations.PositionNone, mutations.PositionCenter,
		mutations.PositionTopLeft, mutations.PositionTop, mutations.PositionTopRight,
		mutations.PositionLeft, mutations.PositionRight,
		mutations.PositionBottomLeft, mutations.PositionBottom, mutations.PositionBottomRight,
	}
	if len(internal) != len(stampPositionNames) {
		t.Fatalf("%d internal positions, %d public ones", len(internal), len(stampPositionNames))
	}
	for i, name := range stampPositionNames {
		p, err := ParseStampPosition(name)
		if err != nil {
			t.Fatalf("ParseStampPosition(%q): %v", name, err)
		}
		if p.String() != name {
			t.Errorf("%q reads back as %q", name, p.String())
		}
		if p.toMutations() != internal[i] {
			t.Errorf("%q maps to internal position %d, want %d", name, p.toMutations(), internal[i])
		}
	}
	if _, err := ParseStampPosition("middle"); !errors.Is(err, ErrInvalidStampPosition) || !IsInvalidInput(err) {
		t.Errorf("ParseStampPosition(middle) = %v, want an invalid input wrapping %v", err, ErrInvalidStampPosition)
	}
}

// TestStampRefusesAnUnknownPosition pins that each stamp refuses a position
// outside the ones [StampPosition] lists when it is queued.
func TestStampRefusesAnUnknownPosition(t *testing.T) {
	t.Parallel()

	r := openBytes(t, positionPagesPDF(t, "/MediaBox [0 0 612 792]"))
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	bad := StampPosition(len(stampPositionNames))
	for name, err := range map[string]error{
		"StampText":  w.StampText(TextStamp{Text: "hi", Position: bad}),
		"StampImage": w.StampImage(ImageStamp{Data: redPNG(t), MimeType: "image/png", Position: bad}),
		"StampPDF":   w.StampPDF(PDFStamp{Source: r, Position: bad}),
	} {
		if !errors.Is(err, ErrInvalidStampPosition) {
			t.Errorf("%s = %v, want %v", name, err, ErrInvalidStampPosition)
		}
	}
}

// TestStampTextPlacesStandardFontInk pins that a text stamp in a standard
// font is placed by the ink of its glyphs, as the Adobe Font Metrics bound
// them. "HI" in Helvetica at 100 points inks from x = 7.7 (H's left bearing,
// 77) to 91 (I's right edge, 722 + 188) and from the baseline to 71.8 (cap
// height 718), so its first glyph starts 49.35 left of and 35.9 below the
// point the ink centers on.
func TestStampTextPlacesStandardFontInk(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		stamp        TextStamp
		wantX, wantY float64
	}{
		{
			name:  "center",
			stamp: TextStamp{Position: PositionCenter},
			wantX: 306 - 49.35, wantY: 396 - 35.9,
		},
		{
			name:  "top-right, moved in",
			stamp: TextStamp{Position: PositionTopRight, X: -10, Y: -20},
			wantX: 602 - 91, wantY: 772 - 71.8,
		},
		{
			name:  "bottom-left",
			stamp: TextStamp{Position: PositionBottomLeft},
			wantX: -7.7, wantY: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := openBytes(t, positionPagesPDF(t, "/MediaBox [0 0 612 792]"))
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			stamp := tc.stamp
			stamp.Text, stamp.FontSize = "HI", 100
			if err := w.StampText(stamp); err != nil {
				t.Fatalf("StampText: %v", err)
			}
			x, y := firstGlyphOf(t, writeAndReopen(t, w), "HI")
			if math.Abs(x-tc.wantX) > 0.01 || math.Abs(y-tc.wantY) > 0.01 {
				t.Errorf("the text starts at (%g, %g), want (%g, %g)", x, y, tc.wantX, tc.wantY)
			}
		})
	}
}

// TestStampPositionPlacesTheInkAsViewed renders each stamp kind on pages of
// several shapes and checks where its ink shows: against its position on the
// page a viewer sees, the same way up whatever the page's /Rotate, and inside
// the page at a corner whatever its rotation.
func TestStampPositionPlacesTheInkAsViewed(t *testing.T) {
	t.Parallel()

	font, err := os.ReadFile(filepath.Join("testdata", "fonts", "example_sans.ttf"))
	if err != nil {
		t.Fatalf("reading the example font: %v", err)
	}
	pageShapes := []struct {
		name    string
		entries string
		w, h    float64 // as viewed
	}{
		{"portrait", "/MediaBox [0 0 612 792]", 612, 792},
		{"landscape", "/MediaBox [0 0 792 612]", 792, 612},
		{"turned a quarter", "/MediaBox [0 0 612 792] /Rotate 90", 792, 612},
		{"away from the origin", "/MediaBox [100 200 712 992]", 612, 792},
	}
	type kind struct {
		name  string
		stamp func(t *testing.T, w *Writer, pos StampPosition, rotation, dx, dy float64) error
		// w and h size the stamp's box, in points.
		w, h float64
	}
	kinds := []kind{
		{
			name: "image",
			stamp: func(t *testing.T, w *Writer, pos StampPosition, rotation, dx, dy float64) error {
				return w.StampImage(ImageStamp{Data: redPNG(t), MimeType: "image/png", Width: 120, Height: 60, Position: pos, Rotation: rotation, X: dx, Y: dy})
			},
			w: 120, h: 60,
		},
		{
			name: "pdf",
			stamp: func(t *testing.T, w *Writer, pos StampPosition, rotation, dx, dy float64) error {
				src := openBytes(t, redPagePDF(120, 60))
				t.Cleanup(func() { _ = src.Close() })
				return w.StampPDF(PDFStamp{Source: src, Position: pos, Rotation: rotation, X: dx, Y: dy})
			},
			w: 120, h: 60,
		},
	}
	textStamp := func(t *testing.T, w *Writer, pos StampPosition, rotation float64) error {
		ttf, err := NewTTFFont(font)
		if err != nil {
			t.Fatalf("NewTTFFont: %v", err)
		}
		return w.StampText(TextStamp{Text: "DRAFT", Font: ttf, FontSize: 80, Color: [3]float64{1, 0, 0}, Position: pos, Rotation: rotation, X: 5, Y: -5})
	}

	for _, shape := range pageShapes {
		for _, k := range kinds {
			for _, rotation := range []float64{0, 30} {
				for _, pos := range []StampPosition{PositionCenter, PositionTopLeft, PositionBottomRight} {
					t.Run(fmt.Sprintf("%s/%s/%g/%s", shape.name, k.name, rotation, pos), func(t *testing.T) {
						t.Parallel()

						// The offset moves the stamp inward: past the edge, the
						// page would cut it off.
						dx, dy := 5.0, -5.0
						if pos == PositionBottomRight {
							dx, dy = -5, 5
						}
						got := stampedInk(t, shape.entries, func(w *Writer) error { return k.stamp(t, w, pos, rotation, dx, dy) })
						rad := rotation * math.Pi / 180
						bw := k.w*math.Abs(math.Cos(rad)) + k.h*math.Abs(math.Sin(rad))
						bh := k.w*math.Abs(math.Sin(rad)) + k.h*math.Abs(math.Cos(rad))
						ax, ay := anchorOf(pos)
						left := ax*(shape.w-bw) + dx
						bottom := ay*(shape.h-bh) + dy
						want := [4]float64{left, shape.h - bottom - bh, left + bw, shape.h - bottom}
						assertInk(t, got, want, 1.5)
					})
				}
			}
		}
		t.Run(shape.name+"/text centered", func(t *testing.T) {
			t.Parallel()

			for _, rotation := range []float64{0, 40} {
				got := stampedInk(t, shape.entries, func(w *Writer) error { return textStamp(t, w, PositionCenter, rotation) })
				cx, cy := (got[0]+got[2])/2, (got[1]+got[3])/2
				if math.Abs(cx-(shape.w/2+5)) > 1.5 || math.Abs(cy-(shape.h/2+5)) > 1.5 {
					t.Errorf("rotated %g: the text's ink centers on (%g, %g) in the image, want (%g, %g)", rotation, cx, cy, shape.w/2+5, shape.h/2+5)
				}
				if rotation == 0 && got[2]-got[0] < got[3]-got[1] {
					t.Errorf("the text's ink is %g wide and %g tall: it does not read the same way up", got[2]-got[0], got[3]-got[1])
				}
			}
		})
	}
}

// TestStampPositionPlacesEachPageOfAMixedDocument pins that one stamp call
// places its stamp on each page by that page's own size.
func TestStampPositionPlacesEachPageOfAMixedDocument(t *testing.T) {
	t.Parallel()

	r := openBytes(t, buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 842 595] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}))
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if err := w.StampImage(ImageStamp{Data: redPNG(t), MimeType: "image/png", Width: 100, Height: 50, Position: PositionCenter}); err != nil {
		t.Fatalf("StampImage: %v", err)
	}
	out := writeAndReopen(t, w)
	for page, size := range map[int][2]float64{1: {612, 792}, 2: {842, 595}, 3: {612, 792}} {
		got := renderedInk(t, out, page)
		want := [4]float64{(size[0] - 100) / 2, (size[1] - 50) / 2, (size[0] + 100) / 2, (size[1] + 50) / 2}
		assertInk(t, got, want, 1.5)
	}
}

// TestStampPositionReadsTheRotateAnEarlierEditStages pins that a placed stamp
// reads the page's /Rotate as the same write stages it: rotated a quarter by
// [Writer.RotatePages] first, the page shows 631.5 wide and 446.25 tall, and
// a stamp at its top-left corner shows there, the same way up. Read from the
// page as the source states it, the stamp landed in the top-right corner,
// turned.
func TestStampPositionReadsTheRotateAnEarlierEditStages(t *testing.T) {
	t.Parallel()

	r := openBytes(t, positionPagesPDF(t, "/MediaBox [0 0 446.25 631.5]"))
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if err := w.RotatePages(90); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}
	if err := w.StampImage(ImageStamp{Data: redPNG(t), MimeType: "image/png", Width: 100, Height: 50, Position: PositionTopLeft}); err != nil {
		t.Fatalf("StampImage: %v", err)
	}
	assertInk(t, renderedInk(t, writeAndReopen(t, w), 1), [4]float64{0, 0, 100, 50}, 1.5)
}

// anchorOf returns where pos sits along each axis of the page, as viewed: 0
// at the left or bottom edge, 0.5 in the middle, 1 at the right or top edge.
func anchorOf(pos StampPosition) (float64, float64) {
	switch pos {
	case PositionTopLeft:
		return 0, 1
	case PositionBottomRight:
		return 1, 0
	}
	return 0.5, 0.5
}

// stampedInk stamps a one-page document whose page states entries, renders
// the result, and returns where its red ink shows.
func stampedInk(t *testing.T, entries string, stamp func(*Writer) error) [4]float64 {
	t.Helper()

	r := openBytes(t, positionPagesPDF(t, entries))
	t.Cleanup(func() { _ = r.Close() })
	w := NewWriter(r)
	t.Cleanup(func() { _ = w.Close() })
	if err := stamp(w); err != nil {
		t.Fatalf("stamping: %v", err)
	}
	return renderedInk(t, writeAndReopen(t, w), 1)
}

// renderedInk renders page at 72 dpi, a pixel to the point, and returns the
// box its red pixels span: left, top, right and bottom in image coordinates,
// y down.
func renderedInk(t *testing.T, r *Reader, page int) [4]float64 {
	t.Helper()

	img, err := r.RenderPage(context.Background(), page, WithRenderDPI(72))
	if err != nil {
		t.Fatalf("RenderPage %d: %v", page, err)
	}
	b := img.Bounds()
	box := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			if cr > 0x8000 && cg < 0x8000 && cb < 0x8000 {
				box[0], box[1] = min(box[0], float64(x-b.Min.X)), min(box[1], float64(y-b.Min.Y))
				box[2], box[3] = max(box[2], float64(x-b.Min.X+1)), max(box[3], float64(y-b.Min.Y+1))
			}
		}
	}
	if math.IsInf(box[0], 1) {
		t.Fatalf("page %d shows no red ink", page)
	}
	return box
}

// assertInk fails unless got and want agree to within tol on every side.
func assertInk(t *testing.T, got, want [4]float64, tol float64) {
	t.Helper()

	for i := range got {
		if math.Abs(got[i]-want[i]) > tol {
			t.Errorf("the ink spans %v, want %v", got, want)
			return
		}
	}
}

// positionPagesPDF builds a one-page document whose page states entries.
func positionPagesPDF(t *testing.T, entries string) []byte {
	t.Helper()

	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R " + entries + " >>",
	})
}

// redPagePDF builds a one-page document whose w by h page is filled red.
func redPagePDF(w, h float64) []byte {
	content := fmt.Sprintf("1 0 0 rg 0 0 %g %g re f", w, h)
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %g %g] /Contents 4 0 R >>", w, h),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
}

// redPNG returns a 4 by 4 opaque red PNG.
func redPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			img.Set(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}
