package slides

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// The notes page size: US Letter portrait, the paper Impress prints notes
// pages on with the en-US defaults, whatever notes size the file gives.
//
// # Corpus evidence
//
//   - pptx: deck.pptx (notes size 540 by 720) and pitch-lo.pptx (A4) print
//     their notes pages on Letter; odp: pitch.odp, whose notes master is
//     A4, too.
const (
	notesWidth  = 612
	notesHeight = 792
)

// The boxes of Impress's default notes page, as fractions of the page
// (sd/xml/objectlist.xml, PageKind::Notes): the slide drawn in the upper
// box, fitted and centred, the notes text in the lower one.
const (
	notesSlideY, notesSlideH                       = 0.076, 0.375
	notesTextX, notesTextY, notesTextW, notesTextH = 0.1, 0.475, 0.8, 0.45
)

// The insets of the default notes text box, in points: Impress's default
// text frame distances, 0.25 centimetre left and right, 0.125 top and
// bottom.
const (
	notesInsetX = 0.25 * 72 / 2.54
	notesInsetY = 0.125 * 72 / 2.54
)

// The insets of a notes master's text box, in points: DrawingML's default
// text insets, 0.1 inch left and right, 0.05 inch top and bottom
// (ECMA-376 Part 1, 21.1.2.1.1 bodyPr lIns and tIns).
const (
	masterInsetX = 7.2
	masterInsetY = 3.6
)

// notesFrameGray is the grey of the hairline frame Impress draws around a
// notes page's slide picture, C0C0C0; notesFrameWidth its width.
const (
	notesFrameGray  = 0xc0 / 255.0
	notesFrameWidth = 0.1
)

// notesPage draws the notes page of the slide s, numbered n: the slide
// scaled into its box inside a hairline frame, its notes below. A slide
// with notes takes the notes master's boxes and shapes when the document
// gives them; otherwise the page takes Impress's default notes layout.
// The paper is Letter, the notes page drawn at its top-left corner at the
// document's notes size, as Impress prints it; the first slide of a
// document whose file gave it no notes takes the paper's size instead.
//
// # Corpus evidence
//
//   - pptx: deck-notes (notes size 540 by 720, a master slide picture box
//     of 360 by 270 at 90, 54): the slides with notes print the slide in
//     that box framed whole and the slide number placeholder; slides 3 to 6,
//     without notes, print the default layout over 540 by 720 at the top
//     of the Letter page, the slide 480 wide at 30, 54.72; slide 1 the
//     default layout over the whole Letter page, 527.98 wide at 42.01,
//     60.19. Every frame is a C0C0C0 line 0.1 point wide.
func (b *builder) notesPage(s *Slide, n int) (*compose.Page, *compose.StructElem) {
	pw, ph := float64(notesWidth), float64(notesHeight)
	if b.opts.PageWidth > 0 && b.opts.PageHeight > 0 {
		pw, ph = b.opts.PageWidth, b.opts.PageHeight
	}
	if b.opts.Landscape && pw < ph {
		pw, ph = ph, pw
	}
	// The notes page's own size, at the paper's top-left corner.
	nw, nh := pw, ph
	if w, h := b.doc.NotesWidth, b.doc.NotesHeight; w > 0 && h > 0 && w <= office.MaxPageSide && h <= office.MaxPageSide && !(s.Notes == nil && b.firstSlide(s)) {
		nw, nh = w, h
	}
	master := s.Notes != nil && usable(b.doc.NotesSlideBox)
	sw, sh := b.slideSize()
	area := compose.Rect{Y: nh * notesSlideY, Width: nw, Height: nh * notesSlideH}
	if master {
		area = b.doc.NotesSlideBox
	}
	k := min(area.Width/sw, area.Height/sh)
	img := compose.Rect{X: area.X + (area.Width-sw*k)/2, Y: area.Y + (area.Height-sh*k)/2, Width: sw * k, Height: sh * k}
	frame := img
	if master {
		frame = area
	}
	content, _, sect := b.slideContent(s, n, sw, sh)
	pg := &compose.Page{Width: pw, Height: ph}
	pg.Content = append(pg.Content, &compose.Group{
		Transform: compose.Matrix{A: k, D: k, E: img.X, F: img.Y}, Opacity: 1,
		Clip:    &compose.Path{Ops: rectOps(compose.Rect{Width: sw, Height: sh}), Fill: &compose.Fill{}},
		Content: content,
	})
	gray := compose.Color{R: notesFrameGray, G: notesFrameGray, B: notesFrameGray, A: 1}
	pg.Content = append(pg.Content, &compose.Path{Ops: rectOps(frame), Stroke: &compose.Stroke{Color: gray, Width: notesFrameWidth}})
	if master {
		// The notes master's other shapes, the slide number among them,
		// resolve their fields for this slide.
		b.shapes, b.links = 0, nil
		for _, sp := range b.doc.NotesShapes {
			pg.Content = append(pg.Content, b.shape(sp, compose.Identity, 0, sect)...)
		}
		pg.Links = append(pg.Links, b.links...)
	}
	if len(s.Notes) > 0 {
		box := compose.Rect{X: nw * notesTextX, Y: nh * notesTextY, Width: nw * notesTextW, Height: nh * notesTextH}
		var in [4]float64
		in[css.Left], in[css.Right], in[css.Top], in[css.Bottom] = notesInsetX, notesInsetX, notesInsetY, notesInsetY
		if master && usable(b.doc.NotesTextBox) {
			box = b.doc.NotesTextBox
			in[css.Left], in[css.Right], in[css.Top], in[css.Bottom] = masterInsetX, masterInsetX, masterInsetY, masterInsetY
		}
		txt := b.shapeText(&TextBody{Paragraphs: s.Notes, Insets: in, Wrap: true}, nil, box)
		pg.Content = append(pg.Content, txt.frags...)
		pg.Links = append(pg.Links, txt.links...)
		sect.Kids = append(sect.Kids, txt.structure...)
	}
	return pg, sect
}

// usable reports whether r is a box with an area within the page limits.
func usable(r compose.Rect) bool {
	return finite(r) && r.Width > 0 && r.Height > 0
}

// firstSlide reports whether s is the document's first slide.
func (b *builder) firstSlide(s *Slide) bool {
	for _, t := range b.doc.Slides {
		if t != nil {
			return t == s
		}
	}
	return false
}
