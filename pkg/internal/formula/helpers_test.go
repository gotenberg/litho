package formula

import (
	"context"
	"math"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/woff"
)

// fixtureDejaVu holds the DejaVu faces of the reference image, its math
// font among them, provisioned with the corpus fonts.
const fixtureDejaVu = "../../../test/integration/testdata/fonts/gotenberg/truetype/dejavu"

// wptFonts holds the math test fonts of web-platform-tests.
const wptFonts = "../typography/mathtable/testdata/wpt/"

var (
	dejavuOnce  sync.Once
	dejavuFonts *text.Fonts
	dejavuErr   error
)

// dejavu returns a Setter over the DejaVu faces, skipping the test when
// the fixture fonts are absent.
func dejavu(t *testing.T) *Setter {
	t.Helper()
	dejavuOnce.Do(func() {
		if _, dejavuErr = os.Stat(fixtureDejaVu + "/DejaVuMathTeXGyre.ttf"); dejavuErr != nil {
			return
		}
		dejavuFonts, dejavuErr = text.NewFonts(context.Background(), nil, []string{fixtureDejaVu})
	})
	if dejavuErr != nil {
		t.Skipf("fixture fonts absent: %v", dejavuErr)
	}
	return NewSetter(dejavuFonts, nil)
}

// mathStyle returns a style of size points in family, the root of a
// formula: compact, depth 0.
func mathStyle(family string, size float64) Style {
	return Style{Families: []string{family}, Size: size, Weight: 400, Color: typeset.Color{A: 1}}
}

// wpt returns a Setter whose fonts hold the web-platform-tests font name
// as the family name over the DejaVu faces, skipping the test when the
// fixture fonts are absent: an index without a face matches nothing.
func wpt(t *testing.T, name string) *Setter {
	t.Helper()
	base := dejavu(t).fonts
	data, err := os.ReadFile(wptFonts + name + ".woff")
	if err != nil {
		t.Fatal(err)
	}
	program, err := woff.Decode(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	fc, err := text.LoadFace(context.Background(), program, text.FaceDescriptor{Family: name})
	if err != nil {
		t.Fatal(err)
	}
	return NewSetter(base.WithFaces(text.NewFaceSet([]*text.Face{fc})), nil)
}

// lay lays root out in st with s, failing the test on an error.
func lay(t *testing.T, s *Setter, root *Element, st Style) *Box {
	t.Helper()
	b, err := s.Layout(context.Background(), root, st)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// near reports whether a and b differ by less than 0.01 point.
func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

// attrs returns e with the attributes given as name, value pairs.
func attrs(e *Element, kv ...string) *Element {
	for i := 0; i+1 < len(kv); i += 2 {
		e.Attrs = append(e.Attrs, Attr{Name: kv[i], Value: kv[i+1]})
	}
	return e
}

// space returns an mspace of width, height and depth, CSS lengths.
func space(width, height, depth string) *Element {
	return attrs(&Element{Name: "mspace"}, "width", width, "height", height, "depth", depth)
}

// positions returns the position, from the formula's left baseline
// point, of every child box of b's root at depth one and two, in order.
func childAt(b *Box, path ...int) (x, y float64, bx *box) {
	cur := b.root
	for _, i := range path {
		n := -1
		for _, it := range cur.items {
			if it.child == nil {
				continue
			}
			n++
			if n == i {
				x += it.x
				y += it.y
				cur = it.child
				break
			}
		}
	}
	return x, y, cur
}

// rects returns the filled rectangles of b drawn at the origin.
func rects(b *Box) []*typeset.Path {
	var out []*typeset.Path
	for _, f := range b.Draw(0, 0) {
		if p, ok := f.(*typeset.Path); ok {
			out = append(out, p)
		}
	}
	return out
}

// formatFloat formats n for a CSS length.
func formatFloat(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }
