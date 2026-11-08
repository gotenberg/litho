package ooxml

import (
	"math"
	"sort"
	"strings"
)

// DiagramShape is a shape the layout engine made from a layout node and
// the presentation point it lays out: a group shape (a layout node whose
// shape has no type) or a preset shape, its box relative to its parent's,
// its rotation, the fill and line its data gives it, its style matrix
// references with the colours of the colour definition, and its text.
type DiagramShape struct {
	// Name is the layout node's name.
	Name string
	// Group marks a group shape: it draws nothing but its kids.
	Group bool
	// X, Y, W and H are the box in EMU, relative to the parent's box.
	X, Y, W, H int64
	// Rot is the rotation in 60000ths of a degree, clockwise, about the
	// box's centre; DiagramRot the layout shape's own rotation in degrees
	// (21.4.2.23 shape rot), which turns the geometry inside the box.
	Rot        int64
	DiagramRot float64
	// DataType is the type of the data point the shape presents ("node",
	// "asst", "sibTrans", ...), "" for none.
	DataType string
	// Kids are the shapes inside a group, in stacking order.
	Kids []*DiagramShape
	// FillSpPr is the dgm:spPr whose fill the shape takes over its style's,
	// LineSpPr the one whose line it takes; nil for none.
	FillSpPr, LineSpPr *Element
	// Style holds the shape's style matrix references; StyleColor the
	// colour each takes from the colour definition, nil for the
	// reference's own.
	Style      DiagramStyle
	StyleColor DiagramStyle
	// Paragraphs is the text, nil for none; BodyPr and ListStyle the text
	// body properties and list style of the first data point that gave
	// text.
	Paragraphs        []DiagramParagraph
	BodyPr, ListStyle *Element
	// Insets are the text insets in EMU the constraints set, by left,
	// top, right, bottom; Inset[i] reports which are set.
	Insets [4]int64
	Inset  [4]bool
	// FontSize is the size in points a run without one takes, 0 for none.
	FontSize float64
	// Autofit shrinks the text to the box; Anchor is its vertical anchor
	// ("t", "ctr", "b", "" when the text algorithm did not run); TextRot
	// the text's rotation in degrees relative to the shape.
	Autofit bool
	Anchor  string
	TextRot int

	tmpl         *shapeTemplate
	sub          string
	zorder, zoff int
	ar           float64
	vcount       int64
}

// DiagramParagraph is one paragraph of a layout shape's text: a source
// paragraph of a data point, the paragraph properties of that point's
// first paragraph LibreOffice applies to every paragraph it appends, and
// what the text algorithm sets.
type DiagramParagraph struct {
	Source, PPr *Element
	Level       int
	// MarL and Indent are the left margin and first-line indent in EMU
	// when HasMarL and HasIndent; Align the alignment token ("l", "ctr",
	// ...), "" for the paragraph's own; Bullet draws a "•" bullet.
	MarL, Indent       int64
	HasMarL, HasIndent bool
	Align              string
	Bullet             bool
}

// Preset returns the preset geometry the shape draws, "" for none.
func (s *DiagramShape) Preset() string {
	if s.Group || s.tmpl == nil {
		return ""
	}
	return s.tmpl.preset
}

// shapeTemplate is what the copies of one layout shape share, as
// LibreOffice's shapes share their custom shape and line properties.
type shapeTemplate struct {
	preset string
	line   *Element
}

// mode is what a layout definition walk does at each atom, after
// LibreOffice's layout atom visitors.
type mode uint8

const (
	modeCreate mode = iota
	modeTemplate
	modeLayout
	modeShallow
)

// look is what a walk looks for at a layout node's atoms.
type look uint8

const (
	lookNode look = iota
	lookConstraint
	lookAlgorithm
	lookRule
)

// engine lays a diagram out, after LibreOffice's oox diagram import
// (oox/source/drawingml/diagram).
type engine struct {
	d       *Diagram
	presShp map[*DiagramPoint]*DiagramShape
	// shpPres maps a shape to the earliest presentation point laid out
	// with it, ptIndex a point to its place in the data model.
	shpPres  map[*DiagramShape]*DiagramPoint
	ptIndex  map[*DiagramPoint]int
	tmpls    map[*DiagramAtom]*shapeTemplate
	children map[[2]string]bool
	shapes   int
	steps    int
	root     *DiagramShape
	rootAtom *DiagramAtom
}

// walker is one walk of the layout definition: the current presentation
// point, the for-each position, and the mode's own state.
type walker struct {
	e         *engine
	m         mode
	cur       *DiagramPoint
	idx, step int
	cnt       int
	look      look
	depth     int
	// modeCreate
	parent *DiagramShape
	// modeTemplate
	tmpl *DiagramShape
	// modeLayout
	constraints []DiagramConstraint
	rules       []string
	// modeShallow
	count int
}

