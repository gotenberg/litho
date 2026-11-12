package slides

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// handoutGap is the room between two slides of a handout page, and
// between the slides and the margins, the header band and the footer
// band: 1 cm, as Impress lays a handout page out
// (SdPage::CalculateHandoutAreas).
const handoutGap = 72 / 2.54

// handoutBand is the share of the handout page's height, less its top and
// left margins, that the header band above the slides and the footer band
// below them each take: 5%, as Impress computes it
// (SdPage::CalculateHandoutAreas).
const handoutBand = 0.05

// validHandouts reports whether per is a count of slides a handout page
// holds: 1, 2, 3, 4, 6 or 9, the layouts Impress and PowerPoint print.
func validHandouts(per int) bool {
	switch per {
	case 1, 2, 3, 4, 6, 9:
		return true
	}
	return false
}

// handoutNoteLines is the count of lines ruled for notes beside each slide
// of a three-a-page handout.
const handoutNoteLines = 7

// handoutGrid returns the grid of a handout page of per slides, landscape
// or not, as Impress lays it out: its columns and rows, and the cells,
// numbered row by row from 0, that hold the slides in show order, row by
// row. Three slides a page take the first column of a portrait page's two
// by three grid, or the first row of a landscape page's three by two
// grid; ruled lists, for each of them, the cell ruled for its notes, the
// one to its right on a portrait page and the one below it on a landscape
// page. It is nil for any other count.
func handoutGrid(per int, landscape bool) (cols, rows int, cells, ruled []int) {
	switch per {
	case 1:
		return 1, 1, []int{0}, nil
	case 2:
		if landscape {
			return 2, 1, []int{0, 1}, nil
		}
		return 1, 2, []int{0, 1}, nil
	case 3:
		if landscape {
			return 3, 2, []int{0, 1, 2}, []int{3, 4, 5}
		}
		return 2, 3, []int{0, 2, 4}, []int{1, 3, 5}
	case 4:
		return 2, 2, []int{0, 1, 2, 3}, nil
	case 6:
		if landscape {
			return 3, 2, []int{0, 1, 2, 3, 4, 5}, nil
		}
		return 2, 3, []int{0, 1, 2, 3, 4, 5}, nil
	}
	return 3, 3, []int{0, 1, 2, 3, 4, 5, 6, 7, 8}, nil
}

// handoutAreas returns the rectangles the slides of a handout page take,
// in show order, on a page w by h with the margins m (indexed by css.Top
// and the others), for slides sw by sh, per slides a page, and, three a
// page, the rectangle ruled for the notes of each. The page less its
// margins, a header band and a footer band of [handoutBand] each, and a
// [handoutGap] all round is cut into the grid of [handoutGrid] with a gap
// between its cells; every slide takes the size of the largest slide that
// fits a cell, its aspect ratio kept, centred in its cell, and a ruled
// rectangle the same place in its own cell (SdPage::CalculateHandoutAreas).
// A page too small for the grid gets empty rectangles.
//
// # Corpus evidence
//
//   - probe, odp: the pinned LibreOffice stores the slide rectangles of a
//     handout layout in the handout master once a macro sets the master's
//     Layout property. For each of the six counts on eight pages (A4
//     portrait and landscape and Letter with 28 by 15.75 cm slides, A4
//     portrait and landscape with 25.4 by 19.05 cm slides, A4 portrait
//     with 19.05 by 25.4 cm slides, and A4 portrait and landscape with
//     four different margins), the 48 sets of rectangles are the ones
//     this function returns to 0.03 mm, LibreOffice rounding to 0.01 mm.
//     Three a page, the master holds the three slides alone.
//   - probe, odp: the pinned LibreOffice prints no handout without its
//     print dialog: a print to file, a Basic macro's print call, the
//     document settings IsPrintHandout and SlidesPerHandout, the user
//     settings Print/Content/Handout and Print/Other/PagesPerHandout and
//     a handout view all print the slides, and its PDF export has no
//     handout option. The order of the slides on a printed page, row by
//     row, and the ruled rectangles are those of its print path
//     (InitHandoutTemplate in sd's DocumentRenderer), not measured.
func handoutAreas(w, h float64, m [4]float64, sw, sh float64, per int) (slides, ruled []compose.Rect) {
	cols, rows, cells, ruledCells := handoutGrid(per, w > h)
	band := (h - m[css.Top] - m[css.Left]) * handoutBand
	top, bottom := m[css.Top]+band, m[css.Bottom]+band
	aw := w - 2*handoutGap - m[css.Left] - m[css.Right]
	ah := h - 2*handoutGap - top - bottom
	partW := (aw - float64(cols-1)*handoutGap) / float64(cols)
	partH := (ah - float64(rows-1)*handoutGap) / float64(rows)
	slides, ruled = make([]compose.Rect, len(cells)), make([]compose.Rect, len(ruledCells))
	if !positive(partW) || !positive(partH) || !positive(sw) || !positive(sh) {
		return slides, ruled
	}
	k := min(partW/sw, partH/sh)
	tw, th := sw*k, sh*k
	x0 := handoutGap + m[css.Left] + (partW-tw)/2
	y0 := handoutGap + top + (partH-th)/2
	at := func(c int) compose.Rect {
		col, row := c%cols, c/cols
		return compose.Rect{X: x0 + float64(col)*(partW+handoutGap), Y: y0 + float64(row)*(partH+handoutGap), Width: tw, Height: th}
	}
	for i, c := range cells {
		slides[i] = at(c)
	}
	for i, c := range ruledCells {
		ruled[i] = at(c)
	}
	return slides, ruled
}

