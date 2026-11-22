package docx

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxSubLayoutPages caps the pages of a header or footer laid out on its
// own. Anti-DoS guard, not spec-mandated: a header is a few lines.
const maxSubLayoutPages = 4

// measureSide is the height of the page a header or footer is measured
// on, in points: taller than any header.
const measureSide = 14400

// pages holds the page setup the layout asks for: the sections by page
// name and the first page of each, once the layout placed them.
type pages struct {
	d       *document
	secs    []*section
	byName  map[string]*section
	hfParts map[string]*ooxml.Element
	// height is the height of a single page, 0 until its content is laid
	// out: the page is then [office.MaxPageSide] tall and trimmed, or
	// [office.Options.FittedPageLimit] tall once tall is set.
	height float64
	// tall lays a single page out past [office.MaxPageSide], once the
	// content was found to pass it.
	tall bool
}

// newPages measures the headers and footers of every section and returns
// the page setup.
func (d *document) newPages(secs []*section) *pages {
	p := &pages{d: d, secs: secs, byName: map[string]*section{}, hfParts: map[string]*ooxml.Element{}}
	for _, s := range secs {
		if _, ok := p.byName[s.name]; !ok {
			p.byName[s.name] = s
		}
		for _, kind := range []string{"default", "first", "even"} {
			probe := &fieldValues{page: 1, pages: 1, format: "decimal"}
			if part := s.headers[kind]; part != "" {
				s.headerH[kind] = p.measure(s, part, probe)
			}
			if part := s.footers[kind]; part != "" {
				s.footerH[kind] = p.measure(s, part, probe)
			}
		}
	}
	return p
}

// root returns the parsed root of a header or footer part, nil when it
// does not parse.
func (p *pages) root(part string) *ooxml.Element {
	if r, ok := p.hfParts[part]; ok {
		return r
	}
	r, err := p.d.part(part)
	if err != nil {
		if !p.d.stop(err) {
			p.d.warn(warn.PartMalformed, warn.Omitted, "a header or footer does not parse and is left out")
		}
		r = nil
	}
	p.hfParts[part] = r
	return r
}

// content builds the blocks of a header or footer part with the page
// fields v, and returns the drawings in it positioned from the page,
// which the caller places once it knows where the part sits.
//
// # Corpus evidence
//
//   - litho/docx doc-watermark-picture: Word prints a picture anchored in
//     a header from the page at its page offsets, mid-page, not beside
//     the header's paragraph.
//   - libreoffice/docx ooxmlimport/n779627b: layout rises from 0 to 0.37.
func (p *pages) content(part string, v *fieldValues) (*layout.Node, []pendingAnchor) {
	r := p.root(part)
	st := p.d.blockStyle()
	// A flow root keeps its paragraphs' margins inside it, as the header
	// and footer areas of Writer do.
	st.Display = css.FlowRoot
	n := &layout.Node{Kind: layout.Element, Style: st}
	if r == nil {
		return n, nil
	}
	s := &story{part: part, fields: v}
	p.d.inMargin, p.d.marginAnchors = true, nil
	n.Children = p.d.finish(p.d.blocks(s, nil, r, 1), false)
	anchors := p.d.marginAnchors
	p.d.inMargin, p.d.marginAnchors = false, nil
	return n, anchors
}

// measure returns the height of a header or footer part laid out at the
// width of section s's text.
func (p *pages) measure(s *section, part string, v *fieldValues) float64 {
	n, _ := p.content(part, v)
	n.ID = "\x00measure"
	areaW := max(s.width-s.margin[css.Left]-s.margin[css.Right]-s.gutter, minPageArea)
	n.Style.Width = css.Pts(areaW)
	res, err := layout.Layout(p.d.ctx, wrap(p.d.blockStyle(), n), p.d.fonts, layout.Options{
		Page:     layout.PageBox{Width: areaW, Height: measureSide},
		Lang:     p.d.lang(),
		MaxPages: maxSubLayoutPages,
	})
	if err != nil {
		// A header or footer that does not lay out takes no room.
		p.d.stop(err)
		return 0
	}
	if r, ok := res.Extents[n.ID]; ok {
		return r.Height
	}
	return 0
}

// wrap returns a root block holding n.
func wrap(st *css.Style, n ...*layout.Node) *layout.Node {
	return &layout.Node{Kind: layout.Element, Style: st, Children: n}
}

