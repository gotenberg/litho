package ooxml

import (
	"math"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// maxSnakePasses caps the passes that resolve the lengths a snake's
// constraints set from one another. Anti-DoS guard, not spec-mandated:
// the snake definitions PowerPoint offers chain at most four lengths (the
// node's width, its height from the width, the transition's width from
// the node's, the spacing from the transition's), so eight passes resolve
// any of them.
const maxSnakePasses = 8

// snakeGrid is what the snake algorithm lays its nodes out on: the size of
// a node, the gap between two nodes of a line (the transition between
// them) and the gap between two lines (sp), in EMU before any scaling.
type snakeGrid struct {
	cw, ch float64
	gap    float64
	lineSp float64
}

// snake lays the kids out in rows or columns (ECMA-376 Part 1, 21.4.2.1
// alg snake, "lays out child layout nodes along a linear path in two
// dimensions, allowing the linear flow to continue across multiple rows or
// columns"; 21.4.7.49 ST_ParameterId grDir, flowDir, contDir, bkpt,
// bkPtFixedVal, off), as PowerPoint lays it out:
//
//   - A node takes the width and height the constraints set (21.4.2.6
//     constr): those of the snake for its name or point type, a factor of
//     the snake's box, a value in millimetres or a factor of another of its
//     lengths, then its own layout node's, then a composite node's aspect
//     ratio (ar).
//   - Two nodes of a line stand the width of the transition between them
//     apart (the snake's width constraint for the sibTrans layout node or
//     point type), two lines the spacing sp apart.
//   - The nodes, their gaps and the spacing scale together, never up, to
//     fit the box. With bkpt endCnv (the default, 21.4.7.11 "nodes are
//     added to the next column or row after filling the current column or
//     row's space") a line holds as many nodes as make them largest, the
//     most nodes when two counts give one size; bal makes the count of
//     lines and of nodes a line as equal as it can, fixed takes
//     bkPtFixedVal nodes a line.
//   - The block of lines sits in the middle of the box unless horzAlign or
//     vertAlign says otherwise, and a short last line in the middle of the
//     block (off ctr, 21.4.7.47 "no offset"; off "off" shifts by an amount
//     the standard does not give and is laid out as ctr).
//   - grDir names the corner the first node takes (21.4.7.35), flowDir
//     whether a line is a row or a column (21.4.7.30), contDir whether the
//     next line runs the same way or back (21.4.7.22).
//
// The standard gives no default for the parameters: they are those of its
// own example (grDir tL, flowDir row, contDir sameDir, off ctr) with bkpt
// endCnv. It says nothing of the scaling, the count a line, the gaps or
// the alignment: those are read from PowerPoint's drawings and Office's
// pages, as the evidence below measures.
//
// # Corpus evidence
//
//   - libreoffice/pptx: the PowerPoint drawings of the Basic Block List
//     (default, node height 0.6 of its width, transitions and spacing 0.1
//     of it) put four nodes in a 480 by 320 point frame two a row, 228.5
//     points wide, 11.5 points from the top (smartart-background-
//     drawingml-fallback); five in a 640 by 426.7 frame two a row, the
//     fifth in the middle of the third (smartart-interopgrabbag, poi/pptx
//     SmartArt); two in 640 by 426.7 one a row, 328.1 wide, 155.9 from the
//     left (tdf104792-smart-art-animation, oox smartart-groupshape). Each
//     is within half a point of the largest size the rule gives.
//   - libreoffice/pptx: Office's pages of smartart-background,
//     smartart-bullet-list, the three frames of smartart-font-size,
//     smartart-rightoleftblockdiagram and smartart1 place every block as
//     the rule does, centred both ways, the short last row of five nodes in
//     the middle.
//   - libreoffice/pptx: Office's page of smartart-snake-rows (bList2:
//     transitions 0.08, spacing 0.16 of the node width) puts six nodes
//     three a row, 172 points apart across and 194.5 down, 159.4 wide.
//   - openxmlsdk/pptx: Office's pages of EmpOrienPres_TP10220212 (pList1,
//     six nodes three a row, 176.6 points apart) and SalesPropPres_
//     TP10220213 (default, five nodes three a row, the last two in the
//     middle) follow the rule.
//   - litho/pptx, litho/docx: Office's pages of smartart-text-size put four
//     default nodes 278.6 by 167.1 points (166.0 by 99.6 in the Word
//     document) two a row, 27.4 apart, centred in the frame.
//   - libreoffice/pptx: bnc870233_2's two one-node frames are saved with
//     the node at the bottom of the frame, not in the middle; a frame
//     taller than the node by 2.4 and 12.5 points.
func (e *engine) snake(a *DiagramAtom, s *DiagramShape, cons []DiagramConstraint) {
	if len(s.Kids) == 0 || s.W <= 0 || s.H <= 0 {
		return
	}
	var nodes []*DiagramShape
	for _, k := range s.Kids {
		if !e.transition(k) {
			nodes = append(nodes, k)
		}
	}
	if len(nodes) == 0 {
		return
	}
	cols := param(a, "flowDir", "row") != "col"
	g, sizes := e.snakeGrid(a, s, cons, nodes, cols)
	if g.cw <= 0 || g.ch <= 0 {
		return
	}
	n := len(nodes)
	// Along a line and across the lines, in the box's own axes.
	along, across := float64(s.W), float64(s.H)
	cAlong, cAcross := g.cw, g.ch
	if !cols {
		along, across = across, along
		cAlong, cAcross = cAcross, cAlong
	}
	scaleFor := func(per int) float64 {
		lines := (n + per - 1) / per
		lineLen := float64(float64(per)*cAlong) + float64(float64(per-1)*g.gap)
		block := float64(float64(lines)*cAcross) + float64(float64(lines-1)*g.lineSp)
		return min(1, along/lineLen, across/block)
	}
	per := n
	switch param(a, "bkpt", "endCnv") {
	case "bal":
		per = int(math.Ceil(math.Sqrt(float64(n)))) // fits int: at most n
	case "fixed":
		per = int(min(max(intParam(a, "bkPtFixedVal", int64(n)), 1), int64(n))) // fits int: within 1..n
	default:
		best := -1.0
		for c := n; c >= 1; c-- {
			if sc := scaleFor(c); sc > best {
				best, per = sc, c
			}
		}
	}
	per = max(1, min(per, n))
	sc := scaleFor(per)
	if !numeric.Finite(sc) || sc <= 0 {
		return
	}
	lines := (n + per - 1) / per
	cA, cX := float64(cAlong*sc), float64(cAcross*sc)
	gap, lineSp := float64(g.gap*sc), float64(g.lineSp*sc)
	lineLen := func(k int) float64 { return float64(float64(k)*cA) + float64(float64(k-1)*gap) }
	blockAlong := lineLen(per)
	blockAcross := float64(float64(lines)*cX) + float64(float64(lines-1)*lineSp)
	hAlign, vAlign := param(a, "horzAlign", "ctr"), param(a, "vertAlign", "mid")
	alongAlign, acrossAlign := hAlign, vAlign
	if !cols {
		alongAlign, acrossAlign = vAlign, hAlign
	}
	startAlong := float64(alignOffset(alongAlign, trunc(along-blockAlong)))
	startAcross := float64(alignOffset(acrossAlign, trunc(across-blockAcross)))
	grDir := param(a, "grDir", "tL")
	revX := grDir == "tR" || grDir == "bR"
	revY := grDir == "bL" || grDir == "bR"
	revAlong, revAcross := revX, revY
	if !cols {
		revAlong, revAcross = revY, revX
	}
	back := param(a, "contDir", "sameDir") == "revDir"
	// place maps a cell's offsets along and across the block to the box.
	place := func(k *DiagramShape, pa, px, wa, wx float64) {
		if revAlong {
			pa = float64(blockAlong - pa - wa)
		}
		if revAcross {
			px = float64(blockAcross - px - wx)
		}
		x, y, w, h := startAlong+pa, startAcross+px, wa, wx
		if !cols {
			x, y, w, h = y, x, h, w
		}
		k.X, k.Y, k.W, k.H = trunc(x), trunc(y), trunc(w), trunc(h)
	}
	var prev *DiagramShape
	var prevAlong, prevAcross float64
	i := 0
	for _, k := range s.Kids {
		if e.transition(k) {
			// A transition fills the gap after the node before it.
			if prev != nil {
				place(k, prevAlong+cA, prevAcross, gap, cX)
			} else {
				k.X, k.Y, k.W, k.H = 0, 0, 0, 0
			}
			continue
		}
		line, pos := i/per, i%per
		count := min(per, n-line*per)
		if back && line%2 == 1 {
			pos = count - 1 - pos
		}
		pa := float64(float64(pos)*(cA+gap)) + float64((blockAlong-lineLen(count))/2)
		px := float64(float64(line) * (cX + lineSp))
		sz := sizes[i]
		wa, wx := float64(sz[0]*sc), float64(sz[1]*sc)
		if !cols {
			wa, wx = wx, wa
		}
		place(k, pa+float64((cA-wa)/2), px+float64((cX-wx)/2), wa, wx)
		prev, prevAlong, prevAcross = k, pa, px
		i++
	}
}

// transition reports a shape that presents a transition point (21.4.7.51
// ST_PtType sibTrans, parTrans).
func (e *engine) transition(k *DiagramShape) bool {
	if k.pt == nil {
		return false
	}
	dp := e.d.byID[k.pt.PresAssocID]
	return dp != nil && (dp.Type == "sibTrans" || dp.Type == "parTrans")
}

// snakeGrid resolves the lengths a snake's constraints set: the size of
// each node, as wide and as tall as the widest and tallest, the gap the
// transitions leave along a line and the spacing between lines. A length
// that refers to another resolves once that one has, in at most
// maxSnakePasses passes.
func (e *engine) snakeGrid(a *DiagramAtom, s *DiagramShape, cons []DiagramConstraint, nodes []*DiagramShape, cols bool) (snakeGrid, [][2]float64) {
	trans := snakeTransitionNames(a)
	kinds := map[string]string{}
	for _, k := range s.Kids {
		if e.transition(k) {
			kinds[k.Name] = "sibTrans"
		} else if _, ok := kinds[k.Name]; !ok {
			kinds[k.Name] = "node"
		}
	}
	for name := range trans {
		kinds[name] = "sibTrans"
	}
	props := map[string]map[string]float64{"": {"w": float64(s.W), "h": float64(s.H)}}
	set := func(name, prop string, v float64) bool {
		m := props[name]
		if m == nil {
			m = map[string]float64{}
			props[name] = m
		}
		if _, ok := m[prop]; ok || !numeric.Finite(v) || v < 0 {
			return false
		}
		m[prop] = v
		return true
	}
	targets := func(c DiagramConstraint) []string {
		switch {
		case c.ForName != "":
			return []string{c.ForName}
		case c.PtType != "" && c.PtType != "none" && c.PtType != "all":
			var out []string
			for _, name := range sortedKeys(kinds) {
				if kinds[name] == c.PtType || (c.PtType == "nonAsst" && kinds[name] == "node") {
					out = append(out, name)
				}
			}
			return out
		case c.Type == "sp" || c.Type == "sibSp":
			return []string{""}
		}
		return nil
	}
	for range maxSnakePasses {
		changed := false
		for _, c := range cons {
			if e.step() {
				break
			}
			if c.Type != "w" && c.Type != "h" && c.Type != "sp" && c.Type != "sibSp" {
				continue
			}
			if !setsLength(c) {
				continue
			}
			v, ok := e.snakeLength(c, cons, props)
			if !ok {
				continue
			}
			for _, name := range targets(c) {
				if name == "" && c.Type != "sp" && c.Type != "sibSp" {
					continue
				}
				if set(name, c.Type, v) {
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	var g snakeGrid
	sizes := make([][2]float64, len(nodes))
	for i, k := range nodes {
		w, h := e.nodeLengths(k, props[k.Name], s)
		sizes[i] = [2]float64{w, h}
		g.cw, g.ch = max(g.cw, w), max(g.ch, h)
	}
	// The transition's length along the line, else its other length: a
	// definition that flows in columns may size it by its width alone.
	along, other := "w", "h"
	if !cols {
		along, other = "h", "w"
	}
	for _, prop := range []string{along, other} {
		for _, name := range sortedKeys(trans) {
			if v, ok := props[name][prop]; ok && g.gap == 0 {
				g.gap = v
			}
		}
	}
	if g.gap == 0 {
		if v, ok := props[""]["sibSp"]; ok {
			g.gap = v
		}
	}
	g.lineSp = g.gap
	if v, ok := props[""]["sp"]; ok {
		g.lineSp = v
	}
	return g, sizes
}

// step counts one step of the layout against maxDiagramSteps and reports
// whether the steps are spent.
func (e *engine) step() bool {
	e.steps++
	return e.steps > maxDiagramSteps
}

// snakeLength returns the length a constraint of a snake sets, in EMU: its
// factor of the length it refers to (the snake's own box for no
// refForName), its value in millimetres when it refers to none, its factor
// of a font size read in millimetres (see [engine.fontLength]); false when
// what it refers to is not resolved yet.
func (e *engine) snakeLength(c DiagramConstraint, cons []DiagramConstraint, props map[string]map[string]float64) (float64, bool) {
	if v, _, ok := e.fontLength(c, cons); ok {
		return v, true
	}
	if c.RefType == "none" {
		if !c.HasValue || c.Unbounded {
			return 0, false
		}
		return float64(c.Value * 36000), true
	}
	ref, ok := props[c.RefForName][c.RefType]
	if !ok || !numeric.Finite(c.Factor) {
		return 0, false
	}
	return float64(ref * c.Factor), true
}

// nodeLengths returns a snake node's width and height in EMU: those the
// snake's constraints set, else those its own layout node's constraints
// set (a width or height in millimetres, or a factor of its other side),
// else the snake's box; a composite node then shrinks one side to its
// aspect ratio (21.4.7.49 ar, "the algorithm can temporarily shrink one
// dimension to achieve that ratio").
func (e *engine) nodeLengths(k *DiagramShape, set map[string]float64, s *DiagramShape) (float64, float64) {
	w, okW := set["w"]
	h, okH := set["h"]
	own, _ := e.own(k.atom, k.pt)
	for range 2 {
		for _, c := range own {
			if (c.For != "self" && c.For != "none" && c.For != "") || c.ForName != "" || !setsLength(c) {
				continue
			}
			switch {
			case c.Type == "w" && !okW:
				w, okW = ownLength(c, h, okH)
			case c.Type == "h" && !okH:
				h, okH = ownLength(c, w, okW)
			}
		}
	}
	if !okW {
		w = float64(s.W)
	}
	if !okH {
		h = float64(s.H)
	}
	if ar := compositeRatio(k.atom); ar > 0 && w > 0 && h > 0 {
		if w/h > ar {
			w = float64(h * ar)
		} else {
			h = w / ar
		}
	}
	return w, h
}

// ownLength returns the length a node's own width or height constraint
// sets: its value in millimetres, or its factor of the node's other side
// when that one is known.
func ownLength(c DiagramConstraint, other float64, known bool) (float64, bool) {
	switch {
	case c.RefType == "none" && c.HasValue && !c.Unbounded:
		return float64(c.Value * 36000), true
	case (c.RefType == "w" || c.RefType == "h") && c.RefType != c.Type && known && numeric.Finite(c.Factor):
		return float64(other * c.Factor), true
	}
	return 0, false
}

// compositeRatio returns the aspect ratio parameter of a layout node's
// composite algorithm, 0 for none.
func compositeRatio(a *DiagramAtom) float64 {
	if a == nil {
		return 0
	}
	for _, k := range a.Kids {
		if k.Kind == AtomAlg && k.Alg == "composite" && numeric.Finite(k.Ratio) && k.Ratio > 0 {
			return k.Ratio
		}
	}
	return 0
}

// snakeTransitionNames returns the names of the layout nodes a snake's
// for-each lays out for its transitions (ptType sibTrans or parTrans).
func snakeTransitionNames(a *DiagramAtom) map[string]bool {
	out := map[string]bool{}
	if a == nil || a.Node == nil {
		return out
	}
	var walk func(x *DiagramAtom, trans bool, depth int)
	walk = func(x *DiagramAtom, trans bool, depth int) {
		if depth >= maxDiagramDepth {
			return
		}
		for _, k := range x.Kids {
			switch k.Kind {
			case AtomForEach:
				walk(k, trans || k.Iter.PtType == "sibTrans" || k.Iter.PtType == "parTrans", depth+1)
			case AtomLayoutNode:
				if trans {
					out[k.Name] = true
				}
			case AtomChoose, AtomIf:
				walk(k, trans, depth+1)
			case AtomAlg, AtomShape, AtomConstraint, AtomRule:
			}
		}
	}
	walk(a.Node, false, 0)
	return out
}

// sortedKeys returns the keys of a map in order, so that a walk over it
// does the same on every run.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
