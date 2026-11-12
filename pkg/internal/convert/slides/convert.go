package slides

import (
	"cmp"
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// The slide size of a document that gives none, Impress's 4:3 screen
// show, 28 by 21 centimetres.
const (
	defaultSlideWidth  = 793.7
	defaultSlideHeight = 595.28
)

// maxSlideShapes caps the shapes drawn on one slide, groups and their
// kids counted. Anti-DoS guard, not spec-mandated: a dense slide holds a
// few hundred shapes.
const maxSlideShapes = 1 << 14

// shapeCost is what one shape drawn costs the conversion's memory budget:
// its fragments, paths and structure element, which live until the
// document is written. The shapes of the model are read whole first, the
// XML they are read from is charged as it is parsed, and the glyphs of
// their text as they are laid out. The value is the most a drawn fragment
// was measured to hold, rounded up.
//
// # Corpus evidence
//
//   - litho/pptx hostile-shape-flood: the 16,384 shapes drawn of its one
//     slide, filled rectangles without text, hold 5.6 MB of live heap once
//     the document is built, 340 bytes a shape.
//   - litho/pptx deck-quarterly-report, deck-training, connectors: 152,
//     686 and 1,035 bytes of live heap a fragment, their glyphs included.
const shapeCost = 1 << 10

// maxGroupDepth caps the nesting of groups followed at
// [limits.MaxSourceGroupDepth], which says why: every level costs a stack
// frame, a transform and a group of the page.
const maxGroupDepth = limits.MaxSourceGroupDepth

// builder holds what one conversion shares.
type builder struct {
	ctx      context.Context
	mem      *limits.Budget
	doc      *Document
	fonts    *text.Fonts
	opts     office.Options
	lang     string
	warnings []compose.Warning
	warned   map[string]bool
	// metrics caches the font metrics of the faces the text matched.
	metrics map[string]text.Metrics
	// slide is the slide being drawn, number its number in the show,
	// hidden slides counted, and count the show's slides, for the fields.
	slide         *Slide
	number, count int
	// shapes counts the shapes drawn on the slide.
	shapes int
	// links collects the slide's links in slide coordinates.
	links []compose.Link
	// pages maps each slide printed to its 1-based page number, the
	// handout page that holds it with handouts; targets maps each
	// destination a link of the pages leads to, to its slide.
	pages   map[*Slide]int
	targets map[*compose.Dest]*Slide
	// autoScript moves the scripts of the body being laid out
	// automatically, as an OpenDocument text's.
	autoScript bool
	// recode maps the characters the bullets of the body being laid out
	// draw to the text they stand for, when the two differ.
	recode map[string]string
	// groupFit is the scaling of each ShrinkGroup of the slide being
	// drawn.
	groupFit map[int]fit
	// hatchTurn is the clockwise turn, in degrees, of the shape whose fill
	// is being drawn: a hatch keeps its angle on the page whatever the
	// shape's turn.
	hatchTurn float64
}

// warn records a warning once per code and message.
func (b *builder) warn(code, msg string) {
	k := code + "\x00" + msg
	if b.warned[k] {
		return
	}
	b.warned[k] = true
	b.warnings = append(b.warnings, compose.Warning{Code: code, Message: msg})
}

// Convert lays doc out as Impress prints a presentation, with the fonts of
// the index fonts, and returns the paged document: one page per slide at
// the slide size, hidden slides left out unless office.Options.HiddenSlides
// asks for them, a notes page per slide after the slides when
// office.Options.NotesPages asks, or handout pages in place of the slide
// pages when office.Options.Handouts asks, the slides' names as the
// outline unless OmitOutline, the date and time fields showing PrintDate.
// PageWidth and PageHeight, when set, are the page size, the slide scaled
// to fit it and centred; Landscape turns a portrait page. It returns ctx's
// error when ctx ends first, office.ErrInvalidOptions for options out of
// range, Handouts other than 0, 1, 2, 3, 4, 6 or 9 or beside NotesPages,
// or a nil fonts or doc, text.ErrNoFonts for an index without faces, and
// office.ErrTooManyPages past the page cap. The output depends on doc,
// fonts' index and opts alone.
//
// # Pages
//
// Each slide prints its background, then its shapes in stacking order. A
// page of another size than the slide shows the slide scaled down to fit,
// never up, centred. The outline holds an entry per slide written, titled
// with the slide's name, and the same entries again after them when notes
// pages print, as Impress writes them. A notes page is the notes paper of
// the options, or its own size without one, with the notes page drawn at
// its top-left corner: a slide with notes in the
// notes master's boxes with its shapes, any other in Impress's default
// notes layout. A shape that jumps to a slide links to that slide's page.
// The structure tree holds a Sect
// per slide page and notes page: the paragraphs of its text as P, its tables as Table, its
// pictures as Figure.
//
// # Handouts
//
// A handout page holds Handouts slides, in show order, the last page what
// is left: the handout page size the document gives (the notes page size,
// else the default paper), its background, the slides laid out on it as
// Impress lays
// a handout out (see [handoutAreas]), each drawn as its slide page is,
// scaled into its rectangle, inside a hairline frame, then the handout
// master's shapes, a page number field showing the handout page's number.
// A handout page takes the orientation of the page the options print it
// on: turned, its slides are laid out again and its master's shapes moved
// (see [builder.handoutPage]); it is then scaled to fit as a slide is. The
// outline entry of a slide and a link to it lead to the
// handout page that holds it; the structure tree holds a Sect per slide,
// in show order.
//
// # Shapes
//
// A geometry shape is filled and stroked along the paths of its geometry,
// turned, sheared and mirrored about its box's centre; its text turns and
// shears with it but is not mirrored, a vertical flip turning it half a
// turn, as Impress draws it. Fills are solid, gradients, pictures
// stretched or tiled, clipped to the shape, and patterns; lines carry
// their dash, cap, join and arrowheads. A group maps its kids from its
// child space onto its box. A table's rows grow to the text of their
// cells.
//
// # Text
//
// Paragraphs are laid out in the shape's text rectangle less its insets,
// at the top, middle or bottom of it; a block anchored at the centre or
// the end is as wide as its widest line and sits there. A line is 1.2
// times the largest font size tall, the baseline the size below its top,
// as Impress spaces a PPTX text body; proportional spacing above 100% adds
// its room above the glyphs, below 100% shrinks the line and puts the
// baseline at four fifths of it; exact spacing keeps the descent and moves
// the baseline; a floor grows a shorter line to it. A body with
// FontLineSpacing takes its faces' ascent and descent instead, as Impress
// spaces an OpenDocument text body. The spacing before the first
// paragraph and after the last is dropped; the spacing between two
// paragraphs is the sum of the two, or the larger with
// MaxParagraphSpacing. A bullet sits at the first line's indent, the text
// at the left margin, or after the bullet when the indent leaves it no
// room; a symbol bullet is centred on the line's text height, and a symbol
// font's bullet draws in OpenSymbol as LibreOffice recodes it. Numbers
// count per level across the body. A text body that shrinks to fit tries
// Impress's scale levels in turn, font sizes rounded to whole points,
// until the text fits, the bodies of a shrink group taking the smallest
// scale any needs; a growing one stretches its box from its anchor; a
// stretched one scales its unwrapped text to fill its rectangle.
// Unwrapped lines wider than the text rectangle widen it, overflowing on
// the side the first paragraph's alignment leaves free; a clipped body
// leaves out the lines past its rectangle. Superscripts and subscripts are
// 58% of the size, moved by their face's share of the size they give up
// in OpenDocument text, by 30% up and 25% down in PresentationML text, or
// by the run's explicit rise. Date and time fields show the print date
// through their number format. A run that jumps to a slide links to that
// slide's page.
//
// # Not supported
//
// Vertical text is laid out horizontally and turned a quarter turn;
// upright East Asian text turns with it. A paragraph's lines all take the
// height of its largest font. Blurred text shadows, text glows, inner
// shadows, reflections and 3D effects are not drawn, as Impress prints
// none of them. A text rotated inside its shape and text highlighting are
// not drawn either.
func Convert(ctx context.Context, doc *Document, fonts *text.Fonts, opts office.Options) (*compose.Document, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if doc == nil || fonts == nil {
		return nil, fmt.Errorf("%w: no document or no font index", office.ErrInvalidOptions)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fonts.Len() == 0 {
		return nil, fmt.Errorf("laying the presentation out: %w", text.ErrNoFonts)
	}
	b := &builder{ctx: ctx, mem: limits.BudgetFrom(ctx), doc: doc, fonts: fonts, opts: opts, lang: cmp.Or(doc.Meta.Lang, opts.DefaultLang), warned: map[string]bool{}, metrics: map[string]text.Metrics{}}
	var shown []*Slide
	for _, s := range doc.Slides {
		if s != nil && (!s.Hidden || opts.HiddenSlides) {
			shown = append(shown, s)
		}
	}
	per := opts.Handouts
	if per != 0 && (!validHandouts(per) || opts.NotesPages) {
		return nil, fmt.Errorf("%w: %d slides a handout page, notes pages %t: 1, 2, 3, 4, 6 or 9, without notes pages", office.ErrInvalidOptions, per, opts.NotesPages)
	}
	// pageOf is the page a printed slide shows on: its own, or the
	// handout page that holds it.
	pageOf := func(i int) int {
		if per > 0 {
			return i/per + 1
		}
		return i + 1
	}
	b.pages = make(map[*Slide]int, len(shown))
	for i, s := range shown {
		if _, ok := b.pages[s]; !ok {
			b.pages[s] = pageOf(i)
		}
	}
	limit := opts.MaxPages
	if limit <= 0 {
		limit = layout.DefaultMaxPages
	}
	pages := len(shown)
	switch {
	case per > 0:
		pages = (len(shown) + per - 1) / per
	case opts.NotesPages:
		pages *= 2
	}
	if pages > limit {
		return nil, fmt.Errorf("%w: more than %d pages", office.ErrTooManyPages, limit)
	}
	out := &compose.Document{
		Metadata:  doc.Meta,
		Structure: &compose.StructElem{Role: "Document", Lang: b.lang},
	}
	out.Metadata.Lang = b.lang
	// A slide's number is its place in the show, hidden slides counted,
	// from the document's first number, on its slide and on its notes
	// page alike.
	numberOf := func(s *Slide) int {
		n := 0
		if doc.FirstNumber != 0 {
			n = doc.FirstNumber - 1
		}
		for _, t := range doc.Slides {
			if t != nil {
				n++
			}
			if t == s {
				return n
			}
		}
		return n
	}
	for _, s := range doc.Slides {
		if s != nil {
			b.count++
		}
	}
	if opts.Sink != nil {
		opts.Sink.Expect(max(pages, 1))
	}
	// Every slide printed has its Sect, in show order, a page's slides in
	// order on it: a destination inside the document names the Sect of
	// the slide it leads to as its structure destination (ISO 14289-2,
	// 8.8). A destination to a slide not laid out yet gets its Sect when that
	// slide is.
	sects := make(map[*Slide]*compose.StructElem, len(shown))
	waiting := map[*Slide][]*compose.Dest{}
	add := func(pg *compose.Page, held []*Slide, kids []*compose.StructElem) error {
		for i, sect := range kids {
			out.Structure.Kids = append(out.Structure.Kids, sect)
			s := held[i]
			if _, ok := sects[s]; !ok {
				sects[s] = sect
			}
			for _, d := range waiting[s] {
				d.Elem = sect
			}
			delete(waiting, s)
		}
		for i := range pg.Links {
			d := pg.Links[i].Dest
			if d == nil || d.Elem != nil {
				continue
			}
			s := b.targets[d]
			if sect, ok := sects[s]; ok || s == nil {
				d.Elem = sect
				continue
			}
			waiting[s] = append(waiting[s], d)
		}
		return b.handOver(out, pg)
	}
	switch {
	case per > 0:
		for i := 0; i < len(shown); i += per {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			group := shown[i:min(i+per, len(shown))]
			pg, kids := b.handoutPage(i/per+1, group, numberOf)
			if err := add(pg, group, kids); err != nil {
				return nil, err
			}
		}
	default:
		for _, s := range shown {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			pg, sect := b.slidePage(s, numberOf(s))
			if err := add(pg, []*Slide{s}, []*compose.StructElem{sect}); err != nil {
				return nil, err
			}
		}
	}
	if opts.NotesPages {
		for _, s := range shown {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			pg, sect := b.notesPage(s, numberOf(s))
			if err := add(pg, []*Slide{s}, []*compose.StructElem{sect}); err != nil {
				return nil, err
			}
		}
	}
	// A page cuts its drawing short when ctx ends during it: the last one
	// has no next page to check ctx before.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pages == 0 {
		// A deck that prints no slide prints one blank page of the slide
		// size, as Impress exports an empty show.
		w, h := b.pageSize(b.slideSize())
		if err := b.handOver(out, &compose.Page{Width: w, Height: h}); err != nil {
			return nil, err
		}
	}
	if !opts.OmitOutline {
		dests := make([]compose.Dest, len(shown))
		for i, s := range shown {
			dests[i] = compose.Dest{Page: pageOf(i), Elem: sects[s]}
		}
		out.Outline = outline(doc.Slides, shown, dests)
		if opts.NotesPages {
			// Impress writes the bookmarks again for the notes pages, each
			// leading to its slide's page.
			out.Outline = append(out.Outline, out.Outline...)
		}
	}
	out.Warnings = b.warnings
	return out, nil
}

// outline returns the outline of the printed slides shown, the i-th
// leading to dests[i]: an entry per slide titled by its Bookmark, else by
// its outline title, and the slides of a section under an entry of the
// section's name that leads to its first printed slide. A section none of
// whose slides prints has no entry.
func outline(all, shown []*Slide, dests []compose.Dest) []compose.Outline {
	titles := outlineTitles(all)
	head := sectionHeads(all)
	var out []compose.Outline
	var open *Slide
	for i, s := range shown {
		dest := dests[i]
		title := s.Bookmark
		if title == "" {
			title = titles[s]
		}
		entry := compose.Outline{Title: title, Dest: dest}
		h := head[s]
		if h == nil {
			open = nil
			out = append(out, entry)
			continue
		}
		if h != open {
			out = append(out, compose.Outline{Title: h.Section, Dest: dest})
			open = h
		}
		last := &out[len(out)-1]
		last.Kids = append(last.Kids, entry)
	}
	return out
}

// sectionHeads maps every slide in a section to the section's first slide.
func sectionHeads(all []*Slide) map[*Slide]*Slide {
	out := map[*Slide]*Slide{}
	var head *Slide
	for _, s := range all {
		if s == nil {
			continue
		}
		switch {
		case s.Section == "":
			head = nil
		case s.SectionStart || head == nil || head.Section != s.Section:
			head = s
		}
		if head != nil {
			out[s] = head
		}
	}
	return out
}

// dest returns a destination leading to the page the slide s prints on,
// nil when s does not print.
func (b *builder) dest(s *Slide) *compose.Dest {
	pg, ok := b.pages[s]
	if !ok {
		return nil
	}
	d := &compose.Dest{Page: pg}
	if b.targets == nil {
		b.targets = map[*compose.Dest]*Slide{}
	}
	b.targets[d] = s
	return d
}

// handOver gives the page pg, laid out, to the sink of the options, and
// its glyphs back to the budget, or appends it to out without a sink.
func (b *builder) handOver(out *compose.Document, pg *compose.Page) error {
	sink := b.opts.Sink
	if sink == nil {
		out.Pages = append(out.Pages, pg)
		return nil
	}
	if err := sink.Page(b.ctx, pg); err != nil {
		return fmt.Errorf("writing a page: %w", err)
	}
	b.mem.Release(compose.CountGlyphs(pg) * limits.GlyphCost)
	return nil
}

// outlineTitles returns the outline title of every slide: its name, else
// "Slide n" for the n-th slide; a name an earlier slide already took gets
// " (2)", " (3)" and so on, as Impress keeps the page names of a
// presentation unique, hidden slides counted.
func outlineTitles(slides []*Slide) map[*Slide]string {
	out := make(map[*Slide]string, len(slides))
	seen := map[string]int{}
	n := 0
	for _, s := range slides {
		if s == nil {
			continue
		}
		n++
		t := s.Name
		if t == "" {
			t = fmt.Sprintf("Slide %d", n)
		}
		seen[t]++
		if k := seen[t]; k > 1 {
			t = fmt.Sprintf("%s (%d)", t, k)
		}
		out[s] = t
	}
	return out
}

// slideSize returns the slide size, Impress's default for a document that
// gives none, each side within the PDF's limits.
func (b *builder) slideSize() (float64, float64) {
	w, h := b.doc.Width, b.doc.Height
	if !positiveFinite(w) || !positiveFinite(h) {
		w, h = defaultSlideWidth, defaultSlideHeight
	}
	return min(w, office.MaxPageSide), min(h, office.MaxPageSide)
}

// pageSize returns the page size a slide of size w by h prints on: the
// options' paper, else the slide's, turned to landscape when the options
// ask and it is portrait, or to portrait when they ask and it is landscape.
func (b *builder) pageSize(w, h float64) (float64, float64) {
	if b.opts.PageWidth > 0 && b.opts.PageHeight > 0 {
		w, h = b.opts.PageWidth, b.opts.PageHeight
	}
	if b.opts.Landscape && w < h || b.opts.Portrait && w > h {
		w, h = h, w
	}
	return w, h
}

// fitMatrix returns the transform that places a sheet of size w by h on a
// page of size pw by ph: scaled down to fit, never up, and centred.
func fitMatrix(w, h, pw, ph float64) compose.Matrix {
	s := min(pw/w, ph/h, 1)
	return compose.Matrix{A: s, D: s, E: (pw - w*s) / 2, F: (ph - h*s) / 2}
}

// slidePage draws the slide s, numbered n, on its page, and returns
// the page with the slide's structure element.
func (b *builder) slidePage(s *Slide, n int) (*compose.Page, *compose.StructElem) {
	sw, sh := b.slideSize()
	pw, ph := b.pageSize(sw, sh)
	content, links, sect := b.slideContent(s, n, sw, sh)
	pg := &compose.Page{Width: pw, Height: ph}
	m := fitMatrix(sw, sh, pw, ph)
	if m == compose.Identity {
		pg.Content = content
		pg.Links = links
	} else {
		pg.Content = []compose.Fragment{&compose.Group{Transform: m, Opacity: 1, Content: content}}
		for _, l := range links {
			l.Rect = transformRect(m, l.Rect)
			pg.Links = append(pg.Links, l)
		}
	}
	return pg, sect
}

// slideContent draws the slide s, numbered n, w by h, in slide
// coordinates: its background and shapes, its links and its structure
// element.
func (b *builder) slideContent(s *Slide, n int, w, h float64) ([]compose.Fragment, []compose.Link, *compose.StructElem) {
	b.slide, b.number, b.shapes, b.links = s, n, 0, nil
	b.groupFit = b.groupFits(s.Shapes)
	sect := &compose.StructElem{Role: "Sect"}
	var out []compose.Fragment
	area := compose.Rect{Width: w, Height: h}
	out = append(out, b.fill(&s.Background, area, rectOps(area), shape.FillNorm)...)
	for _, sp := range s.Shapes {
		if err := b.ctx.Err(); err != nil {
			break
		}
		out = append(out, b.shape(sp, compose.Identity, 0, sect)...)
	}
	return out, b.links, sect
}
