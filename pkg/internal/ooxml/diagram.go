package ooxml

import (
	"sort"
	"strconv"
	"strings"
)

// The namespaces of a SmartArt diagram: its data, layout, style and
// colour parts (ECMA-376 Part 1, 21.4) and the drawing PowerPoint saves
// beside them (MS-ODRAWXML, 2.4).
const (
	NSDgm = "http://schemas.openxmlformats.org/drawingml/2006/diagram"
	NSDsp = "http://schemas.microsoft.com/office/drawing/2008/diagram"
)

// The caps of a diagram. Anti-DoS guards, not spec-mandated: the
// diagrams PowerPoint offers lay out tens of points, and a hand-made one
// with thousands is past any slide.
const (
	// maxDiagramPoints caps the points and the connections of a data
	// model.
	maxDiagramPoints = 10_000
	// maxDiagramAtoms caps the atoms of a layout definition.
	maxDiagramAtoms = 10_000
	// maxDiagramShapes caps the shapes the layout engine creates: a
	// forEach repeats its layout nodes once per point.
	maxDiagramShapes = 20_000
	// maxDiagramSteps caps the atoms the layout walks visit in all: a
	// for-each referencing itself repeats its atoms for every point at
	// every level.
	maxDiagramSteps = 1_000_000
	// maxDiagramDepth caps the nesting of layout nodes and of the data
	// model's parent chains the engine follows.
	maxDiagramDepth = 64
)

// Diagram is a SmartArt diagram as its parts hold it (ECMA-376 Part 1,
// 21.4): the data model with its points and connections, the layout
// definition, the quick style and the colour definition. LayOut lays it
// out into shapes.
type Diagram struct {
	// Points and Connections are the data model (21.4.3.8 dataModel), in
	// document order.
	Points      []*DiagramPoint
	Connections []DiagramConnection
	// Background is the data model's dgm:bg (21.4.3.1), the fill of the
	// diagram's background; Whole its dgm:whole (21.4.3.10), the line.
	Background, Whole *Element
	// Drawings are the relationship ids of the drawings PowerPoint saved
	// (MS-ODRAWXML, 2.4.3 dataModelExt relId).
	Drawings []string
	// Layout is the root layout node of the layout definition (21.4.2.13
	// layoutDef), nil for none.
	Layout *DiagramAtom
	// Styles maps a style label to its quick style (21.4.4.8 styleLbl with
	// its 21.4.4.7 style: lnRef, fillRef, effectRef, fontRef).
	Styles map[string]*DiagramStyle
	// Colors maps a style label to its colour lists (21.4.4.8 styleLbl of
	// 21.4.4.2 colorsDef).
	Colors map[string]*DiagramColors

	forEach   map[string]*DiagramAtom
	byID      map[string]*DiagramPoint
	presNames map[string][]*DiagramPoint
	presOf    map[string][]presItem
}

// DiagramPoint is a point of the data model (21.4.3.4 pt): a node, a
// transition or a presentation point with its presentation properties
// (21.4.3.5 prSet) and layout variables (21.4.2.30 presLayoutVars).
type DiagramPoint struct {
	ModelID, Type, CxnID string
	// PresName, PresStyleLbl and PresAssocID are a presentation point's
	// layout node name, style label and data point.
	PresName, PresStyleLbl, PresAssocID string
	// CustomText marks a point whose text formatting the user changed
	// (prSet custT).
	CustomText bool
	// Dir and HierBranch are the layout variables dir and hierBranch, the
	// tokens as written ("norm" and "" when absent).
	Dir, HierBranch string
	// SpPr is the point's dgm:spPr, Text its dgm:t text body; nil for
	// none.
	SpPr, Text *Element
}

// DiagramConnection is a connection of the data model (21.4.3.2 cxn).
type DiagramConnection struct {
	Type, ModelID, Src, Dest, ParTrans, SibTrans string
	SrcOrd, DestOrd                              int
}

// DiagramStyle is the quick style of one style label: the style matrix
// references a shape of the label takes (20.1.4.2.19 lnRef, 20.1.4.2.10
// fillRef, 20.1.4.2.8 effectRef, 20.1.4.1.17 fontRef), nil for none.
type DiagramStyle struct {
	Line, Fill, Effect, Font *Element
}

