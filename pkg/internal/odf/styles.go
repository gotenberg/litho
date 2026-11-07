package odf

import (
	"strconv"
	"strings"
)

// Scope says which document's automatic styles a style name resolves in
// (ODF 1.3, Part 3, 16.4): content.xml's for the body, styles.xml's for
// the master pages, their headers and footers. A flat document has one.
type Scope uint8

// The scopes.
const (
	ScopeContent Scope = iota
	ScopeStyles
)

// The style families of ODF 1.3, Part 3, 19.480 the converters resolve.
const (
	FamilyParagraph    = "paragraph"
	FamilyText         = "text"
	FamilySection      = "section"
	FamilyTable        = "table"
	FamilyColumn       = "table-column"
	FamilyRow          = "table-row"
	FamilyCell         = "table-cell"
	FamilyGraphic      = "graphic"
	FamilyPresentation = "presentation"
	FamilyDrawingPage  = "drawing-page"
	FamilyRuby         = "ruby"
)

// The property elements of ODF 1.3, Part 3, 17, by their key.
const (
	TextProps         = "style:text-properties"
	ParagraphProps    = "style:paragraph-properties"
	SectionProps      = "style:section-properties"
	TableProps        = "style:table-properties"
	ColumnProps       = "style:table-column-properties"
	RowProps          = "style:table-row-properties"
	CellProps         = "style:table-cell-properties"
	GraphicProps      = "style:graphic-properties"
	PageLayoutProps   = "style:page-layout-properties"
	HeaderFooterProps = "style:header-footer-properties"
	ListLevelProps    = "style:list-level-properties"
	DrawingPageProps  = "style:drawing-page-properties"
)

// maxStyleChain bounds the parent chain resolved for one style: 64.
// Anti-DoS guard, not spec-mandated: a cycle is refused by the visited
// set, and real style hierarchies are under ten deep.
const maxStyleChain = 64

// Props is the merged attributes of one kind of property element along a
// style's chain, keyed by conventional prefix and local name
// ("fo:margin-left", see [Element]), and its child elements by the same
// key, the nearest style's winning. The zero value holds nothing. A
// Props is read-only once resolved.
type Props struct {
	attrs map[string]string
	kids  map[string]*Element
}

// Get returns the value of the property key, "" when unset.
func (p Props) Get(key string) string { return p.attrs[key] }

// Lookup returns the value of the property key and whether it is set.
func (p Props) Lookup(key string) (string, bool) {
	v, ok := p.attrs[key]
	return v, ok
}

// Child returns the child element key of the nearest property element
// along the chain that has one, nil when none.
func (p Props) Child(key string) *Element { return p.kids[key] }

// Empty reports Props that hold nothing.
func (p Props) Empty() bool { return len(p.attrs) == 0 && len(p.kids) == 0 }

// merge lays the attributes and children of the property element el over
// p, returning the result; p's maps are copied, never written.
func (p Props) merge(el *Element) Props {
	if el == nil {
		return p
	}
	out := Props{attrs: make(map[string]string, len(p.attrs)+len(el.Attr)), kids: make(map[string]*Element, len(p.kids))}
	for k, v := range p.attrs {
		out.attrs[k] = v
	}
	for k, v := range p.kids {
		out.kids[k] = v
	}
	for _, a := range el.Attr {
		out.attrs[qname(a.Name)] = a.Value
	}
	for _, c := range el.Children {
		if !c.IsText() {
			out.kids[qname(c.Name)] = c
		}
	}
	return out
}

// Over returns p with q's properties laid over it, q winning: the
// inheritance of text properties from a paragraph into its spans.
func (p Props) Over(q Props) Props {
	if q.Empty() {
		return p
	}
	if p.Empty() {
		return q
	}
	out := Props{attrs: make(map[string]string, len(p.attrs)+len(q.attrs)), kids: map[string]*Element{}}
	for _, m := range []Props{p, q} {
		for k, v := range m.attrs {
			out.attrs[k] = v
		}
		for k, v := range m.kids {
			out.kids[k] = v
		}
	}
	return out
}

// PropsOf returns the attributes and children of the one property element
// el, for elements that carry properties outside a style.
func PropsOf(el *Element) Props { return Props{}.merge(el) }

// Style is one style:style of a family (ODF 1.3, Part 3, 16.2).
type Style struct {
	Name, DisplayName, Family string
	// Parent is style:parent-style-name, Next style:next-style-name,
	// ListStyle style:list-style-name, MasterPage style:master-page-name
	// (present but empty asks for no page break), Class style:class.
	Parent, Next, ListStyle, MasterPage, Class string
	// HasMasterPage reports a style:master-page-name attribute.
	HasMasterPage bool
	// OutlineLevel is style:default-outline-level, 0 for none.
	OutlineLevel int
	// Auto reports an automatic style.
	Auto bool
	// El is the style:style element.
	El *Element
}

