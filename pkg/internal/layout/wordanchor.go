package layout

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// This file places the floats of css.Office.WordAnchor, as Word places an
// anchored drawing that wraps the text (ECMA-376 Part 1, 20.4.2.3): at
// its offsets from its paragraph or its page, whatever floats are there,
// the lines after it wrapping it as it lands. A float whose wrap gap
// reaches above its paragraph wraps the lines before it too: the next
// layout pass seeds it, where it landed, into the root's formatting
// context (framewrap.go), and places it there again whatever its
// paragraph does, as Word keeps a drawing where it placed it when the
// wrapping it causes moves its paragraph.

// maxWordPasses bounds the layout passes a document with floats of
// css.Office.WordAnchor wrapping the lines before their paragraphs takes
// past [maxWrapPasses]: each places one more of them. Anti-DoS guard, not
// spec-mandated: a float reaches above its paragraph by its top gap
// alone, which Word sets to 0 by default, so few documents seed any; the
// floats past the bound wrap the lines after their anchor alone.
const maxWordPasses = 8

// hasWordSeed reports whether seeds holds a float of
// css.Office.WordAnchor.
func hasWordSeed(seeds []wrapSeed) bool {
	for _, s := range seeds {
		if s.node != nil {
			return true
		}
	}
	return false
}

// wordShape is the contour of a float of css.Office.WrapPolygon in flow
// coordinates, with the gaps its lines keep from it.
type wordShape struct {
	pts []wrapPt
	gap [4]float64
}

// wrapPt is a point in flow coordinates.
type wrapPt struct{ x, y float64 }

// maxWrapPoints bounds the points of a wrap contour the engine follows.
// Anti-DoS guard, not spec-mandated: Word writes contours of a few dozen
// points; a longer one wraps along its first points' outline.
const maxWrapPoints = 1024

// newWordShape returns the contour poly, in fractions of the border box
// at x, y of w by h, in flow coordinates; nil when poly has fewer than
// three points.
func newWordShape(poly []css.WrapPoint, x, y, w, h float64, gap [4]float64) *wordShape {
	if len(poly) < 3 {
		return nil
	}
	s := &wordShape{gap: gap, pts: make([]wrapPt, 0, min(len(poly), maxWrapPoints))}
	for _, p := range poly[:min(len(poly), maxWrapPoints)] {
		s.pts = append(s.pts, wrapPt{x: x + float64(clamp01(p.X)*w), y: y + float64(clamp01(p.Y)*h)})
	}
	return s
}

// clamp01 returns v within [0, 1], 0 for NaN.
func clamp01(v float64) float64 {
	if !(v > 0) {
		return 0
	}
	return min(v, 1)
}

// extent returns the left and right edges of the part of the contour over
// the band [y, y+h) grown by the top and bottom gaps, grown by the left
// and right gaps; false when no part of it lies there.
func (s *wordShape) extent(y, h float64) (float64, float64, bool) {
	y0, y1 := y-s.gap[css.Bottom], y+max(h, 0)+s.gap[css.Top]
	lo, hi := math.Inf(1), math.Inf(-1)
	n := len(s.pts)
	for i, a := range s.pts {
		b := s.pts[(i+1)%n]
		if max(a.y, b.y) < y0 || min(a.y, b.y) > y1 {
			continue
		}
		// The part of the edge a-b within [y0, y1].
		ta, tb := 0.0, 1.0
		if dy := b.y - a.y; math.Abs(dy) > 1e-9 {
			t0, t1 := (y0-a.y)/dy, (y1-a.y)/dy
			ta, tb = max(0, min(t0, t1)), min(1, max(t0, t1))
		}
		for _, t := range [2]float64{ta, tb} {
			x := a.x + float64(t*(b.x-a.x))
			lo, hi = min(lo, x), max(hi, x)
		}
	}
	if lo > hi {
		return 0, 0, false
	}
	return lo - s.gap[css.Left], hi + s.gap[css.Right], true
}