// box returns the page box of the page info names (PageFor): its section's
// size and margins, the top and bottom margins grown to hold a header or
// footer taller than its distance leaves, as Word and Writer grow them,
// the section's page borders and the document's page background.
func (p *pages) box(info layout.PageInfo) layout.PageBox {
	if p.d.opts.SinglePage {
		return p.singleBox()
	}
	s := p.byName[info.Name]
	if s == nil {
		s = p.secs[0]
	}
	return p.sectionBox(s, info.Index)
}

// singleBox returns the page box of a single page: the first section's
// width, side and top margins, the last section's bottom margin, and
// [office.MaxPageSide] tall, or [office.Options.FittedPageLimit] once
// [pages.tall] is set, the page the layout trims to its content, or the
// height its content measured once it is known.
func (p *pages) singleBox() layout.PageBox {
	b := p.sectionBox(p.secs[0], 0)
	b.Margin[css.Bottom] = p.sectionBox(p.secs[len(p.secs)-1], 0).Margin[css.Bottom]
	b.Height = office.MaxPageSide
	if p.tall {
		b.Height = p.d.opts.FittedPageLimit()
	}
	if p.height > 0 {
		b.Height = p.height
	}
	return b
}

// sectionBox returns the page box of a page of section s, index being
// the page's 0-based position in the document (see [pages.box]).
func (p *pages) sectionBox(s *section, index int) layout.PageBox {
	m := s.margin
	top, bottom := math.Abs(m[css.Top]), math.Abs(m[css.Bottom])
	hh, fh := 0.0, 0.0
	for _, h := range s.headerH {
		hh = max(hh, h)
	}
	for _, h := range s.footerH {
		fh = max(fh, h)
	}
	if m[css.Top] >= 0 && hh > 0 {
		top = max(top, s.header+hh)
	}
	if m[css.Bottom] >= 0 && fh > 0 {
		bottom = max(bottom, s.footer+fh)
	}
	top = min(top, float64(s.height/2)-minPageArea/2)
	bottom = min(bottom, float64(s.height/2)-minPageArea/2)
	left, right := m[css.Left]+s.gutter, m[css.Right]
	if p.d.settings.mirrorMargins && index%2 == 1 {
		// An even page of mirrored margins has its inside margin, and
		// the gutter, on the right (17.15.1.57 mirrorMargins).
		left, right = right, left
	}
	return layout.PageBox{
		Width: s.width, Height: s.height,
		Margin:       [4]float64{top, right, bottom, left},
		Frame:        s.frame,
		AlignContent: s.align,
		Background:   p.d.background,
		FootnoteRule: footnoteRule(s.contentWidth()),
	}
}

// The footnote separator LibreOffice prints for a DOCX document, whatever
// its separator footnote (ECMA-376 Part 1, 17.11.23 separator) holds: a
// black rule 144 points (two inches) long and 0.75 point thick at the
// start of the text area, 8.1 points below the body text and 4.95 points
// above the first note.
const (
	footnoteRuleLength     = 144
	footnoteRuleThickness  = 0.75
	footnoteRuleSpaceAbove = 8.1
	footnoteRuleSpaceBelow = 4.95
)

// footnoteRule returns the footnote separator of a page whose text area
// is width points wide.
func footnoteRule(width float64) *layout.FootnoteRule {
	return &layout.FootnoteRule{
		Width:      min(1, footnoteRuleLength/max(width, minPageArea)),
		Thickness:  footnoteRuleThickness,
		Color:      css.Color{A: 1},
		Align:      css.AlignLeft,
		Line:       css.Solid,
		SpaceAbove: footnoteRuleSpaceAbove,
		SpaceBelow: footnoteRuleSpaceBelow,
	}
}

// place records where each section starts (Placed).
func (p *pages) place(anchors map[string]typeset.Dest) {
	for _, s := range p.secs {
		if d, ok := anchors[s.id]; ok {
			s.start = d.Page
		}
	}
}

// numbering returns the section owning page (1-based) and the page's
// number: a section owns the pages from the
// page it starts on, a continuous section from the next one; a section
// numbers its pages from its pgNumType start, else on from the previous
// section (17.6.12).
func (p *pages) numbering(page, total int) (*section, int) {
	owner := 0
	starts := make([]int, len(p.secs))
	for i, s := range p.secs {
		st := s.start
		if st == 0 {
			st = total + 1
		}
		if s.breakType == "continuous" && i > 0 && s.name == p.secs[i-1].name {
			st++
		}
		starts[i] = st
		if st <= page {
			owner = i
		}
	}
	number := 0
	for i := 0; i <= owner; i++ {
		first := starts[i]
		if i == owner {
			first = min(first, page)
		}
		if s := p.secs[i].pageStart; s != nil {
			number = *s - 1
		}
		if i < owner {
			next := starts[i+1]
			if next > first {
				number += next - first
			}
			continue
		}
		number += page - first + 1
	}
	return p.secs[owner], number
}

