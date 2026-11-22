package ooxml

import "math"

// hierRect is a box of a hierarchy, in EMU at the sizes its constraints
// give, before the tree is fitted to its frame.
type hierRect struct{ x0, y0, x1, y1 float64 }

// hierItem is a shape of a laid out subtree and its box.
type hierItem struct {
	s *DiagramShape
	r hierRect
}

// hierSub is a laid out subtree: its shapes, the root's first, in the
// coordinates of its root shape's top left corner.
type hierSub struct {
	items []hierItem
}

// shift moves a subtree.
func (t *hierSub) shift(dx, dy float64) {
	for i := range t.items {
		r := &t.items[i].r
		r.x0, r.x1 = r.x0+dx, r.x1+dx
		r.y0, r.y1 = r.y0+dy, r.y1+dy
	}
}

// bounds returns the box around a subtree's shapes.
func (t *hierSub) bounds() hierRect {
	b := hierRect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, it := range t.items {
		b.x0, b.y0 = min(b.x0, it.r.x0), min(b.y0, it.r.y0)
		b.x1, b.y1 = max(b.x1, it.r.x1), max(b.y1, it.r.y1)
	}
	return b
}

// hierTree is one layout of a hierarchy: the constraints its root states
// and what it has laid out. It does not outlive the layout.
type hierTree struct {
	e    *engine
	cons []DiagramConstraint
	w, h float64
	memo map[string]float64
	busy map[string]bool
	// failed marks a tree the layout cannot place as its definition says;
	// the shapes are then left to the equal shares of [hierarchy].
	failed bool
	// across marks a tree that grows from left to right (hierAlign lCtrCh,
	// lT, ...): it is laid out turned, its x and y swapped, and turned
	// back; mirror one that grows from right to left (rCtrCh, ...).
	across, mirror bool
}

// length returns the value a constraint of the hierarchy's root sets on
// a property of the layout node name ("" for the root itself): its
// factor of the property it refers to, else its value (21.4.2.6 constr);
// false when no equality sets it.
func (t *hierTree) length(name, typ string) (float64, bool) {
	if name == "" {
		switch typ {
		case "w":
			return t.w, true
		case "h":
			return t.h, true
		}
	}
	key := name + "\x00" + typ
	if v, ok := t.memo[key]; ok {
		return v, true
	}
	if t.busy[key] || len(t.busy) >= maxDiagramDepth {
		return 0, false
	}
	t.busy[key] = true
	defer delete(t.busy, key)
	v, found := 0.0, false
	for _, c := range t.cons {
		if c.Type != typ || (c.Op != "none" && c.Op != "equ") {
			continue
		}
		switch {
		case c.ForName != name && (name == "" || c.ForName != "" || c.For != "des" || (c.PtType != "none" && c.PtType != "all")):
			// A constraint for every descendant (for des, no forName, no
			// ptType) sets the property of any named node.
			continue
		case c.ForName == "" && name != "" && found:
			// A constraint naming the node wins over one for all.
			continue
		case name == "" && c.For != "none" && c.For != "self":
			continue
		}
		switch {
		case c.RefType != "none":
			ref, ok := t.refLength(c)
			if !ok {
				continue
			}
			v, found = float64(ref*c.Factor), true
		case c.HasValue:
			// A length's value is in millimetres (21.4.2.6 constr val).
			v, found = float64(c.Value*36000), true
		}
	}
	if !found || !(v >= 0) || math.IsInf(v, 0) {
		return 0, false
	}
	t.memo[key] = v
	return v, true
}

// refLength returns the property a constraint refers to: of the layout
// node refForName, or of the shapes presenting points of refPtType.
func (t *hierTree) refLength(c DiagramConstraint) (float64, bool) {
	if c.RefForName == "" && c.RefPtType != "none" && c.RefPtType != "all" {
		return t.typeLength(c.RefPtType, c.RefType)
	}
	return t.length(c.RefForName, c.RefType)
}

