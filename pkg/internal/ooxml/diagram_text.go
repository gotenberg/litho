package ooxml

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// DiagramTextFit reports whether the text of a laid out shape fits its
// box: its runs without a size of their own set at s.FontSize points, its
// text box less s.Insets, no word broken across lines and no line past the
// box's height. The converter measures it with the text engine that draws
// the shape. s is the shape being sized: the callback reads it and must
// not keep it or change it.
type DiagramTextFit func(s *DiagramShape) bool

// The caps of the text sizing of a diagram. Anti-DoS guards, not
// spec-mandated.
const (
	// maxDiagramFits caps the measurements one diagram's text sizing asks
	// for. A size is found in at most 13 measurements of each shape a
	// group ties (its start, then a halving search over whole points, at
	// most 4000), and the diagrams PowerPoint offers hold tens of text
	// shapes, starting at 65 points or less (8 measurements): 4096 size
	// about 500. Past the cap, every measurement fails and the groups
	// left take their floor.
	maxDiagramFits = 4096
	// maxDiagramTexts caps the text shapes one diagram's text sizing sizes;
	// the shapes past it keep the size and autofit the text algorithm gave
	// them. 2048 is past a hundred times the text shapes of the diagrams
	// PowerPoint offers.
	maxDiagramTexts = 2048
	// maxDiagramFontSize is the largest font size a constraint starts at,
	// in points: the largest a DrawingML run holds (ST_TextFontSize,
	// 400000 hundredths of a point).
	maxDiagramFontSize = 4000
	// minDiagramFontSize is the smallest size a rule shrinks text to, in
	// points: the smallest a DrawingML run holds (ST_TextFontSize, 100
	// hundredths of a point).
	minDiagramFontSize = 1
)

// marginSide maps a margin constraint type (21.4.7.21 ST_ConstraintType
// lMarg, tMarg, rMarg, bMarg) to its side, left, top, right, bottom.
var marginSide = map[string]int{"lMarg": 0, "tMarg": 1, "rMarg": 2, "bMarg": 3}

// The font size constraint types (21.4.7.21 ST_ConstraintType: primFontSz
// "the primary font size", secFontSz "the secondary font size").
const (
	primFontSz = "primFontSz"
	secFontSz  = "secFontSz"
)

// textSize is the size search of one text shape: the font size constraint
// it follows (primFontSz, or secFontSz when its primary size refers to
// the secondary one), the size it starts at, the floor its rules let it
// shrink to, and the shapes whose size bounds its own.
type textSize struct {
	s    *DiagramShape
	kind string
	// start and floor are in points; hasStart and hasFloor report a
	// constraint value and a rule that gives them.
	start, floor       float64
	hasStart, hasFloor bool
	// fixed marks text formatted by hand (prSet custT): it keeps its own
	// sizes.
	fixed bool
	// unresolved marks a shape whose box an enclosing layout sizes from a
	// variable the engine does not resolve: it keeps the size and autofit
	// the text algorithm gave it.
	unresolved bool
	// caps bound the size by a factor of other shapes' (an lte constraint).
	caps []sizeCap
	// insets are the factors of the size each inset is (lMarg, tMarg,
	// rMarg, bMarg with refType primFontSz or secFontSz), by left, top,
	// right, bottom; hasInset reports which a constraint sets, stated
	// which one sets otherwise (a value or another reference), which the
	// size leaves alone.
	insets           [4]float64
	hasInset, stated [4]bool
	// group is the index of the group the shape is tied in.
	group int
	size  float64
	done  bool
}

// sizeCap bounds a size by factor times the size of the shapes named name
// under holder (21.4.7.10 ST_BoolOperator lte).
type sizeCap struct {
	holder *DiagramShape
	name   string
	factor float64
}

// sizer is one run of the text sizing of a diagram: the text shapes in
// layout order, the groups their equality constraints tie, the
// measurements left.
type sizer struct {
	e      *engine
	fit    DiagramTextFit
	sizes  []*textSize
	parent []int
	fits   int
	// members lists, for each shape around a text shape, the text shapes
	// under it with how many layout levels down; holders lists those
	// shapes in layout order.
	members map[*DiagramShape][]member
	holders []*DiagramShape
}

