package docx

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
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
			probe := &fieldValues{page: 1, pages: 1, sectionPages: 1, format: "decimal"}
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
		p.d.warn("malformed-part", "a header or footer does not parse and is left out")
		r = nil
	}
	p.hfParts[part] = r
	return r
}

// content builds the blocks of a header or footer part with the page
// fields v.
func (p *pages) content(part string, v *fieldValues) *layout.Node {
	r := p.root(part)
	st := p.d.blockStyle()
	n := &layout.Node{Kind: layout.Element, Style: st}
	if r == nil {
		return n
	}
	s := &story{part: part, fields: v}
	n.Children = finish(p.d.blocks(s, nil, r, 1))
	return n
}

// measure returns the height of a header or footer part laid out at the
// width of section s's text.
func (p *pages) measure(s *section, part string, v *fieldValues) float64 {
	n := p.content(part, v)
	n.ID = "\x00measure"
	areaW := max(s.width-s.margin[css.Left]-s.margin[css.Right]-s.gutter, minPageArea)
	n.Style.Width = css.Pts(areaW)
	res, err := layout.Layout(p.d.ctx, wrap(p.d.blockStyle(), n), p.d.fonts, layout.Options{
		Page:     layout.PageBox{Width: areaW, Height: measureSide},
		Lang:     p.d.lang(),
		MaxPages: maxSubLayoutPages,
	})
	if err != nil {
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
// and the section's page borders.
func (p *pages) box(info layout.PageInfo) layout.PageBox {
	s := p.byName[info.Name]
	if s == nil {
		s = p.secs[0]
	}
	m := s.margin
	top, bottom := abs(m[css.Top]), abs(m[css.Bottom])
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
	top = min(top, s.height/2-minPageArea/2)
	bottom = min(bottom, s.height/2-minPageArea/2)
	return layout.PageBox{
		Width: s.width, Height: s.height,
		Margin: [4]float64{top, m[css.Right], bottom, m[css.Left] + s.gutter},
		Frame:  s.frame,
	}
}

// place records where each section starts (Placed).
func (p *pages) place(anchors map[string]compose.Dest) {
	for _, s := range p.secs {
		if d, ok := anchors[s.id]; ok {
			s.start = d.Page
		}
	}
}

// numbering returns the section owning page (1-based), the page's number
// in it and the section's page count: a section owns the pages from the
// page it starts on, a continuous section from the next one; a section
// numbers its pages from its pgNumType start, else on from the previous
// section (17.6.12).
func (p *pages) numbering(page, total int) (*section, int, int) {
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
	end := total + 1
	for j := owner + 1; j < len(p.secs); j++ {
		if starts[j] > starts[owner] {
			end = starts[j]
			break
		}
	}
	return p.secs[owner], number, max(1, end-max(starts[owner], 1))
}

// margins returns what the margins of a page show (MarginContent): its
// section's header and footer for the page's kind (17.10.1: the first
// page of a section with a title page takes the first header, even pages
// the even one when the settings ask for them, every other page the
// default), with the page fields of the page.
func (p *pages) margins(info layout.PageInfo, total int) []compose.Fragment {
	if info.Blank {
		return nil
	}
	s, number, secPages := p.numbering(info.Index+1, total)
	kind := "default"
	switch {
	case s.titlePage && info.Index+1 == max(s.start, 1):
		kind = "first"
	case p.d.settings.evenAndOdd && number%2 == 0:
		kind = "even"
	}
	v := &fieldValues{page: number, pages: total, sectionPages: secPages, format: pageFormat(s.pageFormat)}
	pb := p.box(info)
	areaW := max(pb.Width-pb.Margin[css.Left]-pb.Margin[css.Right], minPageArea)
	root := &layout.Node{Kind: layout.Element, Style: p.d.blockStyle()}
	place := func(part string, y float64, fromBottom bool, h float64) {
		if part == "" {
			return
		}
		n := p.content(part, v)
		n.Style.Position = css.Absolute
		n.Style.Left = css.Pts(pb.Margin[css.Left])
		n.Style.Width = css.Pts(areaW)
		if fromBottom {
			y = pb.Height - y - h
		}
		n.Style.Top = css.Pts(max(0, y))
		root.Children = append(root.Children, n)
	}
	header, footer := s.headers[kind], s.footers[kind]
	place(header, s.header, false, s.headerH[kind])
	place(footer, s.footer, true, s.footerH[kind])
	if len(root.Children) == 0 {
		return nil
	}
	res, err := layout.Layout(p.d.ctx, wrap(p.d.blockStyle(), root), p.d.fonts, layout.Options{
		Page:     layout.PageBox{Width: pb.Width, Height: pb.Height},
		Lang:     p.d.lang(),
		MaxPages: maxSubLayoutPages,
	})
	if err != nil || len(res.Pages) == 0 {
		return nil
	}
	return artifacts(res.Pages[0].Content)
}

// artifacts returns frags with no structure element: headers and footers
// are pagination artifacts (ISO 32000-1, 14.8.2.2.2).
func artifacts(frags []compose.Fragment) []compose.Fragment {
	for _, f := range frags {
		switch v := f.(type) {
		case *compose.Text:
			v.Parent = nil
		case *compose.Image:
			v.Parent = nil
		case *compose.Path:
			v.Parent = nil
		case *compose.Group:
			artifacts(v.Content)
		}
	}
	return frags
}
