package css

// PagePick selects which assignment of a named string or running element
// a page-margin box shows (CSS GCPM 3, 2.1.2).
type PagePick uint8

// The page picks of string() and element().
const (
	// PickFirst is the first assignment on the page, the entry value
	// when the page has none.
	PickFirst PagePick = iota
	// PickStart is the first assignment when the element starts the
	// page, the entry value otherwise.
	PickStart
	// PickLast is the page's exit value, its last assignment or the
	// entry value.
	PickLast
	// PickFirstExcept is PickFirst, empty on a page that assigns the
	// value.
	PickFirstExcept
	// PickAllOnce shows every element of the name the page assigns, each
	// on that page alone: the notes of a note area.
	PickAllOnce
)

// pagePicks maps the keywords of string() and element() to their pick.
var pagePicks = map[string]PagePick{"first": PickFirst, "start": PickStart, "last": PickLast, "first-except": PickFirstExcept, "all-once": PickAllOnce}

// NamedString is one assignment of string-set: the string's name and the
// content list its value is built from (CSS GCPM 3, 2.1.1).
type NamedString struct {
	Name  string
	Items []ContentItem
}

// FootnoteDisplay is the footnote-display property (CSS GCPM 3, 3.3).
type FootnoteDisplay uint8

// The footnote-display values.
const (
	// FootnoteBlock places the footnote body in the footnote area as a
	// block.
	FootnoteBlock FootnoteDisplay = iota
	// FootnoteInline places it as an inline box.
	FootnoteInline
	// FootnoteCompact places it inline when it fits on a line with the
	// next one, as a block otherwise.
	FootnoteCompact
)

// FootnotePolicy is the footnote-policy property (CSS GCPM 3, 3.8).
type FootnotePolicy uint8

// The footnote-policy values.
const (
	// PolicyAuto lets a footnote body that does not fit move to a later
	// page.
	PolicyAuto FootnotePolicy = iota
	// PolicyLine moves the line holding the call to the next page with
	// the body.
	PolicyLine
	// PolicyBlock moves the paragraph holding the call.
	PolicyBlock
)

// Marks is the marks descriptor (CSS Page 3, 7.1): the page marks drawn
// in the bleed area's surroundings, a bit set.
type Marks uint8

// The page marks.
const (
	// MarksCrop draws crop marks at the trim's corners.
	MarksCrop Marks = 1 << iota
	// MarksCross draws registration crosses at the middle of each side.
	MarksCross
)

// autoBleed is the used bleed of bleed: auto with crop marks (CSS Page 3,
// 7.2): 6pt.
const autoBleed = 6

// UsedBleed returns the used bleed of ps in points (CSS Page 3, 7.2): its
// length, else 6pt when ps draws crop marks and 0 otherwise.
func (ps *PageStyle) UsedBleed() float64 {
	if ps.Bleed.Kind == Points {
		return ps.Bleed.Value
	}
	if ps.Marks&MarksCrop != 0 {
		return autoBleed
	}
	return 0
}

// leaderPatterns are the leader() keywords (CSS Generated Content 3,
// leader()), as WeasyPrint draws them: dotted repeats a full stop alone,
// the leader drawing of WeasyPrint's draw tests, which litho follows
// where Chromium draws no leader.
var leaderPatterns = map[string]string{"dotted": ".", "solid": "_", "space": " "}

