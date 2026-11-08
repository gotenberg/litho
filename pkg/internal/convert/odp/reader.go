package odp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// The caps of one presentation.
const (
	// maxSlides bounds the draw:page elements read: 10,000. Anti-DoS
	// guard, not spec-mandated: a lecture deck holds a few hundred slides,
	// and every slide is a page of the output.
	maxSlides = 10_000
	// maxShapes bounds the shapes one document reads, its masters'
	// counted once per slide that shows them: 200,000. Anti-DoS guard, not
	// spec-mandated: a dense slide holds a few hundred shapes, and each
	// costs a few hundred bytes of the model.
	maxShapes = 200_000
	// maxGroupDepth bounds the nesting of draw:g groups and draw:a links
	// read: 32. Anti-DoS guard, not spec-mandated: Impress nests groups a
	// few levels deep, and the builder walks them recursively.
	maxGroupDepth = 32
	// maxParagraphs bounds the paragraphs one document reads: 500,000.
	// Anti-DoS guard, not spec-mandated: a slide holds a few dozen, and
	// notes a few more.
	maxParagraphs = 500_000
	// maxRuns bounds the runs of one paragraph: 10,000. Anti-DoS guard,
	// not spec-mandated: text:s and text:span elements can each start a
	// run, and a paragraph of a slide holds a few dozen.
	maxRuns = 10_000
	// maxTableCells bounds the cells of one table read, repetitions
	// expanded: 100,000. Anti-DoS guard, not spec-mandated: a table on a
	// slide shows a few hundred cells at most.
	maxTableCells = 100_000
	// maxPictures bounds the distinct pictures one document decodes:
	// 2,000. Anti-DoS guard, not spec-mandated: an illustrated deck
	// holds a few hundred.
	maxPictures = 2000
)

// The slide sizes of a document that names none: Impress's default 16:9
// slide, 28 cm by 15.75 cm, the size LibreOffice gives a new presentation.
const (
	defaultSlideWidth  = 28 * 72 / 2.54
	defaultSlideHeight = 15.75 * 72 / 2.54
)

// reader reads one presentation into the slide model.
type reader struct {
	ctx      context.Context
	pkg      *odf.Package
	fonts    *text.Fonts
	styles   *odf.Styles
	draw     *odf.DrawStyles
	data     *odf.DataStyles
	builtin  *odf.Styles
	warnings []compose.Warning
	warned   map[string]bool
	err      error
	steps    int
	shapes   int
	paras    int
	pics     map[string]*picResult
	pixels   int64
	// layers maps a draw:layer name to its draw:display (ODF 1.3, Part 3,
	// 10.2.3 and 19.153): always, screen, printer or none.
	layers map[string]string
	// footers, headers and dates are the presentation's declarations
	// (10.9.3.2 to 10.9.3.6) by presentation:name.
	footers, headers map[string]string
	dates            map[string]*odf.Element
	// pageNames maps a page's draw:name, and the name Impress shows for
	// it, to its 1-based position; pageCount is the number of pages read.
	pageNames map[string]int
	pageCount int
}

// nameShapes maps the draw:name of every drawing shape el holds, groups
// and links walked, but not its notes, to the page n, a name already
// taken keeping its first page.
func (r *reader) nameShapes(el *odf.Element, n, depth int) {
	if depth > maxGroupDepth {
		return
	}
	for _, k := range el.Children {
		if k.IsText() || k.Is(odf.NSPresentation, "notes") || k.Name.Space != odf.NSDraw {
			continue
		}
		if name := k.A(odf.NSDraw, "name"); name != "" {
			if _, dup := r.pageNames[name]; !dup {
				r.pageNames[name] = n
			}
		}
		if k.Is(odf.NSDraw, "g") || k.Is(odf.NSDraw, "a") {
			r.nameShapes(k, n, depth+1)
		}
	}
}

// newReader returns a reader of pkg.
func newReader(ctx context.Context, pkg *odf.Package, fonts *text.Fonts) *reader {
	r := &reader{
		ctx: ctx, pkg: pkg, fonts: fonts,
		styles: odf.NewStyles(pkg), draw: odf.NewDrawStyles(pkg), data: odf.NewDataStyles(pkg),
		builtin: builtinStyles(),
		warned:  map[string]bool{}, pics: map[string]*picResult{}, layers: map[string]string{},
		footers: map[string]string{}, headers: map[string]string{}, dates: map[string]*odf.Element{},
	}
	for _, l := range odf.Section(pkg.Styles, "master-styles").Child(odf.NSDraw, "layer-set").All(odf.NSDraw, "layer") {
		if name := l.A(odf.NSDraw, "name"); name != "" {
			r.layers[name] = l.A(odf.NSDraw, "display")
		}
	}
	return r
}

