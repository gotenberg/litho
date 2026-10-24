package flatten

import (
	"image"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/page"
)

// photoImage returns a 256x256 RGB image XObject holding a gradient with a
// little noise, every sample at most 200: many colours, none near white.
func photoImage() string {
	const n = 256
	samples := make([]byte, 0, 3*n*n)
	seed := uint32(1)
	for y := range n {
		for x := range n {
			seed = seed*1664525 + 1013904223
			noise := byte(seed >> 28)
			samples = append(samples, byte(x/2)+noise, byte(y/2)+noise, byte(100+x/4+y/8)+noise)
		}
	}
	return stream("/Type /XObject /Subtype /Image /Width 256 /Height 256 /ColorSpace /DeviceRGB /BitsPerComponent 8", string(samples))
}

// tilesContent draws a photograph over the whole page at ca 0.5, an opaque
// blue triangle at the page's top right, then the photograph again at ca 0.5
// over the page's top right corner: one region, whose image takes over the
// triangle. Of the 4 x 4 cells of the 300 dpi tile grid, the triangle
// reaches the two right cells of the top row and only the photograph the
// others.
const tilesContent = "q /GS1 gs 100 0 0 100 0 0 cm /Im Do Q 0 0 1 rg 80 80 m 95 80 l 95 95 l h f " +
	"q /GS1 gs 70 70 30 30 re W n 100 0 0 100 0 0 cm /Im Do Q"

func tilesFixture() fixture {
	return fixture{
		resources: "/XObject << /Im 7 0 R >>",
		content:   tilesContent,
		objs:      []string{photoImage()},
	}
}

// nearWhite counts the pixels of img whose channels all pass 240.
func nearWhite(img *image.NRGBA) []int {
	var out []int
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 240 && img.Pix[i+1] > 240 && img.Pix[i+2] > 240 {
			out = append(out, i/4)
		}
	}
	return out
}

func TestFlattenSplitsAMixedCompositeIntoTiles(t *testing.T) {
	t.Parallel()
	in := tilesFixture().bytes()
	lossless, report := flattenBytes(t, in, Options{})
	if pr := report.Pages[0]; pr.Regions != 1 || pr.Composites != 1 || pr.JPEG != 0 {
		t.Fatalf("lossless report %+v, want one Flate composite", pr)
	}
	tiled, report := flattenBytes(t, in, Options{JPEGQuality: 90})
	// The triangle's cells stay lossless; the others, runs of cells, are
	// JPEG, but for a corner cell too small to hold 256 colours.
	if pr := report.Pages[0]; pr.Regions != 1 || pr.JPEG == 0 || pr.JPEG >= pr.Composites {
		t.Fatalf("report %+v, want tiles, some of them JPEG and some lossless", pr)
	}
	if len(tiled) >= len(lossless) {
		t.Errorf("tiled output %d bytes, lossless %d", len(tiled), len(lossless))
	}
	assertOpaque(t, tiled)
	for _, dpi := range []float64{72, 150, 300} {
		src, want, got := renderAt(t, in, dpi), renderAt(t, lossless, dpi), renderAt(t, tiled, dpi)
		if d := differing(src, got, 24); d > 0.01 {
			t.Errorf("at %v dpi %.2f%% of the pixels differ from the source", dpi, 100*d)
		}
		// The page holds no near-white pixel but where the composite
		// stops short of the page edge, as the lossless one does: a seam
		// between tiles would show the paper.
		if seams := slices.DeleteFunc(nearWhite(got), func(i int) bool { return slices.Contains(nearWhite(want), i) }); len(seams) > 0 {
			w := got.Bounds().Dx()
			t.Errorf("at %v dpi %d pixels show the paper between tiles, first at (%d, %d)", dpi, len(seams), seams[0]%w, seams[0]/w)
		}
	}
}

// TestFlattenDrawsJPEGTilesOverALosslessUnderlay pins that a tiled composite
// draws as [imagetiles.Encoder.Tiles] lays it out: abutting lossless tiles,
// each placed on device pixels on its own, drew a column or row a pixel off
// in poppler and PDFium where the lossless composite drew none.
func TestFlattenDrawsJPEGTilesOverALosslessUnderlay(t *testing.T) {
	t.Parallel()
	in := tilesFixture().bytes()
	lossless, _ := flattenBytes(t, in, Options{})
	tiled, _ := flattenBytes(t, in, Options{JPEGQuality: 90})
	assertUnderlay(t, lossless, tiled)
}

