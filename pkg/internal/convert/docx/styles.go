package docx

import (
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// maxStyleChain caps the basedOn chain followed from a style. Anti-DoS
// guard, not spec-mandated: Word's style hierarchies are a handful deep,
// and a cycle stops at the first style seen twice anyway.
const maxStyleChain = 64

// style is one w:style definition (ECMA-376 Part 1, 17.7.4.17).
type style struct {
	id, typ, name, basedOn, link string
	isDefault                    bool
	ppr                          *paraProps
	rpr                          *runProps
	tbl                          *tableProps
	tc                           *cellProps
	tr                           *rowProps
	// cond holds the conditional formatting of a table style by type
	// (17.7.6: wholeTable, firstRow, band1Horz...).
	cond map[string]*condFormat
}

// condFormat is one w:tblStylePr (ECMA-376 Part 1, 17.7.6.6).
type condFormat struct {
	ppr *paraProps
	rpr *runProps
	tbl *tableProps
	tc  *cellProps
	tr  *rowProps
}

// styleSheet is the resolved styles part (ECMA-376 Part 1, 17.7).
type styleSheet struct {
	byID map[string]*style
	// defRun and defPara are the document defaults (17.7.5).
	defRun  runProps
	defPara paraProps
	// defaultPara, defaultChar and defaultTable are the default styles of
	// each type (17.7.4.17, the default attribute).
	defaultPara, defaultChar, defaultTable string
}

// parseStyles reads the styles part's root, w:styles; nil yields the
// built-in defaults alone.
func (d *document) parseStyles(root *ooxml.Element) *styleSheet {
	s := &styleSheet{byID: map[string]*style{}}
	if root == nil {
		return s
	}
	if dd := root.Child(w, "docDefaults"); dd != nil {
		if r := dd.Child(w, "rPrDefault").Child(w, "rPr"); r != nil {
			s.defRun = *d.parseRunProps(r)
		}
		if p := dd.Child(w, "pPrDefault").Child(w, "pPr"); p != nil {
			s.defPara = *d.parseParaProps(p)
		}
	}
	for _, e := range root.Kids() {
		if !e.Is(w, "style") {
			continue
		}
		st := &style{
			id:        attrOr(e, "styleId"),
			typ:       attrOr(e, "type"),
			name:      attrOr(e.Child(w, "name"), "val"),
			basedOn:   attrOr(e.Child(w, "basedOn"), "val"),
			link:      attrOr(e.Child(w, "link"), "val"),
			isDefault: hasAttr(e, "default") && ooxml.OnOff(attrOr(e, "default"), true),
		}
		if st.typ == "" {
			st.typ = "paragraph"
		}
		if p := e.Child(w, "pPr"); p != nil {
			st.ppr = d.parseParaProps(p)
		}
		if r := e.Child(w, "rPr"); r != nil {
			st.rpr = d.parseRunProps(r)
		}
		if t := e.Child(w, "tblPr"); t != nil {
			st.tbl = d.parseTableProps(t)
		}
		if t := e.Child(w, "tcPr"); t != nil {
			st.tc = d.parseCellProps(t)
		}
		if t := e.Child(w, "trPr"); t != nil {
			st.tr = parseRowProps(t)
		}
		for _, c := range e.Kids() {
			if !c.Is(w, "tblStylePr") {
				continue
			}
			if st.cond == nil {
				st.cond = map[string]*condFormat{}
			}
			cf := &condFormat{}
			if p := c.Child(w, "pPr"); p != nil {
				cf.ppr = d.parseParaProps(p)
			}
			if r := c.Child(w, "rPr"); r != nil {
				cf.rpr = d.parseRunProps(r)
			}
			if t := c.Child(w, "tblPr"); t != nil {
				cf.tbl = d.parseTableProps(t)
			}
			if t := c.Child(w, "tcPr"); t != nil {
				cf.tc = d.parseCellProps(t)
			}
			if t := c.Child(w, "trPr"); t != nil {
				cf.tr = parseRowProps(t)
			}
			st.cond[attrOr(c, "type")] = cf
		}
		if st.id == "" {
			continue
		}
		if _, dup := s.byID[st.id]; dup {
			// The first definition of an ID wins, as Word reads it.
			continue
		}
		s.byID[st.id] = st
		if st.isDefault {
			switch st.typ {
			case "paragraph":
				// The last default paragraph style wins (17.7.4.17: "If
				// this attribute is specified by multiple styles, then the
				// last instance shall be used"), as LibreOffice reads it.
				s.defaultPara = st.id
			case "character":
				if s.defaultChar == "" {
					s.defaultChar = st.id
				}
			case "table":
				if s.defaultTable == "" {
					s.defaultTable = st.id
				}
			}
		}
	}
	return s
}

// hasAttr reports whether e carries the attribute local.
func hasAttr(e *ooxml.Element, local string) bool {
	_, ok := e.Attr(local)
	return ok
}

// chain returns the styles from the root of id's basedOn chain down to
// id, of type typ, a cycle or a missing style ending it.
func (s *styleSheet) chain(id, typ string) []*style {
	var out []*style
	seen := map[string]bool{}
	for id != "" && len(out) < maxStyleChain && !seen[id] {
		seen[id] = true
		st := s.byID[id]
		if st == nil || st.typ != typ {
			break
		}
		out = append(out, st)
		id = st.basedOn
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// paraStyle returns the paragraph and run properties of paragraph style
// id merged along its chain; an unknown id takes the default paragraph
// style (17.7.4.17).
func (s *styleSheet) paraStyle(id string) (paraProps, runProps) {
	if _, ok := s.byID[id]; !ok || s.byID[id].typ != "paragraph" {
		id = s.defaultPara
	}
	var p paraProps
	var r runProps
	for _, st := range s.chain(id, "paragraph") {
		p = p.merge(st.ppr)
		r = r.merge(st.rpr)
	}
	return p, r
}

// charStyle returns the run properties of character style id merged along
// its chain, none for an unknown id.
func (s *styleSheet) charStyle(id string) runProps {
	var r runProps
	for _, st := range s.chain(id, "character") {
		r = r.merge(st.rpr)
	}
	return r
}

// tableStyle returns the chain of table style id, the default table style
// for an unknown id.
func (s *styleSheet) tableStyle(id string) []*style {
	if st, ok := s.byID[id]; !ok || st.typ != "table" {
		id = s.defaultTable
	}
	return s.chain(id, "table")
}
