package css

import (
	"cmp"
	"context"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// PageSelector names the page a [Resolver.Page] call styles (CSS Page 3,
// 4.2).
type PageSelector struct {
	// Name is the page type name, "" for an unnamed page.
	Name string
	// First is set for the first page of the document; Left and Right for
	// a left or right page of a spread.
	First, Left, Right bool
	// Blank is set for a page a forced break to a left or a right page
	// left empty (CSS Page 3, 4.2.3).
	Blank bool
	// Index is the page's 1-based place in the document, which :nth()
	// matches (CSS GCPM 3, 3.1).
	Index int
}

// pageSel is a parsed page selector. nth holds the An+B of its :nth()
// pseudo-classes, each of which the page's index must match.
type pageSel struct {
	name                      string
	first, left, right, blank bool
	nth                       [][2]int
	spec                      uint32
}

// pageRule is an @page rule.
type pageRule struct {
	sels  []pageSel
	decls []pageDecl
	cond  *mediaCond
	// context holds the declarations that style the page context and
	// margins its margin at-rules (CSS Page 3, 4.4 and 5.1).
	context []Declaration
	margins []marginRule
}

// pageSize is a specified size descriptor (CSS Page 3, 5.1).
type pageSize struct {
	w, h   float64
	orient Orientation
}

// pageDecl is one descriptor of an @page rule.
type pageDecl struct {
	important bool
	size      *pageSize
	margins   []entry
	// background holds the background longhands the descriptor sets.
	background []entry
	// bleed and marks are the bleed and marks descriptors (CSS Page 3,
	// 7.1 and 7.2).
	bleed *Dimension
	marks *Marks
	// areaW and areaH are the width and height of the page context.
	areaW, areaH *Dimension
}

// parsePageSelectors parses a page selector list (CSS Page 3, 4.1); an
// empty prelude matches every page. A list litho cannot match returns
// false, and the pseudo-class it does not support when that is the
// reason: the whole rule is dropped, as an invalid selector drops a style
// rule (Selectors 4, 3.1).
func parsePageSelectors(prelude []token) ([]pageSel, string, bool) {
	l := trimWS(prelude)
	if len(l) == 0 {
		return []pageSel{{}}, "", true
	}
	var out []pageSel
	for _, part := range splitCommas(l) {
		part = trimWS(part)
		if len(part) == 0 {
			return nil, "", false
		}
		s, unsupported, ok := parsePageSelector(part)
		if !ok {
			return nil, unsupported, false
		}
		out = append(out, s)
	}
	return out, "", true
}

// parsePageSelector parses one page selector: an optional page type name
// and its pseudo-classes, :first, :left, :right, :blank (CSS Page 3, 4.2)
// and :nth(An+B) (CSS GCPM 3, 3.1).
//
// :nth() of a named page, and :nth(An+B of name), select pages inside a
// page group (CSS GCPM 3, 3.2), which the layout does not track: they are
// not supported, and neither is a pseudo-class the drafts do not define:
// the second result names it.
func parsePageSelector(part []token) (pageSel, string, bool) {
	var s pageSel
	i := 0
	if part[0].typ == tIdent {
		s.name = part[0].value
		i++
	}
	for i < len(part) {
		if part[i].typ != tColon || i+1 >= len(part) {
			return pageSel{}, "", false
		}
		t := part[i+1]
		i += 2
		if t.typ == tFunction {
			if lower(t.value) != "nth" {
				return pageSel{}, ":" + lower(t.value) + "()", false
			}
			if s.name != "" {
				return pageSel{}, s.name + ":nth()", false
			}
			a, b, ok := parseAnB(t.children)
			if !ok {
				if pageGroup(t.children) {
					return pageSel{}, ":nth(of)", false
				}
				return pageSel{}, "", false
			}
			if len(s.nth) == maxPageNth {
				return pageSel{}, "", false
			}
			s.nth = append(s.nth, [2]int{a, b})
			continue
		}
		if t.typ != tIdent {
			return pageSel{}, "", false
		}
		switch lower(t.value) {
		case "first":
			s.first = true
		case "left":
			s.left = true
		case "right":
			s.right = true
		case "blank":
			s.blank = true
		default:
			return pageSel{}, ":" + lower(t.value), false
		}
	}
	// CSS Page 3, 4.4: page type names count as f, :first and :blank
	// as g, :left and :right as h. The drafts give :nth() no weight
	// (CSSWG issue 3524): it counts as g, as :first, the page it
	// generalizes, and as WeasyPrint weighs it.
	f, g, h := 0, len(s.nth), 0
	if s.name != "" {
		f = 1
	}
	if s.first {
		g++
	}
	if s.blank {
		g++
	}
	if s.left {
		h++
	}
	if s.right {
		h++
	}
	s.spec = packSpec(f, g, h)
	return s, "", true
}

// pageGroup reports an :nth() argument that names a page group, An+B of
// name (CSS GCPM 3, 3.2).
func pageGroup(args []token) bool {
	for _, t := range args {
		if t.isIdent("of") {
			return true
		}
	}
	return false
}

// maxPageNth bounds the :nth() pseudo-classes of one page selector: 8.
// Anti-DoS guard, not spec-mandated: a selector names one page pattern,
// and every page of the document tests each.
const maxPageNth = 8

// matches reports whether the selector applies to the page sel names.
func (s *pageSel) matches(sel PageSelector) bool {
	if s.name != "" && s.name != sel.Name {
		return false
	}
	for _, ab := range s.nth {
		if !nthMatches(ab[0], ab[1], sel.Index) {
			return false
		}
	}
	return (!s.first || sel.First) && (!s.left || sel.Left) && (!s.right || sel.Right) && (!s.blank || sel.Blank)
}

// pageSizes are the <page-size> keywords in points (CSS Page 3, 5.1,
// Table 1), portrait.
var pageSizes = map[string][2]float64{
	"a5": {148 * 72 / 25.4, 210 * 72 / 25.4}, "a4": {210 * 72 / 25.4, 297 * 72 / 25.4},
	"a3": {297 * 72 / 25.4, 420 * 72 / 25.4}, "b5": {176 * 72 / 25.4, 250 * 72 / 25.4},
	"b4": {250 * 72 / 25.4, 353 * 72 / 25.4}, "jis-b5": {182 * 72 / 25.4, 257 * 72 / 25.4},
	"jis-b4": {257 * 72 / 25.4, 364 * 72 / 25.4}, "letter": {8.5 * 72, 11 * 72},
	"legal": {8.5 * 72, 14 * 72}, "ledger": {11 * 72, 17 * 72},
}

// parsePageSize parses the size descriptor (CSS Page 3, 5.1): auto, one or
// two lengths, or a page size and an orientation in either order.
func parsePageSize(v []token) (pageSize, bool) {
	if k, ok := keyword(v); ok && k == "auto" {
		return pageSize{}, true
	}
	if len(v) < 1 || len(v) > 2 {
		return pageSize{}, false
	}
	lc := lenCtx{fontSize: mediumFontSize, rootFontSize: mediumFontSize}
	if v[0].typ != tIdent {
		var dims [2]float64
		for i := range dims {
			t := v[0]
			if len(v) == 2 {
				t = v[i]
			}
			l, ok := parseLength(t, nonNeg)
			if !ok || l.vw != 0 || l.vh != 0 || l.vmin != 0 || l.vmax != 0 {
				return pageSize{}, false
			}
			dims[i] = l.points(lc)
			if dims[i] <= 0 {
				return pageSize{}, false
			}
		}
		return pageSize{w: dims[0], h: dims[1]}, true
	}
	var size *[2]float64
	orient := OrientationAuto
	for _, t := range v {
		if t.typ != tIdent {
			return pageSize{}, false
		}
		k := lower(t.value)
		switch {
		case (k == "portrait" || k == "landscape") && orient == OrientationAuto:
			orient = Portrait
			if k == "landscape" {
				orient = Landscape
			}
		case size == nil:
			s, ok := pageSizes[k]
			if !ok {
				return pageSize{}, false
			}
			size = &s
		default:
			return pageSize{}, false
		}
	}
	if size == nil {
		return pageSize{orient: orient}, true
	}
	w, h := size[0], size[1]
	if orient == Landscape {
		w, h = h, w
	}
	return pageSize{w: w, h: h}, true
}

// pageDecls parses the descriptors of an @page block: size, the margin
// properties and the background properties. [pageCtxDecls] reads the rest.
func pageDecls(block []token, c *declCtx) []pageDecl {
	var out []pageDecl
	newListParser(block).declarations(func(it item) bool {
		if it.kind != itemDeclaration {
			return true
		}
		name := lower(it.name)
		switch name {
		case "size":
			s, ok := parsePageSize(significant(it.value))
			if !ok {
				c.warn(warn.CSSInvalid, warn.Approximated, "invalid value for @page size was ignored")
				return true
			}
			out = append(out, pageDecl{important: it.important, size: &s})
		case "margin", "margin-top", "margin-right", "margin-bottom", "margin-left":
			if containsVar(it.value) {
				c.warn(warn.CSSUnsupported, warn.Approximated, "@page %s: var() is not supported in page descriptors", name)
				return true
			}
			e, ok := c.expand(name, it.value)
			if !ok {
				c.warn(warn.CSSInvalid, warn.Approximated, "invalid value for @page %s was ignored", name)
				return true
			}
			out = append(out, pageDecl{important: it.important, margins: e})
		case "width", "height":
			d, ok := pageAreaSide(significant(it.value))
			if !ok {
				c.warn(warn.CSSInvalid, warn.Approximated, "invalid value for @page %s was ignored", name)
				return true
			}
			pd := pageDecl{important: it.important, areaW: &d}
			if name == "height" {
				pd.areaW, pd.areaH = nil, &d
			}
			out = append(out, pd)
		case "bleed", "marks":
			d, ok := pageMarkDecl(name, significant(it.value))
			if !ok {
				c.warn(warn.CSSInvalid, warn.Approximated, "invalid value for @page %s was ignored", name)
				return true
			}
			d.important = it.important
			out = append(out, d)
		case "background", "background-color", "background-image", "background-repeat",
			"background-position", "background-size", "background-origin", "background-clip",
			"background-attachment":
			// CSS Page 3 gives the page box the background properties.
			if containsVar(it.value) {
				c.warn(warn.CSSUnsupported, warn.Approximated, "@page %s: var() is not supported in page descriptors", name)
				return true
			}
			e, ok := c.expand(name, it.value)
			if !ok {
				c.warn(warn.CSSInvalid, warn.Approximated, "invalid value for @page %s was ignored", name)
				return true
			}
			out = append(out, pageDecl{important: it.important, background: e})
		}
		return true
	})
	return out
}

// Page returns the computed style of the page sel names: the @page rules
// that match it cascaded by origin, importance, page selector specificity
// and order (CSS Page 3, 4.2 and 3.3). Margin percentages stay Percent of
// the page size; em units resolve against medium, 16px. ctx's error stops
// it.
func (r *Resolver) Page(ctx context.Context, sel PageSelector) (PageStyle, error) {
	type cand struct {
		d    *pageDecl
		rank int
		spec uint32
		ord  int
	}
	var cands []cand
	for i := range r.pages {
		if i%pageRulesPerCheck == 0 {
			if err := ctx.Err(); err != nil {
				return PageStyle{}, err
			}
		}
		pr := &r.pages[i]
		var spec uint32
		matched := false
		for j := range pr.rule.sels {
			if s := &pr.rule.sels[j]; s.matches(sel) {
				matched = true
				spec = max(spec, s.spec)
			}
		}
		if !matched {
			continue
		}
		for j := range pr.rule.decls {
			d := &pr.rule.decls[j]
			cands = append(cands, cand{d: d, rank: rank(pr.origin, d.important), spec: spec, ord: pr.order})
		}
	}
	slices.SortStableFunc(cands, func(a, b cand) int {
		if c := cmp.Compare(a.rank, b.rank); c != 0 {
			return c
		}
		if c := cmp.Compare(a.spec, b.spec); c != 0 {
			return c
		}
		return cmp.Compare(a.ord, b.ord)
	})
	ps := PageStyle{Margin: [4]Dimension{{Kind: Auto}, {Kind: Auto}, {Kind: Auto}, {Kind: Auto}}}
	lc := lenCtx{fontSize: mediumFontSize, rootFontSize: mediumFontSize}
	for _, c := range cands {
		if c.d.size != nil {
			ps.Width, ps.Height, ps.Orientation = c.d.size.w, c.d.size.h, c.d.size.orient
		}
		for _, e := range c.d.margins {
			side := int(e.id) - int(pMarginTop)
			if side < 0 || side > 3 {
				continue
			}
			switch v := e.v.(type) {
			case dimSpec:
				switch v.kind { //nolint:exhaustive // the other kinds take the default
				case Points:
					ps.Margin[side] = v.l.dim(lc, false)
				default:
					ps.Margin[side] = Dimension{Kind: Auto}
				}
			case cssWide:
				ps.Margin[side] = Dimension{Kind: Auto}
			}
		}
		if c.d.areaW != nil {
			ps.AreaWidth = *c.d.areaW
		}
		if c.d.areaH != nil {
			ps.AreaHeight = *c.d.areaH
		}
		if c.d.bleed != nil {
			ps.Bleed = *c.d.bleed
		}
		if c.d.marks != nil {
			ps.Marks = *c.d.marks
		}
		for _, e := range c.d.background {
			if ps.Background == nil {
				bg := *initialStyle
				ps.Background = &bg
			}
			pageBackground(ps.Background, e, lc)
		}
	}
	return ps, ctx.Err()
}

// pageRulesPerCheck is how many @page rules [Resolver.Page] and
// [Resolver.PageIn] match between two checks of the context.
const pageRulesPerCheck = 1024

// pageBackground computes the background longhand e into st, a style
// whose other properties keep their initial values: the page box has no
// parent to inherit from, and currentColor is the initial colour, black.
func pageBackground(st *Style, e entry, lc lenCtx) {
	if (e.id < pBgColor || e.id > pBgClip) && e.id != pBgAttachment {
		return
	}
	def := &props[e.id]
	v := e.v
	if _, ok := v.(cssWide); ok {
		v = def.initial
		if v == nil {
			def.copyFrom(st, initialStyle)
			return
		}
	}
	if v != nil {
		def.compute(&styler{parent: initialStyle, lc: lc}, st, v)
	}
}
