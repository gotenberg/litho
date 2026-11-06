package layout

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// px converts CSS pixels to points.
func px(v float64) float64 { return v * css.Pt }

var (
	ahemOnce  sync.Once
	ahemFonts *text.Fonts
	ahemErr   error
)

// ahem returns an index holding the Ahem font alone, whose glyphs are full
// em squares, ascent 0.8 em and descent 0.2 em.
func ahem(t testing.TB) *text.Fonts {
	t.Helper()
	ahemOnce.Do(func() {
		ahemFonts, ahemErr = text.NewFonts(context.Background(), nil, []string{"testdata"})
	})
	if ahemErr != nil {
		t.Fatalf("indexing Ahem: %v", ahemErr)
	}
	return ahemFonts
}

// sty returns an Ahem style of size 10px and line height 1, changed by mods.
func sty(mods ...func(*css.Style)) *css.Style {
	st := css.Initial()
	st.Display = css.Block
	st.FontFamilies = []string{"Ahem"}
	st.FontSize = px(10)
	st.LineHeight = css.Dimension{Kind: css.Number, Value: 1}
	for _, m := range mods {
		m(st)
	}
	return st
}

// inline returns an inline Ahem style changed by mods.
func inlineSty(mods ...func(*css.Style)) *css.Style {
	return sty(append([]func(*css.Style){func(s *css.Style) { s.Display = css.Inline }}, mods...)...)
}

// el returns an element node.
func el(st *css.Style, kids ...*Node) *Node {
	return &Node{Kind: Element, Style: st, Children: kids}
}

// txt returns a text node of style st.
func txt(st *css.Style, s string) *Node {
	return &Node{Kind: Text, Style: st, Text: s}
}

// br returns a forced line break of style st.
func br(st *css.Style) *Node {
	return &Node{Kind: LineBreak, Style: st}
}

// pic returns a w by h px picture of one colour.
func pic(c color.Color, w, h int) *Picture {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return &Picture{Source: &compose.ImageSource{Image: img}, Width: px(float64(w)), Height: px(float64(h))}
}

// page returns a page box of w by h px with no margins.
func page(w, h float64) PageBox {
	return PageBox{Width: px(w), Height: px(h)}
}

// run lays root out on page pb and checks the document is valid.
func run(t testing.TB, root *Node, pb PageBox) *Result {
	t.Helper()
	res, err := Layout(context.Background(), root, ahem(t), Options{Page: pb})
	if err != nil {
		t.Fatalf("Layout: %v", err)
	}
	doc := document(res)
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return res
}

// document returns the composition document of res.
func document(res *Result) *compose.Document {
	return &compose.Document{Pages: res.Pages, Structure: res.Structure, Warnings: res.Warnings}
}

// render writes res as PDF and rasterises page n (1-based) at 96 dpi: one
// CSS pixel per pixel.
func render(t testing.TB, res *Result, n int) image.Image {
	t.Helper()
	var b bytes.Buffer
	if err := compose.Write(context.Background(), document(res), &b, compose.Options{}); err != nil {
		t.Fatalf("compose.Write: %v", err)
	}
	if RenderPDF == nil {
		t.Fatal("no PDF renderer installed")
	}
	img, err := RenderPDF(b.Bytes(), n)
	if err != nil {
		t.Fatalf("rendering page %d: %v", n, err)
	}
	return img
}

// RenderPDF rasterises page n (1-based) of a PDF at 96 dpi. The external
// test package installs it: pkg/pdf imports this package, so the internal
// tests cannot import pkg/pdf themselves.
var RenderPDF func(data []byte, n int) (image.Image, error)

// Pixel colours of the draw tests, after WeasyPrint's: _ white, R red, B
// blue, G lime, K black, y yellow, the dark shade of a colour in lower case.
var palette = map[byte]color.NRGBA{
	'_': {255, 255, 255, 255},
	'R': {255, 0, 0, 255},
	'B': {0, 0, 255, 255},
	'G': {0, 255, 0, 255},
	'K': {0, 0, 0, 255},
	'y': {255, 255, 0, 255},
	'r': {128, 0, 0, 255},
	'g': {0, 128, 0, 255},
	'b': {0, 0, 128, 255},
}

// code returns the palette letter of c, '?' for none.
func code(c color.Color) byte {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	best, dist := byte('?'), 3*24*24
	for k, v := range palette {
		d := sq(int(n.R)-int(v.R)) + sq(int(n.G)-int(v.G)) + sq(int(n.B)-int(v.B))
		if d < dist {
			best, dist = k, d
		}
	}
	return best
}

func sq(v int) int { return v * v }

// grid returns img as rows of palette letters.
func grid(img image.Image) []string {
	b := img.Bounds()
	rows := make([]string, 0, b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		var s strings.Builder
		for x := b.Min.X; x < b.Max.X; x++ {
			s.WriteByte(code(img.At(x, y)))
		}
		rows = append(rows, s.String())
	}
	return rows
}