// DiagramColors is the colour definition of one style label (21.4.4.3
// fillClrLst, 21.4.4.10 linClrLst, 21.4.4.6 effectClrLst, 21.4.4.13
// txFillClrLst): each list's colour elements.
type DiagramColors struct {
	Fill, Line, Effect, TextFill []*Element
}

// AtomKind is the kind of an atom of a layout definition.
type AtomKind uint8

// The atoms of a layout definition (21.4.2).
const (
	AtomLayoutNode AtomKind = iota
	AtomAlg
	AtomShape
	AtomConstraint
	AtomRule
	AtomForEach
	AtomChoose
	AtomIf
)

// DiagramAtom is one atom of a layout definition: a layout node
// (21.4.2.13), an algorithm (21.4.2.1 alg), a shape (21.4.2.23), a
// constraint (21.4.2.6 constr), a rule (21.4.2.20), a for-each (21.4.2.8),
// a choose (21.4.2.3) or one of its if and else branches (21.4.2.11,
// 21.4.2.7).
type DiagramAtom struct {
	Kind AtomKind
	Name string
	Kids []*DiagramAtom
	// Parent is the enclosing atom; Node the layout node the atom belongs
	// to (itself for a layout node).
	Parent, Node *DiagramAtom

	// A layout node's child order (chOrder: "b" or "t") and style label.
	ChildOrder, StyleLbl string

	// An algorithm's type, its parameters by type as written, and its
	// aspect ratio parameter ar (0 for none).
	Alg    string
	Params map[string]string
	Ratio  float64

	// A shape's preset type, "" for a group shape (no type attribute);
	// HideGeom drops the geometry; Rot is the rotation in degrees and
	// ZOrderOff the z-order offset.
	ShapeType string
	Group     bool
	HideGeom  bool
	Rot       float64
	ZOrderOff int

	// A constraint (21.4.2.6) or a rule's forName.
	Constraint DiagramConstraint

	// A for-each's or an if's iteration attributes, a for-each's ref.
	Iter IteratorAttr
	Ref  string

	// An if's condition; Else marks the else branch.
	Func, Arg, Op, Val string
	Else               bool
}

// DiagramConstraint is a constraint of a layout node (21.4.2.6 constr).
type DiagramConstraint struct {
	For, ForName, PtType, Type  string
	RefFor, RefForName, RefType string
	RefPtType, Op               string
	Factor, Value               float64
}

// IteratorAttr is the iteration of a for-each or an if (21.4.7.4 the
// axis, 21.4.7.7 ptType, cnt, st, step, hideLastTrans).
type IteratorAttr struct {
	Axis          []string
	PtType        string
	Cnt           int
	HideLastTrans bool
	St, Step      int
}

// presItem is a data point a presentation point presents (a presOf
// connection), with its depth in the data tree, -1 for the document.
type presItem struct {
	src   string
	depth int
}

// ParseDiagram reads a diagram from its parts' roots: the data model
// (21.4.3.8 dataModel), the layout definition (21.4.2.13 layoutDef), the
// quick style (21.4.4.7 styleDef) and the colour definition (21.4.4.2
// colorsDef). Any root may be nil.
func ParseDiagram(data, layout, style, colors *Element) *Diagram {
	d := &Diagram{Styles: map[string]*DiagramStyle{}, Colors: map[string]*DiagramColors{}, forEach: map[string]*DiagramAtom{}}
	if dm := diagramRoot(data, "dataModel"); dm != nil {
		d.parseData(dm)
	}
	if ld := diagramRoot(layout, "layoutDef"); ld != nil {
		n := 0
		if ln := ld.Child(NSDgm, "layoutNode"); ln != nil {
			d.Layout = d.parseAtom(ln, nil, nil, &n, 0)
			if d.Layout.ChildOrder == "" {
				d.Layout.ChildOrder = "b"
			}
		}
	}
	if sd := diagramRoot(style, "styleDef"); sd != nil {
		d.parseStyles(sd)
	}
	if cd := diagramRoot(colors, "colorsDef"); cd != nil {
		d.parseColors(cd)
	}
	d.index()
	return d
}

