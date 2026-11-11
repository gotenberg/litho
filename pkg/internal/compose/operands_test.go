package compose_test

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"regexp"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

var (
	operandLine = regexp.MustCompile(`^(?:[-+0-9. ]+) (?:cm|Tm|m|l|c|w)$`)
	operand     = regexp.MustCompile(`[-+]?[0-9]*\.?[0-9]+`)
)

// largestOperand returns the largest magnitude among the operands of the
// cm, Tm, m, l, c and w operators of out's content streams.
func largestOperand(t *testing.T, out []byte) float64 {
	t.Helper()
	var m float64
	for line := range bytes.SplitSeq(inflated(t, out), []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !operandLine.Match(line) {
			continue
		}
		for _, n := range operand.FindAll(line, -1) {
			v, err := strconv.ParseFloat(string(n), 64)
			if err != nil {
				t.Fatalf("operand %q: %v", n, err)
			}
			m = max(m, math.Abs(v))
		}
	}
	return m
}

// scene returns a page of text, an image, a dashed stroke, a shading, a
// clipped group and a turned one, laid out dy points down a flow and
// brought back onto the page by a group translated by -dy, as the layout
// engines place a page's slice of a long flow.
func scene(t *testing.T, dy float64) *compose.Page {
	t.Helper()
	pix := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range pix.Pix {
		pix.Pix[i] = 0xFF
	}
	for y := range 4 {
		pix.Set(y, y, color.RGBA{A: 0xFF})
	}
	axial := &compose.Shading{
		From: compose.Point{X: 20, Y: 150 + dy}, To: compose.Point{X: 120, Y: 150 + dy},
		Stops: []compose.ColorStop{{Color: red}, {Offset: 1, Color: blue}},
	}
	content := []compose.Fragment{
		run(t, sans, 20, compose.Point{X: 20, Y: 40 + dy}, "Folded"),
		&compose.Image{Rect: compose.Rect{X: 150, Y: 20 + dy, Width: 40, Height: 40}, Source: encode(t, pix)},
		&compose.Path{Ops: rect(20, 80+dy, 160, 30), Stroke: &compose.Stroke{Color: compose.Black, Width: 3, Dash: []float64{6, 3}}},
		&compose.Path{Ops: rect(20, 130+dy, 100, 40), Fill: &compose.Fill{Shading: axial}},
		&compose.Group{
			Clip:    &compose.Path{Ops: rect(140, 130+dy, 20, 20)},
			Content: []compose.Fragment{&compose.Path{Ops: rect(130, 120+dy, 60, 60), Fill: &compose.Fill{Color: compose.Black}}},
		},
		&compose.Group{
			Transform: compose.Matrix{A: 0, B: 1, C: -1, D: 0, E: 260, F: 20 + dy},
			Content:   []compose.Fragment{run(t, serif, 16, compose.Point{X: 0, Y: 0}, "Turned")},
		},
	}
	if dy != 0 {
		content = []compose.Fragment{&compose.Group{Transform: compose.Matrix{A: 1, D: 1, F: -dy}, Content: content}}
	}
	return &compose.Page{Width: 300, Height: 200, Content: content}
}

// samePixels fails t when a and b differ by more than tol on a channel of
// a pixel.
func samePixels(t *testing.T, a, b image.Image, tol int) {
	t.Helper()
	if a.Bounds() != b.Bounds() {
		t.Fatalf("bounds %v and %v", a.Bounds(), b.Bounds())
	}
	differ := 0
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			want := color.NRGBAModel.Convert(a.At(x, y)).(color.NRGBA)
			if !near(b, x, y, want, tol) {
				differ++
			}
		}
	}
	if differ > 0 {
		t.Errorf("%d pixels differ", differ)
	}
}

