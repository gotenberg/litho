package page

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// popplerFixtures are drawn by both litho and the pinned poppler. Each
// testdata/poppler/<name>.png is pdftoppm 26.09.0's rendering of the
// fixture, made with the image test/integration/corpus provisions:
//
//	LITHO_RENDER_FIXTURES=$dir go test ./pkg/internal/render/page -run TestWritePopplerFixtures
//	for f in $dir/*.pdf; do
//	  docker run --rm --entrypoint pdftoppm -v $dir:/in litho-corpus-builder \
//	    -r 72 -cropbox -aa yes -aaVector yes -singlefile -png /in/$(basename $f) /in/$(basename $f .pdf)
//	done
//
// the flags gen-render.sh draws the corpus goldens with.
var popplerFixtures = map[string]fixture{
	"fill_rules": {content: "0 g 10 10 80 80 re 30 30 40 40 re f* 1 0 0 rg 20 20 m 50 95 l 80 20 l h 15 60 m 85 60 l 50 5 l h f"},
	"strokes": {content: "0 0 1 RG 6 w 0 J 0 j 10 85 m 40 95 l 70 85 l S 1 J 1 j 10 65 m 40 75 l 70 65 l S " +
		"2 J 2 j 10 45 m 40 55 l 70 45 l S 0 J 0 j 1 w [6 3] 0 d 10 25.5 m 90 25.5 l S [] 0 d 0 w 10 15 m 90 5 l S " +
		"0 G 3 w 80 50 m 95 90 l 95 50 l h S"},
	"clips": {content: "q 10 10 40 40 re W n 0 g 0 0 100 100 re f Q " +
		"q 50 50 m 100 50 l 75 100 l h W n 1 0 0 rg 0 0 100 100 re f Q " +
		"q 55 5 40 40 re 65 15 20 20 re W* n 0 0 1 rg 0 0 100 100 re f Q"},
	"alpha": {
		page:    "/Resources << /ExtGState << /A << /ca 0.4 /CA 0.6 >> >> >>",
		content: "1 0 0 rg 10 10 60 60 re f q /A gs 0 0 1 rg 30 30 60 60 re f 0 G 8 w 5 50 m 95 50 l S Q",
	},
	"images": {
		page: "/Resources << /XObject << /Im 5 0 R /Mk 6 0 R /Sm 7 0 R >> >>",
		content: "q 40 0 0 40 5 55 cm /Im Do Q 0 0.5 0 rg q 40 0 0 40 55 55 cm /Mk Do Q " +
			"q 40 0 0 40 5 5 cm /Sm Do Q q 40 0 0 40 55 5 cm BI /W 3 /H 1 /CS /RGB /BPC 8 ID \xff\x00\x00\x00\xff\x00\x00\x00\xff EI Q",
		objs: []string{
			stream("/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8",
				"\xff\x00\x00\x00\xff\x00\x00\x00\xff\xff\xff\x00"),
			stream("/Type /XObject /Subtype /Image /Width 8 /Height 8 /ImageMask true", "\x0f\x0f\x0f\x0f\xf0\xf0\xf0\xf0"),
			stream("/Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /SMask 8 0 R", "\x00\x00"),
			stream("/Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x40\xc0"),
		},
	},
	"shadings": {
		page:    "/Resources << /Shading << /Sh 5 0 R >> /Pattern << /P 6 0 R >> >>",
		content: "q 0 50 100 50 re W n /Sh sh Q /Pattern cs /P scn 10 5 m 90 5 l 50 45 l h f",
		objs: []string{
			"<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 100 0] " +
				"/Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> >>",
			"<< /PatternType 2 /Matrix [1 0 0 1 0 0] /Shading << /ShadingType 3 /ColorSpace /DeviceGray /Coords [50 25 0 50 25 30] " +
				"/Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> /Extend [true true] >> >>",
		},
	},
	"forms": {
		page:    "/Resources << /XObject << /Fm 5 0 R >> >>",
		content: "/Fm Do q 0.5 0 0 0.5 50 0 cm /Fm Do Q q 0 1 -1 0 100 50 cm /Fm Do Q",
		objs: []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 40 40] /Matrix [1 0 0 1 5 5]",
			"0 0.6 0 rg -20 -20 100 100 re f 1 g 5 5 30 30 re f 0 g 10 10 m 30 10 l 20 30 l h f")},
	},
	"rotated": {page: "/MediaBox [0 0 120 80] /Rotate 90", content: "1 0 0 rg 0 0 30 20 re f 0 0 1 RG 4 w 10 70 m 110 10 l S"},
}

// TestWritePopplerFixtures writes the PDFs of popplerFixtures to the
// directory LITHO_RENDER_FIXTURES names, for pdftoppm.
func TestWritePopplerFixtures(t *testing.T) {
	dir := os.Getenv("LITHO_RENDER_FIXTURES")
	if dir == "" {
		t.Skip("LITHO_RENDER_FIXTURES is not set")
	}
	for name, f := range popplerFixtures {
		if err := os.WriteFile(filepath.Join(dir, name+".pdf"), f.bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// Tolerances of the comparison with poppler, as the corpus gate's render
// feature sets them: a channel may differ by 64 levels, a pixel matches one
// within one pixel of it, and 1% of the pixels may differ. Two anti-aliasing
// rasterisers disagree on an edge pixel's coverage and place an edge up to a
// pixel apart; a shape in the wrong place or colour differs by far more.
const (
	popplerTolerance    = 64
	popplerNeighbours   = 1
	popplerMaxDiffShare = 0.01
)

// TestAgainstPoppler compares litho's rendering of each fixture with
// poppler's.
func TestAgainstPoppler(t *testing.T) {
	t.Parallel()
	names := make([]string, 0, len(popplerFixtures))
	for name := range popplerFixtures {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			file, err := os.Open(filepath.Join("testdata", "poppler", name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			golden, err := png.Decode(file)
			if err != nil {
				t.Fatal(err)
			}
			want := toNRGBA(golden)
			doc, pg := open(t, popplerFixtures[name].bytes(), limits.Limits{})
			c, _, err := Render(context.Background(), doc, pg, Options{DPI: 72, Background: white})
			if err != nil {
				t.Fatal(err)
			}
			got := c.Image()
			if got.Bounds() != want.Bounds() {
				t.Fatalf("size %v, poppler %v", got.Bounds(), want.Bounds())
			}
			diff := 0
			w, h := got.Bounds().Dx(), got.Bounds().Dy()
			for y := range h {
				for x := range w {
					if !matchesNear(got, want, x, y) || !matchesNear(want, got, x, y) {
						diff++
					}
				}
			}
			if share := float64(diff) / float64(w*h); share > popplerMaxDiffShare {
				t.Errorf("%d of %d pixels differ from poppler's (%.2f%%)", diff, w*h, 100*share)
			}
		})
	}
}

// matchesNear reports whether a pixel within popplerNeighbours of (x, y) in b
// lies within popplerTolerance of pixel (x, y) of a.
func matchesNear(a, b *image.NRGBA, x, y int) bool {
	p := a.NRGBAAt(x, y)
	bounds := b.Bounds()
	for ny := max(0, y-popplerNeighbours); ny <= min(bounds.Dy()-1, y+popplerNeighbours); ny++ {
		for nx := max(0, x-popplerNeighbours); nx <= min(bounds.Dx()-1, x+popplerNeighbours); nx++ {
			q := b.NRGBAAt(nx, ny)
			if close8(rgb{p.R, p.G, p.B}, rgb{q.R, q.G, q.B}, popplerTolerance) {
				return true
			}
		}
	}
	return false
}
