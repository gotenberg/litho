package compose

import (
	"errors"
	"image"
	"image/color"
	"math"
	"testing"
)

func square() []PathOp {
	return []PathOp{
		{Kind: MoveTo, P: [3]Point{{X: 0, Y: 0}}},
		{Kind: LineTo, P: [3]Point{{X: 10, Y: 0}}},
		{Kind: LineTo, P: [3]Point{{X: 10, Y: 10}}},
		{Kind: ClosePath},
	}
}

// hugeImage reports bounds past maxImagePixels without holding pixels.
type hugeImage struct{}

func (hugeImage) ColorModel() color.Model { return color.RGBAModel }
func (hugeImage) Bounds() image.Rectangle { return image.Rect(0, 0, 1<<14, 1<<14) }
func (hugeImage) At(int, int) color.Color { return color.Black }

func TestValidateRefuses(t *testing.T) {
	t.Parallel()
	face := &Face{Key: "f"}
	page := func(content ...Fragment) *Document {
		return &Document{Pages: []*Page{{Width: 100, Height: 100, Content: content}}}
	}
	withLinks := func(links ...Link) *Document {
		return &Document{Pages: []*Page{{Width: 100, Height: 100, Links: links}}}
	}
	orphan := &StructElem{Role: "P"}
	cyclic := &StructElem{Role: "Document"}
	cyclic.Kids = []*StructElem{{Role: "Sect", Kids: []*StructElem{cyclic}}}
	shared := &StructElem{Role: "P"}
	selfOutline := make([]Outline, 1)
	selfOutline[0] = Outline{Title: "loop", Dest: Dest{Page: 1}}
	selfOutline[0].Kids = selfOutline
	nan := math.NaN()
	inf := math.Inf(1)

	for _, tc := range []struct {
		name string
		doc  *Document
		want error
	}{
		{"no pages", &Document{}, ErrNoPages},
		{"nil page", &Document{Pages: []*Page{nil}}, ErrInvalidPage},
		{"zero width", &Document{Pages: []*Page{{Width: 0, Height: 10}}}, ErrInvalidPage},
		{"negative height", &Document{Pages: []*Page{{Width: 10, Height: -1}}}, ErrInvalidPage},
		{"NaN width", &Document{Pages: []*Page{{Width: nan, Height: 10}}}, ErrInvalidPage},
		{"infinite height", &Document{Pages: []*Page{{Width: 10, Height: inf}}}, ErrInvalidPage},
		{"beyond Annex C", &Document{Pages: []*Page{{Width: 14401, Height: 10}}}, ErrInvalidPage},
		{"nil fragment", page(nil), ErrInvalidFragment},
		{"typed nil text", page((*Text)(nil)), ErrInvalidFragment},
		{"nil face", page(&Text{Size: 12}), ErrInvalidText},
		{"zero size", page(&Text{Face: face}), ErrInvalidText},
		{"NaN size", page(&Text{Face: face, Size: nan}), ErrInvalidText},
		{"infinite glyph", page(&Text{Face: face, Size: 12, Glyphs: []Glyph{{X: inf}}}), ErrInvalidText},
		{"NaN origin", page(&Text{Face: face, Size: 12, Origin: Point{Y: nan}}), ErrInvalidText},
		{"negative stroke", page(&Text{Face: face, Size: 12, Stroke: -1}), ErrInvalidText},
		{"stroke past the size", page(&Text{Face: face, Size: 12, Stroke: 13}), ErrInvalidText},
		{"NaN skew", page(&Text{Face: face, Size: 12, Skew: nan}), ErrInvalidText},
		{"skew past 1", page(&Text{Face: face, Size: 12, Skew: -1.5}), ErrInvalidText},
		{"NaN horizontal scale", page(&Text{Face: face, Size: 12, HScale: nan}), ErrInvalidText},
		{"horizontal scale under the least", page(&Text{Face: face, Size: 12, HScale: 0.001}), ErrInvalidText},
		{"horizontal scale past the most", page(&Text{Face: face, Size: 12, HScale: 7}), ErrInvalidText},
		{"negative horizontal scale", page(&Text{Face: face, Size: 12, HScale: -1}), ErrInvalidText},
		{"text color", page(&Text{Face: face, Size: 12, Color: Color{R: 2, A: 1}}), ErrInvalidColor},
		{"zero color", page(&Text{Face: face, Size: 12}), ErrInvalidColor},
		{"transparent fill", page(&Path{Ops: square(), Fill: &Fill{Color: Color{R: 1}}}), ErrInvalidColor},
		{"nil source", page(&Image{}), ErrInvalidImage},
		{"empty source", page(&Image{Source: &ImageSource{}}), ErrInvalidImage},
		{"both sources", page(&Image{Source: &ImageSource{Image: image.NewGray(image.Rect(0, 0, 1, 1)), JPEG: []byte{0xFF}}}), ErrInvalidImage},
		{"bad JPEG", page(&Image{Source: &ImageSource{JPEG: []byte("nope")}}), ErrInvalidImage},
		{"empty picture", page(&Image{Source: &ImageSource{Image: image.NewGray(image.Rect(0, 0, 0, 3))}}), ErrInvalidImage},
		{"NaN image rect", page(&Image{Rect: Rect{Width: nan}, Source: &ImageSource{Image: image.NewGray(image.Rect(0, 0, 1, 1))}}), ErrInvalidImage},
		{"image pixels", page(&Image{Source: &ImageSource{Image: hugeImage{}}}), ErrLimitExceeded},
		{"NaN path point", page(&Path{Ops: []PathOp{{Kind: MoveTo, P: [3]Point{{X: nan}}}}, Fill: &Fill{Color: Black}}), ErrInvalidPath},
		{"NaN curve control", page(&Path{Ops: []PathOp{{Kind: MoveTo}, {Kind: CurveTo, P: [3]Point{{}, {Y: inf}, {}}}}}), ErrInvalidPath},
		{"path without MoveTo", page(&Path{Ops: []PathOp{{Kind: LineTo}}}), ErrInvalidPath},
		{"unknown op", page(&Path{Ops: []PathOp{{Kind: MoveTo}, {Kind: 9}}}), ErrInvalidPath},
		{"negative line width", page(&Path{Ops: square(), Stroke: &Stroke{Color: Black, Width: -1}}), ErrInvalidPath},
		{"miter under 1", page(&Path{Ops: square(), Stroke: &Stroke{Color: Black, Miter: 0.5}}), ErrInvalidPath},
		{"zero dashes", page(&Path{Ops: square(), Stroke: &Stroke{Color: Black, Dash: []float64{0, 0}}}), ErrInvalidPath},
		{"negative dash", page(&Path{Ops: square(), Stroke: &Stroke{Color: Black, Dash: []float64{3, -1}}}), ErrInvalidPath},
		{"fill color", page(&Path{Ops: square(), Fill: &Fill{Color: Color{G: -0.1, A: 1}}}), ErrInvalidColor},
		{"stroke alpha", page(&Path{Ops: square(), Stroke: &Stroke{Color: Color{A: nan}}}), ErrInvalidColor},
		{"shading kind", page(shaded(&Shading{Kind: 7, To: Point{X: 1}, Stops: twoStops()})), ErrInvalidShading},
		{"axial without length", page(shaded(&Shading{Stops: twoStops()})), ErrInvalidShading},
		{"radial equal circles", page(shaded(&Shading{Kind: Radial, FromRadius: 2, ToRadius: 2, Stops: twoStops()})), ErrInvalidShading},
		{"radial zero radii", page(shaded(&Shading{Kind: Radial, To: Point{X: 1}, Stops: twoStops()})), ErrInvalidShading},
		{"negative radius", page(shaded(&Shading{Kind: Radial, FromRadius: -1, ToRadius: 2, Stops: twoStops()})), ErrInvalidShading},
		{"NaN shading point", page(shaded(&Shading{To: Point{X: nan}, Stops: twoStops()})), ErrInvalidShading},
		{"singular shading transform", page(shaded(&Shading{To: Point{X: 1}, Stops: twoStops(), Transform: Matrix{A: 1, B: 1, C: 1, D: 1}})), ErrInvalidShading},
		{"one stop", page(shaded(&Shading{To: Point{X: 1}, Stops: twoStops()[:1]})), ErrInvalidShading},
		{"stops not from 0", page(shaded(&Shading{To: Point{X: 1}, Stops: []ColorStop{{Offset: 0.1, Color: Black}, {Offset: 1, Color: Black}}})), ErrInvalidShading},
		{"stops out of order", page(shaded(&Shading{To: Point{X: 1}, Stops: []ColorStop{{Color: Black}, {Offset: 0.6, Color: Black}, {Offset: 0.4, Color: Black}, {Offset: 1, Color: Black}}})), ErrInvalidShading},
		{"NaN stop", page(shaded(&Shading{To: Point{X: 1}, Stops: []ColorStop{{Color: Black}, {Offset: nan, Color: Black}, {Offset: 1, Color: Black}}})), ErrInvalidShading},
		{"stop color", page(shaded(&Shading{To: Point{X: 1}, Stops: []ColorStop{{Color: Color{R: 2, A: 1}}, {Offset: 1, Color: Black}}})), ErrInvalidColor},
		{"invisible shading", page(shaded(&Shading{To: Point{X: 1}, Stops: []ColorStop{{}, {Offset: 1}}})), ErrInvalidShading},
		{"too many stops", page(shaded(&Shading{To: Point{X: 1}, Stops: make([]ColorStop, maxShadingStops+1)})), ErrLimitExceeded},
		{"opacity above 1", page(&Group{Opacity: 1.5}), ErrInvalidGroup},
		{"negative opacity", page(&Group{Opacity: -0.5}), ErrInvalidGroup},
		{"NaN opacity", page(&Group{Opacity: nan}), ErrInvalidGroup},
		{"NaN transform", page(&Group{Transform: Matrix{A: nan, D: 1}}), ErrInvalidGroup},
		{"empty clip", page(&Group{Clip: &Path{}}), ErrInvalidPath},
		{"NaN clip", page(&Group{Clip: &Path{Ops: []PathOp{{Kind: MoveTo, P: [3]Point{{X: nan}}}}}}), ErrInvalidPath},
		{"nested bad fragment", page(&Group{Content: []Fragment{&Group{Content: []Fragment{&Text{}}}}}), ErrInvalidText},
		{"link with neither", withLinks(Link{}), ErrInvalidLink},
		{"link with both", withLinks(Link{URI: "x", Dest: &Dest{Page: 1}}), ErrInvalidLink},
		{"NaN link rect", withLinks(Link{URI: "x", Rect: Rect{X: nan}}), ErrInvalidLink},
		{"link to page 0", withLinks(Link{Dest: &Dest{Page: 0}}), ErrInvalidDest},
		{"link past the end", withLinks(Link{Dest: &Dest{Page: 2}}), ErrInvalidDest},
		{"NaN dest", withLinks(Link{Dest: &Dest{Page: 1, Y: nan}}), ErrInvalidDest},
		{"outline past the end", &Document{Pages: []*Page{{Width: 1, Height: 1}}, Outline: []Outline{{Title: "x", Dest: Dest{Page: 5}}}}, ErrInvalidDest},
		{"nested outline dest", &Document{Pages: []*Page{{Width: 1, Height: 1}}, Outline: []Outline{{Title: "x", Dest: Dest{Page: 1}, Kids: []Outline{{Dest: Dest{Page: -1}}}}}}, ErrInvalidDest},
		{"cyclic outline", &Document{Pages: []*Page{{Width: 1, Height: 1}}, Outline: selfOutline}, ErrNotATree},
		{"detached parent", page(&Path{Ops: square(), Fill: &Fill{Color: Black}, Parent: orphan}), ErrDetachedElement},
		{"detached parent, untagged", page(&Text{Face: face, Size: 1, Color: Black, Parent: orphan}), ErrDetachedElement},
		{"detached link parent", &Document{Structure: &StructElem{Role: "Document"}, Pages: []*Page{{Width: 1, Height: 1, Links: []Link{{URI: "x", Parent: orphan}}}}}, ErrDetachedElement},
		{"cyclic structure", &Document{Structure: cyclic, Pages: []*Page{{Width: 1, Height: 1}}}, ErrNotATree},
		{"shared element", &Document{Structure: &StructElem{Role: "Document", Kids: []*StructElem{shared, shared}}, Pages: []*Page{{Width: 1, Height: 1}}}, ErrNotATree},
		{"nil element", &Document{Structure: &StructElem{Role: "Document", Kids: []*StructElem{nil}}, Pages: []*Page{{Width: 1, Height: 1}}}, ErrInvalidElement},
		{"NUL in role", &Document{Structure: &StructElem{Role: "P\x00"}, Pages: []*Page{{Width: 1, Height: 1}}}, ErrInvalidElement},
		{"roleless element", &Document{Structure: &StructElem{}, Pages: []*Page{{Width: 1, Height: 1}}}, ErrInvalidElement},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.doc.Validate(); !errors.Is(err, tc.want) {
				t.Errorf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateAccepts(t *testing.T) {
	t.Parallel()
	p := &StructElem{Role: "P"}
	doc := &Document{
		Structure: &StructElem{Role: "Document", Kids: []*StructElem{p}},
		Pages: []*Page{{Width: 14400, Height: 0.5, Content: []Fragment{
			&Text{Face: &Face{}, Size: 1, Color: Black, Parent: p},
			&Group{Opacity: 0, Clip: &Path{Ops: square()}},
			&Group{Opacity: 1},
			&Path{Ops: square(), Stroke: &Stroke{Color: Black, Miter: 1, Dash: []float64{0, 2}}},
		}}},
		Outline: []Outline{{Title: "t", Dest: Dest{Page: 1}}},
	}
	if err := doc.Validate(); err != nil {
		t.Errorf("Validate = %v", err)
	}
}

func TestValidateCaps(t *testing.T) {
	t.Parallel()
	pg := &Page{Width: 10, Height: 10}
	pages := make([]*Page, maxPages+1)
	for i := range pages {
		pages[i] = pg
	}
	face := &Face{Key: "f"}
	frags := make([]Fragment, maxFragmentsPerPage+1)
	path := &Path{Ops: square()}
	for i := range frags {
		frags[i] = path
	}
	deep := &Group{}
	for range maxGroupDepth {
		deep = &Group{Content: []Fragment{deep}}
	}
	ops := make([]PathOp, maxOpsPerPath+1)
	outline := make([]Outline, maxOutlineEntries+1)
	for i := range outline {
		outline[i].Dest.Page = 1
	}
	deepOutline := []Outline{{Dest: Dest{Page: 1}}}
	for range maxTreeDepth {
		deepOutline = []Outline{{Dest: Dest{Page: 1}, Kids: deepOutline}}
	}
	deepElem := &StructElem{Role: "Span"}
	for range maxTreeDepth {
		deepElem = &StructElem{Role: "Span", Kids: []*StructElem{deepElem}}
	}
	// Links share the fragment budget.
	links := make([]Link, maxFragmentsPerPage)
	for i := range links {
		links[i].URI = "x"
	}

	for _, tc := range []struct {
		name string
		doc  *Document
	}{
		{"pages", &Document{Pages: pages}},
		{"fragments", &Document{Pages: []*Page{{Width: 1, Height: 1, Content: frags}}}},
		{"fragments and links", &Document{Pages: []*Page{{Width: 1, Height: 1, Content: []Fragment{path}, Links: links}}}},
		{"nested fragments", &Document{Pages: []*Page{{Width: 1, Height: 1, Content: []Fragment{&Group{Content: frags[1:]}}}}}},
		{"glyphs", &Document{Pages: []*Page{{Width: 1, Height: 1, Content: []Fragment{&Text{Face: face, Size: 1, Glyphs: make([]Glyph, maxGlyphsPerRun+1)}}}}}},
		{"path ops", &Document{Pages: []*Page{{Width: 1, Height: 1, Content: []Fragment{&Path{Ops: ops}}}}}},
		{"dash entries", &Document{Pages: []*Page{{Width: 1, Height: 1, Content: []Fragment{&Path{Ops: square(), Stroke: &Stroke{Color: Black, Dash: make([]float64, maxDashEntries+1)}}}}}}},
		{"group depth", &Document{Pages: []*Page{{Width: 1, Height: 1, Content: []Fragment{deep}}}}},
		{"outline entries", &Document{Pages: []*Page{pg}, Outline: outline}},
		{"outline depth", &Document{Pages: []*Page{pg}, Outline: deepOutline}},
		{"structure depth", &Document{Pages: []*Page{pg}, Structure: deepElem}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.doc.Validate(); !errors.Is(err, ErrLimitExceeded) {
				t.Errorf("Validate = %v, want ErrLimitExceeded", err)
			}
		})
	}
}

func TestValidateAcceptsTheDeepestGroups(t *testing.T) {
	t.Parallel()
	deep := &Group{}
	for range maxGroupDepth - 1 {
		deep = &Group{Content: []Fragment{deep}}
	}
	doc := &Document{Pages: []*Page{{Width: 1, Height: 1, Content: []Fragment{deep}}}}
	if err := doc.Validate(); err != nil {
		t.Errorf("Validate = %v", err)
	}
}

// shaded returns a square filled with sh.
func shaded(sh *Shading) *Path {
	return &Path{Ops: square(), Fill: &Fill{Shading: sh}}
}

// twoStops returns opaque black to opaque white.
func twoStops() []ColorStop {
	return []ColorStop{{Color: Black}, {Offset: 1, Color: Color{R: 1, G: 1, B: 1, A: 1}}}
}

func TestValidatePageBoxes(t *testing.T) {
	tests := []struct {
		name        string
		trim, bleed *Rect
		ok          bool
	}{
		{"none", nil, nil, true},
		{"inside", &Rect{X: 1, Y: 1, Width: 8, Height: 8}, &Rect{Width: 10, Height: 10}, true},
		{"past the page", &Rect{X: 1, Y: 1, Width: 10, Height: 8}, nil, false},
		{"trim outside bleed", &Rect{Width: 10, Height: 10}, &Rect{X: 1, Y: 1, Width: 8, Height: 8}, false},
		{"empty", &Rect{X: 1, Y: 1}, nil, false},
	}
	for _, tt := range tests {
		page := &Page{Width: 10, Height: 10, TrimBox: tt.trim, BleedBox: tt.bleed}
		err := (&Document{Pages: []*Page{page}}).Validate()
		if (err == nil) != tt.ok {
			t.Errorf("%s: Validate() = %v", tt.name, err)
		}
	}
}