// diagramRoot returns root as the dgm element local, or its first child
// of that name.
func diagramRoot(root *Element, local string) *Element {
	if root.Is(NSDgm, local) {
		return root
	}
	return root.Child(NSDgm, local)
}

// parseData reads a data model (21.4.3.8): ptLst, cxnLst, bg, whole and
// the extension naming the saved drawing.
func (d *Diagram) parseData(dm *Element) {
	for _, pt := range dm.Child(NSDgm, "ptLst").Kids() {
		if !pt.Is(NSDgm, "pt") || len(d.Points) >= maxDiagramPoints {
			continue
		}
		p := &DiagramPoint{ModelID: attrOr(pt, "modelId"), Type: tokenOr(pt, "type", "node"), Dir: "norm"}
		if p.Type == "parTrans" || p.Type == "sibTrans" {
			p.CxnID = attrOr(pt, "cxnId")
		}
		if pr := pt.Child(NSDgm, "prSet"); pr != nil {
			p.PresName = attrOr(pr, "presName")
			p.PresStyleLbl = attrOr(pr, "presStyleLbl")
			p.PresAssocID = attrOr(pr, "presAssocID")
			p.CustomText = boolOr(pr, "custT", false)
			for _, v := range pr.Child(NSDgm, "presLayoutVars").Kids() {
				switch v.Local {
				case "dir":
					p.Dir = tokenOr(v, "val", "norm")
				case "hierBranch":
					p.HierBranch = attrOr(v, "val")
				}
			}
		}
		p.SpPr = pt.Child(NSDgm, "spPr")
		p.Text = pt.Child(NSDgm, "t")
		d.Points = append(d.Points, p)
	}
	for _, c := range dm.Child(NSDgm, "cxnLst").Kids() {
		if !c.Is(NSDgm, "cxn") || len(d.Connections) >= maxDiagramPoints {
			continue
		}
		cx := DiagramConnection{
			Type: tokenOr(c, "type", "parOf"), ModelID: attrOr(c, "modelId"),
			Src: attrOr(c, "srcId"), Dest: attrOr(c, "destId"),
			ParTrans: attrOr(c, "parTransId"), SibTrans: attrOr(c, "sibTransId"),
		}
		cx.SrcOrd, _ = Int(attrOr(c, "srcOrd"))
		cx.DestOrd, _ = Int(attrOr(c, "destOrd"))
		d.Connections = append(d.Connections, cx)
	}
	d.Background = dm.Child(NSDgm, "bg")
	d.Whole = dm.Child(NSDgm, "whole")
	if ext := dm.Find(NSDsp, "dataModelExt"); ext != nil {
		d.Drawings = append(d.Drawings, attrOr(ext, "relId"))
	}
}

// boolOr returns e's xsd:boolean attribute local, def when it is absent.
func boolOr(e *Element, local string, def bool) bool {
	v, ok := e.Attr(local)
	if !ok {
		return def
	}
	return OnOff(v, true)
}

// tokenOr returns e's attribute local, def when it is absent.
func tokenOr(e *Element, local, def string) string {
	if v, ok := e.Attr(local); ok {
		return strings.TrimSpace(v)
	}
	return def
}

