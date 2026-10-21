package page

import (
	"context"
	"maps"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// tilePattern writes a tiling pattern stream with extra dictionary entries.
func tilePattern(dict, data string) string {
	return stream("/PatternType 1 /TilingType 1 "+dict, data)
}

// patternPopplerFixtures join popplerFixtures: tiling patterns of both paint
// types, negative steps, nesting, a stroke, a pattern in a form, and an image
// mask painted with a pattern.
var patternPopplerFixtures = map[string]fixture{
	"tiling_coloured": {
		page:    "/Resources << /Pattern << /P 5 0 R >> >>",
		content: "/Pattern cs /P scn 0 0 100 50 re f /Pattern CS /P SCN 8 w 10 70 m 90 90 l S",
		objs: []string{tilePattern("/PaintType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << >>",
			"1 0 0 rg 0 0 5 5 re f 0 0 1 rg 5 5 5 5 re f")},
	},
	"tiling_uncoloured": {
		page:    "/Resources << /Pattern << /P 5 0 R >> /ColorSpace << /CS0 [/Pattern /DeviceRGB] >> >>",
		content: "/CS0 cs 0 0 1 /P scn 0 0 50 100 re f 1 0 0 /P scn 50 0 50 100 re f",
		objs: []string{tilePattern("/PaintType 2 /BBox [0 0 8 8] /XStep 8 /YStep 8 /Resources << >>",
			"0 1 0 rg 1 1 m 7 1 l 4 7 l h f")},
	},
	"tiling_negative": {
		page:    "/Resources << /Pattern << /P 5 0 R >> >>",
		content: "/Pattern cs /P scn 5 5 90 90 re f",
		objs: []string{tilePattern("/PaintType 1 /BBox [0 0 12 6] /XStep -15 /YStep -9 /Matrix [1 0 0 1 3 2] /Resources << >>",
			"0 0.5 0 rg 0 0 12 6 re f")},
	},

	"tiling_nested": {
		page:    "/Resources << /Pattern << /P 5 0 R >> >>",
		content: "/Pattern cs /P scn 0 0 100 100 re f",
		objs: []string{
			tilePattern("/PaintType 1 /BBox [0 0 40 40] /XStep 50 /YStep 50 /Resources << /Pattern << /Q 6 0 R >> >>",
				"/Pattern cs /Q scn 0 0 40 40 re f"),
			tilePattern("/PaintType 1 /BBox [0 0 4 4] /XStep 4 /YStep 4 /Resources << >>", "1 0 0 rg 0 0 2 2 re f"),
		},
	},
	"tiling_form": {
		page:    "/Resources << /XObject << /Fm 5 0 R >> >>",
		content: "q 1 0 0 1 10 10 cm /Fm Do Q",
		objs: []string{
			stream("/Type /XObject /Subtype /Form /BBox [0 0 40 40] /Matrix [2 0 0 2 0 0] /Resources << /Pattern << /P 6 0 R >> >>",
				"/Pattern cs /P scn 0 0 40 40 re f"),
			tilePattern("/PaintType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << >>", "0 0 1 rg 0 0 5 5 re f"),
		},
	},
	"tiling_imagemask": {
		page:    "/Resources << /Pattern << /P 5 0 R >> /XObject << /Mk 6 0 R >> >>",
		content: "/Pattern cs /P scn q 80 0 0 80 10 10 cm /Mk Do Q",
		objs: []string{
			tilePattern("/PaintType 1 /BBox [0 0 6 6] /XStep 6 /YStep 6 /Resources << >>", "1 0 0 rg 0 0 3 6 re f 0 0 1 rg 3 0 3 6 re f"),
			stream("/Type /XObject /Subtype /Image /Width 8 /Height 8 /ImageMask true", "\x0f\x0f\x0f\x0f\xf0\xf0\xf0\xf0"),
		},
	},
}

// latticeFixtures are drawn by litho alone: poppler 26.09.0 tiles a turned
// lattice with periods rounded to whole pixels (11.5 pixels apart draw 12
// apart) and a fractional step without scaling the cell (a 7.5 by 7.5 cell
// leaves a seam every 8 pixels), where these tests pin the exact spacing and
// the scaled cell [renderer.tilingFill] draws.
var latticeFixtures = map[string]fixture{
	"tiling_rotated": {
		page:    "/Resources << /Pattern << /P 5 0 R >> >>",
		content: "/Pattern cs /P scn 0 0 100 100 re f",
		objs: []string{tilePattern("/PaintType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Matrix [0.866 0.5 -0.5 0.866 50 50] /Resources << >>",
			"0 0 1 rg 0 0 5 10 re f")},
	},
	"tiling_fractional": {
		page:    "/Resources << /Pattern << /P 5 0 R >> >>",
		content: "/Pattern cs /P scn 0 0 100 100 re f",
		objs: []string{stream("/PatternType 1 /TilingType 2 /PaintType 1 /BBox [0 0 7.5 7.5] /XStep 7.5 /YStep 7.5 /Resources << >>",
			"0 g 0 0 3 7.5 re f")},
	},
}

func init() {
	maps.Copy(popplerFixtures, patternPopplerFixtures)
}

func TestTilingColoured(t *testing.T) {
	t.Parallel()
	img, report := draw(t, patternPopplerFixtures["tiling_coloured"])
	if len(report.Unsupported) != 0 {
		t.Errorf("report %v, want nothing left out", report.Unsupported)
	}
	// User (2, 2) is device (2, 97); (7, 7) is (7, 92).
	expect(t, img, 8, map[[2]int]rgb{
		{2, 97}: red, {7, 92}: blue, {7, 97}: paper, {12, 97}: red, {47, 52}: blue, {50, 40}: paper,
	})
}

func TestTilingUncoloured(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, patternPopplerFixtures["tiling_uncoloured"])
	// The cell's own green is ignored (ISO 32000-1, 8.7.3.3): each half
	// paints the colour scn gave.
	expect(t, img, 8, map[[2]int]rgb{{4, 96}: blue, {52, 96}: red, {1, 92}: paper})
}