// warn records a warning once per code.
func (r *reader) warn(code, msg string) {
	if r.warned[code] {
		return
	}
	r.warned[code] = true
	r.warnings = append(r.warnings, compose.Warning{Code: code, Message: msg})
}

// tick checks ctx every 1,024 steps, and reports whether reading goes on.
func (r *reader) tick() bool {
	if r.err != nil {
		return false
	}
	r.steps++
	if r.steps%1024 == 0 {
		if err := r.ctx.Err(); err != nil {
			r.err = err
			return false
		}
	}
	return true
}

// document reads the office:presentation body (ODF 1.3, Part 3, 10.2.4
// and 10.9): the declarations, then each draw:page in order with its
// master page.
func (r *reader) document(body *odf.Element) *slides.Document {
	doc := &slides.Document{Meta: r.metadata()}
	var pages []*odf.Element
	for _, el := range body.Children {
		switch {
		case el.Is(odf.NSDraw, "page"):
			pages = append(pages, el)
		case el.Is(odf.NSPresentation, "footer-decl"):
			if n := el.A(odf.NSPresentation, "name"); n != "" {
				r.footers[n] = el.TextContent()
			}
		case el.Is(odf.NSPresentation, "header-decl"):
			if n := el.A(odf.NSPresentation, "name"); n != "" {
				r.headers[n] = el.TextContent()
			}
		case el.Is(odf.NSPresentation, "date-time-decl"):
			if n := el.A(odf.NSPresentation, "name"); n != "" {
				r.dates[n] = el
			}
		case el.Is(odf.NSPresentation, "show"):
			r.warn("custom-show", "custom slide shows are not applied: every slide prints in document order")
		}
	}
	first := r.masterOf(nil)
	if len(pages) > 0 {
		first = r.masterOf(pages[0])
	}
	doc.Width, doc.Height = r.pageSize(first)
	doc.NotesWidth, doc.NotesHeight = r.notesSize(first)
	r.notesMaster(doc, first)
	r.pageCount = min(len(pages), maxSlides)
	r.pageNames = map[string]int{}
	for i, pg := range pages[:r.pageCount] {
		for _, name := range []string{pg.A(odf.NSDraw, "name"), slideName(pg.A(odf.NSDraw, "name"), i+1)} {
			if _, dup := r.pageNames[name]; !dup && name != "" {
				r.pageNames[name] = i + 1
			}
		}
	}
	// A link may name a shape: it goes to the page that holds it, as
	// Impress's "Go to page or object" action does (ODF 1.3, Part 3,
	// 19.414 presentation:action show); a page's own name comes first.
	for i, pg := range pages[:r.pageCount] {
		r.nameShapes(pg, i+1, 0)
	}
	for i, pg := range pages {
		if i >= maxSlides {
			r.warn("too-many-slides", fmt.Sprintf("slides past %d are not read", maxSlides))
			break
		}
		if !r.tick() {
			return doc
		}
		doc.Slides = append(doc.Slides, r.slide(pg, i+1))
	}
	return doc
}

// masterOf returns the master page a draw:page names (ODF 1.3, Part 3,
// 19.194 draw:master-page-name), else the document's first, which
// Impress gives a page that names none or one it lacks; nil for a
// document without one.
func (r *reader) masterOf(pg *odf.Element) *odf.MasterPage {
	if pg != nil {
		if mp := r.styles.Master(pg.A(odf.NSDraw, "master-page-name")); mp != nil {
			return mp
		}
	}
	if len(r.styles.Masters) > 0 {
		return r.styles.Masters[0]
	}
	return nil
}

