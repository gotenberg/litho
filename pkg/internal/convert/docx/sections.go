package docx

import (
	"fmt"
	"maps"
	"math"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// The default page of a section without a pgSz or pgMar: US Letter with
// one-inch margins and half-inch header and footer distances, Word's
// defaults (ECMA-376 Part 1, 17.6.13 leaves them to the application). The
// conversion's default paper, office.Options DefaultPageWidth and
// DefaultPageHeight, replaces the page size.
const (
	defaultPageWidth  = 612
	defaultPageHeight = 792
	defaultMargin     = 72
	defaultHFDistance = 36
)

// maxColumns caps the columns of a section: Word's own limit (17.6.4
// cols allows any number; Word lays out at most 45).
const maxColumns = 45

// minPageArea is the least page area side a section keeps, in points, so
// that margins past the page leave a box the layout accepts.
const minPageArea = 18

// section is one section of the document (17.6.17 sectPr) with the blocks
// it holds.
type section struct {
	index int
	// name is the page type name of its pages: shared with the previous
	// section for a continuous break with the same page setup, which then
	// shares its pages.
	name string
	// id anchors the section's first block, to find its first page.
	id             string
	width, height  float64
	margin         [4]float64
	header, footer float64
	gutter         float64
	cols           int
	colSpace       float64
	colSep         bool
	colTracks      []columnTrack
	// colWidth is the width of the one column a list of columns of
	// different widths gives, in points, 0 for none.
	colWidth float64
	// inset is how far inside the text area of the pages it shares the
	// section's own text area lies, left and right, in points: the
	// difference of its side margins and those of the section whose pages
	// it continues on.
	inset            [2]float64
	breakType        string
	pageStart        *int
	pageFormat       string
	titlePage        bool
	frame            *layout.PageFrame
	headers, footers map[string]string
	blocks           []blockNode
	pending          []pendingAnchor
	headerH, footerH map[string]float64
	start            int
	// rtl lays the columns from right to left (17.6.1 bidi).
	rtl bool
	// balance balances the section's columns: the next section starts
	// on its page, after a continuous break.
	balance bool
	// align is the fraction of a page's free room its text moves down by
	// (17.6.23 vAlign), 0 for top; spread spreads the room between the
	// paragraphs instead (vAlign both).
	align  float64
	spread bool
}

// readPage reads the page size and margins of the sectPr e (17.6.13
// pgSz, 17.6.11 pgMar) over those s holds.
func (s *section) readPage(e *ooxml.Element) {
	if sz := e.Child(w, "pgSz"); sz != nil {
		// A side past the longest a PDF page may have (ISO 32000-1,
		// Annex C.2) keeps the default, as a side of zero does: the page
		// prints rather than the conversion failing.
		if v, ok := ooxml.Twips(attrOr(sz, "w")); ok && v > 0 && v <= office.MaxPageSide {
			s.width = v
		}
		if v, ok := ooxml.Twips(attrOr(sz, "h")); ok && v > 0 && v <= office.MaxPageSide {
			s.height = v
		}
		if attrOr(sz, "orient") == "landscape" && s.width < s.height {
			s.width, s.height = s.height, s.width
		}
	}
	if m := e.Child(w, "pgMar"); m != nil {
		for side, name := range map[css.Side]string{css.Top: "top", css.Right: "right", css.Bottom: "bottom", css.Left: "left"} {
			if v, ok := ooxml.Twips(attrOr(m, name)); ok {
				s.margin[side] = v
			}
		}
		if v, ok := ooxml.Twips(attrOr(m, "header")); ok {
			s.header = max(0, v)
		}
		if v, ok := ooxml.Twips(attrOr(m, "footer")); ok {
			s.footer = max(0, v)
		}
		if v, ok := ooxml.Twips(attrOr(m, "gutter")); ok {
			s.gutter = max(0, v)
		}
	}
}

// pageOf returns a section holding the page size and margins of the
// sectPr e alone, as [document.parseSection] reads them: what the
// drawings of the section use before the section is read whole.
func (d *document) pageOf(e *ooxml.Element) *section {
	pw, ph := d.opts.DefaultPage(defaultPageWidth, defaultPageHeight)
	s := &section{
		width: pw, height: ph,
		margin: [4]float64{defaultMargin, defaultMargin, defaultMargin, defaultMargin},
		cols:   1, colSpace: 36,
	}
	if e != nil {
		s.readPage(e)
		s.readColumns(e.Child(w, "cols"))
	}
	s.applyOptions(d.opts)
	return s
}

// columned reports whether the w:sectPr e sets its text in more than one
// column (17.6.4 cols).
func columned(e *ooxml.Element) bool {
	c := e.Child(w, "cols")
	if c == nil {
		return false
	}
	if v, ok := ooxml.Int(attrOr(c, "num")); ok && v > 1 {
		return true
	}
	if hasAttr(c, "equalWidth") && !ooxml.OnOff(attrOr(c, "equalWidth"), true) {
		n := 0
		for _, col := range c.Kids() {
			if col.Is(w, "col") {
				n++
			}
		}
		return n > 1
	}
	return false
}

// readColumns reads the w:cols c of a section (17.6.4 cols, 17.6.3 col)
// into s; nil leaves one column.
func (s *section) readColumns(c *ooxml.Element) {
	if c == nil {
		return
	}
	if v, ok := ooxml.Int(attrOr(c, "num")); ok && v > 1 {
		s.cols = min(v, maxColumns)
	}
	if v, ok := ooxml.Twips(attrOr(c, "space")); ok {
		s.colSpace = max(0, v)
	}
	s.colSep = hasAttr(c, "sep") && ooxml.OnOff(attrOr(c, "sep"), true)
	if hasAttr(c, "equalWidth") && !ooxml.OnOff(attrOr(c, "equalWidth"), true) {
		// Columns of different widths list each column (17.6.3 col).
		for _, col := range c.Kids() {
			if !col.Is(w, "col") || len(s.colTracks) >= maxColumns {
				continue
			}
			cw, _ := ooxml.Twips(attrOr(col, "w"))
			sp, _ := ooxml.Twips(attrOr(col, "space"))
			s.colTracks = append(s.colTracks, columnTrack{w: max(0, cw), space: max(0, sp)})
		}
		if len(s.colTracks) > 1 {
			s.cols = len(s.colTracks)
		} else {
			if len(s.colTracks) == 1 {
				s.colWidth = s.colTracks[0].w
			}
			s.colTracks = nil
		}
	}
}

// parseSection reads a w:sectPr (17.6.17), headers and footers it does not
// name inherited from prev (17.10.5). A first or even header or footer
// that neither names is none, never the default one (17.10.2
// footerReference, 17.10.5 headerReference).
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/TestVMLData.docx: a section of a title
//     page that references a default footer before its first header, and
//     no first footer, prints its first page with no footer.
func (d *document) parseSection(e *ooxml.Element, prev *section) *section {
	pw, ph := d.opts.DefaultPage(defaultPageWidth, defaultPageHeight)
	s := &section{
		width: pw, height: ph,
		margin: [4]float64{defaultMargin, defaultMargin, defaultMargin, defaultMargin},
		header: defaultHFDistance, footer: defaultHFDistance,
		cols: 1, colSpace: 36, breakType: "nextPage",
		headers: map[string]string{}, footers: map[string]string{},
		headerH: map[string]float64{}, footerH: map[string]float64{},
	}
	if prev != nil {
		maps.Copy(s.headers, prev.headers)
		maps.Copy(s.footers, prev.footers)
	}
	if e == nil {
		return s
	}
	s.readPage(e)
	s.readColumns(e.Child(w, "cols"))
	if t := e.Child(w, "type"); t != nil {
		s.breakType = attrOr(t, "val")
	}
	if b := e.Child(w, "bidi"); b != nil {
		s.rtl = ooxml.Toggle(b)
	}
	if p := e.Child(w, "pgNumType"); p != nil {
		if v, ok := ooxml.Int(attrOr(p, "start")); ok {
			s.pageStart = new(max(-intconv.MaxCountStart, min(v, intconv.MaxCountStart)))
		}
		s.pageFormat = attrOr(p, "fmt")
		d.checkNumberFormat(s.pageFormat)
	}
	if t := e.Child(w, "titlePg"); t != nil {
		s.titlePage = ooxml.Toggle(t)
	}
	if s.gutter > 0 {
		s.placeGutter(d.settings.gutterAtTop, e.Child(w, "rtlGutter") != nil && ooxml.Toggle(e.Child(w, "rtlGutter")))
	}
	if v := e.Child(w, "vAlign"); v != nil {
		s.align, s.spread = sectionAlign(attrOr(v, "val"))
	}
	if b := e.Child(w, "pgBorders"); b != nil {
		s.frame = d.pageFrame(b, s)
	}
	for _, c := range e.Kids() {
		var refs map[string]string
		switch {
		case c.Is(w, "headerReference"):
			refs = s.headers
		case c.Is(w, "footerReference"):
			refs = s.footers
		default:
			continue
		}
		id, _ := c.AttrNS(ooxml.NSR, "id")
		rel, ok := d.pkg.RelationshipByID(d.ctx, d.main, id)
		if !ok || rel.External {
			continue
		}
		typ := attrOr(c, "type")
		if typ == "" {
			typ = "default"
		}
		refs[typ] = rel.Target
	}
	return s
}

// placeGutter moves the section's gutter into the margin it widens: the
// top margin when the document puts the gutter at the top (ECMA-376 Part
// 1, 17.15.1.49 gutterAtTop), the right one for a right-to-left gutter
// (17.6.16 rtlGutter), else the gutter stays beside the left margin, on
// the inside edge of a page with mirrored margins.
//
// # Corpus evidence
//
//   - litho/docx sections-gutter-top-rtl: Word prints a 1 inch gutter with
//     gutterAtTop above the text, which starts 1 inch lower and keeps the
//     full width between the side margins.
//   - litho/docx sections-margins-gutter: Word prints the even pages of
//     mirrored margins with the inside margin and the gutter on the right.
func (s *section) placeGutter(atTop, rtl bool) {
	switch {
	case atTop:
		if s.margin[css.Top] < 0 {
			s.margin[css.Top] -= s.gutter
		} else {
			s.margin[css.Top] += s.gutter
		}
	case rtl:
		s.margin[css.Right] += s.gutter
	default:
		return
	}
	s.gutter = 0
}

// applyOptions lays the conversion options over the section's page (see
// office.Options.Paper): the paper size replaces the page size and its
// orientation, landscape turns a portrait page.
func (s *section) applyOptions(opts office.Options) {
	s.width, s.height = opts.Paper(s.width, s.height)
	// Margins never leave less than a minimal page area.
	if s.margin[css.Left]+s.margin[css.Right]+s.gutter > s.width-minPageArea {
		s.margin[css.Left], s.margin[css.Right], s.gutter = 0, 0, 0
	}
	if math.Abs(s.margin[css.Top])+math.Abs(s.margin[css.Bottom]) > s.height-minPageArea {
		s.margin[css.Top], s.margin[css.Bottom] = 0, 0
	}
}

// samePage reports two sections whose pages have the same size and
// orientation, so that a continuous break between them keeps the page
// (17.18.77 ST_SectionMark continuous). The page and the pages that
// follow keep the earlier section's top and bottom margins, headers and
// footers; the later section's text keeps its own side margins
// ([sideInset]).
func samePage(a, b *section) bool {
	return a.width == b.width && a.height == b.height
}

// sideInset returns how far inside the text area of the pages of owner
// the text area of the section s, which continues on them, lies on the
// left and on the right: Word lays a continuous section's text out
// between its own side margins on the page it shares (ECMA-376 Part 1,
// 17.6.22 type continuous, 17.6.11 pgMar), at most as wide as the width
// its single column states (17.6.3 col). Insets that would leave less
// than [minPageArea] are none.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/fdo52208.docx: Word sets the text of a
//     continuous section of side margins 1680 after one of 1460 and 1420
//     at 84 points from the page's left edge, and breaks its lines at the
//     100 points of its one w:col of w 2000.
func sideInset(owner, s *section) [2]float64 {
	in := [2]float64{
		s.margin[css.Left] + s.gutter - owner.margin[css.Left] - owner.gutter,
		s.margin[css.Right] - owner.margin[css.Right],
	}
	if owner.contentWidth()-in[0]-in[1] < minPageArea {
		return [2]float64{}
	}
	return in
}

// pageFrame reads a w:pgBorders (17.6.10) into a page frame: inset by
// its space from the page edge (offsetFrom page) or outside the text
// (offsetFrom text, the default).
func (d *document) pageFrame(b *ooxml.Element, s *section) *layout.PageFrame {
	st := css.Initial()
	drawn, art := false, false
	var space [4]float64
	for side, name := range map[css.Side]string{css.Top: "top", css.Right: "right", css.Bottom: "bottom", css.Left: "left"} {
		e := b.Child(w, name)
		if e == nil {
			switch name {
			case "left":
				e = b.Child(w, "start")
			case "right":
				e = b.Child(w, "end")
			}
		}
		if e == nil {
			continue
		}
		if isArtBorder(attrOr(e, "val")) {
			art = true
			continue
		}
		bs := d.parseBorder(e)
		if bs.Style == css.BorderNone {
			continue
		}
		st.Border[side] = bs.Border
		strokeBorders(st)
		space[side] = bs.space
		drawn = true
	}
	if art {
		d.warn(warn.ContentUnsupported, warn.Omitted, "an art page border is left out, as Word prints the document")
	}
	if !drawn {
		return nil
	}
	f := &layout.PageFrame{Style: st}
	fromPage := attrOr(b, "offsetFrom") == "page"
	for i := range f.Inset {
		if fromPage {
			f.Inset[i] = space[i]
		} else {
			f.Inset[i] = max(0, math.Abs(s.margin[i])-space[i]-st.Border[i].Width)
		}
	}
	if f.Inset[css.Left]+f.Inset[css.Right] >= s.width-1 || f.Inset[css.Top]+f.Inset[css.Bottom] >= s.height-1 {
		return nil
	}
	return f
}

// lineBorders are the values of ST_Border (ECMA-376 Part 1, 17.18.2) that
// name a line style; every other value names an art border, a picture
// repeated along the side.
var lineBorders = map[string]bool{
	"nil": true, "none": true, "single": true, "thick": true, "double": true, "dotted": true,
	"dashed": true, "dotDash": true, "dotDotDash": true, "triple": true,
	"thinThickSmallGap": true, "thickThinSmallGap": true, "thinThickThinSmallGap": true,
	"thinThickMediumGap": true, "thickThinMediumGap": true, "thinThickThinMediumGap": true,
	"thinThickLargeGap": true, "thickThinLargeGap": true, "thinThickThinLargeGap": true,
	"wave": true, "doubleWave": true, "dashSmallGap": true, "dashDotStroked": true,
	"threeDEmboss": true, "threeDEngrave": true, "outset": true, "inset": true,
}

// isArtBorder reports a page border side of an art style (17.18.2), which
// Word prints without its picture or any line: the side is not drawn.
//
// # Corpus evidence
//
//   - openxmlsdk/docx O14ISOStrict/Word: the eight documents whose one page
//     border is an art border (holly, zigZagStitch, gems, weavingAngles,
//     cakeSlice, peopleHats, handmade2, whiteFlowers) print a blank page.
//   - litho/docx sections-page-borders: the pages framed in apples and in
//     stars print without a frame, where the single, double, dotted and
//     thick frames of the other pages print.
//   - poi/docx document/bib-chernigovka.netdo.ru_download_docs_17459.docx:
//     its mapleMuffins frame does not print.
func isArtBorder(val string) bool {
	return val != "" && !lineBorders[val]
}

// linePitch returns the line pitch in points of the document grid of the
// section sp (17.6.5 docGrid), 0 when its lines keep no grid: a grid of
// type lines or linesAndChars spaces them, the default and snapToChars
// grids do not.
func linePitch(sp *ooxml.Element) float64 {
	g := sp.Child(w, "docGrid")
	switch attrOr(g, "type") {
	case "lines", "linesAndChars":
	default:
		return 0
	}
	if v, ok := ooxml.Twips(attrOr(g, "linePitch")); ok && v >= 1 && v <= maxFontSize {
		return v
	}
	return 0
}

// body builds the document body (17.2.2) into its sections.
func (d *document) body(root *ooxml.Element) []*section {
	body := root.Child(w, "body")
	st := &story{part: d.main}
	var secs []*section
	var cur []blockNode
	var prev *section
	closeSection := func(sp *ooxml.Element) {
		s := d.parseSection(sp, prev)
		s.applyOptions(d.opts)
		s.index = len(secs)
		s.blocks = cur
		s.pending = d.pending
		cur, d.pending = nil, nil
		// A column break is a page break in a section of one column
		// (17.18.3 ST_BrType column) and a break to the next column in
		// one of several.
		for _, n := range d.columnBreaks {
			st := *n.Style
			st.BreakBefore = css.Page
			if s.cols > 1 {
				st.BreakBefore = css.Column
			}
			n.Style = d.share(&st)
		}
		d.columnBreaks = nil
		secs = append(secs, s)
		prev = s
	}
	d.inBody = true
	if body != nil && d.opts.DerivedAltText {
		d.markCaptions(mergeDeleted(body.Kids()))
	}
	if body != nil {
		// Each paragraph lays its lines on the document grid of the
		// section it belongs to, whose sectPr closes the section after it.
		var grids []float64
		var sects []*ooxml.Element
		for _, e := range mergeDeleted(body.Kids()) {
			if sp := e.Child(w, "pPr").Child(w, "sectPr"); sp != nil && e.Is(w, "p") {
				grids = append(grids, linePitch(sp))
				sects = append(sects, sp)
			}
		}
		grids = append(grids, linePitch(body.Child(w, "sectPr")))
		sects = append(sects, body.Child(w, "sectPr"))
		next := func() {
			d.linePitch, d.page, d.columned = 0, nil, false
			if len(secs) < len(grids) {
				d.linePitch = grids[len(secs)]
				d.page = d.pageOf(sects[len(secs)])
				d.columned = columned(sects[len(secs)])
			}
		}
		next()
		kids := mergeDeleted(body.Kids())
		for i, e := range kids {
			if d.ctx.Err() != nil {
				break
			}
			if e.Is(w, "sectPr") {
				continue
			}
			d.bodyFollows = holdsParagraph(kids[i+1:])
			sp := e.Child(w, "pPr").Child(w, "sectPr")
			if !e.Is(w, "p") {
				sp = nil
			}
			// An empty paragraph that only carries a section break is not
			// laid out, as LibreOffice imports it, unless it starts the
			// document: its section then takes a page of its own.
			if sp == nil || !emptyParagraph(e) || len(secs) == 0 && len(cur) == 0 {
				d.blocksInto(st, nil, &ooxml.Element{Children: []*ooxml.Element{e}}, 0, &cur)
			}
			if sp != nil {
				closeSection(sp)
				next()
			}
		}
	}
	closeSection(body.Child(w, "sectPr"))
	d.inBody, d.bodyFollows = false, false
	d.linePitch, d.page, d.columned = 0, nil, false
	// Name the pages: a continuous break keeps the page when its size
	// does not change; any other break starts a page, where the first
	// paragraph keeps only the part of its space before that the last
	// paragraph's space after did not already give, as LibreOffice lays
	// a section's first page out.
	for i, s := range secs {
		s.name = fmt.Sprintf("s%d", i)
		s.id = d.newID("sec")
		switch {
		case i > 0 && d.opts.SinglePage:
			// One page has one width: a section of another lays its
			// content out in the first section's.
			s.name = secs[0].name
			if s.width != secs[0].width {
				d.warn(warn.PageSetupUnsupported, warn.Approximated, fmt.Sprintf("section %d, %.1f points wide, is laid out at the %.1f points of the first section on the single page", i+1, s.width, secs[0].width))
			}
		case i > 0 && s.breakType == "continuous":
			if samePage(secs[i-1], s) {
				s.name = secs[i-1].name
				secs[i-1].balance = true
			}
		}
		owner := s
		for _, o := range secs[:i] {
			if o.name == s.name {
				owner = o
				break
			}
		}
		if owner != s && s.breakType == "continuous" {
			s.inset = sideInset(owner, s)
		}
		// The drawings take the page of the section whose pages s
		// shares, and the columns of s.
		geo := *owner
		geo.cols, geo.colSpace, geo.colTracks = s.cols, s.colSpace, s.colTracks
		for _, a := range s.pending {
			a.resolve(&geo)
			if !d.opts.SinglePage {
				continue
			}
			if ha, ok := a.heightAnchor(&geo); ok {
				d.heightAnchors = append(d.heightAnchors, ha)
			}
		}
		if i > 0 && s.name != secs[i-1].name && len(s.blocks) > 0 && len(secs[i-1].blocks) > 0 {
			first, last := s.blocks[0].info, secs[i-1].blocks[len(secs[i-1].blocks)-1].info
			if first != nil && last != nil {
				first.before = max(0, first.before-last.after)
			}
		}
	}
	return secs
}

// emptyParagraph reports a w:p without runs, fields or drawings.
func emptyParagraph(p *ooxml.Element) bool {
	for _, c := range p.Children {
		if !c.Is(w, "pPr") && !c.Is(w, "bookmarkStart") && !c.Is(w, "bookmarkEnd") && !c.Is(w, "proofErr") {
			return false
		}
	}
	return true
}

// sectionNode returns the block of section s: its page name, its columns,
// its blocks, its break to an odd or even page.
//
// # Corpus evidence
//
//   - litho/docx intl-rtl-hebrew: Word prints a right-to-left section of
//     two columns from its right column, and leaves the left one empty
//     where the content fits the right one: the document's last section
//     is not balanced.
//   - litho/docx sink-paper: Word balances the columns of the last section
//     with the endnotes that follow it.
func (d *document) sectionNode(s *section, extra []*layout.Node) *layout.Node {
	st := d.blockStyle()
	st.Page = s.name
	st.Margin[css.Left], st.Margin[css.Right] = css.Pts(s.inset[0]), css.Pts(s.inset[1])
	if s.cols <= 1 && s.colWidth > 0 {
		// A list of columns of different widths that holds one column
		// sets the text's width (17.6.3 col).
		st.MaxWidth = css.Pts(s.colWidth)
	}
	if s.cols > 1 {
		st.Columns.Count = s.cols
		st.ColumnGap = css.Pts(s.colSpace)
		if len(s.colTracks) > 0 {
			setColumnTracks(st, s.colTracks)
		}
		if !s.balance && len(extra) == 0 {
			// Word balances the columns of a section only where the next
			// section continues on its page, or where the endnotes follow
			// it; any other section fills its columns one after the
			// other, the document's last included.
			st.Columns.Fill = css.FillAuto
		}
		if s.rtl {
			// A right-to-left section lays its first column on the right
			// (17.6.1 bidi); a paragraph keeps its own direction.
			st.Direction = css.RTL
		}
		if s.colSep {
			st.Columns.RuleStyle = css.Solid
			st.Columns.RuleWidth = 0.5
			st.Columns.RuleColor = css.Color{A: 1}
		}
	}
	switch s.breakType {
	case "oddPage":
		st.BreakBefore = css.RightPage
	case "evenPage":
		st.BreakBefore = css.LeftPage
	}
	kids := append(d.finish(s.blocks, s.index == 0), extra...)
	return &layout.Node{Kind: layout.Element, Style: st, ID: s.id, Children: kids}
}

// sectionAlign returns how far a section's text moves down its pages, as
// a fraction of the room it leaves, by its vAlign (ECMA-376 Part 1,
// 17.6.23, 17.18.101 ST_VerticalJc): center sets it in the middle of the
// text area, bottom against its bottom; both spreads the room between
// the paragraphs, reported as true.
//
// # Corpus evidence
//
//   - litho/docx sections-valign and sink-report: Word prints a page of a
//     section of vAlign center with its text centred between the margins,
//     and one of bottom with its last line against the bottom margin. On
//     the page of both, its first paragraph starts where it would at
//     the top and each of the three gaps between its four paragraphs
//     grows by 180.4 pt, a third of the room, its last paragraph's space
//     after reaching the bottom margin.
//   - probe, docx: three paragraphs of a section of vAlign both print 305.3
//     pt further apart each, the first at the top margin, the last
//     paragraph's space after ending at the bottom margin.
func sectionAlign(v string) (float64, bool) {
	switch v {
	case "center":
		return 0.5, false
	case "bottom":
		return 1, false
	case "both":
		return 0, true
	}
	return 0, false
}