// LayOut lays the diagram out in a box of w by h EMU (ECMA-376 Part 1,
// 21.4.2: the layout nodes walked over the data model, each algorithm
// placing its node's shapes), as LibreOffice lays a diagram out when
// PowerPoint saved no drawing of it: its algorithms (composite, conn,
// cycle, hierChild, hierRoot, lin, pyra, snake, sp, tx), the constraints
// and rules they read, its z-order offsets. The result is the diagram's
// group: its first kid the background rectangle when the data model
// gives the background a fill or a line, then the layout's shapes. nil
// when the diagram has no layout or no document point.
func (d *Diagram) LayOut(w, h int64) *DiagramShape {
	rootPt := d.root()
	root := &DiagramShape{Group: true, W: w, H: h}
	e := &engine{d: d, presShp: map[*DiagramPoint]*DiagramShape{}, shpPres: map[*DiagramShape]*DiagramPoint{}, ptIndex: map[*DiagramPoint]int{}, tmpls: map[*DiagramAtom]*shapeTemplate{}, children: map[[2]string]bool{}, root: root, rootAtom: d.Layout}
	for _, c := range d.Connections {
		e.children[[2]string{c.Src, c.Dest}] = true
	}
	for i, p := range d.Points {
		e.ptIndex[p] = i
	}
	if d.Layout != nil && rootPt != nil {
		c := &walker{e: e, m: modeCreate, cur: rootPt, parent: root}
		c.visit(d.Layout)
		l := &walker{e: e, m: modeLayout, cur: rootPt}
		l.visit(d.Layout)
		sortByZOrder(root, 0)
		removeEmptyGroups(root, 0)
		shareLines(root, 0)
	}
	if fillOrLine(d.Background) || fillOrLine(d.Whole) {
		bg := &DiagramShape{Name: "background", W: w, H: h, tmpl: &shapeTemplate{preset: "rect"}, FillSpPr: d.Background, LineSpPr: d.Whole}
		root.Kids = append([]*DiagramShape{bg}, root.Kids...)
	}
	return root
}

// fillOrLine reports an element holding a DrawingML fill or line.
func fillOrLine(e *Element) bool {
	for _, k := range e.Kids() {
		switch k.Local {
		case "noFill", "solidFill", "gradFill", "blipFill", "pattFill", "ln":
			return true
		}
	}
	return false
}

// visit dispatches an atom to the walk's handler.
func (w *walker) visit(a *DiagramAtom) {
	w.e.steps++
	if w.depth >= 4*maxDiagramDepth || w.e.shapes > maxDiagramShapes || w.e.steps > maxDiagramSteps {
		return
	}
	w.depth++
	defer func() { w.depth-- }()
	switch a.Kind {
	case AtomChoose:
		for _, k := range a.Kids {
			if k.Kind == AtomIf && w.e.decide(k, w.cur) {
				w.visit(k)
				break
			}
		}
	case AtomIf:
		w.kids(a)
	case AtomForEach:
		w.forEach(a)
	case AtomLayoutNode:
		w.layoutNode(a)
	case AtomAlg:
		switch {
		case w.m == modeCreate && w.look == lookAlgorithm:
			w.parent.ar = a.Ratio
			w.parent.vcount = verticalShapesCount(a, w.parent)
		case w.m == modeLayout && w.look == lookAlgorithm:
			if s, ok := w.e.presShp[w.cur]; ok {
				w.e.layoutShape(a, s, w.constraints, w.rules)
			}
		}
	case AtomShape:
		if w.m == modeTemplate && w.tmpl == nil {
			w.tmpl = w.e.copyShape(a)
		}
	case AtomConstraint:
		if w.m == modeLayout && w.look == lookConstraint {
			w.constraints = parseConstraint(a.Constraint, w.constraints)
		}
	case AtomRule:
		if w.m == modeLayout && w.look == lookRule && a.Constraint.ForName != "" {
			w.rules = append(w.rules, a.Constraint.ForName)
		}
	}
}

// kids visits an atom's children in order.
func (w *walker) kids(a *DiagramAtom) {
	for _, k := range a.Kids {
		w.visit(k)
	}
}

