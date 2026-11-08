package css

import (
	"slices"
	"sync"
)

// MarginBox names one of the sixteen page-margin boxes (CSS Page 3, 5),
// clockwise from the top-left corner.
type MarginBox uint8

// The page-margin boxes (CSS Page 3, 5, Table 1).
const (
	MarginTopLeftCorner MarginBox = iota
	MarginTopLeft
	MarginTopCenter
	MarginTopRight
	MarginTopRightCorner
	MarginRightTop
	MarginRightMiddle
	MarginRightBottom
	MarginBottomRightCorner
	MarginBottomRight
	MarginBottomCenter
	MarginBottomLeft
	MarginBottomLeftCorner
	MarginLeftBottom
	MarginLeftMiddle
	MarginLeftTop
	// NumMarginBoxes counts the page-margin boxes.
	NumMarginBoxes
)

// marginBoxNames are the at-rule names of the page-margin boxes (CSS Page
// 3, 5.1), indexed by [MarginBox].
var marginBoxNames = [NumMarginBoxes]string{
	"top-left-corner", "top-left", "top-center", "top-right", "top-right-corner",
	"right-top", "right-middle", "right-bottom",
	"bottom-right-corner", "bottom-right", "bottom-center", "bottom-left", "bottom-left-corner",
	"left-bottom", "left-middle", "left-top",
}

// String returns the at-rule name of m without its @.
func (m MarginBox) String() string {
	if m < NumMarginBoxes {
		return marginBoxNames[m]
	}
	return "margin-box"
}

// marginBoxByName maps an at-rule name to its box.
var marginBoxByName = func() map[string]MarginBox {
	out := make(map[string]MarginBox, NumMarginBoxes)
	for i, n := range marginBoxNames {
		out[n] = MarginBox(i)
	}
	return out
}()

// marginDefaults are the text-align and vertical-align each page-margin
// box takes from the user agent (CSS Page 3, 5.1 in the editor's draft,
// the page-margin box defaults; WeasyPrint's and Chromium's user agent
// sheets): the horizontal boxes align their content toward the page
// area's corners and centre it vertically, the vertical boxes centre it
// horizontally and align it toward the corners.
var marginDefaults = [NumMarginBoxes][2]string{
	MarginTopLeftCorner: {"right", "middle"}, MarginTopLeft: {"left", "middle"}, MarginTopCenter: {"center", "middle"},
	MarginTopRight: {"right", "middle"}, MarginTopRightCorner: {"left", "middle"},
	MarginRightTop: {"center", "top"}, MarginRightMiddle: {"center", "middle"}, MarginRightBottom: {"center", "bottom"},
	MarginBottomRightCorner: {"left", "middle"}, MarginBottomRight: {"right", "middle"}, MarginBottomCenter: {"center", "middle"},
	MarginBottomLeft: {"left", "middle"}, MarginBottomLeftCorner: {"right", "middle"},
	MarginLeftBottom: {"center", "bottom"}, MarginLeftMiddle: {"center", "middle"}, MarginLeftTop: {"center", "top"},
}

// marginDefaultDecls returns [marginDefaults] as user agent declarations,
// parsed once the longhands are defined.
var marginDefaultDecls = sync.OnceValue(func() *[NumMarginBoxes][2]Declaration {
	var out [NumMarginBoxes][2]Declaration
	for i, d := range marginDefaults {
		out[i] = [2]Declaration{Hint("text-align", d[0]), Hint("vertical-align", d[1])}
	}
	return &out
})

// flexOrGridContainer reports whether the children of s are flex or grid
// items: s is a flex container, or a flex or grid container laid out as a
// block in this version.
func (s *Style) flexOrGridContainer() bool {
	return s.flexOrGrid || isFlexContainer(s.Display) || isGridContainer(s.Display)
}

// laidAs is a flex or grid display value laid out as d; webkitBox
// records the legacy -webkit-box and -webkit-inline-box, whose
// -webkit-line-clamp Chromium honours (CSS Overflow 4, 6.5).
type laidAs struct {
	d         Display
	webkitBox bool
}