// typeLength returns the value a constraint of the hierarchy's root sets
// on a property of the shapes that present points of a type (21.4.2.6
// constr ptType, with no forName).
func (t *hierTree) typeLength(pt, typ string) (float64, bool) {
	key := "\x01" + pt + "\x00" + typ
	if v, ok := t.memo[key]; ok {
		return v, true
	}
	if t.busy[key] || len(t.busy) >= maxDiagramDepth {
		return 0, false
	}
	t.busy[key] = true
	defer delete(t.busy, key)
	v, found := 0.0, false
	for _, c := range t.cons {
		if c.Type != typ || c.ForName != "" || c.PtType != pt || (c.Op != "none" && c.Op != "equ") {
			continue
		}
		switch {
		case c.RefType != "none":
			ref, ok := t.refLength(c)
			if !ok {
				continue
			}
			v, found = float64(ref*c.Factor), true
		case c.HasValue:
			v, found = float64(c.Value*36000), true
		}
	}
	if !found || !(v >= 0) || math.IsInf(v, 0) {
		return 0, false
	}
	t.memo[key] = v
	return v, true
}

// shapeLength returns a property of a root shape: what its layout node's
// constraints set, else what those of the shapes of its point's type set.
func (t *hierTree) shapeLength(s *DiagramShape, typ string) (float64, bool) {
	if v, ok := t.length(s.Name, typ); ok {
		return v, true
	}
	if s.DataType == "" {
		return 0, false
	}
	return t.typeLength(s.DataType, typ)
}

// turned returns a parameter of the tree's algorithms as it reads in the
// turned layout of a tree that grows across: a direction or an alignment
// with its axes swapped.
func (t *hierTree) turned(v string) string {
	if !t.across {
		return v
	}
	switch v {
	case "fromT":
		return "fromL"
	case "fromB":
		return "fromR"
	case "fromL":
		return "fromT"
	case "fromR":
		return "fromB"
	case "lCtrCh", "rCtrCh":
		return "tCtrCh"
	case "lCtrDes", "rCtrDes":
		return "tCtrDes"
	case "lT", "rT":
		return "tL"
	case "lB", "rB":
		return "tR"
	case "tCtrCh", "tCtrDes", "tL", "tR", "bCtrCh", "bCtrDes", "bL", "bR":
		return ""
	}
	return v
}

// spacing returns a spacing property of a layout node, the root's when
// the node states none, 0 when neither does.
func (t *hierTree) spacing(name, typ string) float64 {
	if v, ok := t.length(name, typ); ok {
		return v
	}
	if v, ok := t.length("", typ); ok {
		return v
	}
	return 0
}

// algOf returns the algorithm a layout node runs for a presentation
// point: its own, or the one of the branch its chooses take; nil for
// none.
func (e *engine) algOf(a *DiagramAtom, p *DiagramPoint) *DiagramAtom {
	var walk func(a *DiagramAtom, depth int) *DiagramAtom
	walk = func(a *DiagramAtom, depth int) *DiagramAtom {
		if a == nil || depth >= maxDiagramDepth {
			return nil
		}
		for _, k := range a.Kids {
			switch k.Kind {
			case AtomAlg:
				return k
			case AtomChoose:
				for _, b := range k.Kids {
					if b.Kind == AtomIf && e.decide(b, p) {
						if alg := walk(b, depth+1); alg != nil {
							return alg
						}
						break
					}
				}
			case AtomLayoutNode, AtomForEach, AtomShape, AtomConstraint, AtomRule, AtomIf:
			}
		}
		return nil
	}
	return walk(a, 0)
}

// algOf returns the algorithm a laid out shape runs, nil for none.
func (t *hierTree) algOf(s *DiagramShape) *DiagramAtom {
	if s.atom == nil {
		return nil
	}
	return t.e.algOf(s.atom, s.pt)
}

// assistants reports a hierChild shape whose hierarchy roots present
// assistants (21.4.7.37 ST_PtType asst).
func (t *hierTree) assistants(c *DiagramShape) bool {
	for _, k := range c.Kids {
		if k.pt == nil {
			continue
		}
		if p := t.e.d.byID[k.pt.PresAssocID]; p != nil && p.Type == "asst" {
			return true
		}
	}
	return false
}