// gcpmItem parses the content items of CSS GCPM 3 and the leader() of CSS
// Generated Content 3: leader(), string() and element(). ok reports a
// function it knows; valid whether its arguments parse.
func gcpmItem(name string, args [][]token) (it ContentItem, ok, valid bool) {
	switch name {
	case "leader":
		if len(args) != 1 || len(args[0]) != 1 {
			return ContentItem{}, true, false
		}
		t := args[0][0]
		switch t.typ { //nolint:exhaustive // the other token types are invalid here
		case tString:
			return ContentItem{Kind: ItemLeader, Text: t.value}, true, true
		case tIdent:
			p, known := leaderPatterns[lower(t.value)]
			return ContentItem{Kind: ItemLeader, Text: p}, true, known
		}
		return ContentItem{}, true, false
	case "string", "element":
		if len(args) < 1 || len(args) > 2 || len(args[0]) != 1 || args[0][0].typ != tIdent {
			return ContentItem{}, true, false
		}
		it := ContentItem{Kind: ItemNamedString, Text: args[0][0].value}
		if name == "element" {
			it.Kind = ItemElement
		}
		if len(args) == 2 {
			k, isKw := keyword(args[1])
			p, known := pagePicks[k]
			if !isKw || !known {
				return ContentItem{}, true, false
			}
			it.Pick = p
		}
		return it, true, true
	}
	return ContentItem{}, false, false
}

// contentParts are the arguments of content() (CSS GCPM 3, 2.1.1.1).
var contentParts = map[string]bool{"text": true, "before": true, "after": true, "first-letter": true, "marker": true}

// maxStringSets bounds the named strings one string-set value assigns:
// 32. Anti-DoS guard, not spec-mandated: a heading sets a string or two,
// and each one costs a text extraction of its element.
const maxStringSets = 32

// parseStringSet parses string-set (CSS GCPM 3, 2.1.1): none, or a comma
// separated list of a name followed by a content list of strings,
// counter(), counters(), content() and attr().
func parseStringSet(v []token, c *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return []NamedString(nil), true
	}
	parts := splitCommas(v)
	if len(parts) > maxStringSets {
		return nil, false
	}
	var out []NamedString
	for _, part := range parts {
		part = significant(part)
		if len(part) < 2 || part[0].typ != tIdent || part[0].isIdent("none") || len(part) > maxContentItems+1 {
			return nil, false
		}
		ns := NamedString{Name: part[0].value}
		for _, t := range part[1:] {
			if t.typ == tFunction && eqFold(t.value, "content") {
				a := significant(t.children)
				k := "text"
				if len(a) > 0 {
					kw, ok := keyword(a)
					if !ok || !contentParts[kw] {
						return nil, false
					}
					k = kw
				}
				ns.Items = append(ns.Items, ContentItem{Kind: ItemContent, Text: k})
				continue
			}
			it, ok := contentItem(t, c)
			if !ok || it.Kind == ItemImage || it.Kind == ItemElement || it.Kind == ItemNamedString {
				return nil, false
			}
			ns.Items = append(ns.Items, it)
		}
		out = append(out, ns)
	}
	return out, true
}

// running is the specified value position: running(name).
type running struct{ name string }

// note is the specified value position: note(name).
type note struct{ name string }

// footnoteFloat is the specified value float: footnote.
type footnoteFloat struct{}

// pageMarkDecl parses the bleed or marks descriptor of an @page rule (CSS
// Page 3, 7.1 and 7.2): bleed takes auto or a length, marks none or crop
// and cross in any order.
func pageMarkDecl(name string, v []token) (pageDecl, bool) {
	if name == "bleed" {
		if k, ok := keyword(v); ok && k == "auto" {
			return pageDecl{bleed: &Dimension{Kind: Auto}}, true
		}
		t, ok := single(v)
		if !ok {
			return pageDecl{}, false
		}
		l, ok := parseLength(t, nonNeg)
		if !ok || l.vw != 0 || l.vh != 0 || l.vmin != 0 || l.vmax != 0 {
			return pageDecl{}, false
		}
		d := Pts(l.points(lenCtx{fontSize: mediumFontSize, rootFontSize: mediumFontSize}))
		return pageDecl{bleed: &d}, true
	}
	var m Marks
	if k, ok := keyword(v); ok && k == "none" {
		return pageDecl{marks: &m}, true
	}
	if len(v) == 0 || len(v) > 2 {
		return pageDecl{}, false
	}
	for _, t := range v {
		var bit Marks
		switch {
		case t.isIdent("crop"):
			bit = MarksCrop
		case t.isIdent("cross"):
			bit = MarksCross
		}
		if bit == 0 || m&bit != 0 {
			return pageDecl{}, false
		}
		m |= bit
	}
	return pageDecl{marks: &m}, true
}