// member is a text shape under a shape, depth layout levels down.
type member struct {
	i, depth int
}

// nodePoint is a layout node laid out for a presentation point.
type nodePoint struct {
	a *DiagramAtom
	p *DiagramPoint
}

// nodeLists are a layout node's constraints and rules for a point.
type nodeLists struct {
	cons  []DiagramConstraint
	rules []DiagramRule
}

// sizeText gives each text shape of the layout the font size its layout
// definition's constraints and rules give it (ECMA-376 Part 1, 21.4.2.8
// constr, 21.4.2.24 rule), as PowerPoint sizes the text of a diagram it
// lays out:
//
//   - A shape starts at the val of the font size constraint that names it:
//     its layout node's own, else the nearest enclosing layout node's whose
//     forName names it, for its children (ch) or descendants (des).
//   - A rule of its own or one that names it shrinks it, down to the val
//     of the font size rules met before a rule that lets the shape itself
//     grow without limit (val INF): the standard's example is a rule "that
//     will shrink the primary font size down to a lower limit of 2pt font
//     when the text no longer fits". A shape without such a rule keeps its
//     starting size.
//   - The shapes one constraint of an enclosing layout node names share
//     the one value it sets, whether it states their equality (op equ,
//     21.4.7.10 ST_BoolOperator) or a value (op none); a constraint that
//     sets a size to another's (refType, refForName) ties the two; one of
//     op lte, or a reference with a factor other than 1, keeps a size at
//     most that factor of the size it refers to, which is found first.
//   - A secondary size follows the primary one it refers to.
//   - The size of a group is the largest whole point at or under its
//     start at which the text of every shape of the group fits, its
//     insets that factor of the size; the start itself when it fits; the
//     floor when nothing fits.
//   - Text formatted by hand keeps its sizes, and a run that states its
//     size keeps it.
//   - A shape an enclosing layout sizes beside a width or height that
//     refers to a user-defined variable (refType userA to userZ), which
//     the layout engine does not resolve, is not in the box PowerPoint
//     computes: its text keeps the size, autofit and insets the text
//     algorithm gives it.
//
// The standard is silent on how the search runs and on what a tie is: the
// whole-point steps, the floor of the rules before an unbounded growth,
// the ties of one constraint over several nodes, and the inset of a side
// no constraint sets are read from the sizes PowerPoint saves in its
// drawings, as the evidence below says. A shape whose text no constraint
// sizes keeps the size and autofit the text algorithm gave it.
//
// # Corpus evidence
//
//   - libreoffice/pptx, poi/pptx: every size PowerPoint saves in the 19
//     drawings of 98 laid out texts is a whole point; the nodes an equality
//     constraint ties share one (smartart-background-drawingml-fallback's
//     four default nodes at 42 points, set by "Background", the longest
//     word; tdf125551's five VerticalCurvedList nodes at 32); a lone letter
//     keeps its 65-point start (smartart-interopgrabbag).
//   - libreoffice/pptx: the insets PowerPoint saves are the constraint
//     factor of the size it chose: 0.3 of 42 points (247650 EMU at 65,
//     160020 at 42) for the default layout, 0.56 of 16 for venn2
//     (smartart-tdf134221), 0.05 of 7 for orgChart1 text formatted by hand
//     (n819614).
//   - libreoffice/pptx: smartart-preserve's two list1 parentText nodes,
//     which one constraint of op none for des sets to 65 and no equality
//     ties, are both saved at 30 points.
//   - libreoffice/pptx: smartart-linear-rule's hProcess3 sizes a padding
//     from userP; PowerPoint prints its parTx text 145 points apart and
//     45 points from the frame's edge where litho's columns stand 150.8
//     apart from 10: sized at PowerPoint's 50.5 points in those columns,
//     the page scored 0.956 against 0.970 with the text the algorithm
//     gives.
//   - libreoffice/pptx: n819614's 56 orgChart1 nodes, formatted by hand
//     (custT) at 7 points, are saved at 7 points.
func (e *engine) sizeText(fit DiagramTextFit) {
	z := &sizer{e: e, fit: fit, members: map[*DiagramShape][]member{}}
	var texts []*DiagramShape
	collectTexts(e.root, 0, &texts)
	if len(texts) == 0 {
		return
	}
	for i, s := range texts {
		depth := 1
		for a := s.parent; a != nil && depth < maxDiagramDepth; a, depth = a.parent, depth+1 {
			if _, ok := z.members[a]; !ok {
				z.holders = append(z.holders, a)
			}
			z.members[a] = append(z.members[a], member{i, depth})
		}
	}
	for _, s := range texts {
		z.sizes = append(z.sizes, z.spec(s))
	}
	z.parent = make([]int, len(z.sizes))
	for i := range z.parent {
		z.parent[i] = i
	}
	z.tie()
	z.solve()
}

