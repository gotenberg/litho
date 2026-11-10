package docx

import (
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// The default page of a section without a pgSz or pgMar: US Letter with
// one-inch margins and half-inch header and footer distances, Word's
// defaults (ECMA-376 Part 1, 17.6.13 leaves them to the application).
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
	id               string
	width, height    float64
	margin           [4]float64
	header, footer   float64
	gutter           float64
	cols             int
	colSpace         float64
	colSep           bool
	colTracks        []columnTrack
	breakType        string
	pageStart        *int
	pageFormat       string
	titlePage        bool
	frame            *layout.PageFrame
	headers, footers map[string]string
	// shared names the first and even headers and footers ("header
	// first", "footer even") the section does not define that show the
	// default one: see [section.part].
	shared           map[string]bool
	blocks           []blockNode
	pending          []pendingAnchor
	headerH, footerH map[string]float64
	start            int
}

// parseSection reads a w:sectPr (17.6.17), headers and footers it does not
// name inherited from prev (17.10.5).
func (d *document) parseSection(e *ooxml.Element, prev *section) *section {
	s := &section{
		width: defaultPageWidth, height: defaultPageHeight,
		margin: [4]float64{defaultMargin, defaultMargin, defaultMargin, defaultMargin},
		header: defaultHFDistance, footer: defaultHFDistance,
		cols: 1, colSpace: 36, breakType: "nextPage",
		headers: map[string]string{}, footers: map[string]string{},
		headerH: map[string]float64{}, footerH: map[string]float64{},
		shared: map[string]bool{},
	}
	if prev != nil {
		maps.Copy(s.headers, prev.headers)
		maps.Copy(s.footers, prev.footers)
	}
	if e == nil {
		return s
	}
	if sz := e.Child(w, "pgSz"); sz != nil {
		if v, ok := ooxml.Twips(attrOr(sz, "w")); ok && v > 0 {
			s.width = v
		}
		if v, ok := ooxml.Twips(attrOr(sz, "h")); ok && v > 0 {
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
	if c := e.Child(w, "cols"); c != nil {
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
				s.colTracks = nil
			}
		}
	}
	if t := e.Child(w, "type"); t != nil {
		s.breakType = attrOr(t, "val")
	}
	if p := e.Child(w, "pgNumType"); p != nil {
		if v, ok := ooxml.Int(attrOr(p, "start")); ok {
			s.pageStart = new(v)
		}
		s.pageFormat = attrOr(p, "fmt")
	}
	if t := e.Child(w, "titlePg"); t != nil {
		s.titlePage = ooxml.Toggle(t)
	}
	if v := e.Child(w, "vAlign"); v != nil && attrOr(v, "val") != "top" {
		d.warn("approximated", "a section's vertical alignment is laid out from the top")
	}
	if b := e.Child(w, "pgBorders"); b != nil {
		s.frame = d.pageFrame(b, s)
	}
	// LibreOffice gives a first or even page the default header or
	// footer the section does not define for that kind when the default
	// reference comes before the section's first reference of the kind;
	// any other undefined one is empty.
	seenDefault := map[string]bool{}
	seenKind := map[string]bool{}
	for _, c := range e.Kids() {
		var refs map[string]string
		var which string
		switch {
		case c.Is(w, "headerReference"):
			refs, which = s.headers, "header"
		case c.Is(w, "footerReference"):
			refs, which = s.footers, "footer"
		default:
			continue
		}
		switch t := attrOr(c, "type"); t {
		case "", "default":
			seenDefault[which] = true
		case "first", "even":
			if !seenKind[t] {
				seenKind[t] = true
				for _, other := range []string{"header", "footer"} {
					s.shared[other+" "+t] = seenDefault[other]
				}
			}
		}
		id, _ := c.AttrNS(ooxml.NSR, "id")
		rel, ok := d.pkg.RelationshipByID(d.main, id)
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

// part returns the kind of header or footer (which) a page of the given
// kind shows: its own when the section chain defines it, else the default
// one when LibreOffice shares it ([section.shared]), else the kind, which
// names none and leaves the page without one, as Word shows it.
func (s *section) part(which, kind string) string {
	refs := s.headers
	if which == "footer" {
		refs = s.footers
	}
	if kind == "default" || refs[kind] != "" || !s.shared[which+" "+kind] {
		return kind
	}
	return "default"
}

// applyOptions lays the conversion options over the section's page: the
// paper size replaces the page size, landscape turns a portrait page.
func (s *section) applyOptions(w, h float64, landscape bool) {
	if w > 0 && h > 0 {
		portrait := s.width <= s.height
		s.width, s.height = w, h
		if !portrait && s.width < s.height {
			s.width, s.height = s.height, s.width
		}
	}
	if landscape && s.width < s.height {
		s.width, s.height = s.height, s.width
	}
	// Margins never leave less than a minimal page area.
	if s.margin[css.Left]+s.margin[css.Right]+s.gutter > s.width-minPageArea {
		s.margin[css.Left], s.margin[css.Right], s.gutter = 0, 0, 0
	}
	if abs(s.margin[css.Top])+abs(s.margin[css.Bottom]) > s.height-minPageArea {
		s.margin[css.Top], s.margin[css.Bottom] = 0, 0
	}
}

// samePage reports two sections whose pages have the same size and
// orientation, so that a continuous break between them keeps the page
// (17.18.77 ST_SectionMark continuous). The later section's margins,
// gutter, headers and footers are not applied: the page and the pages
// that follow keep the earlier section's setup, as LibreOffice lays a
// DOCX document out.
func samePage(a, b *section) bool {
	return a.width == b.width && a.height == b.height
}

// pageFrame reads a w:pgBorders (17.6.10) into a page frame: inset by
// its space from the page edge (offsetFrom page) or outside the text
// (offsetFrom text, the default).
func (d *document) pageFrame(b *ooxml.Element, s *section) *layout.PageFrame {
	st := css.Initial()
	drawn := false
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
		bs := d.parseBorder(e)
		if bs.Style == css.BorderNone {
			continue
		}
		st.Border[side] = bs.Border
		strokeBorders(st)
		space[side] = bs.space
		drawn = true
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
			f.Inset[i] = max(0, abs(s.margin[i])-space[i]-st.Border[i].Width)
		}
	}
	if f.Inset[css.Left]+f.Inset[css.Right] >= s.width-1 || f.Inset[css.Top]+f.Inset[css.Bottom] >= s.height-1 {
		return nil
	}
	return f
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
		s.applyOptions(d.opts.PageWidth, d.opts.PageHeight, d.opts.Landscape)
		s.index = len(secs)
		s.blocks = cur
		s.pending = d.pending
		cur, d.pending = nil, nil
		// A column break starts a page in a section of one column. In
		// columns it starts the next line, an approximation: the model has
		// no column break.
		// A column break is a page break in a section of one column
		// (17.18.3 ST_BrType column) and a break to the next column in
		// one of several.
		for _, st := range d.columnBreaks {
			st.BreakBefore = css.Page
			if s.cols > 1 {
				st.BreakBefore = css.Column
			}
		}
		d.columnBreaks = nil
		secs = append(secs, s)
		prev = s
	}
	d.inBody = true
	if body != nil {
		// Each paragraph lays its lines on the document grid of the
		// section it belongs to, whose sectPr closes the section after it.
		var grids []float64
		for _, e := range mergeDeleted(body.Kids()) {
			if sp := e.Child(w, "pPr").Child(w, "sectPr"); sp != nil && e.Is(w, "p") {
				grids = append(grids, linePitch(sp))
			}
		}
		grids = append(grids, linePitch(body.Child(w, "sectPr")))
		next := func() {
			d.linePitch = 0
			if len(secs) < len(grids) {
				d.linePitch = grids[len(secs)]
			}
		}
		next()
		for _, e := range mergeDeleted(body.Kids()) {
			if d.ctx.Err() != nil {
				break
			}
			if e.Is(w, "sectPr") {
				continue
			}
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
	d.inBody = false
	d.linePitch = 0
	// Name the pages: a continuous break keeps the page when its size
	// does not change; any other break starts a page, where the first
	// paragraph keeps only the part of its space before that the last
	// paragraph's space after did not already give, as LibreOffice lays
	// a section's first page out.
	for i, s := range secs {
		s.name = fmt.Sprintf("s%d", i)
		s.id = d.newID("sec")
		if i > 0 && s.breakType == "continuous" {
			if samePage(secs[i-1], s) {
				s.name = secs[i-1].name
			}
		}
		owner := s
		for _, o := range secs[:i] {
			if o.name == s.name {
				owner = o
				break
			}
		}
		for _, a := range s.pending {
			a.resolve(owner)
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
func (d *document) sectionNode(s *section, extra []*layout.Node) *layout.Node {
	st := d.blockStyle()
	st.Page = s.name
	if s.cols > 1 {
		st.Columns.Count = s.cols
		st.ColumnGap = css.Pts(s.colSpace)
		if len(s.colTracks) > 0 {
			setColumnTracks(st, s.colTracks)
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
	kids := append(finish(s.blocks), extra...)
	return &layout.Node{Kind: layout.Element, Style: st, ID: s.id, Children: kids}
}
