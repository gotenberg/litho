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
// (SdPage::CalculateHandoutAreas, nGapW and nGapH).
const handoutGap = 72 / 2.54

// handoutBand is the share of the handout page's height, less its top and
// left margins, that the header band above the slides and the footer band
// below them each take: 5%, as Impress computes it
// (SdPage::CalculateHandoutAreas, nHeaderFooterHeight).
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

// handoutGrid returns the grid of a handout page of per slides, landscape
// or not, as Impress lays it out: its columns and rows, and the cells,
// numbered row by row from 0, that hold the slides in show order, row by
// row. Three slides a page take the first column of a portrait page's two
// by three grid, or the first row of a landscape page's three by two
// grid, the other cells left for notes.
func handoutGrid(per int, landscape bool) (cols, rows int, cells []int) {
	switch per {
	case 1:
		return 1, 1, []int{0}
	case 2:
		if landscape {
			return 2, 1, []int{0, 1}
		}
		return 1, 2, []int{0, 1}
	case 3:
		if landscape {
			return 3, 2, []int{0, 1, 2}
		}
		return 2, 3, []int{0, 2, 4}
	case 4:
		return 2, 2, []int{0, 1, 2, 3}
	case 6:
		if landscape {
			return 3, 2, []int{0, 1, 2, 3, 4, 5}
		}
		return 2, 3, []int{0, 1, 2, 3, 4, 5}
	}
	return 3, 3, []int{0, 1, 2, 3, 4, 5, 6, 7, 8}
}

// handoutAreas returns the rectangles the slides of a handout page take,
// in show order, on a page w by h with the margins m (indexed by css.Top
// and the others), for slides sw by sh, per slides a page. The page less
// its margins, a header band and a footer band of [handoutBand] each, and
// a [handoutGap] all round is cut into the grid of [handoutGrid] with a
// gap between its cells; every slide takes the size of the largest slide
// that fits a cell, its aspect ratio kept, centred in its cell
// (SdPage::CalculateHandoutAreas). A page too small for the grid gets
// empty rectangles.
//
// # Corpus evidence
//
//   - probe, odp: the pinned LibreOffice writes the handout master it
//     creates for a deck of 28 by 15.75 cm slides on an A4 portrait page
//     without margins with six slide thumbnails 8.999 by 5.061 cm at x 1
//     and 11 cm, y 3.742, 12.318 and 20.894 cm, and its header and footer
//     frames 1.484 cm tall: the six-a-page rectangles this function
//     returns, to the hundredth of a millimetre.
//   - The other counts and orientations are not measured: the pinned
//     LibreOffice prints no handout from the command line, its print
//     content option being the print dialog's alone, and neither its
//     export filter nor its document and user settings reach it.
func handoutAreas(w, h float64, m [4]float64, sw, sh float64, per int) []compose.Rect {
	cols, rows, cells := handoutGrid(per, w > h)
	band := (h - m[css.Top] - m[css.Left]) * handoutBand
	top, bottom := m[css.Top]+band, m[css.Bottom]+band
	aw := w - 2*handoutGap - m[css.Left] - m[css.Right]
	ah := h - 2*handoutGap - top - bottom
	partW := (aw - float64(cols-1)*handoutGap) / float64(cols)
	partH := (ah - float64(rows-1)*handoutGap) / float64(rows)
	out := make([]compose.Rect, len(cells))
	if !(partW > 0) || !(partH > 0) || !(sw > 0) || !(sh > 0) {
		return out
	}
	k := min(partW/sw, partH/sh)
	tw, th := sw*k, sh*k
	x0 := handoutGap + m[css.Left] + (partW-tw)/2
	y0 := handoutGap + top + (partH-th)/2
	for i, c := range cells {
		col, row := c%cols, c/cols
		out[i] = compose.Rect{X: x0 + float64(col)*(partW+handoutGap), Y: y0 + float64(row)*(partH+handoutGap), Width: tw, Height: th}
	}
	return out
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

// handoutPage draws the handout page numbered n holding the slides group,
// numberOf giving each slide's number: the handout master's background,
// each slide scaled into its rectangle of [handoutAreas] inside the
// hairline frame a notes page draws round its slide, then the handout
// master's shapes, its page number field showing n. It returns the page
// with the structure element of each slide, in order. The page is the
// handout page size; on the paper of the options, or turned by them, the
// handout page is scaled down to fit, never up, and centred, as a slide
// is.
//
// # Corpus evidence
//
//   - The frame is the one the pinned LibreOffice draws round the slide
//     of a notes page (see [builder.notesPage]): a handout page's slides
//     are the same kind of page object. Not measured on a handout page.
func (b *builder) handoutPage(n int, group []*Slide, numberOf func(*Slide) int) (*compose.Page, []*compose.StructElem) {
	hw, hh := b.handoutSize()
	pw, ph := b.pageSize(hw, hh)
	sw, sh := b.slideSize()
	areas := handoutAreas(hw, hh, b.doc.HandoutMargins, sw, sh, b.opts.Handouts)
	whole := compose.Rect{Width: hw, Height: hh}
	content := b.fill(&b.doc.HandoutBackground, whole, rectOps(whole), shape.FillNorm)
	var links []compose.Link
	sects := make([]*compose.StructElem, 0, len(group))
	gray := compose.Color{R: notesFrameGray, G: notesFrameGray, B: notesFrameGray, A: 1}
	for i, s := range group {
		frags, slideLinks, sect := b.slideContent(s, numberOf(s), sw, sh)
		sects = append(sects, sect)
		if i >= len(areas) || !(areas[i].Width > 0) {
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