// assertPixels renders page n of res and compares it with want, rows of
// palette letters separated by new lines and indented freely.
func assertPixels(t *testing.T, res *Result, n int, want string) {
	t.Helper()
	var rows []string
	for _, l := range strings.Split(want, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			rows = append(rows, l)
		}
	}
	got := grid(render(t, res, n))
	if strings.Join(got, "\n") != strings.Join(rows, "\n") {
		t.Errorf("page %d pixels:\n%s\nwant:\n%s", n, strings.Join(got, "\n"), strings.Join(rows, "\n"))
	}
}

// texts returns the text runs of page n of res in paint order, with their
// origins in page coordinates, flattening groups.
func texts(res *Result, n int) []placedText {
	var out []placedText
	var walk func([]compose.Fragment, compose.Matrix)
	walk = func(frags []compose.Fragment, m compose.Matrix) {
		for _, f := range frags {
			switch v := f.(type) {
			case *compose.Text:
				var b strings.Builder
				for _, g := range v.Glyphs {
					b.WriteString(g.Text)
				}
				out = append(out, placedText{text: b.String(), x: m.E + v.Origin.X, y: m.F + v.Origin.Y, run: v})
			case *compose.Group:
				tm := v.Transform
				if tm == (compose.Matrix{}) {
					tm = compose.Identity
				}
				walk(v.Content, compose.Matrix{A: 1, D: 1, E: m.E + tm.E, F: m.F + tm.F})
			}
		}
	}
	walk(res.Pages[n-1].Content, compose.Identity)
	return out
}

// placedText is one text run of a page.
type placedText struct {
	text string
	x, y float64
	run  *compose.Text
}

// near reports whether a and b agree within a thousandth of a point.
func near(a, b float64) bool {
	d := a - b
	return d < 1e-3 && d > -1e-3
}

// boxes generates and lays out root on page pb and returns its root box.
func boxes(t testing.TB, root *Node, pb PageBox) *box {
	t.Helper()
	e := newEngine(context.Background(), ahem(t), Options{Page: pb}, pb.Width-pb.Margin[css.Left]-pb.Margin[css.Right], pb.Height-pb.Margin[css.Top]-pb.Margin[css.Bottom])
	rb := e.generate(root)
	e.layoutRoot(rb)
	if e.err != nil {
		t.Fatalf("layout: %v", e.err)
	}
	return rb
}

// kid returns the box reached from b by the child indices path.
func kid(t testing.TB, b *box, path ...int) *box {
	t.Helper()
	for _, i := range path {
		if i >= len(b.kids) {
			t.Fatalf("box has %d kids, want index %d", len(b.kids), i)
		}
		b = b.kids[i]
	}
	return b
}

// rect returns the border box of b in CSS pixels.
func rect(b *box) [4]float64 {
	r := func(v float64) float64 { return math.Round(v/css.Pt*1000) / 1000 }
	return [4]float64{r(b.x), r(b.y), r(b.borderW()), r(b.borderH())}
}

// Style modifiers, lengths in CSS pixels.

var (
	red   = css.Color{R: 1, A: 1}
	blue  = css.Color{B: 1, A: 1}
	lime  = css.Color{G: 1, A: 1}
	black = css.Color{A: 1}
)

func width(v float64) func(*css.Style)  { return func(s *css.Style) { s.Width = css.Pts(px(v)) } }
func height(v float64) func(*css.Style) { return func(s *css.Style) { s.Height = css.Pts(px(v)) } }
func bg(c css.Color) func(*css.Style)   { return func(s *css.Style) { s.BackgroundColor = c } }
func fg(c css.Color) func(*css.Style)   { return func(s *css.Style) { s.Color = c } }
func display(d css.Display) func(*css.Style) {
	return func(s *css.Style) { s.Display = d }
}
func float(f css.Float) func(*css.Style) { return func(s *css.Style) { s.Float = f } }

// dims returns the Dimensions of px values, NaN for auto.
func dims(v ...float64) [4]css.Dimension {
	var out [4]css.Dimension
	for i := range out {
		x := v[i%len(v)]
		if math.IsNaN(x) {
			out[i] = css.Dimension{Kind: css.Auto}
		} else {
			out[i] = css.Pts(px(x))
		}
	}
	return out
}

func margin(v ...float64) func(*css.Style)  { return func(s *css.Style) { s.Margin = dims(v...) } }
func padding(v ...float64) func(*css.Style) { return func(s *css.Style) { s.Padding = dims(v...) } }
func border(w float64, bs css.BorderStyle, c css.Color) func(*css.Style) {
	return func(s *css.Style) {
		for i := range s.Border {
			s.Border[i] = css.Border{Width: px(w), Style: bs, Color: c}
		}
	}
}

// auto is the auto value for [dims].
var auto = math.NaN()

// doc wraps blocks in a root and a body, so no background reaches the
// canvas.
func doc(kids ...*Node) *Node {
	return el(sty(), el(sty(), kids...))
}
