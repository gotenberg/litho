package svg

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// RenderPDF rasterises the first page of a PDF at 96 dpi. The external
// test package installs it.
var RenderPDF func(data []byte) (image.Image, error)

var (
	ahemOnce  sync.Once
	ahemFonts *text.Fonts
	errAhem   error
)

// ahem returns an index holding the Ahem font alone, whose glyphs are full
// em squares, ascent 0.8 em.
func ahem(t testing.TB) *text.Fonts {
	t.Helper()
	ahemOnce.Do(func() {
		ahemFonts, errAhem = text.NewFonts(context.Background(), nil, []string{filepath.Join("..", "layout", "testdata")})
	})
	if errAhem != nil {
		t.Fatalf("indexing Ahem: %v", errAhem)
	}
	return ahemFonts
}

// parse parses src with Ahem fonts, failing the test on an error.
func parse(t testing.TB, src string) *Document {
	t.Helper()
	d, err := Parse(context.Background(), []byte(src), Options{Fonts: ahem(t)})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d
}

// draw parses src and draws it on a w by h pixel page at 96 dpi, the
// drawing filling the page.
func draw(t testing.TB, src string, w, h int) image.Image {
	t.Helper()
	return drawDoc(t, parse(t, src), w, h)
}

// drawDoc draws d on a w by h pixel page.
func drawDoc(t testing.TB, d *Document, w, h int) image.Image {
	t.Helper()
	pw, ph := float64(w)*ptPerPx, float64(h)*ptPerPx
	frags := d.Draw(context.Background(), compose.Rect{Width: pw, Height: ph})
	doc := &compose.Document{Pages: []*compose.Page{{Width: pw, Height: ph, Content: frags}}}
	var b bytes.Buffer
	if err := compose.Write(context.Background(), doc, &b, compose.Options{}); err != nil {
		t.Fatalf("compose.Write: %v", err)
	}
	if RenderPDF == nil {
		t.Fatal("no PDF renderer installed")
	}
	img, err := RenderPDF(b.Bytes())
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if dir := os.Getenv("SVG_TEST_DUMP"); dir != "" {
		dump(t, img, filepath.Join(dir, t.Name()+".png"))
	}
	return img
}

// at returns the colour of img at (x, y) as 8-bit RGB over white.
func at(img image.Image, x, y int) [3]uint8 {
	c := color.NRGBAModel.Convert(img.At(img.Bounds().Min.X+x, img.Bounds().Min.Y+y)).(color.NRGBA)
	blend := func(v uint8) uint8 {
		return uint8((int(v)*int(c.A) + 255*(255-int(c.A)) + 127) / 255) //nolint:gosec // within 0..255
	}
	return [3]uint8{blend(c.R), blend(c.G), blend(c.B)}
}

// near reports whether two colours lie within tol levels per channel.
func near(a, b [3]uint8, tol int) bool {
	for i := range 3 {
		d := int(a[i]) - int(b[i])
		if d < -tol || d > tol {
			return false
		}
	}
	return true
}

var (
	white = [3]uint8{255, 255, 255}
	red   = [3]uint8{255, 0, 0}
	green = [3]uint8{0, 128, 0}
	lime  = [3]uint8{0, 255, 0}
	blue  = [3]uint8{0, 0, 255}
	black = [3]uint8{0, 0, 0}
)

// probe is a pixel and the colour it must have.
type probe struct {
	x, y int
	want [3]uint8
}

// checkProbes fails the test for every probe img does not match within
// tol levels.
func checkProbes(t *testing.T, img image.Image, tol int, probes []probe) {
	t.Helper()
	for _, p := range probes {
		if got := at(img, p.x, p.y); !near(got, p.want, tol) {
			t.Errorf("pixel (%d, %d) = %v, want %v", p.x, p.y, got, p.want)
		}
	}
}

// dump writes img to path, for looking at a drawing while debugging.
func dump(t testing.TB, img image.Image, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("dump: %v", err)
	}
	f, err := os.Create(path) //nolint:gosec // a debugging path the developer chose
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	defer f.Close() //nolint:errcheck // closing after a failed encode
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("dump: %v", err)
	}
}