func TestWriteFoldsGroupTranslations(t *testing.T) {
	t.Parallel()
	const dy = 60000
	plain := write(t, &compose.Document{Pages: []*compose.Page{scene(t, 0)}})
	far := write(t, &compose.Document{Pages: []*compose.Page{scene(t, dy)}})
	// The translation is folded at every version, so the far scene writes
	// the numbers the plain one does.
	if got := largestOperand(t, far); got > 300 {
		t.Errorf("largest operand %v, want the page's own coordinates", got)
	}
	// So is the shading's: its coordinates are the page's too.
	coords := regexp.MustCompile(`/Coords \[[^\]]*\]`)
	if a, b := coords.Find(plain), coords.Find(far); a == nil || !bytes.Equal(a, b) {
		t.Errorf("shading %s, want %s", b, a)
	}
	samePixels(t, render(t, open(t, plain), 1), render(t, open(t, far), 1), 8)
}

func TestWriteFitsOperandsInPDF14(t *testing.T) {
	t.Parallel()
	// A drawing 20 metres wide across the page, an image as wide, text
	// far along the flow, two shadings as wide, one with a transform, and
	// a turned group placed 50000 points away, whose content comes back
	// onto the page.
	pix := image.NewGray(image.Rect(0, 0, 2, 1))
	pix.Pix[0] = 0x00
	pix.Pix[1] = 0xFF
	page := &compose.Page{Width: 300, Height: 200, Content: []compose.Fragment{
		&compose.Path{Ops: rect(-28000, 20, 56300, 20), Stroke: &compose.Stroke{Color: compose.Black, Width: 4, Dash: []float64{9, 3}}},
		&compose.Image{Rect: compose.Rect{X: -49850, Y: 60, Width: 100000, Height: 20}, Source: encode(t, pix)},
		run(t, sans, 12, compose.Point{X: 40000, Y: 100}, "far"),
		&compose.Path{Ops: rect(-30000, 120, 60300, 20), Fill: &compose.Fill{Shading: &compose.Shading{
			From: compose.Point{X: -30000}, To: compose.Point{X: 30300},
			Stops: []compose.ColorStop{{Color: red}, {Offset: 0.5, Color: red}, {Offset: 0.5, Color: blue}, {Offset: 1, Color: blue}},
		}}},
		&compose.Path{Ops: rect(-30000, 150, 60300, 20), Fill: &compose.Fill{Shading: &compose.Shading{
			From: compose.Point{X: -30000}, To: compose.Point{X: 30300},
			Stops:     []compose.ColorStop{{Color: red}, {Offset: 0.5, Color: red}, {Offset: 0.5, Color: blue}, {Offset: 1, Color: blue}},
			Transform: compose.Matrix{A: 1, D: 1, F: 0.5},
		}}},
		&compose.Group{
			Transform: compose.Matrix{A: 0, B: 1, C: -1, D: 0, E: 50000, F: 100},
			Content: []compose.Fragment{
				&compose.Group{
					Clip:    &compose.Path{Ops: rect(-50, 49790, 60, 60)},
					Content: []compose.Fragment{&compose.Path{Ops: rect(-40, 49800, 40, 40), Fill: &compose.Fill{Color: compose.Black}}},
				},
			},
		},
	}}
	doc := &compose.Document{Pages: []*compose.Page{page}}
	at14 := writeVersion(t, doc, compose.Version14)
	at17 := writeVersion(t, doc, compose.Version17)
	if got := largestOperand(t, at14); got > 32767 {
		t.Errorf("largest operand at PDF 1.4 %v, want at most 32767", got)
	}
	if got := largestOperand(t, at17); got <= 32767 {
		t.Errorf("largest operand at PDF 1.7 %v: the test no longer reaches the PDF 1.4 limit", got)
	}
	img14, img17 := render(t, open(t, at14), 1), render(t, open(t, at17), 1)
	samePixels(t, img17, img14, 8)
	for _, c := range []struct {
		x, y int
		what string
	}{
		{148, 21, "the wide stroke"},
		{100, 70, "the wide image's dark half"},
		{180, 90, "the turned group's square"},
	} {
		if !dark(img14.At(c.x, c.y)) {
			t.Errorf("%s: pixel (%d, %d) = %v, want dark", c.what, c.x, c.y, img14.At(c.x, c.y))
		}
	}
}