// parseAtom reads a layout node, for-each or if (21.4.2.13, 21.4.2.8,
// 21.4.2.11) and the atoms it holds, as LibreOffice's layout node context
// does.
func (d *Diagram) parseAtom(el *Element, parent, node *DiagramAtom, n *int, depth int) *DiagramAtom {
	a := &DiagramAtom{Name: attrOr(el, "name"), Parent: parent, Node: node}
	switch el.Local {
	case "layoutNode":
		a.Kind = AtomLayoutNode
		a.Node = a
		a.StyleLbl = attrOr(el, "styleLbl")
		if v, ok := el.Attr("chOrder"); ok {
			a.ChildOrder = strings.TrimSpace(v)
		} else if node != nil {
			a.ChildOrder = node.ChildOrder
		}
	case "forEach":
		a.Kind = AtomForEach
		a.Ref = attrOr(el, "ref")
		a.Iter = parseIterator(el)
		d.forEach[a.Name] = a
	case "if", "else":
		a.Kind = AtomIf
		a.Else = el.Local == "else"
		a.Iter = parseIterator(el)
		a.Func, a.Arg, a.Op, a.Val = attrOr(el, "func"), tokenOr(el, "arg", "none"), attrOr(el, "op"), attrOr(el, "val")
	}
	if depth >= maxDiagramDepth {
		return a
	}
	add := func(k *DiagramAtom) {
		*n++
		a.Kids = append(a.Kids, k)
	}
	for _, k := range el.Kids() {
		if k.Space != NSDgm || *n >= maxDiagramAtoms {
			continue
		}
		switch k.Local {
		case "layoutNode", "forEach":
			add(d.parseAtom(k, a, a.Node, n, depth+1))
		case "shape":
			s := &DiagramAtom{Kind: AtomShape, Parent: a, Node: a.Node}
			if v, ok := k.Attr("type"); ok {
				if !boolOr(k, "hideGeom", false) {
					s.ShapeType = strings.TrimSpace(v)
				} else {
					s.HideGeom = true
				}
			} else {
				s.Group = true
			}
			if v, ok := k.Attr("rot"); ok {
				s.Rot, _ = strconv.ParseFloat(strings.TrimSpace(v), 64)
			}
			s.ZOrderOff, _ = Int(attrOr(k, "zOrderOff"))
			add(s)
		case "alg":
			s := &DiagramAtom{Kind: AtomAlg, Parent: a, Node: a.Node, Alg: attrOr(k, "type"), Params: map[string]string{}}
			for _, p := range k.Kids() {
				if !p.Is(NSDgm, "param") {
					continue
				}
				typ := attrOr(p, "type")
				if typ == "ar" {
					s.Ratio = parseDouble(attrOr(p, "val"), 0)
					continue
				}
				s.Params[typ] = strings.TrimSpace(attrOr(p, "val"))
			}
			add(s)
		case "choose":
			c := &DiagramAtom{Kind: AtomChoose, Name: attrOr(k, "name"), Parent: a, Node: a.Node}
			for _, b := range k.Kids() {
				if b.Is(NSDgm, "if") || b.Is(NSDgm, "else") {
					*n++
					c.Kids = append(c.Kids, d.parseAtom(b, c, a.Node, n, depth+1))
				}
			}
			add(c)
		case "constrLst":
			for _, c := range k.Kids() {
				if !c.Is(NSDgm, "constr") {
					continue
				}
				add(&DiagramAtom{Kind: AtomConstraint, Parent: a, Node: a.Node, Constraint: DiagramConstraint{
					For: tokenOr(c, "for", "none"), ForName: attrOr(c, "forName"), PtType: tokenOr(c, "ptType", "none"),
					Type: tokenOr(c, "type", "none"), RefFor: tokenOr(c, "refFor", "none"), RefForName: attrOr(c, "refForName"),
					RefType: tokenOr(c, "refType", "none"), RefPtType: tokenOr(c, "refPtType", "none"),
					Factor: parseDouble(attrOr(c, "fact"), 1), Value: parseDouble(attrOr(c, "val"), 0),
					Op: tokenOr(c, "op", "none"),
				}})
			}
		case "ruleLst":
			for _, r := range k.Kids() {
				if r.Is(NSDgm, "rule") {
					add(&DiagramAtom{Kind: AtomRule, Parent: a, Node: a.Node, Constraint: DiagramConstraint{ForName: attrOr(r, "forName")}})
				}
			}
		}
	}
	return a
}

// parseIterator reads the iteration attributes of a for-each or an if,
// with their defaults (21.4.2.8: cnt 0 for all, st 1, step 1,
// hideLastTrans true; ptType all), the count -1 standing for all.
func parseIterator(el *Element) IteratorAttr {
	it := IteratorAttr{Axis: strings.Fields(attrOr(el, "axis")), Cnt: -1, HideLastTrans: true, Step: 1, PtType: "all"}
	if v, ok := el.Attr("cnt"); ok {
		if n, ok := Int(v); ok {
			it.Cnt = n
		}
	}
	it.HideLastTrans = boolOr(el, "hideLastTrans", true)
	it.St, _ = Int(attrOr(el, "st"))
	if v, ok := el.Attr("step"); ok {
		if n, ok := Int(v); ok {
			it.Step = n
		}
	}
	if t := strings.Fields(attrOr(el, "ptType")); len(t) > 0 {
		it.PtType = t[0]
	}
	return it
}