// hierTree lays a hierarchy out as Office lays its organisation charts
// out (21.4.2.1 hierRoot and hierChild): every root shape at the size
// its constraints give, its assistants in rows of two either side of the
// line down from its centre, its other children under them in a row
// packed against each other's outline at sibSp, a column or two columns
// as the algorithms' linDir and secLinDir say, the root centred over the
// first and last child (hierAlign tCtrCh), at their left or right offset
// by alignOff of its width (tL, tR); rows sp apart. A tree whose roots
// align on a side (lCtrCh, rT, ...) grows across: it is laid out turned,
// and from the right for r. The sizes are the constraints' on the root's
// layout node, on the shapes of the points' type (ptType) or on every
// descendant. The tree is then scaled to fit the frame whole and centred
// in it. It reports false, leaving every shape as it was, for a tree it
// cannot place that way: a root shape without a size, roots aligned at
// the bottom, a parameter it does not know.
//
// # Corpus evidence
//
//   - libreoffice/pptx: smartart-org-chart's orgChart1 prints its five
//     166.32 by 83.52-point boxes 34.56 points apart down and across
//     (0.21 of the width the constraints state), Manager2 beside Manager
//     though Manager's children reach under it, the assistant left of the
//     line down from Manager, half a gap from it, the tree as tall as its
//     frame and centred across it.
//   - libreoffice/pptx: smartart-org-chart2 hangs the leaf children of B2
//     and C2 in a column 0.25 of a width right of their parent (hierRoot
//     tL, alignOff 0.25), and B2's column sits 27.5 points, sibSp, right of
//     C2's at their bottom row.
//   - openxmlsdk/pptx: SmartArt_OrgChart1 puts its two assistants either
//     side of the line, their subtrees half a gap from it, and
//     HierarchyDiagram_TP10174325 its two assistants alike above three
//     children, the root centred over them.
//   - openxmlsdk/pptx: HorizHierChart_TP10220217's hierarchy2 (lCtrCh, its
//     sizes on ptType node) grows to the right, levels 0.4 of a box apart
//     and siblings 0.15 of a box apart down, each root centred between its
//     first and last child; FamilyTree_TP10107970's hierarchy5 grows from
//     the right, its levels 0.4 of a box apart (an sp for every
//     descendant).
func (e *engine) hierTree(a *DiagramAtom, s *DiagramShape, cons []DiagramConstraint) bool {
	own, _ := e.own(s.atom, s.pt)
	t := &hierTree{e: e, cons: append(append([]DiagramConstraint(nil), cons...), own...), w: float64(s.W), h: float64(s.H), memo: map[string]float64{}, busy: map[string]bool{}}
	if align := t.firstAlign(a, s, 0); align != "" {
		t.across = align[0] == 'l' || align[0] == 'r'
		t.mirror = align[0] == 'r'
	}
	var sub hierSub
	switch a.Alg {
	case "hierChild":
		sub = t.container(a, s, 0)
	case "hierRoot":
		sub = t.root(a, s, 0)
	}
	if t.failed || len(sub.items) == 0 {
		return false
	}
	for i := range sub.items {
		r := &sub.items[i].r
		if t.across {
			r.x0, r.y0, r.x1, r.y1 = r.y0, r.x0, r.y1, r.x1
		}
		if t.mirror {
			r.x0, r.x1 = -r.x1, -r.x0
		}
	}
	b := sub.bounds()
	bw, bh := b.x1-b.x0, b.y1-b.y0
	if !(bw > 0) || !(bh > 0) {
		return false
	}
	f := min(1, t.w/bw, t.h/bh)
	ox := float64((t.w - float64(bw*f)) / 2)
	oy := float64((t.h - float64(bh*f)) / 2)
	abs := map[*DiagramShape]hierRect{}
	for _, it := range sub.items {
		abs[it.s] = hierRect{
			float64(float64((it.r.x0-b.x0)*f) + ox), float64(float64((it.r.y0-b.y0)*f) + oy),
			float64(float64((it.r.x1-b.x0)*f) + ox), float64(float64((it.r.y1-b.y0)*f) + oy),
		}
	}
	t.place(s, abs, hierRect{0, 0, t.w, t.h}, 0)
	e.hierDone[s] = true
	return true
}

// firstAlign returns the hierAlign of the first hierRoot of a tree, ""
// for none.
func (t *hierTree) firstAlign(a *DiagramAtom, s *DiagramShape, depth int) string {
	if a != nil && a.Alg == "hierRoot" {
		return param(a, "hierAlign", "tCtrCh")
	}
	if depth >= maxDiagramDepth {
		return ""
	}
	for _, k := range s.Kids {
		if v := t.firstAlign(t.algOf(k), k, depth+1); v != "" {
			return v
		}
	}
	return ""
}

