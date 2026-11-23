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
//   - Two neighbouring nodes stand their width plus sibSp apart, centre to
//     centre, along the chord between them. With a first node in the
//     centre (ctrShpMap fNode), a ring node stands the centre node's half
//     width, sp and its own half width from the centre when that is
//     farther.
//   - The block, its connectors with it, then scales to the largest size
//     the box holds and sits in its middle.
//
// The standard gives neither the radius nor the scaling: those are read
// from Office's pages. A cycle whose constraints size no node, or whose
// nodes turn along the path (rotPath alongPath), keeps the quarter-box
// layout of [cycle]: Office's radius for turned nodes follows neither
// rule above, and no page shows a cycle of unsized nodes.
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
func (e *engine) officeCycle(a *DiagramAtom, s *DiagramShape) bool {
	if param(a, "rotPath", "none") == "alongPath" || s.W <= 0 || s.H <= 0 {
		return false
	}
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
	if n > 1 {
		half := math.Abs(step) / 2 * math.Pi / 180
		if sin := detmath.Sin(half); sin > 0 {
			radius = (nodeW + props[""]["sibSp"]) / float64(2*sin)
		}
	}
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
