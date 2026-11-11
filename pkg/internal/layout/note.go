package layout

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// placedArea is the painted note area of a page and the offset of its
// layout origin in the page area.
type placedArea struct {
	disp *ditem
	x, y float64
}

// noteStyle returns the style of the note area of page p: the page's
// @note-area style, else a block of the root's inherited properties.
func (e *engine) noteStyle(p int) *css.Style {
	if st := e.box(p).NoteArea; st != nil {
		return st
	}
	if e.rootBox != nil {
		return e.anonOf(e.rootBox.st)
	}
	return css.Initial()
}

// layoutNoteArea lays out the note area of page p holding the bodies of
// calls, styled st, at the origin, off the page cuts, and returns it. The
// area is a block container, or a flex or grid container whose items are
// the bodies, as its display asks.
func (e *engine) layoutNoteArea(p int, calls []*box, st *css.Style) *box {
	area := &box{st: st, kind: kindBlock, first: true, last: true, parent: e.rootBox}
	if e.rootBox != nil {
		area.lang, area.depth = e.rootBox.lang, e.rootBox.depth+1
	}
	for _, c := range calls {
		if c.fnBody != nil {
			c.fnBody.parent = area
			c.fnBody.flexItem, c.fnBody.gridItem = false, false
			area.kids = append(area.kids, c.fnBody)
		}
	}
	switch {
	case st.Display == css.Flex || st.Display == css.InlineFlex || isGrid(st.Display):
		grid := isGrid(st.Display)
		kids := make([]*box, 0, len(area.kids))
		for _, k := range area.kids {
			if k.inline {
				a := &box{
					st: e.anonOf(st), kind: kindBlock, anon: true, first: true, last: true, parent: area,
					lang: area.lang, depth: area.depth + 1, inlineKids: true, kids: []*box{k},
				}
				k.parent = a
				k = a
			}
			k.flexItem, k.gridItem = !grid, grid
			kids = append(kids, k)
		}
		area.kids = kids
		if grid {
			area.grid = &gridData{}
		} else {
			area.flex = &flexData{}
		}
	default:
		e.normalize(area)
	}
	e.noPaginate++
	e.layoutBlock(area, cblock{w: e.areaW(p)}, &bfc{}, 0, &marginState{}, breakNone)
	e.layoutPositioned(area)
	e.noPaginate--
	return area
}

// noteRoom returns the height the note area a takes from the top of its
// page area: its margin box's, none when it is positioned out of the flow.
func (e *engine) noteRoom(a *box) float64 {
	if isAbs(a.st) {
		return 0
	}
	return areaHeight(a)
}

// noteOffset returns where the note area a, laid out at the origin, goes
// in the page area of page p: an absolutely positioned area by its insets
// against the page area (CSS 2.1, 10.3.7 and 10.6.4, the static position
// being the area's top left), a right float against the right edge, any
// other at the top left.
func (e *engine) noteOffset(p int, a *box) (float64, float64) {
	aw, ah := areaOf(e.box(p))
	x0, y0 := a.x-a.m[css.Left], a.y-a.m[css.Top]
	// The area was laid out as a block in the flow, whose right margin
	// takes the room its width leaves: out of the flow it is the
	// specified one, auto being 0.
	mw := a.borderW() + a.m[css.Left]
	if a.st.Margin[css.Right].Kind != css.Auto {
		mw += resolve(a.st.Margin[css.Right], aw)
	}
	x, y := 0.0, 0.0
	switch {
	case isAbs(a.st):
		if l, ok := insetOf(a.st.Left, aw); ok {
			x = l
		} else if r, ok := insetOf(a.st.Right, aw); ok {
			x = aw - r - mw
		}
		if t, ok := insetOf(a.st.Top, ah); ok {
			y = t
		} else if b, ok := insetOf(a.st.Bottom, ah); ok {
			y = ah - b - a.marginH()
		}
	case a.st.Float == css.FloatRight:
		x = aw - mw
	}
	return x - x0, y - y0
}

// placeAreaNote is [engine.placeNote] for a note call: its body joins the
// note area of page p when the page, its footnote area included, has the
// room for the area grown by it.
func (e *engine) placeAreaNote(p int, call *box, bottom float64, later bool) (ok, room bool) {
	fp := e.footAt(p)
	if len(fp.notes) >= maxFootnotesPerPage {
		return false, false
	}
	calls := append(slices.Clone(fp.notes), call)
	h := e.noteRoom(e.layoutNoteArea(p, calls, e.noteStyle(p)))
	first := later && len(fp.notes) == 0
	_, ah := areaOf(e.box(p))
	used := max(bottom-e.pageTop(p), 0)
	if used+h+fp.h > ah+eps && !(first && used <= eps) {
		return false, true
	}
	fp.notes, fp.nh = calls, h
	e.foot.where[call] = p
	e.geom.recut(p)
	return true, false
}

// noteShift returns how far the note area of page k pushes the page's
// flow down.
func (e *engine) noteShift(k int) float64 {
	if e.foot == nil || k < 0 || k >= len(e.foot.pages) {
		return 0
	}
	return e.foot.pages[k].nh
}

// paintNoteAreas lays out and paints the note area of every page that
// holds bodies.
func (e *engine) paintNoteAreas() {
	f := e.foot
	f.ndisp = map[int]placedArea{}
	for p, fp := range f.pages {
		if len(fp.notes) == 0 || e.err != nil {
			continue
		}
		area := e.layoutNoteArea(p, fp.notes, e.noteStyle(p))
		pt := &painter{e: e, top: &ditem{group: true, opacity: 1}, mono: -1, alpha: 1}
		pt.frames = []*frame{{container: pt.top, root: area}}
		pt.context(area)
		x, y := e.noteOffset(p, area)
		f.ndisp[p] = placedArea{disp: pt.top, x: x, y: y}
	}
}

// assembleNoteArea adds the note area of page k to pg, whose page box is
// pb, over the flow.
func (e *engine) assembleNoteArea(pg *compose.Page, k int, pb PageBox, used map[*compose.StructElem]bool) {
	pa, ok := e.foot.ndisp[k]
	if !ok {
		return
	}
	frags := e.flatten(pa.disp, 1, false)
	for _, fr := range frags {
		markTree(fr, used)
	}
	pg.Content = append(pg.Content, &compose.Group{
		Transform: compose.Matrix{A: 1, D: 1, E: pb.Margin[css.Left] + pa.x, F: pb.Margin[css.Top] + pa.y},
		Opacity:   1,
		Content:   frags,
	})
}