// layoutWordFloat lays the float f of css.Office.WordAnchor out at its
// offsets from the content box of its formatting context's root, top
// being the top of its paragraph's first line, and records its exclusion
// in bc. It returns the index of the exclusion in bc, -1 for none.
func (e *engine) layoutWordFloat(f *box, cb cblock, bc *bfc, top float64) int {
	if e.err != nil {
		return -1
	}
	f.laidOut = true
	resolveEdges(f, cb.w)
	e.resolveSizing(f, cb, -1, -1)
	f.m = [4]float64{}
	a := f.st.WordAnchor
	// The offset is from the column, the content box of the formatting
	// context's root, whatever the indents of the paragraph.
	left := cb.x
	for p := f.parent; p != nil; p = p.parent {
		if p.establishesBFC() {
			left = p.contentX()
			break
		}
	}
	x, y := left+a.X, top+a.Y
	page := e.pageOf(top + eps)
	switch {
	case a.FromPage:
		y = e.pageTop(page) + a.Y
	case a.Writer:
		y = e.writerArea(a, top) + a.Y
	}
	s, frozen := e.frozen[f.node]
	if frozen {
		x, y, page = s.bx, e.pageTop(s.page)+s.by, s.page
	} else if a.NoOverlap && f.kind == kindReplaced {
		w, h := e.replacedSize(f, cb)
		x, y = clearOverlap(bc, x, y, w, h, cb.x+cb.w)
	}
	f.anchorTop = top
	e.noPaginate++
	if f.kind == kindReplaced {
		f.w, f.h = e.replacedSize(f, cb)
		f.x, f.y = x, y
	} else {
		switch w, ok := specWidth(f, f.st.Width, cb.w); {
		case f.tpart == partWrapper:
			f.w = e.wrapperWidth(f, cb.w, cb.w)
		case ok:
			f.w = constrainWidth(f, w, cb.w)
		default:
			f.w = constrainWidth(f, e.shrinkToFit(f, cb.w), cb.w)
		}
		f.x, f.y = x, y
		e.layoutOwnContext(f, cb)
		if (a.AlignX != 0 || a.AlignY != 0) && !frozen {
			// A frozen float stands where the pass that placed it
			// aligned it.
			f.shift(-float64(a.AlignX*f.borderW()), -float64(a.AlignY*f.borderH()))
		}
	}
	e.noPaginate--
	breaks := !f.st.PageClamp && f.kind != kindReplaced && e.paginating() && f.borderH() > e.box(page).Height
	if breaks {
		// A float that may break, a floating table, taller than its page
		// runs on across the pages it reaches, laid out where it stands.
		f.x, f.y = x, y
		e.layoutOwnContext(f, cb)
	}
	switch {
	case breaks:
	case e.cols == nil:
		e.clampToPage(f, page)
	default:
		// A column has no page box: the float stays inside the column's
		// slice of the flow, which would else count a column more.
		top := e.pageTop(page)
		if ny := max(min(f.y, top+e.areaH(page)-f.borderH()), top); ny != f.y {
			f.shift(0, ny-f.y)
		}
	}
	if len(bc.floats) >= maxFloats {
		e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("more than %d floats in one formatting context: later ones do not push content aside", maxFloats), f.y)
		return -1
	}
	bc.floats = append(bc.floats, wordRect(f))
	return len(bc.floats) - 1
}

// writerArea returns the top of the area of the paragraph whose first line
// starts at top, which a float of css.WordAnchor.Writer is placed from:
// its Rise above the line, but not above the page area's top, Writer
// dropping the spacing above a paragraph that starts a page.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, a paragraph of 1 cm
//     top margin that a frame of none wrap moves to the next page prints
//     its line at the page area's top and the frame 1 cm below it, the
//     frame's svg:y; a paragraph of 0.5 cm top margin that starts the
//     next page prints its frame placed at its top at the page area's
//     top.
func (e *engine) writerArea(a css.WordAnchor, top float64) float64 {
	return max(top-a.Rise, e.pageTop(e.pageOf(top+eps)))
}

// wordRect returns the exclusion of the float f of css.Office.WordAnchor,
// laid out: its border box grown by its wrap gap, along its contour when
// it has one.
func wordRect(f *box) floatRect {
	g := f.st.WrapGap
	bw, bh := f.borderW(), f.borderH()
	r := floatRect{
		side: f.float, wrap: f.st.FrameWrap, word: true, writer: f.st.WordAnchor.Writer,
		x0: f.x - g[css.Left], y0: f.y - g[css.Top],
		x1: f.x + bw + g[css.Right], y1: f.y + bh + g[css.Bottom],
	}
	r.shape = newWordShape(f.st.WrapPolygon, f.x, f.y, bw, bh, g)
	r.border = [4]float64{f.x, f.y, f.x + bw, f.y + bh}
	return r
}

