package html

import (
	"context"
	"strings"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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
	// ctx is the conversion's context: a page setup does not outlive the
	// conversion it serves.
	ctx  context.Context
	r    *css.Resolver
	o    Options
	rtl  bool
	root *css.Style
	// cache holds the resolved keys, warnings the page box warnings once
	// each.
	cache    map[pageKey]*pageEntry
	seen     map[string]bool
	warnings []typeset.Warning
	// err is ctx's error, which stopped a page's resolution. The layout
	// callbacks that ask for a page return no error: the layout stops at
	// its own next check of ctx, and [prepared.assemble] reports err.
	err error
}

// newPageSetup returns the page setup of the document r cascades, whose
// root element's style is root and direction rtl, on the paper of o,
// resolved under ctx.
func newPageSetup(ctx context.Context, r *css.Resolver, o Options, rtl bool, root *css.Style) *pageSetup {
	return &pageSetup{ctx: ctx, r: r, o: o, rtl: rtl, root: root, cache: map[pageKey]*pageEntry{}, seen: map[string]bool{}}
}

// key returns the key of the page info names: the first page is a right
// page in a left-to-right document, a left one otherwise (CSS Page 3,
// 4.2.1).
func (s *pageSetup) key(info layout.PageInfo) pageKey {
	right := (info.Index%2 == 0) != s.rtl
	return pageKey{name: info.Name, first: info.Index == 0, right: right, blank: info.Blank}
}

// entry returns the resolved box and style of the page info names. When
// ctx's error stops the resolution, it records the error in s.err and
// returns the paper of the options, with no page-margin box.
func (s *pageSetup) entry(info layout.PageInfo) *pageEntry {
	k := s.key(info)
	if e, ok := s.cache[k]; ok {
		return e
	}
	sel := css.PageSelector{Name: k.name, First: k.first, Right: k.right, Left: !k.right, Blank: k.blank}
	b, ws, err := pageBox(s.ctx, s.r, s.o, sel)
	if err != nil {
		return s.stopped(err)
	}
	for _, w := range ws {
		if !s.seen[w.Message] {
			s.seen[w.Message] = true
			s.warnings = append(s.warnings, w)
		}
	}
	style, err := s.r.PageIn(s.ctx, sel, s.root)
	if err != nil {
		return s.stopped(err)
	}
	e := &pageEntry{box: b, style: style}
	s.cache[k] = e
	return e
}

// stopped records err, which stopped a page's resolution, and returns the
// entry of the options' paper and margins.
func (s *pageSetup) stopped(err error) *pageEntry {
	if s.err == nil {
		s.err = err
	}
	o := s.o
	return &pageEntry{box: layout.PageBox{
		Width: o.PageWidth, Height: o.PageHeight,
		Margin: [4]float64{css.Top: o.MarginTop, css.Right: o.MarginRight, css.Bottom: o.MarginBottom, css.Left: o.MarginLeft},
	}}
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
	if boxes && p.b != nil && p.b.gcpm.active() {
		// The named strings and running elements a page shows follow
		// where their elements landed (CSS GCPM 3, 2).
		p.layout.Placed = func(a map[string]typeset.Dest) { p.b.gcpm.placed = a }
	}
	if !boxes && p.o.HeaderTemplate == "" && p.o.FooterTemplate == "" {
		return
	}
	p.layout.MarginContent = func(info layout.PageInfo, pages int) []typeset.Fragment {
		// The headers and footers, drawn once the layout is done, need
		// the page boxes of the pages it laid out.
		p.infos = append(p.infos, info)
		if !boxes {
			return nil
		}
		return p.marginContent(ctx, info, pages)
	}
}

// startPage returns the start page value of the root element root (CSS
// Page 3, 4.1): the page property of its first in-flow block-level child,
// recursively, else its own, "" for auto. It reads the computed styles
// before any box exists, for the viewport of the first page.
func startPage(c *css.Computed, root *dom.Node) string {
	name := ""
	n := root
	for depth := 0; n != nil && depth < maxTreeDepth; depth++ {
		st := c.Style(n)
		if st == nil {
			break
		}
		if st.Page != "" {
			name = st.Page
		}
		var next *dom.Node
		for k := n.FirstChild; k != nil; k = k.NextSibling {
			if k.Type == dom.TextNode && strings.TrimSpace(k.Data) != "" {
				return name
			}
			if k.Type != dom.ElementNode {
				continue
			}
			ks := c.Style(k)
			if ks == nil || ks.Display == css.DisplayNone || ks.Float != css.FloatNone || ks.Position == css.Absolute || ks.Position == css.Fixed {
				continue
			}
			if ks.Display == css.Inline || ks.Display == css.InlineBlock {
				return name
			}
			next = k
			break
		}
		n = next
	}
	return name
}