// FontFace is one style:font-face declaration (ODF 1.3, Part 3, 16.23).
type FontFace struct {
	Name string
	// Family is the first family of svg:font-family, unquoted; the
	// declaration's name when it has none.
	Family string
	// Generic is style:font-family-generic: roman, swiss, modern,
	// decorative, script or system.
	Generic string
	// Pitch is style:font-pitch: fixed or variable.
	Pitch string
}

// ListLevel is one level of a list style (ODF 1.3, Part 3, 16.31 to
// 16.33): a text:list-level-style-number, -bullet or -image, or a
// text:outline-level-style.
type ListLevel struct {
	// Kind is "number", "bullet", "image" or "outline".
	Kind string
	El   *Element
	// Props merges the style:list-level-properties and its
	// style:list-level-label-alignment child's attributes.
	Props Props
	// Text is the level's style:text-properties.
	Text Props
}

// MaxListLevels is the levels a list style has (ODF 1.3, Part 3,
// 19.840, text:level: 1 to 10).
const MaxListLevels = 10

// ListStyle is a text:list-style or the text:outline-style.
type ListStyle struct {
	Name string
	// Levels is indexed by level less one; a level the style leaves out
	// is nil.
	Levels [MaxListLevels]*ListLevel
	// Consecutive is text:consecutive-numbering.
	Consecutive bool
}

// PageLayout is a style:page-layout (ODF 1.3, Part 3, 16.5): the page's
// properties and its header's and footer's.
type PageLayout struct {
	Name           string
	Page           Props
	Header, Footer Props
	// PageUsage is style:page-usage: all, left, right or mirrored.
	PageUsage string
}

// MasterPage is a style:master-page (ODF 1.3, Part 3, 16.9).
type MasterPage struct {
	Name, DisplayName, PageLayout, Next string
	// Header, HeaderLeft, HeaderFirst and their Footer counterparts are
	// style:header, style:header-left, style:header-first and the footer
	// elements, nil when absent; El is the master page.
	Header, HeaderLeft, HeaderFirst *Element
	Footer, FooterLeft, FooterFirst *Element
	El                              *Element
}

// Styles indexes the styles of a document: its font faces, default
// styles, common and automatic styles of both scopes, list styles, page
// layouts, master pages and notes configurations. It is read-only once
// built and safe for concurrent reads.
type Styles struct {
	Fonts       map[string]FontFace
	defaults    map[string]*Element
	common      map[string]*Style
	auto        [2]map[string]*Style
	lists       map[string]*ListStyle
	autoLists   [2]map[string]*ListStyle
	PageLayouts map[string]*PageLayout
	// Masters are the master pages in document order; Master looks one up.
	Masters []*MasterPage
	masters map[string]*MasterPage
	// Outline is the text:outline-style, nil when none.
	Outline *ListStyle
	// Notes maps a note class, footnote or endnote, to its
	// text:notes-configuration.
	Notes map[string]*Element
}

// styleKey keys a style by family and name.
func styleKey(family, name string) string { return family + "\x00" + name }

// NewStyles indexes the styles of p.
func NewStyles(p *Package) *Styles {
	s := &Styles{
		Fonts: map[string]FontFace{}, defaults: map[string]*Element{},
		common: map[string]*Style{}, lists: map[string]*ListStyle{},
		PageLayouts: map[string]*PageLayout{}, masters: map[string]*MasterPage{},
		Notes: map[string]*Element{},
	}
	for i := range s.auto {
		s.auto[i] = map[string]*Style{}
		s.autoLists[i] = map[string]*ListStyle{}
	}
	parts := []struct {
		el    *Element
		scope Scope
	}{{p.Styles, ScopeStyles}, {p.Content, ScopeContent}}
	if p.Flat {
		parts = parts[:1]
	}
	for _, part := range parts {
		if part.el == nil {
			continue
		}
		for _, ff := range Section(part.el, "font-face-decls").All(NSStyle, "font-face") {
			s.addFont(ff)
		}
		s.addSection(Section(part.el, "styles"), false, part.scope)
		s.addSection(Section(part.el, "automatic-styles"), true, part.scope)
		for _, mp := range Section(part.el, "master-styles").All(NSStyle, "master-page") {
			s.addMaster(mp)
		}
	}
	if p.Flat {
		// A flat document's automatic styles serve both scopes.
		s.auto[ScopeContent] = s.auto[ScopeStyles]
		s.autoLists[ScopeContent] = s.autoLists[ScopeStyles]
	}
	return s
}