// flexOrGrid are the single display keywords of flex and grid containers.
var flexOrGrid = map[string]bool{
	"flex": true, "grid": true, "inline-flex": true, "inline-grid": true, "-webkit-box": true,
	"-webkit-flex": true, "-ms-flexbox": true, "-webkit-inline-box": true, "-webkit-inline-flex": true,
	"-ms-inline-flexbox": true,
}

// footnoteArea is the [marginRule] box of an @footnote rule, and
// noteArea that of an @note-area rule.
const (
	footnoteArea = NumMarginBoxes
	noteArea     = NumMarginBoxes + 1
)

// marginRule is a margin at-rule of an @page rule (CSS Page 3, 5.1), or
// an @footnote rule (CSS GCPM 3, 3.4).
type marginRule struct {
	box   MarginBox
	decls []Declaration
}

// pageProp defines the page property (CSS Page 3, 4.1): auto or a
// case-sensitive page type name.
func pageProp() propDef {
	return propDef{
		name: "page",
		parse: func(v []token, _ *declCtx) (any, bool) {
			t, ok := single(v)
			if !ok || t.typ != tIdent {
				return nil, false
			}
			if lower(t.value) == "auto" {
				return "", true
			}
			return t.value, true
		},
		compute: func(s *styler, st *Style, v any) {
			// The page property applies to the boxes that create class A
			// break points: not to flex or grid items (CSS Page 3, 4.1).
			if name, ok := v.(string); ok && (s.parent == nil || !s.parent.flexOrGridContainer()) {
				st.Page = name
			}
		},
		copyFrom: field(func(s *Style) *string { return &s.Page }),
	}
}

// pageCtxDecls parses the declarations of an @page block that style the
// page context (CSS Page 3, 4.4), every property but the descriptors
// [pageDecls] reads, and its margin at-rules.
func pageCtxDecls(block []token, c *declCtx) ([]Declaration, []marginRule) {
	var ctx []Declaration
	var boxes []marginRule
	newListParser(block).declarations(func(it item) bool {
		switch it.kind {
		case itemDeclaration:
			if pageDescriptor(lower(it.name)) {
				return true
			}
			if d := c.declare(it.name, it.value, it.important); d.declares() {
				ctx = append(ctx, d)
			}
		case itemAtRule:
			b, ok := marginBoxByName[lower(it.name)]
			if lower(it.name) == "footnote" {
				// The footnote area (CSS GCPM 3, 3.4) cascades as a
				// page-margin box does.
				b, ok = footnoteArea, true
			}
			if lower(it.name) == "note-area" {
				// The note area cascades as a page-margin box does.
				b, ok = noteArea, true
			}
			if !ok || !it.hasBlock {
				c.warn("unsupported-css-at-rule", "the @%s rule inside @page is not supported and was ignored", lower(it.name))
				return true
			}
			boxes = append(boxes, marginRule{box: b, decls: parseDeclList(it.block, c)})
		case itemQualified, itemError:
		}
		return true
	})
	return ctx, boxes
}

// pageDescriptor reports a declaration [pageDecls] reads as a page box
// descriptor rather than a page context property.
func pageDescriptor(name string) bool {
	switch name {
	case "size", "margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
		"background", "background-color", "background-image", "background-repeat",
		"background-position", "background-size", "background-origin", "background-clip",
		"background-attachment", "bleed", "marks", "width", "height":
		return true
	}
	return false
}

// HasNamedPages reports whether an @page rule selects named or blank
// pages, which only a layout that tracks page names can match.
func (r *Resolver) HasNamedPages() bool {
	for i := range r.pages {
		for _, s := range r.pages[i].rule.sels {
			if s.name != "" || s.blank {
				return true
			}
		}
	}
	return false
}

// HasMarginBoxes reports whether an @page rule holds a margin at-rule.
func (r *Resolver) HasMarginBoxes() bool {
	for i := range r.pages {
		for _, m := range r.pages[i].rule.margins {
			if m.box != footnoteArea && m.box != noteArea {
				return true
			}
		}
	}
	return false
}

