package compose

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/typography/embed"
)

// TakeContent removes from p the text runs, images and paths whose parent
// is elem or an element under it, and returns them in paint order, in page
// coordinates: a fragment taken from inside a group comes back inside a
// copy of the group, its transform, clip and opacity kept. The groups of p
// it takes from are replaced by copies, so a group the model shares is not
// changed. A converter calls it to move what a form control shows from the
// page into the [Field] that draws it. A nil elem takes nothing.
func TakeContent(p *Page, elem *StructElem) []Fragment {
	if elem == nil {
		return nil
	}
	under := map[*StructElem]bool{}
	stack := []*StructElem{elem}
	for len(stack) > 0 && len(under) < MaxStructElems {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if e == nil || under[e] {
			continue
		}
		under[e] = true
		stack = append(stack, e.Kids...)
	}
	kept, taken := take(p.Content, under, 0)
	p.Content = kept
	return taken
}

// take splits content into what stays and what belongs to an element of
// under, at group depth depth.
func take(content []Fragment, under map[*StructElem]bool, depth int) (kept, taken []Fragment) {
	kept = content[:0:0]
	for _, f := range content {
		var parent *StructElem
		switch v := f.(type) {
		case *Text:
			parent = v.Parent
		case *Image:
			parent = v.Parent
		case *Path:
			parent = v.Parent
		case *Group:
			if v == nil || depth >= maxGroupDepth {
				kept = append(kept, f)
				continue
			}
			k, t := take(v.Content, under, depth+1)
			if len(t) == 0 {
				kept = append(kept, f)
				continue
			}
			if len(k) > 0 {
				g := *v
				g.Content = k
				kept = append(kept, &g)
			}
			g := *v
			g.Content = t
			taken = append(taken, &g)
			continue
		}
		if parent != nil && under[parent] {
			taken = append(taken, f)
			continue
		}
		kept = append(kept, f)
	}
	return kept, taken
}

// Measurer measures fragments with the metrics of their faces, loading each
// face's program once. It is used by one goroutine.
type Measurer struct {
	fonts map[string]*embed.Font
}

// NewMeasurer returns an empty Measurer.
func NewMeasurer() *Measurer {
	return &Measurer{fonts: map[string]*embed.Font{}}
}

// font returns the loaded program of face, nil when it does not load.
func (m *Measurer) font(face *Face) *embed.Font {
	if face == nil {
		return nil
	}
	if f, ok := m.fonts[face.Key]; ok {
		return f
	}
	f, err := embed.Load(face.Program)
	if err != nil {
		f = nil
	}
	m.fonts[face.Key] = f
	return f
}

// Glyph returns the glyph of face that draws r, false when the face lacks
// one or does not load.
func (m *Measurer) Glyph(face *Face, r rune) (uint16, bool) {
	f := m.font(face)
	if f == nil {
		return 0, false
	}
	return f.GID(r)
}

// Bounds returns the smallest rectangle that holds frags, in their
// coordinates, false for fragments that hold nothing it measures: a text
// run spans its glyphs' advances, from its face's ascent above the
// baseline to its descent below it; an image its rectangle; a path its
// points and control points, its stroke's half width added; a group what
// it holds under its transform.
func (m *Measurer) Bounds(frags []Fragment) (Rect, bool) {
	b := bounds{x0: math.Inf(1), y0: math.Inf(1), x1: math.Inf(-1), y1: math.Inf(-1)}
	m.add(&b, frags, Identity, 0)
	if !(b.x1 >= b.x0) || !(b.y1 >= b.y0) {
		return Rect{}, false
	}
	return Rect{X: b.x0, Y: b.y0, Width: b.x1 - b.x0, Height: b.y1 - b.y0}, true
}

// bounds is a growing bounding box.
type bounds struct{ x0, y0, x1, y1 float64 }

// point adds (x, y) under t.
func (b *bounds) point(t Matrix, x, y float64) {
	px, py := t.A*x+t.C*y+t.E, t.B*x+t.D*y+t.F
	if !finite(px) || !finite(py) {
		return
	}
	b.x0, b.x1 = math.Min(b.x0, px), math.Max(b.x1, px)
	b.y0, b.y1 = math.Min(b.y0, py), math.Max(b.y1, py)
}

// box adds the rectangle from (x0, y0) to (x1, y1) under t.
func (b *bounds) box(t Matrix, x0, y0, x1, y1 float64) {
	b.point(t, x0, y0)
	b.point(t, x1, y0)
	b.point(t, x0, y1)
	b.point(t, x1, y1)
}

func (m *Measurer) add(b *bounds, frags []Fragment, t Matrix, depth int) {
	for _, f := range frags {
		switch v := f.(type) {
		case *Text:
			m.text(b, v, t)
		case *Image:
			if v != nil {
				b.box(t, v.Rect.X, v.Rect.Y, v.Rect.X+v.Rect.Width, v.Rect.Y+v.Rect.Height)
			}
		case *Path:
			if v == nil {
				continue
			}
			half := 0.0
			if v.Stroke != nil {
				half = v.Stroke.Width / 2
			}
			for _, op := range v.Ops {
				n := 0
				switch op.Kind {
				case MoveTo, LineTo:
					n = 1
				case CurveTo:
					n = 3
				case ClosePath:
				}
				for _, p := range op.P[:n] {
					b.box(t, p.X-half, p.Y-half, p.X+half, p.Y+half)
				}
			}
		case *Group:
			if v == nil || depth >= maxGroupDepth {
				continue
			}
			g := v.Transform
			if g == (Matrix{}) {
				g = Identity
			}
			m.add(b, v.Content, multiply(g, t), depth+1)
		}
	}
}

// text adds the box of the run v under t.
func (m *Measurer) text(b *bounds, v *Text, t Matrix) {
	if v == nil || len(v.Glyphs) == 0 {
		return
	}
	f := m.font(v.Face)
	if f == nil {
		return
	}
	upem := float64(f.UnitsPerEm())
	if upem == 0 {
		upem = 1000
	}
	hs := v.HScale
	if hs == 0 {
		hs = 1
	}
	ascent := float64(f.Ascent()) / upem * v.Size
	descent := -float64(f.Descent()) / upem * v.Size
	for _, g := range v.Glyphs {
		w, _ := f.Width(g.GID)
		x := v.Origin.X + g.X*hs
		y := v.Origin.Y + g.Y
		b.box(t, x, y-ascent, x+float64(w)/upem*v.Size*hs, y+descent)
	}
}

// multiply returns the transform that applies a, then b.
func multiply(a, b Matrix) Matrix {
	return Matrix{
		A: a.A*b.A + a.B*b.C,
		B: a.A*b.B + a.B*b.D,
		C: a.C*b.A + a.D*b.C,
		D: a.C*b.B + a.D*b.D,
		E: a.E*b.A + a.F*b.C + b.E,
		F: a.E*b.B + a.F*b.D + b.F,
	}
}