// clearOverlap returns where a float of css.WordAnchor NoOverlap, its
// border box w by h at x, y, goes so that it overlaps the border box of
// no float of css.Office.WordAnchor of bc: right of each one it meets,
// or, past right, below it at x.
func clearOverlap(bc *bfc, x, y, w, h, right float64) (float64, float64) {
	x0 := x
	for range len(bc.floats) + 1 {
		moved := false
		for _, r := range bc.floats {
			if !r.word {
				continue
			}
			b := r.border
			if x < b[2]-eps && x+w > b[0]+eps && y < b[3]-eps && y+h > b[1]+eps {
				if b[2]+w <= right+eps {
					x = b[2]
				} else {
					x, y = x0, b[3]
				}
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	return x, y
}

// collectWordWraps returns the exclusions of the floats of
// css.Office.WordAnchor of the tree rooted at b that the previous pass
// placed, and of the first other one whose wrap gap reaches above its
// paragraph, in tree order, relative to their page's area. Seeding one
// more float a pass places each where its paragraph lands once the
// floats before it wrap the text, as Word places them one after the
// other. A Writer frame also reaches the content before its paragraph
// when its border box starts within [writerTouch] of the bottom of the
// block before the paragraph, Writer finding the two touching.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, a 3 by 2 cm frame
//     at svg:y 0 of the second of two unspaced paragraphs, of right,
//     dynamic or parallel wrap, placed from the top or at the top, moves
//     the first paragraph's line right of itself; so does it when the
//     second paragraph has a 0.5 cm top margin, and not when the first
//     has a 0.5 cm bottom margin.
//   - libreoffice/odt: layout/abi11870-2.odt sets a picture at the top
//     of a heading 0.423 cm below its own top, the heading before it
//     ending 0.212 cm above that: LibreOffice leaves that heading's line
//     where it is.
func (e *engine) collectWordWraps(b *box, out []wrapSeed) []wrapSeed {
	added := false
	var walk func(b, before *box)
	walk = func(b, before *box) {
		var last *box
		for _, k := range b.kids {
			if k.float != css.FloatNone && k.laidOut && k.st.WordAnchor.Set && len(out) < maxFloats {
				_, frozen := e.frozen[k.node]
				r := wordRect(k)
				reach, touch := k.anchorTop, false
				if r.writer {
					// A Writer frame wraps the lines before its paragraph
					// when it reaches above the paragraph's area.
					reach = e.writerArea(k.st.WordAnchor, k.anchorTop)
					touch = before != nil && k.y < before.y+before.borderH()+writerTouch-eps
				}
				if frozen || (!added && (r.y0 < reach-eps || touch)) {
					added = added || !frozen
					p := e.pageOf(k.y + eps)
					top := e.pageTop(p)
					out = append(out, wrapSeed{
						page: p, mode: r.wrap, node: k.node, shape: k.st.WrapPolygon, gap: k.st.WrapGap,
						x0: r.x0, y0: r.y0 - top, x1: r.x1, y1: r.y1 - top,
						bx: k.x, by: k.y - top, bw: k.borderW(), bh: k.borderH(),
					})
				}
			}
			walk(k, last)
			if k.float == css.FloatNone && !k.abs && k.laidOut {
				last = k
			}
		}
	}
	walk(b, nil)
	return out
}

// split returns the first piece of the band [l, r) beside the floats of
// css.FrameWrapBoth of c over [y, y+h) that ends past from: the band
// less each such float's exclusion. It returns false when no piece does.
func (c *bfc) split(y, h, l, r, from float64) (float64, float64, bool) {
	var cuts [][2]float64
	for _, f := range c.floats {
		by, bh := f.wordBand(y, h)
		if f.wrap != css.FrameWrapBoth || !f.overlaps(by, bh) {
			continue
		}
		x0, x1, ok := f.extentOver(by, bh)
		if ok && x1 > l+eps && x0 < r-eps {
			cuts = append(cuts, [2]float64{x0, x1})
		}
	}
	slices.SortStableFunc(cuts, func(a, b [2]float64) int { return cmp.Compare(a[0], b[0]) })
	at := l
	for _, cut := range cuts {
		if cut[0] > at+eps && min(cut[0], r) > from+eps {
			return max(at, from), min(cut[0], r), true
		}
		at = max(at, cut[1])
	}
	if r > at+eps && r > from+eps {
		return max(at, from), r, true
	}
	return 0, 0, false
}
