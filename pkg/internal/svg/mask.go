package svg

import (
	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxMaskChain caps the mask elements one mask's content reaches through
// the masks of its own children. Anti-DoS guard, not spec-mandated: a
// mask inside a mask is rare, and a reference back into the chain is an
// error the renderer drops.
const maxMaskChain = 8

// mask returns the soft mask the mask element of id gives the element n,
// styled st, whose bounding box is box, computed when nil, in n's user
// space (CSS Masking 1, 7.1; SVG 1.1, 14.4): the content of the mask
// element clipped to its region, x, y, width and height in maskUnits,
// objectBoundingBox by default at -10%, -10%, 120% and 120%; its content
// in maskContentUnits, userSpaceOnUse by default; by luminance unless its
// mask-type is alpha. found reports an id that names a mask element and
// is not already being drawn; a mask whose region is empty masks
// everything out.
func (r *renderer) mask(n *html.Node, st *style, id string, box *bbox, depth int) (m *typeset.SoftMask, found bool) {
	me := r.d.ids[id]
	if me == nil || me.Data != "mask" || r.masking[me] || len(r.masking) >= maxMaskChain {
		return nil, false
	}
	ms := r.d.styleOf(me, 0)
	kind := typeset.MaskLuminosity
	if ms.maskAlpha {
		kind = typeset.MaskAlpha
	}
	empty := &typeset.SoftMask{Kind: kind}
	var b bbox
	if box != nil {
		b = *box
	} else {
		b = r.bboxOf(n, st, identity, 0)
	}
	w, h := b.x1-b.x0, b.y1-b.y0
	hasBox := b.ok && numeric.Positive(w) && numeric.Positive(h)
	region := func(k string, a axis, def string) float64 {
		l, ok := parseLength(attrOf(me, k))
		if _, set := attrOK(me, k); !set || !ok {
			l, _ = parseLength(def)
		}
		if attrOf(me, "maskUnits") == "userSpaceOnUse" {
			return l.resolve(r.lc(ms), a)
		}
		v := l.v
		if l.u == unitPercent {
			v /= 100
		}
		if a == axisX {
			return b.x0 + v*w
		}
		return b.y0 + v*h
	}
	bboxRegion := attrOf(me, "maskUnits") != "userSpaceOnUse"
	if bboxRegion && !hasBox {
		return empty, true
	}
	x, y := region("x", axisX, "-10%"), region("y", axisY, "-10%")
	rw, rh := region("width", axisX, "120%"), region("height", axisY, "120%")
	if bboxRegion {
		rw, rh = rw-b.x0, rh-b.y0
	}
	if !(rw > 0) || !(rh > 0) {
		return empty, true
	}
	cm := identity
	if attrOf(me, "maskContentUnits") == "objectBoundingBox" {
		if !hasBox {
			return empty, true
		}
		cm = typeset.Matrix{A: w, D: h, E: b.x0, F: b.y0}
	}
	if r.masking == nil {
		r.masking = map[*html.Node]bool{}
	}
	r.masking[me] = true
	defer delete(r.masking, me)
	c := &container{}
	r.children(me, ms, sink{c: c, pending: cm, alpha: 1}, depth)
	if len(c.out) == 0 {
		return empty, true
	}
	clip := &typeset.Path{Ops: rectOps(x, y, rw, rh), Fill: &typeset.Fill{}}
	return &typeset.SoftMask{Kind: kind, Content: []typeset.Fragment{&typeset.Group{Clip: clip, Opacity: 1, Content: c.out}}}, true
}
