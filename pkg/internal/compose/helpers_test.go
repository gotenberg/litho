package compose_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"image"
	"image/color"
	"io"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/typography/embed"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/pdf"
)

// buildFace builds a TrueType face covering printable ASCII, each glyph a
// filled rectangle from the baseline up to 500 units (fontbuild), the
// space empty. Advances vary by character, scaled by scale percent, so two
// faces tell apart by width.
func buildFace(name string, scale int) *compose.Face {
	glyphs := make([]fontbuild.Glyph, 0, 95)
	for r := rune(0x20); r <= 0x7E; r++ {
		adv := (400 + int(r%7)*50) * scale / 100
		glyphs = append(glyphs, fontbuild.Glyph{
			Name:    "g" + string(rune('A'+r%26)) + string(rune('a'+r/26)),
			Unicode: r,
			Advance: uint16(adv), //nolint:gosec // test advances stay under 2000
			Empty:   r == ' ',
		})
	}
	prog := fontbuild.TrueType{PostScriptName: name, Family: name, Subfamily: "Regular", Glyphs: glyphs, NotdefAdvance: 500}.Build()
	return &compose.Face{Program: prog, Key: name}
}

var (
	sans  = buildFace("ComposeSans", 100)
	serif = buildFace("ComposeSerif", 130)
)

// run lays text out on one baseline from origin at its natural advances.
func run(t *testing.T, face *compose.Face, size float64, origin compose.Point, text string) *compose.Text {
	t.Helper()
	f, err := embed.Load(face.Program)
	if err != nil {
		t.Fatalf("embed.Load: %v", err)
	}
	upem := float64(f.UnitsPerEm())
	x := 0.0
	glyphs := make([]compose.Glyph, 0, len(text))
	for _, r := range text {
		gid, ok := f.GID(r)
		if !ok {
			t.Fatalf("face %s lacks %q", face.Key, r)
		}
		w, _ := f.Width(gid)
		glyphs = append(glyphs, compose.Glyph{GID: gid, X: x, Text: string(r)})
		x += float64(w) * size / upem
	}
	return &compose.Text{Face: face, Size: size, Color: compose.Black, Origin: origin, Glyphs: glyphs}
}

func letter(content ...compose.Fragment) *compose.Page {
	return &compose.Page{Width: 612, Height: 792, Content: content}
}

func write(t *testing.T, doc *compose.Document) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := compose.Write(context.Background(), doc, &buf, compose.Options{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return buf.Bytes()
}

func open(t *testing.T, data []byte) *pdf.Reader {
	t.Helper()
	r, err := pdf.Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("pdf.Open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// render draws page n at 72 dpi, one pixel per point, so pixel (x, y) is
// model point (x, y).
func render(t *testing.T, r *pdf.Reader, n int) image.Image {
	t.Helper()
	img, err := r.RenderPage(context.Background(), n, pdf.WithRenderDPI(72))
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	return img
}

func dark(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r < 0x4000 && g < 0x4000 && b < 0x4000
}

// inkIn counts the dark pixels of img in [x0, x1) by [y0, y1).
func inkIn(img image.Image, x0, y0, x1, y1 int) int {
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if dark(img.At(x, y)) {
				n++
			}
		}
	}
	return n
}

// near reports whether the pixel at (x, y) is within tol of want on every
// channel, in 8-bit units.
func near(img image.Image, x, y int, want color.NRGBA, tol int) bool {
	got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	d := func(a, b uint8) bool { return int(a)-int(b) <= tol && int(b)-int(a) <= tol }
	return d(got.R, want.R) && d(got.G, want.G) && d(got.B, want.B)
}

func rect(x, y, w, h float64) []compose.PathOp {
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: x, Y: y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x + w, Y: y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x + w, Y: y + h}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x, Y: y + h}}},
		{Kind: compose.ClosePath},
	}
}

// inflated returns the decoded bytes of every Flate stream of out, one
// after the other, so a test can search content streams.
func inflated(t *testing.T, out []byte) []byte {
	t.Helper()
	var all bytes.Buffer
	rest := out
	for {
		i := bytes.Index(rest, []byte("stream\r\n"))
		if i < 0 {
			return all.Bytes()
		}
		rest = rest[i+len("stream\r\n"):]
		j := bytes.Index(rest, []byte("\r\nendstream"))
		if j < 0 {
			return all.Bytes()
		}
		if zr, err := zlib.NewReader(bytes.NewReader(rest[:j])); err == nil {
			_, _ = io.Copy(&all, zr) // a stream that is not Flate is skipped
			all.WriteByte('\n')
		}
		rest = rest[j:]
	}
}