// noteLines returns the lines ruled across r for notes, as one path:
// [handoutNoteLines] of them the width of r, a seventh of its height
// apart, the last along its bottom edge.
func noteLines(r compose.Rect) []compose.PathOp {
	var ops []compose.PathOp
	for i := 1; i <= handoutNoteLines; i++ {
		y := r.Y + r.Height*float64(i)/handoutNoteLines
		ops = append(ops, lineOps(r.X, y, r.X+r.Width, y)...)
	}
	return ops
}

// handoutSize returns the handout page size: the one the file gives, else
// the notes page size, else Letter, each side within the PDF's limits.
func (b *builder) handoutSize() (float64, float64) {
	for _, s := range [][2]float64{{b.doc.HandoutWidth, b.doc.HandoutHeight}, {b.doc.NotesWidth, b.doc.NotesHeight}} {
		if w, h := s[0], s[1]; w > 0 && h > 0 && w <= office.MaxPageSide && h <= office.MaxPageSide {
			return w, h
		}
	}
	return notesWidth, notesHeight
}

// turned reports whether a handout page w by h turns to print on a page pw
// by ph: one is upright and the other on its side. A square page turns
// nothing.
func turned(w, h, pw, ph float64) bool {
	return w != h && pw != ph && (w > h) != (pw > ph)
}

// turnBox returns the box a shape of the handout master takes once its
// page, w by h with the margins m (indexed by css.Top and the others), is
// turned, the margins staying on their sides: the centre of the box r
// keeps its place in proportion to the page less its margins; a flexible
// box, a rectangle or a text frame, keeps its share of that area too, and
// any other keeps its size, so a picture is not stretched. A page whose
// margins leave no area either way keeps r.
//
// It is the rule Impress sizes the boxes it gives a handout master by:
// each a share of the page less its margins, in a corner of that area
// (SdPage::CreateDefaultPresObj), so the four boxes of such a master land
// on a turned page where Impress would have put them had it made the
// master for that page.
//
// # Corpus evidence
//
//   - probe, odp: the handout master the pinned LibreOffice gives a deck
//     that has none, on an A4 portrait page without margins, holds a
//     header, a date, a footer and a page number box 9.113 by 1.484 cm,
//     43.4% of the width and 5% of the height, one in each corner.
//   - probe, odp: where LibreOffice puts the boxes of a master it turns
//     is not measured. It turns a handout page in its print dialog and
//     its page setup alone; a macro that sets the master's size leaves
//     the boxes where they were, and one that empties the master and
//     sets its layout brings back the slides without them.
func turnBox(r compose.Rect, flexible bool, w, h float64, m [4]float64) compose.Rect {
	cw, ch := w-m[css.Left]-m[css.Right], h-m[css.Top]-m[css.Bottom]
	tw, th := h-m[css.Left]-m[css.Right], w-m[css.Top]-m[css.Bottom]
	if !positive(cw) || !positive(ch) || !positive(tw) || !positive(th) {
		return r
	}
	fx, fy := tw/cw, th/ch
	if flexible {
		return compose.Rect{
			X: m[css.Left] + (r.X-m[css.Left])*fx, Y: m[css.Top] + (r.Y-m[css.Top])*fy,
			Width: r.Width * fx, Height: r.Height * fy,
		}
	}
	cx := m[css.Left] + (r.X+r.Width/2-m[css.Left])*fx
	cy := m[css.Top] + (r.Y+r.Height/2-m[css.Top])*fy
	return compose.Rect{X: cx - r.Width/2, Y: cy - r.Height/2, Width: r.Width, Height: r.Height}
}

