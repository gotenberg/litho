package sheet

import (
	"context"
	"fmt"
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

// a4 is a page setup on A4 with half-inch margins and no header.
func a4() PageSetup {
	return PageSetup{Width: 595.28, Height: 841.89, Margin: [4]float64{36, 36, 36, 36}, Scale: 100}
}

// newDoc returns a document of one sheet with Calc's defaults:
// Liberation Sans 10, a point of padding, 12.78-point rows.
func newDoc(sh *Sheet) *Document {
	if sh.DefaultColumnWidth == 0 {
		sh.DefaultColumnWidth = 64
	}
	if sh.DefaultRowHeight == 0 {
		sh.DefaultRowHeight = 12.78
	}
	if sh.Page.Width == 0 {
		sh.Page = a4()
	}
	return &Document{
		Default: Style{Font: Font{Families: []string{"Liberation Sans"}, Size: 10}, Padding: [4]float64{1, 1, 1, 1}},
		Sheets:  []*Sheet{sh},
	}
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

// run is a text run placed on a page: its text, its origin's x and its
// baseline's y from the bottom edge, as a PDF reader reports them, and its
// end.
type run struct {
	text       string
	x, y, endX float64
	size       float64
}

// runs returns the text runs of a page in page coordinates, sorted top
// down then left to right.
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
				w := 0.0
				if len(v.Glyphs) > 0 && fixtureFonts != nil {
					last := v.Glyphs[len(v.Glyphs)-1]
					m := newMeasurer(context.Background(), fixtureFonts, "")
					w = last.X + m.width(font{families: []string{"Liberation Sans"}, size: v.Size}, last.Text)
				}
				x := m.A*v.Origin.X + m.C*v.Origin.Y + m.E
				y := m.B*v.Origin.X + m.D*v.Origin.Y + m.F
				out = append(out, run{text: b.String(), x: x, y: pg.Height - y, endX: x + w*m.A, size: v.Size * m.A})
			case *compose.Group:
				t := v.Transform
				if t == (compose.Matrix{}) {
					t = compose.Identity
				}
				walk(v.Content, mul(t, m))
			}
		}
	}
	walk(pg.Content, compose.Identity)
	sort.SliceStable(out, func(i, j int) bool {
		if d := out[i].y - out[j].y; d > 0.5 || d < -0.5 {
			return out[i].y > out[j].y
		}
		return out[i].x < out[j].x
	})
	return out
}

// mul returns the transform a then b.
func mul(a, b compose.Matrix) compose.Matrix {
	return compose.Matrix{
		A: a.A*b.A + a.B*b.C, B: a.A*b.B + a.B*b.D,
		C: a.C*b.A + a.D*b.C, D: a.C*b.B + a.D*b.D,
		E: a.E*b.A + a.F*b.C + b.E, F: a.E*b.B + a.F*b.D + b.F,
	}
}

// dump returns the runs of a page, one a line, for a failure message.
func dump(pg *compose.Page) string {
	var b strings.Builder
	for _, r := range runs(pg) {
		fmt.Fprintf(&b, "%q x=%.2f..%.2f y=%.2f size=%.2f\n", r.text, r.x, r.endX, r.y, r.size)
	}
	return b.String()
}

// find returns the run whose text is s.
func find(t testing.TB, pg *compose.Page, s string) run {
	t.Helper()
	for _, r := range runs(pg) {
		if r.text == s {
			return r
		}
	}
	t.Fatalf("no run %q on the page:\n%s", s, dump(pg))
	return run{}
}

// textCell returns a cell of text.
func textCell(c int, s string, st *Style) Cell {
	return Cell{Column: c, Value: Value{Kind: Text, Text: s}, Style: st}
}

// numCell returns a cell of a number.
func numCell(c int, v float64, st *Style) Cell {
	return Cell{Column: c, Value: Value{Kind: Number, Number: v}, Style: st}
}

// near reports a and b within tol.
func near(a, b, tol float64) bool {
	d := a - b
	return d <= tol && d >= -tol
}

var _ = css.Top