func TestTilingNegativeSteps(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, patternPopplerFixtures["tiling_negative"])
	green := rgb{0, 128, 0}
	// Cells sit at x = 3 + 15i, y = 2 + 9j, each 12 by 6.
	expect(t, img, 8, map[[2]int]rgb{
		{10, 93}: green, {16, 93}: paper, {20, 93}: green, {10, 91}: paper, {10, 87}: green,
	})
}

func TestTilingRotated(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, latticeFixtures["tiling_rotated"])
	// Pattern x = 0.866 (ux - 50) + 0.5 (uy - 50) for user (ux, uy): blue
	// where it lies in [0, 5] modulo 10.
	checked := 0
	for y := 0; y < 100; y += 3 {
		for x := 0; x < 100; x += 3 {
			ux, uy := float64(x)+0.5, 100-float64(y)-0.5
			px := math.Mod(0.866*(ux-50)+0.5*(uy-50), 10)
			if px < 0 {
				px += 10
			}
			switch {
			case px > 1 && px < 4:
				expect(t, img, 8, map[[2]int]rgb{{x, y}: blue})
			case px > 6 && px < 9:
				expect(t, img, 8, map[[2]int]rgb{{x, y}: paper})
			default:
				continue
			}
			checked++
		}
	}
	if checked < 500 {
		t.Fatalf("checked %d pixels", checked)
	}
}

func TestTilingFractionalStep(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, latticeFixtures["tiling_fractional"])
	// A step of 7.5 pixels rounds to 8, the bar of 3 scaled to 3.2 (Table
	// 75, /TilingType 1 spacing for every type).
	want := map[[2]int]rgb{}
	for i := range 12 {
		want[[2]int{8*i + 1, 50}] = black
		want[[2]int{8*i + 2, 3}] = black
		want[[2]int{8*i + 5, 50}] = paper
	}
	expect(t, img, 8, want)
}

// TestTilingCellUnadjusted pins that a tiling cell paints without stroke
// adjustment, from the initial /SA false, where the page's own hairline
// snaps to one row: sharednested_2b strokes a 5 by 5 square in a 10 by 10
// cell at the default width of one pixel, which poppler 26.09.0 and PDFium
// draw at half strength across two rows, its bottom edge on the cell's /BBox
// clipped to the half inside.
func TestTilingCellUnadjusted(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{
		page:    "/Resources << /Pattern << /P 5 0 R >> >>",
		content: "q /Pattern cs /P scn 0 0 100 100 re f Q 0 G 20 50 m 80 50 l S",
		objs: []string{tilePattern("/PaintType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << >>",
			"0 G 0 0 5 5 re S 0 g 7.25 7.25 0.5 0.5 re f")},
	})
	grey, faint := rgb{128, 128, 128}, rgb{191, 191, 191}
	// The cell at the origin spans device rows 90 to 99. Its square's top
	// edge (user y 5, device 95) covers half of rows 94 and 95, its bottom
	// edge (device 100) and left edge (device x 0) the half of row 99 and
	// column 0 inside the /BBox, and the quarter-pixel square a quarter of
	// pixel (7, 92).
	expect(t, img, 8, map[[2]int]rgb{
		{2, 94}: grey, {2, 95}: grey, {2, 99}: grey, {0, 97}: grey, {2, 97}: paper, {7, 92}: faint,
	})
	// The page's hairline at device y 50 snaps to one solid row.
	if a, b := at(t, img, 50, 49), at(t, img, 50, 50); !close8(a, black, 8) && !close8(b, black, 8) {
		t.Errorf("page hairline rows 49, 50 = %v, %v, want one black", a, b)
	}
}