// step counts one step of the sizing against the layout's steps and
// reports whether the steps are spent.
func (z *sizer) step() bool { return z.e.step() }

// collectTexts appends the text shapes of s and its kids, in layout order,
// at most maxDiagramTexts.
func collectTexts(s *DiagramShape, depth int, out *[]*DiagramShape) {
	if depth >= maxDiagramDepth || len(*out) >= maxDiagramTexts {
		return
	}
	if s.text && len(s.Paragraphs) > 0 && s.atom != nil {
		*out = append(*out, s)
	}
	for _, k := range s.Kids {
		collectTexts(k, depth+1, out)
	}
}

// own returns the constraints and rules a layout node states for the
// presentation point p: its own lists and those of the branches its
// chooses take (21.4.2.5 choose), not those of the layout nodes and
// for-each it holds.
func (e *engine) own(a *DiagramAtom, p *DiagramPoint) ([]DiagramConstraint, []DiagramRule) {
	key := nodePoint{a, p}
	if l, ok := e.lists[key]; ok {
		return l.cons, l.rules
	}
	var cons []DiagramConstraint
	var rules []DiagramRule
	var walk func(a *DiagramAtom, depth int)
	walk = func(a *DiagramAtom, depth int) {
		if depth >= maxDiagramDepth {
			return
		}
		for _, k := range a.Kids {
			switch k.Kind {
			case AtomConstraint:
				cons = append(cons, k.Constraint)
			case AtomRule:
				rules = append(rules, k.Rule)
			case AtomChoose:
				for _, b := range k.Kids {
					if b.Kind == AtomIf && e.decide(b, p) {
						walk(b, depth+1)
						break
					}
				}
			case AtomLayoutNode, AtomForEach, AtomAlg, AtomShape, AtomIf:
			}
		}
	}
	if a != nil {
		walk(a, 0)
	}
	e.lists[key] = nodeLists{cons, rules}
	return cons, rules
}

// names reports whether a constraint or a rule held by the layout node of
// holder, depth levels above s, is for s: its forName names s, or it has
// none, for the children of the holder (ch, depth 1) or its descendants
// (des). A constraint for self holds for the holder alone (depth 0).
func names(forAxis, forName string, s *DiagramShape, depth int) bool {
	switch forAxis {
	case "self", "", "none":
		return depth == 0 && (forName == "" || forName == s.Name)
	case "ch":
		return depth == 1 && (forName == "" || forName == s.Name)
	case "des":
		return depth >= 1 && (forName == "" || forName == s.Name)
	}
	return false
}