func TestFlattenKeepsAPhotographicCompositeWhole(t *testing.T) {
	t.Parallel()
	// Without the square every cell is photographic: one JPEG image.
	f := tilesFixture()
	f.content = "q /GS1 gs 100 0 0 100 0 0 cm /Im Do Q"
	_, report := flattenBytes(t, f.bytes(), Options{JPEGQuality: 90})
	if pr := report.Pages[0]; pr.Composites != 1 || pr.JPEG != 1 {
		t.Errorf("report %+v, want one JPEG composite", pr)
	}
}

func TestOverpaintDrawsOnlyTheTilesItsClipReaches(t *testing.T) {
	t.Parallel()
	c := &coverage{g: page.Geometry{Matrix: geom.Matrix{1, 0, 0, -1, 0, 10}, Width: 20, Height: 10}}
	img := regionImg{
		px: image.Rect(0, 0, 20, 10),
		tiles: []tileImg{
			{name: "FlatIm1", matrix: geom.Identity, px: image.Rect(0, 0, 11, 10)},
			{name: "FlatIm2", matrix: geom.Identity, px: image.Rect(10, 0, 20, 10)},
		},
	}
	out := string(c.overpaint(position{ctm: geom.Identity}, []image.Rectangle{image.Rect(2, 2, 5, 5)}, img))
	if !strings.Contains(out, "/FlatIm1 Do") || strings.Contains(out, "/FlatIm2 Do") {
		t.Errorf("overpaint %q, want FlatIm1 only", out)
	}
	out = string(c.overpaint(position{ctm: geom.Identity}, []image.Rectangle{image.Rect(9, 2, 12, 5)}, img))
	if !strings.Contains(out, "/FlatIm1 Do") || !strings.Contains(out, "/FlatIm2 Do") {
		t.Errorf("overpaint %q, want both tiles", out)
	}
}

func TestFlattenTilesTheContentOfAForm(t *testing.T) {
	t.Parallel()
	// The whole page is one form at ca 0.5, as producers draw a page: the
	// tiles are cut by what the form paints, the triangle, and not by the
	// form's box, the whole page.
	f := tilesFixture()
	f.resources = "/XObject << /Fm 8 0 R >>"
	f.content = "q /GS1 gs /Fm Do Q"
	f.objs = append(f.objs, stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /XObject << /Im 7 0 R >> >>",
		"q 100 0 0 100 0 0 cm /Im Do Q 0 0 1 rg 80 80 m 95 80 l 95 95 l h f"))
	_, report := flattenBytes(t, f.bytes(), Options{JPEGQuality: 90})
	if pr := report.Pages[0]; pr.Regions != 1 || pr.JPEG == 0 || pr.JPEG >= pr.Composites {
		t.Errorf("report %+v, want tiles, some of them JPEG and some lossless", pr)
	}
}

func TestRectEdges(t *testing.T) {
	t.Parallel()
	edges := rectEdges(Rect{0, 0, 100, 50}, 0)
	if len(edges) != 4 {
		t.Fatalf("rectEdges = %v, want four sides", edges)
	}
	if inside := (Rect{10, 10, 90, 40}); slices.ContainsFunc(edges, inside.Overlaps) {
		t.Errorf("an edge of %v reaches its inside %v", edges, inside)
	}
	for _, corner := range []Rect{{-1, -1, 0.0005, 0.0005}, {99.9995, 49.9995, 101, 51}} {
		if !slices.ContainsFunc(edges, corner.Overlaps) {
			t.Errorf("no edge holds the corner %v", corner)
		}
	}
	if edges := rectEdges(Rect{0, 0, 3, 50}, 1); len(edges) != 1 || edges[0] != (Rect{0, 0, 3, 50}) {
		t.Errorf("a stroke wider than half the box: %v, want the box", edges)
	}
}
