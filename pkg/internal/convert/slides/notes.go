package slides

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// The notes page of a document that gives no size: US Letter portrait,
// the paper Impress prints notes pages on with the en-US defaults, whatever
// a PPTX file's notes size says.
const (
	notesWidth  = 612
	notesHeight = 792
)

// The boxes of Impress's default notes page, as fractions of the page
// (sd/xml/objectlist.xml, PageKind::Notes): the slide drawn in the upper
// box, fitted and centred, the notes text in the lower one.
const (
	notesSlideY, notesSlideH                        = 0.076, 0.375
	notesTextX, notesTextY, notesTextW, notesTextH = 0.1, 0.475, 0.8, 0.45
)

// The insets of the default notes text box, in points: Impress's default
// text frame distances, 0.25 centimetre left and right, 0.125 top and
// bottom.
const (
	notesInsetX = 0.25 * 72 / 2.54
	notesInsetY = 0.125 * 72 / 2.54
)

// notesPage draws the notes page of the slide s, the n-th written: the
// slide scaled into the page's upper part inside a hairline frame, its
// notes below, as Impress's default notes master places them.
func (b *builder) notesPage(s *Slide, n int) (*compose.Page, *compose.StructElem) {
	pw, ph := float64(notesWidth), float64(notesHeight)
	if w, h := b.doc.NotesWidth, b.doc.NotesHeight; w > 0 && h > 0 && !math.IsInf(w, 0) && !math.IsInf(h, 0) {
		pw, ph = min(w, office.MaxPageSide), min(h, office.MaxPageSide)
	}
	sw, sh := b.slideSize()
	area := compose.Rect{Y: ph * notesSlideY, Width: pw, Height: ph * notesSlideH}
	k := min(area.Width/sw, area.Height/sh)
	img := compose.Rect{X: area.X + (area.Width-sw*k)/2, Y: area.Y + (area.Height-sh*k)/2, Width: sw * k, Height: sh * k}
	content, _, sect := b.slideContent(s, n, sw, sh)
	pg := &compose.Page{Width: pw, Height: ph}
	pg.Content = append(pg.Content, &compose.Group{
		Transform: compose.Matrix{A: k, D: k, E: img.X, F: img.Y}, Opacity: 1,
		Clip:    &compose.Path{Ops: rectOps(compose.Rect{Width: sw, Height: sh}), Fill: &compose.Fill{}},
		Content: content,
	})
	pg.Content = append(pg.Content, &compose.Path{Ops: rectOps(img), Stroke: &compose.Stroke{Color: compose.Black}})
	if len(s.Notes) > 0 {
		box := compose.Rect{X: pw * notesTextX, Y: ph * notesTextY, Width: pw * notesTextW, Height: ph * notesTextH}
		var in [4]float64
		in[css.Left], in[css.Right], in[css.Top], in[css.Bottom] = notesInsetX, notesInsetX, notesInsetY, notesInsetY
		txt := b.shapeText(&TextBody{Paragraphs: s.Notes, Insets: in, Wrap: true}, nil, box)
		pg.Content = append(pg.Content, txt.frags...)
		pg.Links = append(pg.Links, txt.links...)
		sect.Kids = append(sect.Kids, txt.structure...)
	}
	return pg, sect
}