// handoutPage draws the handout page numbered n holding the slides group,
// numberOf giving each slide's number: the handout master's background,
// each slide scaled into its rectangle of [handoutAreas] inside the
// hairline frame a notes page draws round its slide, three a page the
// lines ruled for its notes ([noteLines]) beside it, then the handout
// master's shapes, its page number field showing n. It returns the page
// with the structure element of each slide, in order.
//
// The handout page takes the orientation of the page it prints on, the
// paper of the options or its own size, turned as they ask: a page
// [turned] has its sides swapped, the slides laid out on it again, as
// Impress lays a handout out for the orientation of the printer's page
// (InitHandoutTemplate in sd's DocumentRenderer), and the master's shapes
// moved by [turnBox]. It is then scaled down to fit the page, never up,
// and centred, as a slide is.
//
// # Corpus evidence
//
//   - gotenberg/pptx: deck-notes' notes pages frame their slide in a
//     C0C0C0 line 0.1 point wide (see [builder.notesPage]); a handout
//     page's slides are the same kind of page object, whose frame on a
//     handout page is not measured.
//   - probe, odp: the lines ruled for notes are black and solid in the
//     pinned LibreOffice's print path (InitHandoutTemplate in sd's
//     DocumentRenderer), only beside the slides a page holds, and take no
//     width of their own. Their printed width is not measured: they are
//     drawn the width of the frame, where a line of no width would be the
//     thinnest a device draws, which ISO 32000-1, 8.4.3.2 advises against.
func (b *builder) handoutPage(n int, group []*Slide, numberOf func(*Slide) int) (*compose.Page, []*compose.StructElem) {
	mw, mh := b.handoutSize()
	pw, ph := b.pageSize(mw, mh)
	hw, hh := mw, mh
	turn := turned(mw, mh, pw, ph)
	if turn {
		hw, hh = mh, mw
	}
	sw, sh := b.slideSize()
	areas, ruled := handoutAreas(hw, hh, b.doc.HandoutMargins, sw, sh, b.opts.Handouts)
	whole := compose.Rect{Width: hw, Height: hh}
	content := b.fill(&b.doc.HandoutBackground, whole, rectOps(whole), shape.FillNorm)
	var links []compose.Link
	sects := make([]*compose.StructElem, 0, len(group))
	gray := compose.Color{R: notesFrameGray, G: notesFrameGray, B: notesFrameGray, A: 1}
	for i, s := range group {
		frags, slideLinks, sect := b.slideContent(s, numberOf(s), sw, sh)
		sects = append(sects, sect)
		if i >= len(areas) || !positive(areas[i].Width) {
			continue
		}
		r := areas[i]
		k := r.Width / sw
		m := compose.Matrix{A: k, D: k, E: r.X, F: r.Y}
		content = append(content, &compose.Group{
			Transform: m, Opacity: 1,
			Clip:    &compose.Path{Ops: rectOps(compose.Rect{Width: sw, Height: sh}), Fill: &compose.Fill{}},
			Content: frags,
		})
		for _, l := range slideLinks {
			l.Rect = transformRect(m, l.Rect)
			links = append(links, l)
		}
		content = append(content, &compose.Path{Ops: rectOps(r), Stroke: &compose.Stroke{Color: gray, Width: notesFrameWidth}})
		if i < len(ruled) {
			content = append(content, &compose.Path{Ops: noteLines(ruled[i]), Stroke: &compose.Stroke{Color: compose.Color{A: 1}, Width: notesFrameWidth}})
		}
	}
	// The handout master's shapes repeat on every handout page: pagination
	// artifacts (ISO 32000-1, 14.8.2.2.2), their links left out with their
	// structure.
	b.slide, b.number, b.shapes, b.links = nil, n, 0, nil
	scratch := &compose.StructElem{}
	for _, sp := range b.doc.HandoutShapes {
		if sp == nil {
			continue
		}
		art := *sp
		art.Decorative = true
		if turn {
			// A group's shapes stay in the space of the box it had.
			if art.Kind == KindGroup && art.Child == (compose.Rect{}) {
				art.Child = art.Box
			}
			art.Box = turnBox(art.Box, art.Kind == KindGeometry && art.Geometry == nil, mw, mh, b.doc.HandoutMargins)
		}
		content = append(content, b.shape(&art, compose.Identity, 0, scratch)...)
	}
	b.links = nil
	pg := &compose.Page{Width: pw, Height: ph}
	if m := fitMatrix(hw, hh, pw, ph); m == compose.Identity {
		pg.Content, pg.Links = content, links
	} else {
		pg.Content = []compose.Fragment{&compose.Group{Transform: m, Opacity: 1, Content: content}}
		for _, l := range links {
			l.Rect = transformRect(m, l.Rect)
			pg.Links = append(pg.Links, l)
		}
	}
	return pg, sects
}