func TestTilingNested(t *testing.T) {
	t.Parallel()
	img, report := draw(t, patternPopplerFixtures["tiling_nested"])
	if report.Unsupported[Skipped] != 0 {
		t.Errorf("report %v, want no skip", report.Unsupported)
	}
	expect(t, img, 8, map[[2]int]rgb{{1, 98}: red, {3, 98}: paper, {45, 98}: paper, {51, 98}: red})
}

func TestTilingInForm(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, patternPopplerFixtures["tiling_form"])
	// The pattern maps into the form's space (ISO 32000-1, 8.7.2), scaled
	// by its /Matrix: cells of 20 device pixels from (10, 10).
	expect(t, img, 8, map[[2]int]rgb{{15, 85}: blue, {25, 85}: paper, {35, 85}: blue, {15, 75}: paper})
}

func TestTilingImageMask(t *testing.T) {
	t.Parallel()
	img, report := draw(t, patternPopplerFixtures["tiling_imagemask"])
	if len(report.Unsupported) != 0 {
		t.Errorf("report %v, want nothing left out", report.Unsupported)
	}
	// The mask's top half paints its left columns (sample 0 paints), the
	// pattern showing through: stripes of 3 units from x = 0.
	expect(t, img, 8, map[[2]int]rgb{{14, 15}: red, {16, 15}: blue, {70, 15}: paper, {80, 80}: red})
}

func TestTilingSelfReference(t *testing.T) {
	t.Parallel()
	_, report := draw(t, fixture{
		page:    "/Resources << /Pattern << /P 5 0 R >> >>",
		content: "/Pattern cs /P scn 0 0 100 100 re f",
		objs: []string{tilePattern("/PaintType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << /Pattern << /P 5 0 R >> >>",
			"/Pattern cs /P scn 0 0 10 10 re f")},
	})
	if report.Unsupported[Skipped] == 0 {
		t.Errorf("report %v, want the self-reference skipped", report.Unsupported)
	}
}

func TestTilingCellCap(t *testing.T) {
	t.Parallel()
	// A cell a thousand times its step overlaps a million cells of a tile:
	// past maxPatternCells it is skipped, not drawn.
	_, report := draw(t, fixture{
		page:    "/Resources << /Pattern << /P 5 0 R >> >>",
		content: "/Pattern cs /P scn 0 0 100 100 re f",
		objs:    []string{tilePattern("/PaintType 1 /BBox [0 0 100 100] /XStep 0.1 /YStep 0.1 /Resources << >>", "0 g 0 0 1 1 re f")},
	})
	if report.Unsupported[Skipped] != 1 {
		t.Errorf("report %v, want one skip", report.Unsupported)
	}
}

func TestTilingBadPatterns(t *testing.T) {
	t.Parallel()
	for name, dict := range map[string]string{
		"zero step":   "/PaintType 1 /BBox [0 0 10 10] /XStep 0 /YStep 10",
		"paint type":  "/PaintType 3 /BBox [0 0 10 10] /XStep 10 /YStep 10",
		"no bbox":     "/PaintType 1 /XStep 10 /YStep 10",
		"uncoloured":  "/PaintType 2 /BBox [0 0 10 10] /XStep 10 /YStep 10",
		"nan matrix":  "/PaintType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Matrix [0 0 0 0 0 0]",
		"short array": "/PaintType 1 /BBox [0 0 10] /XStep 10 /YStep 10",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			img, _ := draw(t, fixture{
				page:    "/Resources << /Pattern << /P 5 0 R >> >>",
				content: "/Pattern cs /P scn 0 0 100 100 re f",
				objs:    []string{tilePattern(dict, "0 g 0 0 10 10 re f")},
			})
			expect(t, img, 0, map[[2]int]rgb{{50, 50}: paper})
		})
	}
}

func BenchmarkTilingFill(b *testing.B) {
	data := patternPopplerFixtures["tiling_coloured"].bytes()
	doc, pg := open(b, data, limits.Limits{})
	for b.Loop() {
		if _, _, err := Render(context.Background(), doc, pg, Options{DPI: 300}); err != nil {
			b.Fatal(err)
		}
	}
}