// place gives every shape under s its box relative to its parent's: a
// laid out shape its own, a group the box around what it holds, any
// other shape the parent's top left corner.
func (t *hierTree) place(s *DiagramShape, abs map[*DiagramShape]hierRect, parent hierRect, depth int) {
	if depth >= maxDiagramDepth {
		return
	}
	for _, k := range s.Kids {
		r, ok := abs[k]
		if !ok {
			r, ok = t.around(k, abs, 0)
		}
		if !ok {
			k.X, k.Y = 0, 0
			t.place(k, abs, parent, depth+1)
			continue
		}
		k.X, k.Y = trunc(r.x0-parent.x0), trunc(r.y0-parent.y0)
		k.W, k.H = trunc(r.x1-r.x0), trunc(r.y1-r.y0)
		t.e.hierDone[k] = true
		t.place(k, abs, r, depth+1)
	}
}

// around returns the box around the laid out shapes under s.
func (t *hierTree) around(s *DiagramShape, abs map[*DiagramShape]hierRect, depth int) (hierRect, bool) {
	if depth >= maxDiagramDepth {
		return hierRect{}, false
	}
	b, found := hierRect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}, false
	for _, k := range s.Kids {
		r, ok := abs[k]
		if !ok {
			r, ok = t.around(k, abs, depth+1)
		}
		if ok {
			b.x0, b.y0, b.x1, b.y1 = min(b.x0, r.x0), min(b.y0, r.y0), max(b.x1, r.x1), max(b.y1, r.y1)
			found = true
		}
	}
	if found {
		abs[s] = b
	}
	return b, found
}

// root lays out a hierRoot shape: its root shape, then its assistants and
// its children below it.
func (t *hierTree) root(a *DiagramAtom, s *DiagramShape, depth int) hierSub {
	var out hierSub
	if depth >= maxDiagramDepth || t.e.step() {
		t.failed = true
		return out
	}
	align := param(a, "hierAlign", "tCtrCh")
	if t.across {
		align = t.turned(align)
	}
	switch align {
	case "tCtrCh", "tCtrDes", "tL", "tR":
	default:
		t.failed = true
		return out
	}
	var comp *DiagramShape
	var asst, kids []*DiagramShape
	var asstAlg, kidAlg []*DiagramAtom
	for _, k := range s.Kids {
		ka := t.algOf(k)
		switch {
		case ka != nil && ka.Alg == "hierChild":
			if t.assistants(k) {
				asst, asstAlg = append(asst, k), append(asstAlg, ka)
			} else {
				kids, kidAlg = append(kids, k), append(kidAlg, ka)
			}
		case ka != nil && ka.Alg == "hierRoot":
			t.failed = true
			return out
		case comp == nil:
			comp = k
		}
	}
	if comp == nil {
		t.failed = true
		return out
	}
	w, okW := t.shapeLength(comp, "w")
	h, okH := t.shapeLength(comp, "h")
	if !okW || !okH || !(w > 0) || !(h > 0) {
		t.failed = true
		return out
	}
	if t.across {
		w, h = h, w
	}
	sp := t.spacing(s.Name, "sp")
	off := 0.0
	cons, _ := t.e.own(s.atom, s.pt)
	for _, c := range cons {
		if c.Type == "alignOff" && c.HasValue {
			off = c.Value
		}
	}
	out.items = append(out.items, hierItem{comp, hierRect{0, 0, w, h}})
	line := float64(w / 2)
	y := float64(h + sp)
	for i, c := range asst {
		rows := t.container(asstAlg[i], c, depth+1)
		if t.failed {
			return out
		}
		if len(rows.items) == 0 {
			continue
		}
		rows.shift(line, y-rows.bounds().y0)
		out.items = append(out.items, rows.items...)
		y = float64(rows.bounds().y1 + sp)
	}
	for i, c := range kids {
		row := t.container(kidAlg[i], c, depth+1)
		if t.failed {
			return out
		}
		if len(row.items) == 0 {
			continue
		}
		b := row.bounds()
		var dx float64
		switch {
		case t.turned(param(kidAlg[i], "secLinDir", "")) == "fromT":
			// Two columns hang either side of the line down the root.
			dx = line
		case align == "tL":
			dx = float64(w*off) - b.x0
		case align == "tR":
			dx = float64(w-float64(w*off)) - b.x1
		case align == "tCtrDes":
			dx = line - float64((b.x0+b.x1)/2)
		default:
			first, last := t.firstLast(row, c)
			dx = line - float64((first+last)/2)
		}
		row.shift(dx, y-b.y0)
		out.items = append(out.items, row.items...)
		y = float64(row.bounds().y1 + sp)
	}
	return out
}

