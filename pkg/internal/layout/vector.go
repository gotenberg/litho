package layout

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// composeGroupDepth is the composition model's bound on group nesting, 24,
// which the page group, the engine's own groups (at most maxGroupDepth)
// and a vector picture's share.
const composeGroupDepth = maxGroupDepth + 4

// maxVectorNodes bounds the fragments one vector background paints over
// all its tiles: past it fewer tiles are drawn, with a warning. Anti-DoS
// guard, not spec-mandated: a tiled icon of a few dozen paths covers a
// page in a few hundred tiles.
const maxVectorNodes = 200_000

// pictureFrag returns the fragment that paints pic over r, its leaves
// hanging from parent: an image for a raster picture, a group of the
// vector picture's fragments for a vector one, nil when the vector draws
// nothing.
func (e *engine) pictureFrag(pic *Picture, r compose.Rect, parent *compose.StructElem) compose.Fragment {
	if pic.Vector == nil {
		return &compose.Image{Rect: r, Source: pic.Source, Parent: parent}
	}
	frags := pic.Vector(e.ctx, r)
	if len(frags) == 0 || depthOf(frags) > MaxVectorDepth {
		return nil
	}
	return &compose.Group{Opacity: 1, Transform: compose.Identity, Content: adopt(frags, parent, 0)}
}

// adopt returns a copy of frags whose leaves hang from parent: a vector
// picture's fragments are shared between its placements.
func adopt(frags []compose.Fragment, parent *compose.StructElem, depth int) []compose.Fragment {
	out := make([]compose.Fragment, 0, len(frags))
	for _, f := range frags {
		switch v := f.(type) {
		case *compose.Group:
			g := *v
			if depth < MaxVectorDepth {
				g.Content = adopt(v.Content, parent, depth+1)
			}
			out = append(out, &g)
		case *compose.Text:
			c := *v
			c.Parent = parent
			out = append(out, &c)
		case *compose.Image:
			c := *v
			c.Parent = parent
			out = append(out, &c)
		case *compose.Path:
			c := *v
			c.Parent = parent
			out = append(out, &c)
		}
	}
	return out
}

// depthOf returns the group nesting of frags.
func depthOf(frags []compose.Fragment) int {
	d := 0
	for _, f := range frags {
		if g, ok := f.(*compose.Group); ok {
			d = max(d, 1+depthOf(g.Content))
		}
	}
	return d
}

// fragNodes returns how many fragments f is, its groups' content and the
// groups themselves counted.
func fragNodes(f compose.Fragment) int {
	g, ok := f.(*compose.Group)
	if !ok {
		return 1
	}
	n := 1
	for _, k := range g.Content {
		n += fragNodes(k)
	}
	return n
}

// fitLeaf returns f, a leaf of the paint tree under depth open groups, or
// nil, with a warning, when it is a group too deep for the room the page
// group and those groups leave in the composition model.
func (e *engine) fitLeaf(f compose.Fragment, depth int) compose.Fragment {
	g, ok := f.(*compose.Group)
	if !ok {
		return f
	}
	if 1+depth+depthOf([]compose.Fragment{g}) > composeGroupDepth {
		e.warnOnce(codeGroupDepth, fmt.Sprintf("a vector picture under groups nested deeper than %d levels was left out", composeGroupDepth-1-MaxVectorDepth), -1)
		return nil
	}
	return f
}

// naturalPicture returns pic as a background or an object-fit sizes it
// within the area w by h: a vector picture without a natural width or
// height takes the area's, or the size its ratio fits in the area when it
// has one and lacks both (CSS Backgrounds 3, 3.9, and CSS Images 3, 5.5,
// the default sizing algorithm against the area). Any other picture is
// returned as it is.
func naturalPicture(pic *Picture, w, h float64) *Picture {
	if pic.Vector == nil || !pic.NoNaturalWidth && !pic.NoNaturalHeight || !numeric.Positive(w) || !numeric.Positive(h) {
		return pic
	}
	p := *pic
	ratio := 0.0
	if !pic.NoRatio && pic.Width > 0 && pic.Height > 0 {
		ratio = pic.Width / pic.Height
	}
	switch {
	case pic.NoNaturalWidth && pic.NoNaturalHeight && ratio > 0:
		p.Width, p.Height = w, w/ratio
		if p.Height > h {
			p.Width, p.Height = h*ratio, h
		}
	case pic.NoNaturalWidth && pic.NoNaturalHeight:
		p.Width, p.Height = w, h
	case pic.NoNaturalWidth:
		p.Width = w
		if ratio > 0 {
			p.Width = p.Height * ratio
		}
	default:
		p.Height = h
		if ratio > 0 {
			p.Height = p.Width / ratio
		}
	}
	p.NoNaturalWidth, p.NoNaturalHeight = false, false
	return &p
}

// vectorTiles trims the tiles of a vector background to the
// [maxVectorNodes] budget, one fragment per tile counted at least.
func (e *engine) vectorTiles(tiles []compose.Rect, nodes int, y float64) []compose.Rect {
	limit := max(maxVectorNodes/max(nodes, 1), 1)
	if len(tiles) <= limit {
		return tiles
	}
	e.warn(codeBackgroundTiles, fmt.Sprintf("a vector background repeated past %d fragments was drawn %d times", maxVectorNodes, limit), y)
	return tiles[:limit]
}

// pictureRatio returns the natural aspect ratio of pic, 0 for none.
func pictureRatio(pic *Picture) float64 {
	if pic == nil || pic.NoRatio || !numeric.Positive(pic.Width) || !numeric.Positive(pic.Height) {
		return 0
	}
	return pic.Width / pic.Height
}

// objectPicture returns pic as object-fit sizes it in a content box of w
// by h (CSS Images 3, 5.5): a vector picture without natural dimensions
// has the box's, through its ratio for contain, cover, none and
// scale-down.
func objectPicture(st *css.Style, pic *Picture, w, h float64) *Picture {
	if pic.Vector == nil || !pic.NoNaturalWidth && !pic.NoNaturalHeight {
		return pic
	}
	if st.ObjectFit == css.FitFill {
		p := *pic
		p.Width, p.Height = w, h
		return &p
	}
	return naturalPicture(pic, w, h)
}