// spec reads the size search of a text shape from its own constraints
// and rules and those of the layout nodes around it.
func (z *sizer) spec(s *DiagramShape) *textSize {
	t := &textSize{s: s, kind: primFontSz, fixed: z.e.customText(s)}
	savedInsets, savedInset := s.Insets, s.Inset
	defer func() {
		if t.unresolved {
			s.Insets, s.Inset = savedInsets, savedInset
		}
	}()
	cons, rules := z.e.own(s.atom, s.pt)
	for _, c := range cons {
		// A primary size that refers to the secondary one makes the
		// secondary size the shape's (21.4.2.8: "primFontSz refType
		// secFontSz").
		if c.Type == primFontSz && c.RefType == secFontSz && (c.Op == "none" || c.Op == "equ") && names(c.For, c.ForName, s, 0) {
			t.kind = secFontSz
		}
	}
	sameAs := map[int]sideRef{}
	insets := func(cons []DiagramConstraint, depth int) {
		for _, c := range cons {
			i, ok := marginSide[c.Type]
			if !ok || t.hasInset[i] || t.stated[i] || !names(c.For, c.ForName, s, depth) {
				continue
			}
			if (c.RefType == primFontSz || c.RefType == secFontSz) && numeric.Finite(c.Factor) && c.Factor >= 0 {
				t.insets[i], t.hasInset[i] = c.Factor, true
				continue
			}
			if j, ok := marginSide[c.RefType]; ok && depth == 0 && (c.RefFor == "none" || c.RefFor == "self") && numeric.Finite(c.Factor) && c.Factor >= 0 {
				// A margin that refers to another of the node's margins is
				// that factor of it, once that one is read.
				sameAs[i] = sideRef{j, c.Factor}
				t.stated[i] = true
				continue
			}
			t.stated[i] = true
			if c.RefType == "none" {
				// A value is in millimetres, as the layout's lengths are.
				s.Insets[i], s.Inset[i] = trunc(max(c.Value, 0)*36000), true
			}
		}
	}
	insets(cons, 0)
	floors := func(rules []DiagramRule, depth int) {
		for _, r := range rules {
			if !names(r.For, r.ForName, s, depth) {
				continue
			}
			if r.Type == t.kind {
				// A rule of NaN, zero or a negative floor shrinks nothing.
				if numeric.Finite(r.Value) && r.Value > 0 {
					f := max(r.Value, minDiagramFontSize)
					if !t.hasFloor || f < t.floor {
						t.floor, t.hasFloor = f, true
					}
				}
				continue
			}
			if r.Unbounded && depth == 0 {
				// The rules after one that grows the shape without limit are
				// never reached: the shape grows to its text first.
				return
			}
		}
	}
	floors(rules, 0)
	starts := func(cons []DiagramConstraint, depth int) {
		for _, c := range cons {
			if c.Type == t.kind && !t.hasStart && c.HasValue && c.RefType == "none" && names(c.For, c.ForName, s, depth) && c.Value > 0 {
				t.start, t.hasStart = min(c.Value, maxDiagramFontSize), true
			}
		}
	}
	starts(cons, 0)
	defer func() {
		// A linear layout that sized shapes from this size bounded it by
		// its room (engine.fontLengths).
		if c, ok := z.e.fontCaps[fontKey(t.kind, s.Name)]; ok && t.hasStart {
			t.start = min(t.start, c)
		}
	}()
	depth := 1
	for a := s.parent; a != nil && depth < maxDiagramDepth; a, depth = a.parent, depth+1 {
		ac, ar := z.e.own(a.atom, a.pt)
		for _, c := range ac {
			if (c.Type == "w" || c.Type == "h") && strings.HasPrefix(c.RefType, "user") {
				t.unresolved = true
			}
		}
		starts(ac, depth)
		floors(ar, depth)
		insets(ac, depth)
		for _, c := range ac {
			if c.Type != t.kind || !names(c.For, c.ForName, s, depth) || !boundBy(c) {
				continue
			}
			t.caps = append(t.caps, sizeCap{holder: a, name: c.RefForName, factor: c.Factor})
			if !t.hasStart {
				// A size bounded by another's starts at that bound.
				t.start, t.hasStart = maxDiagramFontSize, true
			}
		}
	}
	for i, r := range sameAs {
		if t.hasInset[r.side] {
			t.insets[i], t.hasInset[i] = float64(t.insets[r.side]*r.factor), true
		}
	}
	for i := range t.insets {
		if !t.hasInset[i] && !t.stated[i] && !s.Inset[i] {
			t.insets[i], t.hasInset[i] = defaultInsetFactor, true
		}
	}
	return t
}

