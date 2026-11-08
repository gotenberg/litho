package slides

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

var (
	fixtureFontsOnce sync.Once
	fixtureFonts     *text.Fonts
)

// pinnedFonts returns an index of the pinned fixture fonts, skipping the
// test when they are not fetched.
func pinnedFonts(t testing.TB) *text.Fonts {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "test", "integration", "testdata", "fonts")
	fixtureFontsOnce.Do(func() {
		if _, err := os.Stat(dir); err != nil {
			return
		}
		fixtureFonts, _ = text.NewFonts(context.Background(), nil, []string{dir})
	})
	if fixtureFonts == nil || fixtureFonts.Len() == 0 {
		t.Skip("fixture fonts not fetched (make html-fixtures)")
	}
	return fixtureFonts
}

// convert converts doc with the pinned fonts.
func convert(t testing.TB, doc *Document, opts office.Options) *compose.Document {
	t.Helper()
	out, err := Convert(context.Background(), doc, pinnedFonts(t), opts)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return out
}

// deck returns a 4:3 deck of the given slides, 720 by 540 points.
func deck(slides ...*Slide) *Document {
	return &Document{Width: 720, Height: 540, Slides: slides}
}

// arial is a run style of Arial at size points, black.
func arial(size float64) RunStyle {
	return RunStyle{Families: []string{"Arial"}, Size: size, Color: css.Color{A: 1}}
}

// para returns a paragraph of one run of text in st.
func para(s string, st RunStyle) Paragraph {
	return Paragraph{Runs: []Run{{Text: s, Style: st}}, Style: st}
}

// box returns a text shape at x, y, w, h without insets, wrapping.
func box(x, y, w, h float64, paras ...Paragraph) *Shape {
	return &Shape{Box: compose.Rect{X: x, Y: y, Width: w, Height: h}, Text: &TextBody{Paragraphs: paras, Wrap: true}}
}

// run is a text run placed on a page: its text, its origin, its size.
type run struct {
	text string
	x, y float64
	size float64
}

// runs returns the text runs of a page in page coordinates, y down, sorted
// top down then left to right.
func runs(pg *compose.Page) []run {
	var out []run
	var walk func(frags []compose.Fragment, m compose.Matrix)
	walk = func(frags []compose.Fragment, m compose.Matrix) {
		for _, f := range frags {
			switch v := f.(type) {
			case *compose.Text:
				var b strings.Builder
				for _, g := range v.Glyphs {
					b.WriteString(g.Text)
				}
				x, y := apply(m, v.Origin.X, v.Origin.Y)
				if len(v.Glyphs) > 0 {
					x, y = apply(m, v.Origin.X+v.Glyphs[0].X, v.Origin.Y+v.Glyphs[0].Y)
				}
				out = append(out, run{text: b.String(), x: x, y: y, size: v.Size * m.D})
			case *compose.Group:
				t := v.Transform
				if t == (compose.Matrix{}) {
					t = compose.Identity
				}
				walk(v.Content, then(t, m))
			}
		}
	}
	walk(pg.Content, compose.Identity)
	sort.SliceStable(out, func(i, j int) bool {
		if d := out[i].y - out[j].y; d > 0.5 || d < -0.5 {
			return out[i].y < out[j].y
		}
		return out[i].x < out[j].x
	})
	return out
}

// find returns the first run whose text starts with s.
func find(t *testing.T, pg *compose.Page, s string) run {
	t.Helper()
	for _, r := range runs(pg) {
		if strings.HasPrefix(strings.TrimSpace(r.text), s) {
			return r
		}
	}
	t.Fatalf("no run %q in %v", s, runs(pg))
	return run{}
}

// near reports whether a and b differ by at most tol.
func near(a, b, tol float64) bool {
	d := a - b
	return d <= tol && d >= -tol
}