// forEach walks a for-each (21.4.2.8): the one it references, else its
// atoms once per point, as LibreOffice counts them: the most points any
// of its layout nodes presents for a node or nonAsst iteration, one for
// any other, cut at cnt; a transition iteration after the last sibling
// is skipped when hideLastTrans says so.
func (w *walker) forEach(a *DiagramAtom) {
	switch w.m {
	case modeTemplate:
		return
	case modeShallow:
		w.kids(a)
		return
	case modeCreate, modeLayout:
	}
	if a.Ref != "" {
		if r, ok := w.e.d.forEach[a.Ref]; ok {
			w.visit(r)
		}
		return
	}
	if a.Iter.HideLastTrans && len(a.Iter.Axis) > 0 && a.Iter.Axis[0] == "followSib" && w.idx+w.step >= w.cnt {
		return
	}
	n := 1
	if a.Iter.PtType == "node" || a.Iter.PtType == "nonAsst" {
		s := &walker{e: w.e, m: modeShallow, cur: w.cur, depth: w.depth}
		s.kids(a)
		n = s.count
	}
	cnt := n
	if a.Iter.Cnt != -1 {
		cnt = min(n, a.Iter.Cnt)
	}
	oldIdx, oldStep, oldCnt := w.idx, w.step, w.cnt
	step := a.Iter.Step
	w.step, w.cnt = step, cnt
	for w.idx = 0; w.idx < cnt && step > 0; w.idx += step {
		w.kids(a)
	}
	w.idx, w.step, w.cnt = oldIdx, oldStep, oldCnt
}

// childPoint returns the presentation point a layout node lays out at
// the walk's position: the idx-th of its name, when it is a child of the
// current point; nil otherwise.
func (w *walker) childPoint(a *DiagramAtom) *DiagramPoint {
	list := w.e.d.presNames[a.Name]
	if w.idx < 0 || w.idx >= len(list) || w.cur == nil {
		return nil
	}
	p := list[w.idx]
	if !w.e.children[[2]string{w.cur.ModelID, p.ModelID}] {
		return nil
	}
	return p
}

// layoutNode handles a layout node for the walk's mode.
func (w *walker) layoutNode(a *DiagramAtom) {
	switch w.m {
	case modeTemplate:
		return
	case modeShallow:
		w.count = max(w.count, len(w.e.d.presNames[a.Name]))
		return
	case modeCreate, modeLayout:
	}
	if w.look != lookNode {
		return
	}
	p := w.childPoint(a)
	if p == nil {
		return
	}
	prev := w.cur
	if w.m == modeLayout {
		n := len(w.constraints)
		w.cur = p
		for _, l := range []look{lookConstraint, lookRule, lookAlgorithm, lookNode} {
			w.look = l
			w.kids(a)
		}
		w.cur = prev
		w.constraints = w.constraints[:n]
		return
	}
	parent := w.parent
	if a == w.e.rootAtom {
		s := w.e.root
		w.e.setupShape(a, s, p, w.idx)
		s.Name = a.Name
		w.e.bind(p, s)
	} else {
		t := &walker{e: w.e, m: modeTemplate, cur: p, depth: w.depth}
		t.kids(a)
		if s := t.tmpl; s != nil {
			w.e.setupShape(a, s, p, w.idx)
			s.Name = a.Name
			parent.Kids = append(parent.Kids, s)
			parent = s
			w.e.bind(p, s)
		}
	}
	prevParent := w.parent
	w.cur, w.parent = p, parent
	w.look = lookNode
	w.kids(a)
	w.look = lookAlgorithm
	w.kids(a)
	w.look = lookNode
	w.cur, w.parent = prev, prevParent
}

// bind records the shape laid out for a presentation point.
func (e *engine) bind(p *DiagramPoint, s *DiagramShape) {
	e.presShp[p] = s
	if q, ok := e.shpPres[s]; !ok || e.ptIndex[p] < e.ptIndex[q] {
		e.shpPres[s] = p
	}
}

// copyShape returns a new shape from a layout shape: copies share the
// shape's preset and line, as LibreOffice's copies share their custom
// shape and line properties.
func (e *engine) copyShape(a *DiagramAtom) *DiagramShape {
	t, ok := e.tmpls[a]
	if !ok {
		t = &shapeTemplate{preset: a.ShapeType}
		e.tmpls[a] = t
	}
	e.shapes++
	return &DiagramShape{tmpl: t, Group: a.Group, sub: a.ShapeType, DiagramRot: a.Rot, zoff: a.ZOrderOff}
}