// parseDouble parses a double, def when it does not parse.
func parseDouble(v string, def float64) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return def
	}
	return f
}

// parseStyles reads a quick style definition (21.4.4.7 styleDef): each
// label's style matrix references; a label repeated adds to the first.
func (d *Diagram) parseStyles(sd *Element) {
	for _, lbl := range sd.Kids() {
		if !lbl.Is(NSDgm, "styleLbl") {
			continue
		}
		name := attrOr(lbl, "name")
		st := d.Styles[name]
		if st == nil {
			st = &DiagramStyle{}
		}
		style := lbl.Child(NSDgm, "style")
		if r := style.Child(NSA, "lnRef"); r != nil {
			st.Line = r
		}
		if r := style.Child(NSA, "fillRef"); r != nil {
			st.Fill = r
		}
		if r := style.Child(NSA, "effectRef"); r != nil {
			st.Effect = r
		}
		if r := style.Child(NSA, "fontRef"); r != nil {
			st.Font = r
		}
		d.Styles[name] = st
	}
}

// parseColors reads a colour definition (21.4.4.2 colorsDef): each
// label's colour lists; a label repeated replaces the lists it states.
func (d *Diagram) parseColors(cd *Element) {
	for _, lbl := range cd.Kids() {
		if !lbl.Is(NSDgm, "styleLbl") {
			continue
		}
		name := attrOr(lbl, "name")
		c := d.Colors[name]
		if c == nil {
			c = &DiagramColors{}
		}
		for _, l := range lbl.Kids() {
			var dst *[]*Element
			switch l.Local {
			case "fillClrLst":
				dst = &c.Fill
			case "linClrLst":
				dst = &c.Line
			case "effectClrLst":
				dst = &c.Effect
			case "txFillClrLst":
				dst = &c.TextFill
			default:
				continue
			}
			*dst = nil
			for _, col := range l.Kids() {
				if col.Space == NSA && len(*dst) < maxDiagramPoints {
					*dst = append(*dst, col)
				}
			}
		}
		d.Colors[name] = c
	}
}

// index builds the maps the layout engine reads: the points by model id
// and by presentation name, and the data points each presentation point
// presents in destination order with their depth, as LibreOffice's
// diagram data model builds them.
func (d *Diagram) index() {
	d.byID = map[string]*DiagramPoint{}
	d.presNames = map[string][]*DiagramPoint{}
	d.presOf = map[string][]presItem{}
	for _, p := range d.Points {
		if _, ok := d.byID[p.ModelID]; !ok {
			d.byID[p.ModelID] = p
		}
		if p.PresName != "" {
			d.presNames[p.PresName] = append(d.presNames[p.PresName], p)
		}
	}
	byOrd := map[string]map[int]string{}
	for _, c := range d.Connections {
		if c.Type != "presOf" {
			continue
		}
		m := byOrd[c.Dest]
		if m == nil {
			m = map[int]string{}
			byOrd[c.Dest] = m
		}
		m[c.DestOrd] = c.Src
	}
	parent := map[string]string{}
	for _, c := range d.Connections {
		if c.Type == "parOf" && c.ParTrans != "" && c.SibTrans != "" && c.Src != "" && c.Dest != "" {
			if _, ok := parent[c.Dest]; !ok {
				parent[c.Dest] = c.Src
			}
		}
	}
	for dest, m := range byOrd {
		ords := make([]int, 0, len(m))
		for o := range m {
			ords = append(ords, o)
		}
		sort.Ints(ords)
		items := make([]presItem, 0, len(ords))
		for _, o := range ords {
			depth := 0
			for id, n := m[o], 0; n < maxDiagramDepth; n++ {
				p, ok := parent[id]
				if !ok {
					break
				}
				depth++
				id = p
			}
			if depth == 0 {
				depth = -1
			}
			items = append(items, presItem{src: m[o], depth: depth})
		}
		d.presOf[dest] = items
	}
}

// root returns the data model's document point (21.4.3.4 pt type doc),
// nil for none.
func (d *Diagram) root() *DiagramPoint {
	for _, p := range d.Points {
		if p.Type == "doc" {
			return p
		}
	}
	return nil
}
