package html

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// pageKey is what selects the @page rules of a page: its name, whether it
// is the first, its side and whether it is blank (CSS Page 3, 4.2).
type pageKey struct {
	name                string
	first, right, blank bool
}

// pageEntry is the box and style of the pages of one key.
type pageEntry struct {
	box   layout.PageBox
	style css.PageStyle
}

// pageSetup resolves the page boxes and page styles of a document whose
// pages differ by name, blankness or page-margin boxes, as the layout
// discovers its pages. Each key is resolved once.
type pageSetup struct {
	r    *css.Resolver
	o    Options
	rtl  bool
	root *css.Style
	// cache holds the resolved keys, warnings the page box warnings once
	// each.
	cache    map[pageKey]*pageEntry
	seen     map[string]bool
	warnings []compose.Warning
}

// newPageSetup returns the page setup of the document r cascades, whose
// root element's style is root and direction rtl, on the paper of o.
func newPageSetup(r *css.Resolver, o Options, rtl bool, root *css.Style) *pageSetup {
	return &pageSetup{r: r, o: o, rtl: rtl, root: root, cache: map[pageKey]*pageEntry{}, seen: map[string]bool{}}
}

// key returns the key of the page info names: the first page is a right
// page in a left-to-right document, a left one otherwise (CSS Page 3,
// 4.2.1).
func (s *pageSetup) key(info layout.PageInfo) pageKey {
	right := (info.Index%2 == 0) != s.rtl
	return pageKey{name: info.Name, first: info.Index == 0, right: right, blank: info.Blank}
}

// entry returns the resolved box and style of the page info names.
func (s *pageSetup) entry(info layout.PageInfo) *pageEntry {
	k := s.key(info)
	if e, ok := s.cache[k]; ok {
		return e
	}
	sel := css.PageSelector{Name: k.name, First: k.first, Right: k.right, Left: !k.right, Blank: k.blank}
	b, ws := pageBox(s.r, s.o, sel)
	for _, w := range ws {
		if !s.seen[w.Message] {
			s.seen[w.Message] = true
			s.warnings = append(s.warnings, w)
		}
	}
	e := &pageEntry{box: b, style: s.r.PageIn(sel, s.root)}
	s.cache[k] = e
	return e
}

// box returns the page box of the page info names, in points of the page:
// the layout lays it out as the print setup says ([printSetup.layoutBox]).
func (s *pageSetup) box(info layout.PageInfo) layout.PageBox { return s.entry(info).box }

// setupPages hands the layout what the page setup needs: the boxes of
// named and blank pages, which only the layout discovers (CSS Page 3, 4.1
// and 4.2.3), and the page-margin boxes (CSS Page 3, 5).
func (p *prepared) setupPages(ctx context.Context, r *css.Resolver) {
	if r.HasNamedPages() {
		p.layout.PageFor = func(info layout.PageInfo) layout.PageBox { return p.setup.layoutBox(p.pages.box(info)) }
	}
	boxes := r.HasMarginBoxes()
	if !boxes && p.o.HeaderTemplate == "" && p.o.FooterTemplate == "" {
		return
	}
	p.layout.MarginContent = func(info layout.PageInfo, pages int) []compose.Fragment {
		// The headers and footers, drawn once the layout is done, need
		// the page boxes of the pages it laid out.
		p.infos = append(p.infos, info)
		if !boxes {
			return nil
		}
		return p.marginContent(ctx, info, pages)
	}
}