// setupShape fills a shape from the presentation point it lays out, as
// LibreOffice's LayoutNode::setupShape does: the fill and line of the
// shallowest data point it presents, the text of every one in order with
// its depth as the paragraphs' level, and the quick style and colours of
// the point's style label (the layout node's when the point has none),
// the idx-th colour of each list.
func (e *engine) setupShape(node *DiagramAtom, s *DiagramShape, p *DiagramPoint, idx int) {
	if items, ok := e.d.presOf[p.ModelID]; ok {
		minDepth := math.MaxInt
		for _, it := range items {
			minDepth = min(minDepth, it.depth)
		}
		for _, it := range items {
			dp := e.d.byID[it.src]
			if dp == nil || (dp.SpPr == nil && dp.Text == nil) {
				continue
			}
			s.DataType = dp.Type
			if it.depth == minDepth {
				if s.tmpl != nil {
					s.tmpl.line = dp.SpPr
				}
				s.LineSpPr = dp.SpPr
				s.FillSpPr = dp.SpPr
			}
			paras := dp.Text.childrenOf(NSA, "p")
			if len(paras) == 0 || !hasRuns(paras[0]) {
				continue
			}
			if s.Paragraphs == nil {
				s.BodyPr = dp.Text.Child(NSA, "bodyPr")
				s.ListStyle = dp.Text.Child(NSA, "lstStyle")
			}
			ppr := paras[0].Child(NSA, "pPr")
			for _, sp := range paras {
				level := 0
				if it.depth != -1 {
					level = it.depth
				}
				if v, ok := ppr.Attr("lvl"); ok {
					if n, ok := Int(v); ok && n != 0 {
						level = n
					}
				}
				s.Paragraphs = append(s.Paragraphs, DiagramParagraph{Source: sp, PPr: ppr, Level: level})
			}
		}
	} else if fillOrLine(p.SpPr) {
		s.FillSpPr = p.SpPr
	}
	label := p.PresStyleLbl
	if label == "" {
		label = node.StyleLbl
	}
	if label == "" {
		return
	}
	if st, ok := e.d.Styles[label]; ok {
		s.Style = *st
		s.StyleColor = DiagramStyle{}
	}
	if c, ok := e.d.Colors[label]; ok {
		s.StyleColor.Fill = colorAt(c.Fill, idx, s.StyleColor.Fill)
		s.StyleColor.Line = colorAt(c.Line, idx, s.StyleColor.Line)
		s.StyleColor.Effect = colorAt(c.Effect, idx, s.StyleColor.Effect)
		s.StyleColor.Font = colorAt(c.TextFill, idx, s.StyleColor.Font)
	}
}

// colorAt returns the idx-th colour of a list, cycling, def for an empty
// list.
func colorAt(list []*Element, idx int, def *Element) *Element {
	if len(list) == 0 {
		return def
	}
	if idx < 0 {
		return list[len(list)-1]
	}
	return list[idx%len(list)]
}

// childrenOf returns e's children space:local.
func (e *Element) childrenOf(space, local string) []*Element {
	var out []*Element
	for _, k := range e.Kids() {
		if k.Is(space, local) {
			out = append(out, k)
		}
	}
	return out
}

// hasRuns reports a paragraph holding a run, a break or a field.
func hasRuns(p *Element) bool {
	for _, k := range p.Kids() {
		if k.Space == NSA && (k.Local == "r" || k.Local == "br" || k.Local == "fld") {
			return true
		}
	}
	return false
}

// parseConstraint appends a constraint a layout walk keeps: an equality
// with a type, naming the layout node it is for unless it sets spacing
// or margins or is for transitions.
func parseConstraint(c DiagramConstraint, out []DiagramConstraint) []DiagramConstraint {
	required := true
	switch c.Type {
	case "sp", "lMarg", "rMarg", "tMarg", "bMarg":
		required = false
	}
	if c.PtType == "sibTrans" {
		required = false
	}
	if required && c.ForName == "" {
		return out
	}
	if (c.Op == "none" || c.Op == "equ") && c.Type != "none" {
		out = append(out, c)
	}
	return out
}

// decide evaluates an if (21.4.2.11) for a presentation point, as
// LibreOffice does: the dir and hierBranch variables, the count of the
// point's children and the depth below it; any other function holds.
func (e *engine) decide(a *DiagramAtom, p *DiagramPoint) bool {
	if a.Else {
		return true
	}
	if p == nil {
		return false
	}
	switch a.Func {
	case "var":
		switch a.Arg {
		case "dir":
			return compareTokens(a.Op, p.Dir, a.Val)
		case "hierBranch":
			hb := p.HierBranch
			if hb == "" {
				hb = "std"
				if par := e.d.byID[e.navigate("presParOf", p.ModelID, false)]; par != nil && par.HierBranch != "" {
					hb = par.HierBranch
				}
			}
			return compareTokens(a.Op, hb, a.Val)
		}
	case "cnt":
		return compareInts(a.Op, e.nodeCount(a, p), toInt32(a.Val))
	case "maxDepth":
		return compareInts(a.Op, e.maxDepth(p.PresAssocID, 0), toInt32(a.Val))
	}
	return true
}