// sideRef is a margin set as a factor of another side's margin.
type sideRef struct {
	side   int
	factor float64
}

// defaultInsetFactor is the inset of a side no margin constraint sets, a
// factor of the text's size. The standard gives none; it is the inset
// PowerPoint saves for such text.
//
// # Corpus evidence
//
//   - libreoffice/pptx: smartart-rotation2's three pList2 nodes, whose
//     definition sets no margin, are saved with insets of 256032 EMU at
//     36 points, 0.56 of the size; smartart-tdf134221's venn2 texts with
//     113792 at 16 and 149352 at 21, 0.56 again.
//   - poi/pptx: smartart-simple's venn1 texts, whose margins are
//     constraints of no value, are saved with insets of 0.
const defaultInsetFactor = 0.56

// find returns the group of size i.
func (z *sizer) find(i int) int {
	for z.parent[i] != i {
		z.parent[i] = z.parent[z.parent[i]]
		i = z.parent[i]
	}
	return i
}

// union ties the groups of i and j, the earlier shape leading.
func (z *sizer) union(i, j int) {
	a, b := z.find(i), z.find(j)
	if a == b {
		return
	}
	if b < a {
		a, b = b, a
	}
	z.parent[b] = a
}

// tie joins the groups the font size constraints of the enclosing layout
// nodes make: every shape one constraint names under one instance of its
// layout node, whether it states equality (op equ) or sets the one value
// they share (op none), and, for a constraint that sets a size to
// another's, the shapes named by its refForName under that instance.
func (z *sizer) tie() {
	for _, h := range z.holders {
		cons, _ := z.e.own(h.atom, h.pt)
		for _, c := range cons {
			if (c.Op != "equ" && c.Op != "none") || (c.Type != primFontSz && c.Type != secFontSz) {
				continue
			}
			refers := c.RefForName != "" && (c.RefType == primFontSz || c.RefType == secFontSz) && !boundBy(c)
			first := -1
			for _, m := range z.members[h] {
				if z.step() {
					return
				}
				t := z.sizes[m.i]
				named := t.kind == c.Type && names(c.For, c.ForName, t.s, m.depth)
				referred := refers && t.kind == c.RefType && t.s.Name == c.RefForName
				if !named && !referred {
					continue
				}
				if first < 0 {
					first = m.i
				} else {
					z.union(first, m.i)
				}
			}
		}
	}
}

// boundBy reports a font size constraint that bounds a size by a factor
// of another's rather than tying the two: op lte (21.4.7.10
// ST_BoolOperator), or a reference with a factor other than 1.
func boundBy(c DiagramConstraint) bool {
	if c.RefForName == "" || (c.RefType != primFontSz && c.RefType != secFontSz) || !numeric.Finite(c.Factor) || c.Factor <= 0 {
		return false
	}
	return c.Op == "lte" || (c.Op == "none" || c.Op == "equ") && c.Factor != 1
}

// solve sizes the groups in layout order, a group whose size is bounded
// by another's after that one; groups whose bounds refer to each other in
// a cycle are sized in layout order, each without the bounds not yet
// known.
func (z *sizer) solve() {
	for i := range z.sizes {
		z.sizes[i].group = z.find(i)
	}
	groups := map[int][]*textSize{}
	var order []int
	for _, t := range z.sizes {
		if _, ok := groups[t.group]; !ok {
			order = append(order, t.group)
		}
		groups[t.group] = append(groups[t.group], t)
	}
	for range order {
		next := -1
		for _, g := range order {
			if groups[g][0].done {
				continue
			}
			if next < 0 {
				next = g
			}
			if z.ready(groups[g]) {
				next = g
				break
			}
		}
		z.size(groups[next])
	}
}

// bounding returns the shapes a bound of t refers to: those its name
// names under its holder, outside t's group.
func (z *sizer) bounding(t *textSize, c sizeCap) []*textSize {
	var out []*textSize
	for _, m := range z.members[c.holder] {
		if z.step() {
			break
		}
		if u := z.sizes[m.i]; u.group != t.group && u.s.Name == c.name {
			out = append(out, u)
		}
	}
	return out
}

