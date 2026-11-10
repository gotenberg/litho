package mutations

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestStampPlacementMatrixPlacesTheBoxAsViewed pins that a placed stamp's box,
// moved by [stampPlacement.matrix] and then shown as a viewer shows the page
// ([viewedPageMatrix]), lands against its position: whatever the page's box
// origin and /Rotate, the stamp is placed on the page a viewer sees.
func TestStampPlacementMatrixPlacesTheBoxAsViewed(t *testing.T) {
	t.Parallel()

	box := func(v ...float64) ast.PdfArray {
		arr := make(ast.PdfArray, len(v))
		for i, x := range v {
			arr[i] = ast.PdfReal(x)
		}
		return arr
	}
	for _, tc := range []struct {
		name    string
		media   ast.PdfArray
		rotate  int
		place   stampPlacement
		wantBox [4]float64 // as viewed: llx, lly, urx, ury
	}{
		{
			name:    "center of a portrait page",
			media:   box(0, 0, 612, 792),
			place:   stampPlacement{position: PositionCenter, box: [4]float64{0, 0, 100, 50}},
			wantBox: [4]float64{256, 371, 356, 421},
		},
		{
			name:    "top-left of a box away from the origin, offset",
			media:   box(100, 200, 712, 992),
			place:   stampPlacement{position: PositionTopLeft, box: [4]float64{0, 0, 100, 50}, dx: 10, dy: -20},
			wantBox: [4]float64{10, 722, 110, 772},
		},
		{
			name:    "bottom-right of a page turned a quarter",
			media:   box(0, 0, 612, 792),
			rotate:  90,
			place:   stampPlacement{position: PositionBottomRight, box: [4]float64{0, 0, 100, 50}},
			wantBox: [4]float64{692, 0, 792, 50},
		},
		{
			name:    "top of a page turned half",
			media:   box(0, 0, 612, 792),
			rotate:  180,
			place:   stampPlacement{position: PositionTop, box: [4]float64{0, 0, 100, 50}},
			wantBox: [4]float64{256, 742, 356, 792},
		},
		{
			name:    "left of a page turned three quarters",
			media:   box(0, 0, 612, 792),
			rotate:  270,
			place:   stampPlacement{position: PositionLeft, box: [4]float64{0, 0, 100, 50}},
			wantBox: [4]float64{0, 281, 100, 331},
		},
		{
			// A square rotated 45 degrees is bounded by a box sqrt(2) times as
			// wide, which the corner holds.
			name:    "a rotated stamp in a corner stays on the page",
			media:   box(0, 0, 612, 792),
			place:   stampPlacement{position: PositionTopRight, box: [4]float64{0, 0, 100, 100}, rotation: 45},
			wantBox: [4]float64{612 - 100*math.Sqrt2, 792 - 100*math.Sqrt2, 612, 792},
		},
		{
			// A text box sits above and below its baseline.
			name:    "a box that does not start at its origin",
			media:   box(0, 0, 200, 200),
			place:   stampPlacement{position: PositionCenter, box: [4]float64{5, -20, 95, 60}},
			wantBox: [4]float64{55, 60, 145, 140},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			page := &document.Page{MediaBox: tc.media, Rotate: tc.rotate}
			m := tc.place.matrix(page, tc.rotate)
			var view [6]float64
			for i, v := range viewedPageMatrix(normalizedBox(page.ViewedBox()), tc.rotate) {
				view[i] = float64(v.(ast.PdfReal))
			}
			rad := tc.place.rotation * math.Pi / 180
			c, s := math.Cos(rad), math.Sin(rad)
			got := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
			b := tc.place.box
			for _, p := range [4][2]float64{{b[0], b[1]}, {b[2], b[1]}, {b[0], b[3]}, {b[2], b[3]}} {
				// The stamp's own rotation, then the placement, then the view.
				x, y := p[0]*c-p[1]*s, p[0]*s+p[1]*c
				x, y = m[0]*x+m[2]*y+m[4], m[1]*x+m[3]*y+m[5]
				x, y = view[0]*x+view[2]*y+view[4], view[1]*x+view[3]*y+view[5]
				got[0], got[1] = min(got[0], x), min(got[1], y)
				got[2], got[3] = max(got[2], x), max(got[3], y)
			}
			for i := range got {
				if math.Abs(got[i]-tc.wantBox[i]) > 1e-6 {
					t.Fatalf("the stamp shows at %v, want %v", got, tc.wantBox)
				}
			}
		})
	}
}

// TestPlacedStampWritesNoStreamNothingDraws pins that a placed stamp, which
// moves onto every page, writes only the moved copy of its operators: the
// unmoved stream an unplaced stamp shares between its pages would be an object
// no page names.
func TestPlacedStampWritesNoStreamNothingDraws(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := stampDoc(t)
	if err := StampImage(ctx, alloc, doc, pages, ImageStampOptions{
		Data:     solidPNG(t),
		MimeType: "image/png",
		Width:    20,
		Height:   10,
		Position: PositionCenter,
	}, nil); err != nil {
		t.Fatalf("StampImage: %v", err)
	}
	content := stampedContent(t, ctx, alloc)
	if n := strings.Count(content, "/Artifact BMC"); n != 1 {
		t.Errorf("the stamp's operators are written %d times, want once: %q", n, content)
	}
	if !strings.Contains(content, "1 0 0 1 90 95 cm") {
		t.Errorf("the stamp is not moved to the center of the 200 by 200 page: %q", content)
	}
}

// TestStampRefusesAnUnknownPosition pins that a position outside the ones
// [StampPosition] lists is refused before anything is written.
func TestStampRefusesAnUnknownPosition(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := stampDoc(t)
	before := len(alloc.Pending())
	err := StampText(ctx, alloc, doc, pages, TextStampOptions{Text: "hi", Position: positionCount}, nil)
	if err == nil || !strings.Contains(err.Error(), ErrInvalidStampPosition.Error()) {
		t.Fatalf("StampText = %v, want %v", err, ErrInvalidStampPosition)
	}
	if after := len(alloc.Pending()); after != before {
		t.Errorf("a refused stamp allocated %d objects", after-before)
	}
}

// TestLineInkBox pins how a line of glyphs is bounded: the union of each
// glyph's ink from its pen position, scaled to the font size, and the advances
// on the baseline when nothing inks.
func TestLineInkBox(t *testing.T) {
	t.Parallel()

	glyphs := []inkGlyph{
		{advance: 0.5, box: [4]float64{0.1, -0.2, 0.4, 0.7}, inked: true},
		{advance: 0.25},
		{advance: 0.5, box: [4]float64{0.05, 0, 0.45, 0.6}, inked: true},
	}
	if got, want := lineInkBox(glyphs, 10), [4]float64{1, -2, 12, 7}; got != want {
		t.Errorf("lineInkBox = %v, want %v", got, want)
	}
	if got, want := lineInkBox([]inkGlyph{{advance: 0.25}, {advance: 0.25}}, 10), [4]float64{0, 0, 5, 0}; got != want {
		t.Errorf("lineInkBox of spaces = %v, want %v", got, want)
	}
}

// solidPNG returns a 4 by 4 opaque red PNG.
func solidPNG(t *testing.T) []byte {
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