// compareTokens compares two tokens for equ and neq; any other operator
// fails.
func compareTokens(op, a, b string) bool {
	switch op {
	case "equ":
		return a == b
	case "neq":
		return a != b
	}
	return false
}

// compareInts compares two numbers by an operator (21.4.7.20
// ST_FunctionOperator); an unknown operator fails.
func compareInts(op string, a, b int) bool {
	switch op {
	case "equ":
		return a == b
	case "gt":
		return a > b
	case "gte":
		return a >= b
	case "lt":
		return a < b
	case "lte":
		return a <= b
	case "neq":
		return a != b
	}
	return false
}

// toInt32 parses the leading integer of s, 0 for none, as
// OUString::toInt32 does.
func toInt32(s string) int {
	s = strings.TrimSpace(s)
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	n := 0
	for i := 0; i < len(s) && s[i] >= '0' && s[i] <= '9' && n < 1<<30; i++ {
		n = n*10 + int(s[i]-'0')
	}
	if neg {
		return -n
	}
	return n
}

// navigate follows the first connection of a type from a point, source
// to destination or back; "" for none.
func (e *engine) navigate(typ, from string, forward bool) string {
	for _, c := range e.d.Connections {
		if c.Type != typ {
			continue
		}
		if forward && c.Src == from {
			return c.Dest
		}
		if !forward && c.Dest == from {
			return c.Src
		}
	}
	return ""
}

// nodeCount counts the children of a presentation point's data point
// (of its first child for the axis "ch ch"), as LibreOffice counts them
// for the cnt function whatever the axis.
func (e *engine) nodeCount(a *DiagramAtom, p *DiagramPoint) int {
	id := p.PresAssocID
	if len(a.Iter.Axis) == 2 && a.Iter.Axis[0] == "ch" && a.Iter.Axis[1] == "ch" {
		id = e.navigate("parOf", id, true)
	}
	if id == "" {
		return 0
	}
	n := 0
	for _, c := range e.d.Connections {
		if c.Type == "parOf" && c.Src == id {
			n++
		}
	}
	return n
}

// maxDepth returns the length of the longest parOf chain below a data
// point.
func (e *engine) maxDepth(id string, depth int) int {
	if depth >= maxDiagramDepth {
		return 0
	}
	n := 0
	for _, c := range e.d.Connections {
		if c.Type == "parOf" && c.Src == id {
			n = max(n, e.maxDepth(c.Dest, depth+1)+1)
		}
	}
	return n
}

// sortByZOrder orders a shape's kids by their z-order offsets (21.4.2.23
// zOrderOff), as LibreOffice does: a kid moves above the next ones by
// its positive offset.
func sortByZOrder(s *DiagramShape, depth int) {
	if depth >= maxDiagramDepth {
		return
	}
	k := s.Kids
	for i := range k {
		k[i].zorder = i
	}
	for i := range k {
		off := k[i].zoff
		if off <= 0 {
			continue
		}
		k[i].zorder += off
		k[i].zoff = 0
		for j := 0; j < off; j++ {
			n := i + j + 1
			if n >= len(k) {
				break
			}
			k[n].zorder--
		}
	}
	sort.SliceStable(k, func(a, b int) bool { return k[a].zorder < k[b].zorder })
	for _, c := range k {
		sortByZOrder(c, depth+1)
	}
}

// shareLines gives every shape the line its layout shape's copies share,
// the last a data point set.
func shareLines(s *DiagramShape, depth int) {
	if depth >= maxDiagramDepth {
		return
	}
	if s.tmpl != nil {
		s.LineSpPr = s.tmpl.line
	}
	for _, k := range s.Kids {
		shareLines(k, depth+1)
	}
}

// removeEmptyGroups drops the group shapes without kids, once their
// spacing influenced the layout.
func removeEmptyGroups(s *DiagramShape, depth int) {
	if depth >= maxDiagramDepth {
		return
	}
	s.Kids = dropEmptyGroups(s.Kids)
	for _, c := range s.Kids {
		removeEmptyGroups(c, depth+1)
	}
}

// dropEmptyGroups returns kids without its group shapes that hold none.
func dropEmptyGroups(kids []*DiagramShape) []*DiagramShape {
	out := kids[:0]
	for _, k := range kids {
		if k.Group && len(k.Kids) == 0 {
			continue
		}
		out = append(out, k)
	}
	return out
}