// pageAreaSide parses the width or height of an @page rule: auto or a
// length, em resolving against medium as for the other page descriptors.
func pageAreaSide(v []token) (Dimension, bool) {
	if k, ok := keyword(v); ok && k == "auto" {
		return Dimension{Kind: Auto}, true
	}
	t, ok := single(v)
	if !ok {
		return Dimension{}, false
	}
	l, ok := parseLength(t, nonNeg)
	if !ok || l.vw != 0 || l.vh != 0 || l.vmin != 0 || l.vmax != 0 {
		return Dimension{}, false
	}
	return Pts(l.points(lenCtx{fontSize: mediumFontSize, rootFontSize: mediumFontSize})), true
}

// gcpmProps defines the properties of CSS GCPM 3 and extends position and
// float with their paged values: running() and footnote. It runs once the
// other longhands are defined.
func gcpmProps() {
	props[pStringSet] = propDef{
		name: "string-set", parse: parseStringSet,
		compute: func(_ *styler, st *Style, v any) {
			if s, ok := v.([]NamedString); ok {
				st.StringSet = s
			}
		},
		copyFrom: field(func(s *Style) *[]NamedString { return &s.StringSet }),
	}
	props[pFootnoteDisplay] = enumProp("footnote-display", false, func(s *Style) *FootnoteDisplay { return &s.FootnoteDisplay }, map[string]FootnoteDisplay{
		"block": FootnoteBlock, "inline": FootnoteInline, "compact": FootnoteCompact,
	})
	props[pFootnotePolicy] = enumProp("footnote-policy", false, func(s *Style) *FootnotePolicy { return &s.FootnotePolicy }, map[string]FootnotePolicy{
		"auto": PolicyAuto, "line": PolicyLine, "block": PolicyBlock,
	})
	pos := props[pPosition]
	props[pPosition] = propDef{
		name: pos.name, inherited: pos.inherited, initial: pos.initial,
		parse: func(v []token, c *declCtx) (any, bool) {
			if t, ok := single(v); ok && t.typ == tFunction && (eqFold(t.value, "running") || eqFold(t.value, "note")) {
				a := significant(t.children)
				if len(a) != 1 || a[0].typ != tIdent {
					return nil, false
				}
				if eqFold(t.value, "note") {
					return note{name: a[0].value}, true
				}
				return running{name: a[0].value}, true
			}
			return pos.parse(v, c)
		},
		compute: func(s *styler, st *Style, v any) {
			st.Running, st.Note = "", ""
			switch r := v.(type) {
			case running:
				st.Position, st.Running = Static, r.name
				return
			case note:
				st.Position, st.Note = Static, r.name
				return
			}
			pos.compute(s, st, v)
		},
		copyFrom: func(dst, src *Style) { pos.copyFrom(dst, src); dst.Running, dst.Note = src.Running, src.Note },
	}
	fl := props[pFloat]
	props[pFloat] = propDef{
		name: fl.name, inherited: fl.inherited, initial: fl.initial,
		parse: func(v []token, c *declCtx) (any, bool) {
			if k, ok := keyword(v); ok && k == "footnote" {
				return footnoteFloat{}, true
			}
			return fl.parse(v, c)
		},
		compute: func(s *styler, st *Style, v any) {
			if _, ok := v.(footnoteFloat); ok {
				st.Float, st.Footnote = FloatNone, true
				return
			}
			st.Footnote = false
			fl.compute(s, st, v)
		},
		copyFrom: func(dst, src *Style) { fl.copyFrom(dst, src); dst.Footnote = src.Footnote },
	}
}