// addFont records a style:font-face.
func (s *Styles) addFont(ff *Element) {
	name := ff.A(NSStyle, "name")
	if name == "" {
		return
	}
	fam := Unquote(ff.A(NSSVG, "font-family"))
	if fam == "" {
		fam = name
	}
	s.Fonts[name] = FontFace{Name: name, Family: fam, Generic: ff.A(NSStyle, "font-family-generic"), Pitch: ff.A(NSStyle, "font-pitch")}
}

// addSection records the styles of an office:styles or
// office:automatic-styles element.
func (s *Styles) addSection(sec *Element, auto bool, scope Scope) {
	if sec == nil {
		return
	}
	for _, el := range sec.Children {
		switch {
		case el.Is(NSStyle, "style"):
			st := &Style{
				Name: el.A(NSStyle, "name"), DisplayName: el.A(NSStyle, "display-name"),
				Family: el.A(NSStyle, "family"), Parent: el.A(NSStyle, "parent-style-name"),
				Next: el.A(NSStyle, "next-style-name"), ListStyle: el.A(NSStyle, "list-style-name"),
				Class: el.A(NSStyle, "class"), Auto: auto, El: el,
			}
			st.MasterPage, st.HasMasterPage = el.Lookup(NSStyle, "master-page-name")
			if lv, err := strconv.Atoi(el.A(NSStyle, "default-outline-level")); err == nil && lv > 0 {
				st.OutlineLevel = min(lv, MaxListLevels)
			}
			if st.Name == "" {
				continue
			}
			m := s.common
			if auto {
				m = s.auto[scope]
			}
			if _, dup := m[styleKey(st.Family, st.Name)]; !dup {
				m[styleKey(st.Family, st.Name)] = st
			}
		case el.Is(NSStyle, "default-style"):
			if f := el.A(NSStyle, "family"); f != "" && s.defaults[f] == nil {
				s.defaults[f] = el
			}
		case el.Is(NSText, "list-style"):
			ls := parseList(el)
			m := s.lists
			if auto {
				m = s.autoLists[scope]
			}
			if _, dup := m[ls.Name]; !dup && ls.Name != "" {
				m[ls.Name] = ls
			}
		case el.Is(NSText, "outline-style"):
			if s.Outline == nil {
				s.Outline = parseList(el)
			}
		case el.Is(NSStyle, "page-layout"):
			pl := &PageLayout{Name: el.A(NSStyle, "name"), PageUsage: el.A(NSStyle, "page-usage")}
			pl.Page = PropsOf(el.Child(NSStyle, "page-layout-properties"))
			pl.Header = PropsOf(el.Child(NSStyle, "header-style").Child(NSStyle, "header-footer-properties"))
			pl.Footer = PropsOf(el.Child(NSStyle, "footer-style").Child(NSStyle, "header-footer-properties"))
			if _, dup := s.PageLayouts[pl.Name]; !dup && pl.Name != "" {
				s.PageLayouts[pl.Name] = pl
			}
		case el.Is(NSText, "notes-configuration"):
			class := el.A(NSText, "note-class")
			if class == "" {
				class = "footnote"
			}
			if s.Notes[class] == nil {
				s.Notes[class] = el
			}
		}
	}
}

// parseList reads a text:list-style or text:outline-style (ODF 1.3, Part
// 3, 16.30 and 16.34).
func parseList(el *Element) *ListStyle {
	ls := &ListStyle{Name: el.A(NSStyle, "name"), Consecutive: el.A(NSText, "consecutive-numbering") == "true"}
	for _, lv := range el.Children {
		var kind string
		switch {
		case lv.Is(NSText, "list-level-style-number"):
			kind = "number"
		case lv.Is(NSText, "list-level-style-bullet"):
			kind = "bullet"
		case lv.Is(NSText, "list-level-style-image"):
			kind = "image"
		case lv.Is(NSText, "outline-level-style"):
			kind = "outline"
		default:
			continue
		}
		n, err := strconv.Atoi(lv.A(NSText, "level"))
		if err != nil || n < 1 || n > MaxListLevels || ls.Levels[n-1] != nil {
			continue
		}
		lp := lv.Child(NSStyle, "list-level-properties")
		props := PropsOf(lp).merge(lp.Child(NSStyle, "list-level-label-alignment"))
		ls.Levels[n-1] = &ListLevel{Kind: kind, El: lv, Props: props, Text: PropsOf(lv.Child(NSStyle, "text-properties"))}
	}
	return ls
}

