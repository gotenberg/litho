package ooxml

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// officeCycle lays the kids out on a circle as Office does (ECMA-376
// Part 1, 21.4.2.1 alg cycle: "lays out child layout nodes around a circle
// or portion of a circle using equal angle spacing"; 21.4.7.49 stAng,
// spanAng, ctrShpMap) and reports whether it did:
//
//   - Each node takes the width and height the cycle's constraints set for
//     its name or point type (21.4.2.6 constr), then those its own layout
//     node sets from its other side (a circle's h refType w).
//   - Two neighbouring nodes stand sibSp apart along the chord between
//     their centres, measured between their outlines there ([cycleGap]),
//     and never closer than the radius at which their boxes just stop
//     overlapping ([cycleApart]).
//     With a first node in the centre (ctrShpMap fNode), a ring node
//     stands the centre node's half width, sp and its own half width from
//     the centre when that is farther.
//   - Nodes that turn along the path (rotPath alongPath) stand where
//     their boxes, upright, just stop overlapping, whatever sibSp, then
//     turn by their angle on the circle.
//   - The block, its connectors with it, then scales to the largest size
//     the box holds and sits in its middle.
//
// The standard gives neither the radius nor the scaling: those are read
// from Office's pages. A cycle whose constraints size no node keeps the
// quarter-box layout of [cycle]: no page shows one. Turned nodes at a
// sibSp other than 0 follow a rule the pages do not settle: three of them
// keep where their boxes touch at -0.35 and 0.5, twelve at 0.5 and sixteen
// at -0.1 stand where the gap rule of upright nodes puts them.
//
// # Corpus evidence
//
//   - libreoffice/pptx: smartart-cycle's cycle2, five circles of the
//     frame's width and sibSp half of it: Office prints them 96.48 points
//     wide on a circle of radius 123.3 (the chord rule: 1.5 x 96.48 /
//     (2 sin 36 degrees) = 123.1), the block 319.7 points tall in the 320
//     point frame, centred.
//   - libreoffice/pptx: smartart-multidirectional's cycle7, three round
//     rectangles half as tall as wide and sibSp 0.65 of the width: 165.6
//     by 82.8 points, 273.6 apart (1.65 x 165.6 = 273.2).
//   - libreoffice/pptx: smartart-center-cycle's radial3, a centre circle
//     and three of half its size, sp and sibSp -0.2 of the small one: the
//     centre 197.3 points wide, the ring 127.8 from it (0.65 x 197.3 =
//     128.2), the block 319.7 wide in the 320-point square of its
//     composite.
//   - probe, pptx: arrow5's three square arrows upright in a 480 by 320
//     point frame, sibSp -0.35 and 0 of the node width: both prints put
//     them 159.84 points wide, 0.667 widths from the centre, where the
//     boxes of two neighbours just touch (the chord rule gives 0.375 and
//     0.577), the block 319.7 points tall. Turned along the path, at
//     sibSp -0.35, 0 and 0.5, the same 0.667.
//   - probe, pptx: arrow5's four, six, eight, ten, twelve and sixteen
//     square arrows turned along the path at sibSp 0 stand 1.0, 1.157,
//     1.414, 2.017, 2.757 and 3.081 widths from the centre, where their
//     upright boxes just touch (1, 1.155, 1.414, 2, 2.732 and 3.086, set by
//     the neighbours 30 and 60 degrees round for twelve), the block 319.7
//     points tall; twelve at sibSp 0.5 stand about 3.73 widths out, and
//     libreoffice/pptx sd/smartart-autoTxRot's sixteen at sibSp -0.1 stand
//     2.83, where the boxes stand 0.5 and -0.1 widths apart along the line
//     of centres (3.70, 2.83).
//   - probe, pptx: arrow5's three upright arrows at sibSp 0.5 stand 0.959
//     widths out, three upright ellipses 0.869 ([cycleGap]).
func (e *engine) officeCycle(a *DiagramAtom, s *DiagramShape) bool {
	if s.W <= 0 || s.H <= 0 {
		return false
	}
	turned := param(a, "rotPath", "none") == "alongPath"

	own, _ := e.own(a.Node, s.pt)
	kinds := map[string]string{}
	var nodes []*DiagramShape
	for _, k := range s.Kids {
		if k.sub == "conn" || e.transition(k) {
			kinds[k.Name] = "sibTrans"
			continue
		}
		nodes = append(nodes, k)
		if _, ok := kinds[k.Name]; !ok {
			kinds[k.Name] = "node"
		}
	}
	if len(nodes) == 0 {
		return false
	}
	props := e.cycleLengths(own, kinds, s)
	sized := false
	for _, k := range nodes {
		if _, ok := props[k.Name]["w"]; ok {
			sized = true
		}
	}
	if !sized {
		return false
	}
	ring := nodes
	var centre *DiagramShape
	if param(a, "ctrShpMap", "none") == "fNode" {
		centre, ring = nodes[0], nodes[1:]
	}
	type box struct{ x, y, w, h float64 }
	at := map[*DiagramShape]box{}
	sizeOf := func(k *DiagramShape) (float64, float64) {
		return e.nodeLengths(k, props[k.Name], s)
	}
	var nodeW, nodeH, centreW float64
	for _, k := range ring {
		w, h := sizeOf(k)
		nodeW, nodeH = max(nodeW, w), max(nodeH, h)
	}
	if centre != nil {
		w, h := sizeOf(centre)
		centreW = w
		at[centre] = box{-w / 2, -h / 2, w, h}
	}
	stAng := float64(intParam(a, "stAng", 0))
	spanAng := float64(intParam(a, "spanAng", 360))
	n := len(ring)
	step := 0.0
	if n > 0 {
		step = spanAng / float64(n)
	}
	var radius float64
	if !turned {
		radius = e.cycleGap(ring, sizeOf, stAng, step, props[""]["sibSp"])
	}
	radius = max(radius, e.cycleApart(ring, sizeOf, stAng, step))
	if centre != nil && n > 0 {
		radius = max(radius, float64(centreW/2)+props[""]["sp"]+float64(nodeW/2))
	}
	if !numeric.Finite(radius) || radius < 0 {
		radius = 0
	}
	for i, k := range ring {
		w, h := sizeOf(k)
		rad := float64(stAng+float64(float64(i)*step)) * math.Pi / 180
		cx, cy := float64(radius*detmath.Sin(rad)), -float64(radius*detmath.Cos(rad))
		at[k] = box{cx - float64(w/2), cy - float64(h/2), w, h}
	}
	if turned {
		for i, k := range ring {
			k.Rot = trunc(float64(stAng+float64(float64(i)*step)) * perDegree)
		}
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, b := range at {
		minX, minY = min(minX, b.x), min(minY, b.y)
		maxX, maxY = max(maxX, b.x+b.w), max(maxY, b.y+b.h)
	}
	bw, bh := maxX-minX, maxY-minY
	if !(bw > 0) || !(bh > 0) || !numeric.Finite(bw) || !numeric.Finite(bh) {
		return false
	}
	sc := min(float64(s.W)/bw, float64(s.H)/bh)
	ox := (float64(s.W) - float64(bw*sc)) / 2
	oy := (float64(s.H) - float64(bh*sc)) / 2
	place := func(k *DiagramShape, b box) {
		k.X = trunc(ox + float64((b.x-minX)*sc))
		k.Y = trunc(oy + float64((b.y-minY)*sc))
		k.W, k.H = trunc(float64(b.w*sc)), trunc(float64(b.h*sc))
	}
	for _, k := range nodes {
		place(k, at[k])
	}
	// A connector sits halfway along the chord to the next node, its
	// arrow along the way the cycle runs.
	connW, connH := nodeW/3, nodeH/3
	connAngle := 0.0
	if spanAng <= 0 {
		connAngle = 180
	}
	next := 0
	for _, k := range s.Kids {
		if kinds[k.Name] != "sibTrans" {
			if k != centre {
				next++
			}
			continue
		}
		w, h := connW, connH
		if v, ok := props[k.Name]["w"]; ok {
			w = v
		}
		if v, ok := props[k.Name]["h"]; ok {
			h = v
		}
		ang := stAng + float64(float64(next)*step) - float64(step/2)
		rad := ang * math.Pi / 180
		r := float64(radius * detmath.Cos(math.Abs(step)/2*math.Pi/180))
		cx, cy := float64(r*detmath.Sin(rad)), -float64(r*detmath.Cos(rad))
		place(k, box{cx - float64(w/2), cy - float64(h/2), w, h})
		k.Rot = trunc((connAngle + ang) * perDegree)
	}
	return true
}

// cycleGap returns the radius at which every two neighbouring ring nodes,
// each taken upright at its angle, stand sibSp apart along the line
// between their centres, measured between their outlines on that line:
// an ellipse's own outline, the box of any other shape. The last node
// neighbours the first on a full circle. One pass over the ring.
//
// # Corpus evidence
//
//   - probe, pptx: arrow5's three upright down arrows at sibSp 0.5 of the
//     node width stand 0.959 widths from the centre, their boxes 0.5 widths
//     apart along the line of centres (0.955); three upright ellipses at
//     0.5 stand 0.869 widths out, the circles 0.5 apart (0.866).
func (e *engine) cycleGap(ring []*DiagramShape, sizeOf func(*DiagramShape) (float64, float64), stAng, step, sibSp float64) float64 {
	n := len(ring)
	if n < 2 {
		return 0
	}
	pairs := n - 1
	if math.Abs(float64(step*float64(n))) >= 360 {
		pairs = n
	}
	reach := func(k *DiagramShape, c, s float64) float64 {
		w, h := sizeOf(k)
		a, b := w/2, h/2
		if !(a > 0) || !(b > 0) {
			return 0
		}
		if k.Preset() == "ellipse" {
			return 1 / math.Sqrt(float64(c*c/float64(a*a))+float64(s*s/float64(b*b)))
		}
		r := math.Inf(1)
		if c > apartEpsilon {
			r = a / c
		}
		if s > apartEpsilon {
			r = min(r, b/s)
		}
		return r
	}
	var r float64
	for i := range pairs {
		if e.step() {
			break
		}
		j := (i + 1) % n
		ai := float64(stAng+float64(float64(i)*step)) * math.Pi / 180
		aj := float64(stAng+float64(float64(j)*step)) * math.Pi / 180
		dx := detmath.Sin(aj) - detmath.Sin(ai)
		dy := detmath.Cos(ai) - detmath.Cos(aj)
		d := detmath.Hypot(dx, dy)
		if !(d > apartEpsilon) {
			continue
		}
		c, s := math.Abs(dx/d), math.Abs(dy/d)
		need := float64(reach(ring[i], c, s)+reach(ring[j], c, s)+sibSp) / d
		if numeric.Finite(need) {
			r = max(r, need)
		}
	}
	return r
}

// cycleApart returns the smallest radius at which no two neighbouring
// ring nodes overlap, each taken upright at its angle: for neighbours whose
// centres lie dx and dy apart on a unit circle, the smaller of the radii
// that part them across, their half widths summed over dx, or down, their
// half heights over dy. The last node neighbours the first on a full
// circle. One pass over the ring.
func (e *engine) cycleApart(ring []*DiagramShape, sizeOf func(*DiagramShape) (float64, float64), stAng, step float64) float64 {
	n := len(ring)
	if n < 2 {
		return 0
	}
	pairs := n - 1
	if math.Abs(float64(step*float64(n))) >= 360 {
		pairs = n
	}
	var r float64
	for i := range pairs {
		if e.step() {
			break
		}
		j := (i + 1) % n
		wi, hi := sizeOf(ring[i])
		wj, hj := sizeOf(ring[j])
		ai := float64(stAng+float64(float64(i)*step)) * math.Pi / 180
		aj := float64(stAng+float64(float64(j)*step)) * math.Pi / 180
		dx := math.Abs(detmath.Sin(ai) - detmath.Sin(aj))
		dy := math.Abs(detmath.Cos(ai) - detmath.Cos(aj))
		need := math.Inf(1)
		if dx > apartEpsilon {
			need = float64(wi+wj) / 2 / dx
		}
		if dy > apartEpsilon {
			need = min(need, float64(hi+hj)/2/dy)
		}
		if numeric.Finite(need) {
			r = max(r, need)
		}
	}
	return r
}

// apartEpsilon is the distance on a unit circle below which two node
// centres count as level across or down, so that only the other direction
// can part them.
const apartEpsilon = 1e-9

// cycleLengths resolves the lengths a cycle's constraints set (21.4.2.6
// constr): the width and height of each kid by name or point type, a
// factor of the cycle's box, of a length of another kid or a value in
// millimetres; sp and sibSp, which may be negative, under "". A length that
// refers to another resolves once that one has, in at most maxSnakePasses
// passes.
func (e *engine) cycleLengths(cons []DiagramConstraint, kinds map[string]string, s *DiagramShape) map[string]map[string]float64 {
	props := map[string]map[string]float64{"": {"w": float64(s.W), "h": float64(s.H)}}
	targets := func(c DiagramConstraint) []string {
		switch {
		case c.Type == "sp" || c.Type == "sibSp":
			if c.For == "" || c.For == "self" || c.For == "none" {
				return []string{""}
			}
			return nil
		case c.For != "ch":
			return nil
		case c.ForName != "":
			return []string{c.ForName}
		case c.PtType != "" && c.PtType != "all":
			var out []string
			for _, name := range sortedKeys(kinds) {
				if kinds[name] == c.PtType || (c.PtType == "nonAsst" && kinds[name] == "node") {
					out = append(out, name)
				}
			}
			return out
		}
		return nil
	}
	refOf := func(c DiagramConstraint) (float64, bool) {
		if c.RefType == "none" {
			if !c.HasValue || c.Unbounded {
				return 0, false
			}
			return float64(c.Value * 36000), true
		}
		name := ""
		if c.RefFor == "ch" {
			name = c.RefForName
			if name == "" {
				for _, n := range sortedKeys(kinds) {
					if kinds[n] == c.RefPtType {
						name = n
						break
					}
				}
			}
			if name == "" {
				return 0, false
			}
		}
		v, ok := props[name][c.RefType]
		if !ok || !numeric.Finite(c.Factor) {
			return 0, false
		}
		return float64(v * c.Factor), true
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
			v, ok := refOf(c)
			if !ok || !numeric.Finite(v) || (v < 0 && c.Type != "sp" && c.Type != "sibSp") {
				continue
			}
			for _, name := range targets(c) {
				m := props[name]
				if m == nil {
					m = map[string]float64{}
					props[name] = m
				}
				if _, ok := m[c.Type]; !ok {
					m[c.Type] = v
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return props
}