// HasFootnoteArea reports whether an @page rule holds an @footnote rule
// (CSS GCPM 3, 3.4).
func (r *Resolver) HasFootnoteArea() bool {
	for i := range r.pages {
		for _, m := range r.pages[i].rule.margins {
			if m.box == footnoteArea {
				return true
			}
		}
	}
	return false
}

// HasNoteArea reports whether an @page rule holds an @note-area rule.
func (r *Resolver) HasNoteArea() bool {
	for i := range r.pages {
		for _, m := range r.pages[i].rule.margins {
			if m.box == noteArea {
				return true
			}
		}
	}
	return false
}

// hasPageAreas reports whether an @page rule holds a margin at-rule, an
// @footnote rule or an @note-area rule.
func (r *Resolver) hasPageAreas() bool {
	for i := range r.pages {
		if len(r.pages[i].rule.margins) > 0 {
			return true
		}
	}
	return false
}

// PageIn is [Resolver.Page] with the page-margin boxes: the page context
// inherits from root, the root element's computed style (nil for the
// initial style), and each page-margin box from the page context (CSS Page
// 3, 4.4 and 5). [PageStyle.MarginBoxes] holds the boxes whose content
// computes to a list: content normal computes to none on a page-margin box
// and generates nothing (CSS Page 3, 5.2).
func (r *Resolver) PageIn(sel PageSelector, root *Style) PageStyle {
	ps := r.Page(sel)
	if !r.hasPageAreas() {
		return ps
	}
	type src struct {
		rule *resolvedPage
		spec uint32
	}
	var srcs []src
	for i := range r.pages {
		pr := &r.pages[i]
		var spec uint32
		matched := false
		for j := range pr.rule.sels {
			if s := &pr.rule.sels[j]; s.matches(sel) {
				matched = true
				spec = max(spec, s.spec)
			}
		}
		if matched {
			srcs = append(srcs, src{rule: pr, spec: spec})
		}
	}
	if root == nil {
		root = initialStyle
	}
	cs := &computeState{r: r, root: root}
	var ctxCands []cand
	var boxCands [NumMarginBoxes + 2][]cand
	for _, s := range srcs {
		for i := range s.rule.rule.context {
			d := &s.rule.rule.context[i]
			ctxCands = append(ctxCands, cand{d: d, rank: rank(s.rule.origin, d.Important), spec: uint64(s.spec), ord: s.rule.order, idx: i})
		}
		for j := range s.rule.rule.margins {
			m := &s.rule.rule.margins[j]
			for i := range m.decls {
				d := &m.decls[i]
				boxCands[m.box] = append(boxCands[m.box], cand{d: d, rank: rank(s.rule.origin, d.Important), spec: uint64(s.spec), ord: s.rule.order, idx: j<<16 | i})
			}
		}
	}
	pageCtx, vars := cs.cascade(ctxCands, root, nil, false)
	if fc := boxCands[footnoteArea]; len(fc) > 0 {
		st, _ := cs.cascade(slices.Clone(fc), pageCtx, vars, false)
		st.Display, st.Float, st.Position = Block, FloatNone, Static
		ps.Footnote = st
	}
	if nc := boxCands[noteArea]; len(nc) > 0 {
		// The note area keeps its display, float and position: they
		// place it on the page.
		ps.NoteArea, _ = cs.cascade(slices.Clone(nc), pageCtx, vars, false)
	}
	for b := range NumMarginBoxes {
		if len(boxCands[b]) == 0 {
			continue
		}
		cands := slices.Clone(boxCands[b])
		defaults := &marginDefaultDecls()[b]
		for i := range defaults {
			cands = append(cands, cand{d: &defaults[i], rank: rank(UserAgent, false), ord: -1, idx: i})
		}
		st, _ := cs.cascade(cands, pageCtx, vars, false)
		if st.Content.Kind != ContentList {
			continue
		}
		// The display property does not apply to page-margin boxes (CSS
		// Page 3, 5.2): each is a block container.
		st.Display, st.Float, st.Position = Block, FloatNone, Static
		if ps.MarginBoxes == nil {
			ps.MarginBoxes = map[MarginBox]*Style{}
		}
		ps.MarginBoxes[b] = st
	}
	return ps
}