// pageSize returns the slide size of a master page: its page layout's
// fo:page-width and fo:page-height (ODF 1.3, Part 3, 16.5 and 20.215),
// Impress's default where it gives none.
func (r *reader) pageSize(mp *odf.MasterPage) (float64, float64) {
	w, h := defaultSlideWidth, defaultSlideHeight
	if mp == nil {
		return w, h
	}
	if pl := r.styles.PageLayouts[mp.PageLayout]; pl != nil {
		pw, okW := odf.Length(pl.Page.Get("fo:page-width"))
		ph, okH := odf.Length(pl.Page.Get("fo:page-height"))
		if okW && okH && (pw <= 0 || ph <= 0) {
			// A layout of no size takes the printer's paper, A4 in the
			// layout's orientation (measured on the pinned LibreOffice:
			// sd/invalidCxAndCyAttribute.odp, 0 cm by 0 cm portrait,
			// prints 595.28 by 841.89 points).
			if pl.Page.Get("style:print-orientation") == "landscape" {
				return a4Height, a4Width
			}
			return a4Width, a4Height
		}
		if okW && pw > 0 {
			w = pw
		}
		if okH && ph > 0 {
			h = ph
		}
	}
	return w, h
}

// The A4 paper, 21 cm by 29.7 cm.
const (
	a4Width  = 21 * 72 / 2.54
	a4Height = 29.7 * 72 / 2.54
)

// notesSize returns the notes page size of a master page: the page
// layout its presentation:notes names (ODF 1.3, Part 3, 16.19), 0 by 0
// when it has none.
func (r *reader) notesSize(mp *odf.MasterPage) (float64, float64) {
	if mp == nil {
		return 0, 0
	}
	notes := mp.El.Child(odf.NSPresentation, "notes")
	pl := r.styles.PageLayouts[notes.A(odf.NSStyle, "page-layout-name")]
	if pl == nil {
		return 0, 0
	}
	w, okW := odf.Length(pl.Page.Get("fo:page-width"))
	h, okH := odf.Length(pl.Page.Get("fo:page-height"))
	if !okW || !okH || w <= 0 || h <= 0 {
		return 0, 0
	}
	return w, h
}

// notesMaster reads the notes page of the master page mp (ODF 1.3, Part
// 3, 16.19 presentation:notes) into the notes layout of doc: its
// draw:page-thumbnail as the slide's box, its frame of
// presentation:class notes as the notes text's, and its shapes of no
// class as the shapes every notes page draws. Its header, footer,
// date-time and page-number placeholders are left out: Impress prints
// none of them on a notes page whose settings it was not given, the
// date of the notes master of sd/tdf112557.odp among them (measured on
// the pinned LibreOffice).
func (r *reader) notesMaster(doc *slides.Document, mp *odf.MasterPage) {
	if mp == nil {
		return
	}
	notes := mp.El.Child(odf.NSPresentation, "notes")
	if notes == nil {
		return
	}
	box := func(el *odf.Element) compose.Rect {
		x, _ := odf.Length(el.A(odf.NSSVG, "x"))
		y, _ := odf.Length(el.A(odf.NSSVG, "y"))
		w, _ := odf.Length(el.A(odf.NSSVG, "width"))
		h, _ := odf.Length(el.A(odf.NSSVG, "height"))
		return compose.Rect{X: x, Y: y, Width: max(w, 0), Height: max(h, 0)}
	}
	var decorations []*odf.Element
	for _, el := range notes.Children {
		class := el.A(odf.NSPresentation, "class")
		switch {
		case el.Is(odf.NSDraw, "page-thumbnail"):
			doc.NotesSlideBox = box(el)
		case el.Is(odf.NSDraw, "frame") && class == "notes":
			doc.NotesTextBox = box(el)
		case class == "" && !el.IsText():
			decorations = append(decorations, el)
		}
	}
	pc := &pageCtx{scope: odf.ScopeStyles, mp: mp, master: true}
	doc.NotesShapes = r.shapeList(decorations, pc, 0)
}

// defaultPageName matches the names Impress writes for a page the user
// did not name, "page1": it shows such a page as "Slide 1" (sd's API
// names, "page" and the number).
var defaultPageName = regexp.MustCompile(`^page[0-9]+$`)

// slideName returns the name of the n-th slide: its draw:name, "Slide n"
// for none or for a default one.
func slideName(name string, n int) string {
	if name == "" || defaultPageName.MatchString(name) {
		return fmt.Sprintf("Slide %d", n)
	}
	return name
}