// firstLast returns the centres across of the first and last root shapes
// a container's row holds.
func (t *hierTree) firstLast(row hierSub, c *DiagramShape) (float64, float64) {
	first, last := math.NaN(), math.NaN()
	for _, k := range c.Kids {
		for _, it := range row.items {
			if it.s.parent == k {
				m := float64((it.r.x0 + it.r.x1) / 2)
				if math.IsNaN(first) {
					first = m
				}
				last = m
				break
			}
		}
	}
	if math.IsNaN(first) {
		b := row.bounds()
		first = float64((b.x0 + b.x1) / 2)
		last = first
	}
	return first, last
}

// container lays out a hierChild shape's hierarchy roots (21.4.2.1
// hierChild): along linDir, a row packed against the outline of the
// subtrees before at sibSp (fromL, fromR) or a column sibSp apart (fromT);
// with secLinDir fromT, rows of two either side of the line through the
// container's origin, half of sibSp from it, secSibSp apart down.
func (t *hierTree) container(a *DiagramAtom, s *DiagramShape, depth int) hierSub {
	var out hierSub
	if depth >= maxDiagramDepth || t.e.step() {
		t.failed = true
		return out
	}
	dir := t.turned(param(a, "linDir", "fromL"))
	sec := t.turned(param(a, "secLinDir", ""))
	gap := t.spacing(s.Name, "sibSp")
	var subs []hierSub
	for _, k := range s.Kids {
		ka := t.algOf(k)
		if ka == nil || ka.Alg != "hierRoot" {
			continue
		}
		sub := t.root(ka, k, depth+1)
		if t.failed {
			return out
		}
		if len(sub.items) > 0 {
			subs = append(subs, sub)
		}
	}
	if len(subs) == 0 {
		return out
	}
	switch {
	case sec == "fromT" && (dir == "fromL" || dir == "fromR"):
		secGap := t.spacing(s.Name, "secSibSp")
		y := 0.0
		for i := 0; i < len(subs); i += 2 {
			pair := subs[i:min(i+2, len(subs))]
			if dir == "fromR" && len(pair) == 2 {
				pair = []hierSub{pair[1], pair[0]}
			}
			bottom := y
			for j, sub := range pair {
				b := sub.bounds()
				if j == 0 {
					sub.shift(-float64(gap/2)-b.x1, y-b.y0)
				} else {
					sub.shift(float64(gap/2)-b.x0, y-b.y0)
				}
				bottom = max(bottom, sub.bounds().y1)
				out.items = append(out.items, sub.items...)
			}
			y = bottom + secGap
		}
	case dir == "fromT" || dir == "fromB":
		align := param(a, "chAlign", "l")
		y := 0.0
		for _, sub := range subs {
			b := sub.bounds()
			dx := -b.x0
			if align == "r" {
				dx = -b.x1
			}
			sub.shift(dx, y-b.y0)
			out.items = append(out.items, sub.items...)
			y = sub.bounds().y1 + gap
		}
	case dir == "fromL" || dir == "fromR":
		if dir == "fromR" {
			for i, j := 0, len(subs)-1; i < j; i, j = i+1, j-1 {
				subs[i], subs[j] = subs[j], subs[i]
			}
		}
		for i, sub := range subs {
			b := sub.bounds()
			sub.shift(0, -b.y0)
			if i == 0 {
				sub.shift(-b.x0, 0)
				out.items = append(out.items, sub.items...)
				continue
			}
			dx, ok := t.clear(out.items, sub.items, gap)
			if !ok {
				return out
			}
			sub.shift(dx, 0)
			out.items = append(out.items, sub.items...)
		}
	default:
		t.failed = true
	}
	return out
}

// clear returns how far right a subtree moves to stand gap clear of every
// shape before it that it faces across.
func (t *hierTree) clear(placed, next []hierItem, gap float64) (float64, bool) {
	dx := math.Inf(-1)
	for _, n := range next {
		for _, p := range placed {
			if t.e.step() {
				t.failed = true
				return 0, false
			}
			if n.r.y0 < p.r.y1 && p.r.y0 < n.r.y1 {
				dx = max(dx, p.r.x1+gap-n.r.x0)
			}
		}
	}
	if math.IsInf(dx, -1) {
		b := (&hierSub{items: placed}).bounds()
		nb := (&hierSub{items: next}).bounds()
		dx = b.x1 + gap - nb.x0
	}
	return dx, true
}