// margins returns what the margins of a page show (MarginContent): its
// section's header and footer for the page's kind (17.10.1: the first
// page of a section with a title page takes the first header, even pages
// the even one when the settings ask for them, every other page the
// default; a first or even one the sections leave undefined is empty, or
// the default one as [section.part] says), with the page fields of the
// page.
func (p *pages) margins(info layout.PageInfo, total int) []typeset.Fragment {
	if info.Blank {
		return nil
	}
	s, number := p.numbering(info.Index+1, total)
	kind := "default"
	switch {
	case s.titlePage && info.Index+1 == max(s.start, 1):
		kind = "first"
	case p.d.settings.evenAndOdd && number%2 == 0:
		kind = "even"
	}
	foot, footKind := s, kind
	if p.d.opts.SinglePage {
		s, number, kind, foot, footKind = p.singleParts()
		total = 1
	}
	pb := p.box(info)
	if info.Height > 0 {
		pb.Height = info.Height
	}
	areaW := max(pb.Width-pb.Margin[css.Left]-pb.Margin[css.Right], minPageArea)
	root := &layout.Node{Kind: layout.Element, Style: p.d.blockStyle()}
	place := func(s *section, part string, y float64, fromBottom bool, h float64) {
		if part == "" {
			return
		}
		v := &fieldValues{page: number, pages: total, format: pageFormat(s.pageFormat)}
		n, anchors := p.content(part, v)
		n.Style.Position = css.Absolute
		n.Style.Left = css.Pts(pb.Margin[css.Left])
		n.Style.Width = css.Pts(areaW)
		if fromBottom {
			y = pb.Height - y - h
		}
		n.Style.Top = css.Pts(max(0, y))
		// A drawing positioned from the page sits on the page drawn, the
		// single page trimmed to its content included.
		geo := *s
		geo.height = pb.Height
		for _, a := range anchors {
			// The part is the drawing's containing block: its offsets
			// from the page become offsets from the part's corner.
			a.resolve(&geo)
			a.st.Left = css.Pts(a.st.Left.Value - pb.Margin[css.Left])
			a.st.Top = css.Pts(a.st.Top.Value - n.Style.Top.Value)
		}
		root.Children = append(root.Children, n)
	}
	hk, fk := s.part("header", kind), foot.part("footer", footKind)
	place(s, s.headers[hk], s.header, false, s.headerH[hk])
	place(foot, foot.footers[fk], foot.footer, true, foot.footerH[fk])
	if len(root.Children) == 0 {
		return nil
	}
	res, err := layout.Layout(p.d.ctx, wrap(p.d.blockStyle(), root), p.d.fonts, layout.Options{
		Page:     layout.PageBox{Width: pb.Width, Height: pb.Height},
		Lang:     p.d.lang(),
		MaxPages: maxSubLayoutPages,
	})
	if err != nil || len(res.Pages) == 0 {
		// A header or footer that does not lay out prints nothing.
		p.d.stop(err)
		return nil
	}
	return artifacts(res.Pages[0].Content)
}

// singleParts returns what the margins of a single page show: the
// header of the first section's first page, its kind and the page's
// number, then the footer of the last section on a page after its first,
// as the last page of the document shows it, and its kind (17.10.1). The
// page is numbered as the first section numbers its first page (17.6.12
// pgNumType start), and counts one page.
func (p *pages) singleParts() (head *section, number int, headKind string, foot *section, footKind string) {
	head, foot = p.secs[0], p.secs[len(p.secs)-1]
	number = 1
	if head.pageStart != nil {
		number = *head.pageStart
	}
	even := p.d.settings.evenAndOdd && number%2 == 0
	headKind, footKind = "default", "default"
	switch {
	case head.titlePage:
		headKind = "first"
	case even:
		headKind = "even"
	}
	if even {
		footKind = "even"
	}
	return head, number, headKind, foot, footKind
}

// artifacts returns frags with no structure element: headers and footers
// are pagination artifacts (ISO 32000-1, 14.8.2.2.2).
func artifacts(frags []typeset.Fragment) []typeset.Fragment {
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Text:
			v.Parent = nil
		case *typeset.Image:
			v.Parent = nil
		case *typeset.Path:
			v.Parent = nil
		case *typeset.Group:
			artifacts(v.Content)
		}
	}
	return frags
}