// slide reads one draw:page (ODF 1.3, Part 3, 10.2.4): its drawing-page
// style's visibility and background (17.25), the master page's shapes
// that show through, its own shapes, its title and its notes.
func (r *reader) slide(pg *odf.Element, n int) *slides.Slide {
	s := &slides.Slide{Name: slideName(pg.A(odf.NSDraw, "name"), n)}
	mp := r.masterOf(pg)
	dp := r.styles.Resolve(odf.ScopeContent, odf.FamilyDrawingPage, pg.A(odf.NSDraw, "style-name"), odf.DrawingPageProps)
	var mdp odf.Props
	if mp != nil {
		mdp = r.styles.Resolve(odf.ScopeStyles, odf.FamilyDrawingPage, mp.El.A(odf.NSDraw, "style-name"), odf.DrawingPageProps)
	}
	s.Hidden = dp.Get("presentation:visibility") == "hidden"
	if dp.Get("presentation:background-visible") != "false" {
		// A page's own fill replaces the master's background; none, or no
		// fill at all, shows the master's (20.232).
		if f := dp.Get("draw:fill"); f != "" && f != "none" {
			s.Background = r.fill(dp, "none")
		} else {
			s.Background = r.fill(mdp, "none")
		}
	}
	pc := &pageCtx{scope: odf.ScopeContent, mp: mp, background: s.Background, number: n}
	pc.decls(r, pg, dp)
	if mp != nil && dp.Get("presentation:background-objects-visible") != "false" {
		mc := *pc
		mc.scope, mc.master = odf.ScopeStyles, true
		s.Shapes = r.shapeList(mp.El.Children, &mc, 0)
	}
	s.Shapes = append(s.Shapes, r.shapeList(pg.Children, pc, 0)...)
	s.Title = r.title(pg, pc)
	if notes := pg.Child(odf.NSPresentation, "notes"); notes != nil {
		s.Notes = r.notes(notes, pc)
	}
	return s
}

// title returns the text of the page's title placeholder (ODF 1.3, Part
// 3, 19.393 presentation:class title), its lines joined by spaces.
func (r *reader) title(pg *odf.Element, pc *pageCtx) string {
	for _, el := range pg.Children {
		if !el.Is(odf.NSDraw, "frame") || el.A(odf.NSPresentation, "class") != "title" || el.A(odf.NSPresentation, "placeholder") == "true" {
			continue
		}
		var parts []string
		for _, p := range r.textBody(el.Child(odf.NSDraw, "text-box"), r.shapeStyle(el, pc), pc).Paragraphs {
			var b strings.Builder
			for _, run := range p.Runs {
				if run.Break {
					b.WriteByte(' ')
				}
				b.WriteString(run.Text)
			}
			if t := strings.Join(strings.Fields(b.String()), " "); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// notes reads a page's presentation:notes (ODF 1.3, Part 3, 16.19): the
// paragraphs of its notes placeholder (presentation:class notes), else
// of the text frames it holds, which a converted document writes without
// a class.
func (r *reader) notes(notes *odf.Element, pc *pageCtx) []slides.Paragraph {
	nc := *pc
	var out, other []slides.Paragraph
	for _, el := range notes.Children {
		if !el.Is(odf.NSDraw, "frame") || el.A(odf.NSPresentation, "placeholder") == "true" {
			continue
		}
		box := el.Child(odf.NSDraw, "text-box")
		if box == nil {
			continue
		}
		body := r.textBody(box, r.shapeStyle(el, &nc), &nc)
		if el.A(odf.NSPresentation, "class") == "notes" {
			out = append(out, body.Paragraphs...)
		} else {
			other = append(other, body.Paragraphs...)
		}
	}
	if len(out) == 0 {
		out = other
	}
	return out
}

// metadata reads meta.xml's office:meta (ODF 1.3, Part 3, 4): the title,
// the creator, the subject, else the description, the keywords, the
// generator and the language; never a date.
func (r *reader) metadata() compose.Metadata {
	var m compose.Metadata
	meta := odf.Section(r.pkg.Meta, "meta")
	if meta == nil {
		return m
	}
	get := func(space, local string) string {
		return strings.Join(strings.Fields(meta.Child(space, local).TextContent()), " ")
	}
	m.Title = get(odf.NSDC, "title")
	m.Author = get(odf.NSDC, "creator")
	if m.Author == "" {
		m.Author = get(odf.NSMeta, "initial-creator")
	}
	m.Subject = get(odf.NSDC, "subject")
	if m.Subject == "" {
		m.Subject = get(odf.NSDC, "description")
	}
	var kw []string
	for _, k := range meta.All(odf.NSMeta, "keyword") {
		if s := strings.TrimSpace(k.TextContent()); s != "" {
			kw = append(kw, s)
		}
	}
	m.Keywords = strings.Join(kw, ", ")
	m.Creator = get(odf.NSMeta, "generator")
	m.Lang = get(odf.NSDC, "language")
	return m
}
