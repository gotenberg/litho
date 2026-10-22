package page

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// TestRectangularClipRows clips to rectangles that cut device rows and
// columns. A viewer keeps the rows a clipping rectangle touches whole, and
// litho does so too; columns keep their exact coverage, as in poppler.
func TestRectangularClipRows(t *testing.T) {
	t.Parallel()
	half := rgb{128, 128, 128}
	for name, tt := range map[string]struct {
		f    fixture
		tol  int
		want map[[2]int]rgb
	}{
		// User y 49.5 to 50 is device y 50 to 50.5, half of row 50.
		"half a row": {
			f:    fixture{content: "0 49.5 100 0.5 re W n 0 g 0 0 100 100 re f"},
			want: map[[2]int]rgb{{50, 50}: black, {50, 49}: paper, {50, 51}: paper},
		},
		// A clip inside one row paints the row from edge to edge.
		"inside a row": {
			f:    fixture{content: "0 30.2 100 0.3 re W n 0 g 0 0 100 100 re f"},
			want: map[[2]int]rgb{{50, 69}: black, {50, 68}: paper, {50, 70}: paper},
		},
		// Half of column 49 keeps half coverage.
		"half a column": {
			f:    fixture{content: "49.5 0 0.5 100 re W n 0 g 0 0 100 100 re f"},
			tol:  2,
			want: map[[2]int]rgb{{49, 50}: half, {48, 50}: paper, {50, 50}: paper},
		},
		// Whole rows reached through a matrix gain no row from rounding.
		"scaled whole rows": {
			f:    fixture{content: "0.1 0 0 0.1 0 0 cm 0 200 1000 100 re W n 0 g 0 0 1000 1000 re f"},
			want: map[[2]int]rgb{{50, 70}: black, {50, 79}: black, {50, 69}: paper, {50, 80}: paper},
		},
		// A rectangle with no height clips everything away.
		"no height": {
			f:    fixture{content: "10 50.3 80 0 re W n 0 g 0 0 100 100 re f"},
			want: map[[2]int]rgb{{50, 49}: paper, {50, 50}: paper},
		},
		// Two clips sharing a row intersect to that row.
		"two clips": {
			f:    fixture{content: "0 49.7 100 10 re W n 0 0 100 50.2 re W n 0 g 0 0 100 100 re f"},
			want: map[[2]int]rgb{{50, 49}: black, {50, 50}: black, {50, 48}: paper, {50, 51}: paper},
		},
		// A form's bounding box clips as a clipping path does (ISO 32000-1,
		// 8.10.1).
		"form bounding box": {
			f: fixture{
				page:    "/Resources << /XObject << /Fm 5 0 R >> >>",
				content: "/Fm Do",
				objs:    []string{stream("/Type /XObject /Subtype /Form /BBox [0 49.5 100 50]", "0 g 0 0 100 100 re f")},
			},
			want: map[[2]int]rgb{{50, 50}: black, {50, 49}: paper, {50, 51}: paper},
		},
		// An image under the clip takes the whole row as well.
		"image": {
			f: fixture{
				page:    "/Resources << /XObject << /Im 5 0 R >> >>",
				content: "0 49.5 100 0.5 re W n q 100 0 0 100 0 0 cm /Im Do Q",
				objs:    []string{stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x00")},
			},
			want: map[[2]int]rgb{{50, 50}: black, {50, 49}: paper, {50, 51}: paper},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			img, _ := draw(t, tt.f)
			expect(t, img, tt.tol, tt.want)
		})
	}
}

func TestSnapRows(t *testing.T) {
	t.Parallel()
	rect := func(y0, y1 float64) geom.Rect {
		return geom.Rect{Min: geom.Point{X: 1.25, Y: y0}, Max: geom.Point{X: 3.75, Y: y1}}
	}
	for _, tt := range []struct{ in, want geom.Rect }{
		{rect(2.5, 2.75), rect(2, 3)},
		{rect(2, 3), rect(2, 3)},
		{rect(2-1e-9, 3+1e-9), rect(2, 3)},
		{rect(2.4, 2.4), rect(2.4, 2.4)},
		{rect(3, 2), rect(3, 2)},
	} {
		if got := snapRows(tt.in); got != tt.want {
			t.Errorf("snapRows(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestWholePixelClipColumns clips to a rectangle one pixel wide that cuts
// columns 49 and 50 in half. poppler cuts a glyph it fills alone and a
// transparency group at whole pixels, and both columns paint solid; a fill,
// an image and a form that is no group keep half coverage, as Splash's
// quarter-pixel cut gives them.
func TestWholePixelClipColumns(t *testing.T) {
	t.Parallel()
	const clipCols = "49.5 0 1 100 re W n "
	half := rgb{128, 128, 128}
	cut := map[[2]int]rgb{{49, 50}: half, {50, 50}: half, {48, 50}: paper, {51, 50}: paper}
	whole := map[[2]int]rgb{{49, 50}: black, {50, 50}: black, {48, 50}: paper, {51, 50}: paper}
	form := func(group string) fixture {
		return fixture{
			page:    "/Resources << /XObject << /Fm 5 0 R >> >>",
			content: clipCols + "/Fm Do",
			objs:    []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100]"+group, "0 g 0 0 100 100 re f")},
		}
	}
	for name, tt := range map[string]struct {
		f    fixture
		want map[[2]int]rgb
	}{
		"glyph": {
			f:    fixture{page: textPage, content: clipCols + "0 g BT /F 200 Tf 0 40 Td (A) Tj ET", objs: trueTypeObjs()},
			want: whole,
		},
		"isolated group": {f: form(" /Group << /S /Transparency /I true >>"), want: whole},
		"group":          {f: form(" /Group << /S /Transparency >>"), want: whole},
		"form":           {f: form(""), want: cut},
		"fill":           {f: fixture{content: clipCols + "0 g 0 0 100 100 re f"}, want: cut},
		"image": {
			f: fixture{
				page:    "/Resources << /XObject << /Im 5 0 R >> >>",
				content: clipCols + "q 100 0 0 100 0 0 cm /Im Do Q",
				objs:    []string{stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x00")},
			},
			want: cut,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			img, _ := draw(t, tt.f)
			expect(t, img, 2, tt.want)
		})
	}
}

func TestPixelRect(t *testing.T) {
	t.Parallel()
	rect := func(x0, y0, x1, y1 float64) geom.Rect {
		return geom.Rect{Min: geom.Point{X: x0, Y: y0}, Max: geom.Point{X: x1, Y: y1}}
	}
	for _, tt := range []struct{ in, want geom.Rect }{
		{rect(1.25, 2.5, 3.75, 2.75), rect(1, 2, 4, 3)},
		{rect(1, 2, 4, 3), rect(1, 2, 4, 3)},
		{rect(1-1e-9, 2, 4+1e-9, 3), rect(1, 2, 4, 3)},
		{rect(1.5, 2, 1.5, 3), rect(1.5, 2, 1.5, 3)},
		{rect(1, 2.4, 4, 2.4), rect(1, 2.4, 4, 2.4)},
	} {
		if got := pixelRect(tt.in); got != tt.want {
			t.Errorf("pixelRect(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