// ready reports whether every bound of the group's shapes refers to sized
// shapes.
func (z *sizer) ready(group []*textSize) bool {
	for _, t := range group {
		for _, c := range t.caps {
			for _, u := range z.bounding(t, c) {
				if !u.done {
					return false
				}
			}
		}
	}
	return true
}

// size finds and sets the size of one group.
func (z *sizer) size(all []*textSize) {
	var group []*textSize
	for _, t := range all {
		t.done = true
		if !t.unresolved {
			group = append(group, t)
		}
	}
	hi, lo := math.Inf(1), 0.0
	starts, floors := false, false
	for _, t := range group {
		if t.fixed {
			continue
		}
		if t.hasStart {
			hi, starts = min(hi, t.start), true
		}
		if t.hasFloor {
			lo, floors = max(lo, t.floor), true
		}
		for _, c := range t.caps {
			for _, u := range z.bounding(t, c) {
				if u.done && u.size > 0 {
					hi = min(hi, float64(c.factor*u.size))
				}
			}
		}
	}
	for _, t := range group {
		t.done = true
	}
	if !starts {
		for _, t := range group {
			if t.fixed {
				z.setFixed(t)
			}
		}
		return
	}
	hi = max(hi, minDiagramFontSize)
	if !floors || lo > hi {
		lo = hi
	}
	lo = max(lo, minDiagramFontSize)
	best := lo
	if z.fitsAt(group, hi) {
		best = hi
	} else {
		// The largest whole point in [lo, hi) at which every shape fits, by
		// halving: text that fits at a size fits at any smaller one.
		a, b := math.Ceil(lo), math.Ceil(hi)-1
		for a <= b {
			m := math.Floor((a + b) / 2)
			if z.fitsAt(group, m) {
				best, a = m, m+1
			} else {
				b = m - 1
			}
		}
	}
	for _, t := range group {
		if t.fixed {
			z.setFixed(t)
			continue
		}
		z.set(t, best)
	}
}

// fitsAt reports whether every shape of the group fits at size pt; false
// once the measurements run out.
func (z *sizer) fitsAt(group []*textSize, pt float64) bool {
	if z.fit == nil {
		return true
	}
	for _, t := range group {
		if t.fixed {
			continue
		}
		if z.fits >= maxDiagramFits {
			return false
		}
		z.fits++
		z.set(t, pt)
		if !z.fit(t.s) {
			return false
		}
	}
	return true
}

// set gives a shape the size pt and the insets its factors make of it,
// in whole EMU.
func (z *sizer) set(t *textSize, pt float64) {
	t.size = pt
	t.s.FontSize = pt
	t.s.Autofit = false
	z.insets(t, pt)
}

// setFixed gives the insets of text formatted by hand the factors of its
// first run's size, the size its runs keep.
func (z *sizer) setFixed(t *textSize) {
	t.s.Autofit = false
	t.s.FontSize = 0
	if pt := firstRunSize(t.s); pt > 0 {
		t.size = pt
		z.insets(t, pt)
	}
}

// insets sets the insets the factors of t make of the size pt.
func (z *sizer) insets(t *textSize, pt float64) {
	for i, f := range t.insets {
		if t.hasInset[i] {
			t.s.Insets[i] = int64(math.Round(float64(f*pt) * 12700))
			t.s.Inset[i] = true
		}
	}
}

// firstRunSize returns the size in points the first run of a shape's text
// states (21.1.2.3.9 rPr sz, in hundredths of a point), 0 for none.
func firstRunSize(s *DiagramShape) float64 {
	for _, p := range s.Paragraphs {
		for _, k := range p.Source.Kids() {
			if k.Space != NSA || k.Local != "r" {
				continue
			}
			v, ok := k.Child(NSA, "rPr").Attr("sz")
			if !ok {
				return 0
			}
			n, ok := Int(strings.TrimSpace(v))
			if !ok || n < 100 || n > maxDiagramFontSize*100 {
				return 0
			}
			return float64(n) / 100
		}
	}
	return 0
}