// addMaster records a style:master-page.
func (s *Styles) addMaster(el *Element) {
	mp := &MasterPage{
		Name: el.A(NSStyle, "name"), DisplayName: el.A(NSStyle, "display-name"),
		PageLayout: el.A(NSStyle, "page-layout-name"), Next: el.A(NSStyle, "next-style-name"),
		Header: el.Child(NSStyle, "header"), HeaderLeft: el.Child(NSStyle, "header-left"),
		HeaderFirst: el.Child(NSStyle, "header-first"),
		Footer:      el.Child(NSStyle, "footer"), FooterLeft: el.Child(NSStyle, "footer-left"),
		FooterFirst: el.Child(NSStyle, "footer-first"), El: el,
	}
	if mp.Name == "" || s.masters[mp.Name] != nil {
		return
	}
	s.masters[mp.Name] = mp
	s.Masters = append(s.Masters, mp)
}

// Master returns the master page name, nil when none.
func (s *Styles) Master(name string) *MasterPage { return s.masters[name] }

// Style returns the style family:name as scope sees it: its automatic
// style first, else the common one; nil when neither exists.
func (s *Styles) Style(scope Scope, family, name string) *Style {
	if st := s.auto[scope][styleKey(family, name)]; st != nil {
		return st
	}
	return s.common[styleKey(family, name)]
}

// List returns the list style name as scope sees it, nil when none.
func (s *Styles) List(scope Scope, name string) *ListStyle {
	if ls := s.autoLists[scope][name]; ls != nil {
		return ls
	}
	return s.lists[name]
}

// Chain returns the style family:name and its ancestors, the farthest
// first, as scope sees them (ODF 1.3, Part 3, 16.2: an automatic style's
// parent is a common style). A cycle or a chain past [maxStyleChain]
// stops where it repeats.
func (s *Styles) Chain(scope Scope, family, name string) []*Style {
	var chain []*Style
	seen := map[*Style]bool{}
	st := s.Style(scope, family, name)
	for st != nil && !seen[st] && len(chain) < maxStyleChain {
		seen[st] = true
		chain = append(chain, st)
		if st.Parent == "" {
			break
		}
		if st.Auto {
			st = s.common[styleKey(family, st.Parent)]
			if st == nil {
				st = s.Style(scope, family, chain[len(chain)-1].Parent)
			}
			continue
		}
		st = s.common[styleKey(family, st.Parent)]
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// Resolve returns the properties of kind (a property element key such as
// [ParagraphProps]) of the style family:name: the family's default style
// (16.3), then each style of [Styles.Chain] from the farthest, the
// nearest winning (16.2, the inheritance of properties). A name that
// resolves to no style gives the default style's properties.
func (s *Styles) Resolve(scope Scope, family, name, kind string) Props {
	var p Props
	if d := s.defaults[family]; d != nil {
		p = p.merge(propChild(d, kind))
	}
	for _, st := range s.Chain(scope, family, name) {
		p = p.merge(propChild(st.El, kind))
	}
	return p
}

// Default returns the properties of kind of the family's default style.
func (s *Styles) Default(family, kind string) Props {
	return PropsOf(propChild(s.defaults[family], kind))
}

// propChild returns the property element key ("style:text-properties")
// of el.
func propChild(el *Element, key string) *Element {
	local := strings.TrimPrefix(key, "style:")
	return el.Child(NSStyle, local)
}

// Inherited returns the first non-empty value along the chain of
// family:name that get reads from a style, the nearest first: the
// attributes of style:style itself (the outline level, the list style,
// the master page) are inherited too (16.2).
func (s *Styles) Inherited(scope Scope, family, name string, get func(*Style) (string, bool)) string {
	chain := s.Chain(scope, family, name)
	for i := len(chain) - 1; i >= 0; i-- {
		if v, ok := get(chain[i]); ok {
			return v
		}
	}
	return ""
}

// Props returns the attributes and children of the style's own property
// element kind, nothing inherited.
func (st *Style) Props(kind string) Props {
	if st == nil {
		return Props{}
	}
	return PropsOf(propChild(st.El, kind))
}

// Layers returns the property elements of kind that [Styles.Resolve]
// merges for family:name, in merge order: the default style's, then the
// chain's from the farthest. A caller that resolves a value relative to
// the one below it, a font size in percent, walks them.
func (s *Styles) Layers(scope Scope, family, name, kind string) []Props {
	var out []Props
	if d := s.defaults[family]; d != nil {
		if p := PropsOf(propChild(d, kind)); !p.Empty() {
			out = append(out, p)
		}
	}
	for _, st := range s.Chain(scope, family, name) {
		if p := st.Props(kind); !p.Empty() {
			out = append(out, p)
		}
	}
	return out
}

// Has reports whether the property key is set.
func (p Props) Has(key string) bool {
	_, ok := p.attrs[key]
	return ok
}
