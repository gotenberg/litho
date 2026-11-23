package ooxml

import (
	"maps"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/detmath"
)

// The algorithms of a layout node (ECMA-376 Part 1, 21.4.2.1 alg and
// 21.4.7.1 ST_AlgorithmType), as LibreOffice's AlgAtom::layoutShape
// computes them, in EMU truncated to integers where it truncates. The
// layout box of a shape is relative to its parent's.

// perDegree is the unit of a DrawingML angle, 60000ths of a degree.
const perDegree = 60000

// param returns an algorithm parameter (21.4.2.17 param), def when
// absent.
func param(a *DiagramAtom, typ, def string) string {
	if v, ok := a.Params[typ]; ok {
		return v
	}
	return def
}

// intParam returns an integer algorithm parameter, def when absent or
// not an integer. It reads 64 bits whatever the width of int, so a value
// past 2^31 means the same on every platform.
func intParam(a *DiagramAtom, typ string, def int64) int64 {
	v, ok := a.Params[typ]
	if !ok {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

// trunc converts to an integer EMU as C++ converts a double to an int.
func trunc(f float64) int64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return int64(max(-1<<53, min(1<<53, f)))
}

// verticalShapesCount returns how many shapes a shape stacks vertically
// for the hierarchy algorithms: 1 for a leaf (0 for a connector), the
// kids' sum along a vertical direction, else their maximum.
func verticalShapesCount(a *DiagramAtom, s *DiagramShape) int64 {
	if len(s.Kids) == 0 {
		if s.sub != "conn" {
			return 1
		}
		return 0
	}
	dir := "fromL"
	if a.Alg == "hierRoot" {
		dir = "fromT"
	} else if v, ok := a.Params["linDir"]; ok {
		dir = v
	}
	sec := param(a, "secLinDir", "")
	var n int64
	switch {
	case dir == "fromT" || dir == "fromB":
		for _, k := range s.Kids {
			n += k.vcount
		}
	case (dir == "fromL" || dir == "fromR") && sec == "fromT":
		for _, k := range s.Kids {
			n += k.vcount
		}
		n = (n + 1) / 2
	default:
		for _, k := range s.Kids {
			n = max(n, k.vcount)
		}
	}
	return n
}

// layoutShape runs an algorithm over a shape: it places the shape's kids,
// or for conn and tx the shape itself.
func (e *engine) layoutShape(a *DiagramAtom, s *DiagramShape, cons []DiagramConstraint, rules []string) {
	if a.Alg != "lin" {
		s.Kids = dropEmptyGroups(s.Kids)
	}
	switch a.Alg {
	case "composite":
		e.composite(a, s, cons)
	case "conn":
		connAlg(a, s)
	case "cycle":
		if !e.officeCycle(a, s) {
			cycle(a, s)
		}
	case "hierChild", "hierRoot":
		if !e.hierDone[s] && !e.hierTree(a, s, cons) {
			hierarchy(a, s)
		}
	case "lin":
		e.linear(a, s, cons, rules)
	case "pyra":
		pyramid(s)
	case "snake":
		e.snake(a, s, cons)
	case "sp":
		s.Paragraphs = nil
	case "tx":
		e.textAlg(a, s, cons)
	}
}

// layoutProps holds the layout properties constraints set, by layout node
// name ("" for the parent) and property.
type layoutProps map[string]map[string]int64

// set sets a property, making its map.
func (lp layoutProps) set(name, prop string, v int64) {
	m := lp[name]
	if m == nil {
		m = map[string]int64{}
		lp[name] = m
	}
	m[prop] = v
}

// get returns a property and whether it is set.
func (lp layoutProps) get(name, prop string) (int64, bool) {
	v, ok := lp[name][prop]
	return v, ok
}

// composite places each kid by the constraints on its layout node's name
// (21.4.2.1 composite): left, top, width, height, the centres and the far
// edges, each a factor of a reference property, a value in millimetres or
// a factor of a font size read in millimetres, then centres the kids
// vertically in the free height. An equality of no value sets nothing.
//
// Kids sized from a font size that do not fit the composite's box bound
// that size: the lengths are linear in the size, so the size falls by the
// share that brings the kids' far edges back to the box, in whole points
// (see [engine.capFont]).
//
// # Corpus evidence
//
//   - libreoffice/pptx: smartart-dir's hList1 stacks a parTx 0.8 and a desTx
//     1.22 of the primary size tall in a 182.5-point composite: they fit up
//     to 31.87 points, and Office prints both texts at 31 points, the
//     boxes 177.5 points tall in all.
//   - libreoffice/pptx: smartart-tdf134221's venn2 stands its two smaller
//     circles on the bottom of the largest, centred on it, which no
//     constraint places but the default top left; PowerPoint's drawing
//     saves them at 63.5 and 127.1 points down, where the rule puts them.
func (e *engine) composite(a *DiagramAtom, s *DiagramShape, cons []DiagramConstraint) {
	run := &compositeRun{e: e, a: a, s: s, cons: cons, keys: map[string]bool{}}
	// A bound (lte) met at one size and not at the next makes the far edges
	// piecewise linear: the share is taken again from the size it gave.
	for range maxCompositeFits {
		run.scale = 1
		run.lay(false)
		if len(run.keys) == 0 {
			break
		}
		r1, b1 := run.right, run.bottom
		run.scale = 0
		run.lay(false)
		r0, b0 := run.right, run.bottom
		f := 1.0
		if b1 > float64(s.H) && b1 > b0 {
			f = min(f, (float64(s.H)-b0)/(b1-b0))
		}
		if r1 > float64(s.W) && r1 > r0 {
			f = min(f, (float64(s.W)-r0)/(r1-r0))
		}
		if f >= 1 {
			break
		}
		for _, key := range sortedKeys(run.keys) {
			e.capFont(key, cons, max(f, 0))
		}
	}
	run.scale = 1
	run.lay(true)
}

// maxCompositeFits caps the times a composite takes the share of a font
// size its kids leave room for. Anti-DoS guard, not spec-mandated: a share
// is taken again only where a bound starts or stops holding, once for
// each bound of the definitions PowerPoint offers (hList1's parTx, at
// most 0.4 of its width tall).
const maxCompositeFits = 4

// compositeRun is one evaluation of a composite's constraints: at scale 1
// with every font-sized length at its size, at 0 without them. It records
// the font sizes the lengths refer to and the far edges of the kids before
// they are cut to the box. It does not outlive the composite's layout.
type compositeRun struct {
	e             *engine
	a             *DiagramAtom
	s             *DiagramShape
	cons          []DiagramConstraint
	scale         float64
	keys          map[string]bool
	right, bottom float64
}

// lay evaluates the constraints and, when place is set, gives the kids
// their boxes.
func (r *compositeRun) lay(place bool) {
	a, s, cons := r.a, r.s, r.cons
	r.right, r.bottom = 0, 0
	props := layoutProps{}
	var xOff int64
	vmin, vmax := int64(math.MaxInt32), int64(0)
	if a.Ratio != 1 {
		props.set("", "w", s.W)
		props.set("", "h", s.H)
		props.set("", "l", 0)
		props.set("", "t", 0)
		props.set("", "r", s.W)
		props.set("", "b", s.H)
	} else {
		w := min(s.W, s.H)
		props.set("", "w", w)
		props.set("", "h", s.H)
		if w < s.W {
			xOff = (s.W - w) / 2
		}
		props.set("", "l", xOff)
		props.set("", "t", 0)
		props.set("", "r", s.W-xOff)
		props.set("", "b", s.H)
	}
	for _, c := range cons {
		r.apply(c, props)
	}
	type box struct{ x, y, w, h int64 }
	boxes := make([]box, len(s.Kids))
	users := make([]map[string]int64, len(s.Kids))
	var bounds []DiagramConstraint
	own, _ := r.e.own(a.Node, s.pt)
	// The composite's own lte and gte constraints bound the lengths the
	// equalities set (21.4.7.10 ST_BoolOperator), a kid's that lays out
	// no shape of its own included: what refers to it reads the bound.
	for _, c := range own {
		if c.ForName != "" && (c.Op == "lte" || c.Op == "gte") && geometric[c.Type] {
			holdToBound(c, props)
		}
	}
	for i, k := range s.Kids {
		for _, c := range cons {
			if c.ForName == k.Name {
				r.apply(c, props)
			}
		}
		for _, c := range own {
			if c.ForName == k.Name && (c.Op == "lte" || c.Op == "gte") && geometric[c.Type] {
				bounds = append(bounds, c)
			}
		}
		for _, n := range a.Node.Kids {
			if n.Kind != AtomLayoutNode || n.Name != k.Name {
				continue
			}
			for _, ca := range n.Kids {
				if ca.Kind != AtomConstraint {
					continue
				}
				c := ca.Constraint
				if c.ForName != "" || c.RefForName != "" || (c.Value == 0 && c.RefType == "none") {
					continue
				}
				c.ForName, c.RefForName = n.Name, n.Name
				if c.Op == "lte" || c.Op == "gte" {
					bounds = append(bounds, c)
					continue
				}
				r.apply(c, props)
			}
		}
		// A kid's own lte or gte constraint bounds a length the others set
		// (21.4.7.10 ST_BoolOperator) and sets none.
		for _, c := range bounds {
			holdToBound(c, props)
		}
		bounds = bounds[:0]
		w, h := s.W, s.H
		var x, y int64
		if p, ok := props[k.Name]; ok {
			// A length is cut to the box before it places the kid; the far
			// edges that bound a font size take it whole.
			rw, rh := w, h
			if v, ok := p["w"]; ok {
				w, rw = min(v, s.W), v
			}
			if v, ok := p["h"]; ok {
				h, rh = min(v, s.H), v
			}
			if v, ok := p["l"]; ok {
				x = v
			} else if v, ok := p["ctrX"]; ok {
				x = v - w/2
			} else if v, ok := p["r"]; ok {
				x = v - w
			}
			if v, ok := p["t"]; ok {
				y = v
			} else if v, ok := p["ctrY"]; ok {
				y = v - h/2
			} else if v, ok := p["b"]; ok {
				y = v - h
			}
			if l, ok := p["l"]; ok {
				if rr, ok := p["r"]; ok {
					w = rr - l
				}
			}
			if t, ok := p["t"]; ok {
				if b, ok := p["b"]; ok {
					h = b - t
				}
			}
			// The offsets move an edge, a centre or a length the others set
			// (21.4.7.21 ST_ConstraintType lOff, rOff, tOff, bOff, ctrXOff,
			// ctrYOff, wOff, hOff).
			for _, o := range sortedKeys(p) {
				v := p[o]
				switch o {
				case "lOff":
					x, w = x+v, w-v
				case "rOff", "wOff":
					w += v
				case "tOff":
					y, h = y+v, h-v
				case "bOff", "hOff":
					h += v
				case "ctrXOff":
					x += v
				case "ctrYOff":
					y += v
				}
			}
			r.right = max(r.right, float64(x+max(w, rw)))
			r.bottom = max(r.bottom, float64(y+max(h, rh)))
			// The box a kid takes is what a later kid's constraints refer
			// to, its edges the constraints left unstated included.
			for prop, v := range map[string]int64{"l": x, "t": y, "w": w, "h": h, "r": x + w, "b": y + h, "ctrX": x + w/2, "ctrY": y + h/2} {
				if _, ok := p[prop]; !ok {
					p[prop] = v
				}
			}
			x += xOff
			w = min(w, s.W-x)
			h = min(h, s.H-y)
		}
		boxes[i] = box{x, y, w, h}
		for _, prop := range sortedKeys(props[k.Name]) {
			if strings.HasPrefix(prop, "user") {
				if users[i] == nil {
					users[i] = map[string]int64{}
				}
				users[i][prop] = props[k.Name][prop]
			}
		}
		vmin = min(vmin, y)
		vmax = max(vmax, y+h)
	}
	if !place {
		return
	}
	for i, k := range s.Kids {
		k.W, k.H, k.X, k.Y = boxes[i].w, boxes[i].h, boxes[i].x, boxes[i].y
		k.user = users[i]
		customSize(k)
	}
	ph, _ := props.get("", "h")
	if vmin < 0 || vmin > vmax || vmax > ph {
		return
	}
	if diff := ph - (vmax - vmin); diff > 0 {
		for _, k := range s.Kids {
			k.Y += diff / 2
		}
	}
}

// apply applies one constraint of a composite: a length that refers to a
// font size is its factor of that size read in millimetres (see
// [engine.fontLength]), times the run's scale; an equality of no value
// sets nothing (21.4.7.10 ST_BoolOperator equ).
func (r *compositeRun) apply(c DiagramConstraint, props layoutProps) {
	if c.ForName == "" || (c.Op == "equ" && !c.HasValue && c.RefType == "none") {
		return
	}
	if v, key, ok := r.e.fontLength(c, r.cons); ok {
		r.keys[key] = true
		props.set(c.ForName, c.Type, trunc(float64(v*r.scale)))
		return
	}
	applyConstraint(c, props)
}

// applyConstraint applies one constraint to the layout properties: its
// reference property times its factor, an edge, centre or length inferred
// from the others the reference states (see [edge]), else its value in
// millimetres (points for a font size reference); a reference to an edge
// or length not known sets nothing.
func applyConstraint(c DiagramConstraint, props layoutProps) {
	if c.ForName == "" {
		return
	}
	ref, ok := props[c.RefForName]
	if !ok {
		return
	}
	if v, ok := ref[c.RefType]; ok {
		props.set(c.ForName, c.Type, trunc(float64(v)*c.Factor))
		return
	}
	if v, ok := edge(ref, c.RefType); ok {
		props.set(c.ForName, c.Type, trunc(float64(v)*c.Factor))
		return
	}
	if geometric[c.RefType] {
		// A length or edge it refers to that is not known sets nothing.
		return
	}
	unit := 36000.0
	if c.RefType == "primFontSz" || c.RefType == "secFontSz" {
		unit = 12700
	}
	props.set(c.ForName, c.Type, trunc(c.Value*unit))
}

// geometric lists the constraint types that are an edge, a centre or a
// length of a box (21.4.7.21 ST_ConstraintType).
var geometric = map[string]bool{"l": true, "t": true, "r": true, "b": true, "w": true, "h": true, "ctrX": true, "ctrY": true}

// edge returns an edge, centre or length of a box that its constraints do
// not state but the others they state make: the right edge is the left
// one plus the width, or the centre plus half of it, and so on across and
// down (21.4.7.21 ST_ConstraintType l, r, ctrX, w; t, b, ctrY, h).
func edge(p map[string]int64, typ string) (int64, bool) {
	axes := [2][4]string{{"l", "r", "ctrX", "w"}, {"t", "b", "ctrY", "h"}}
	for _, ax := range axes {
		near, far, ctr, length := ax[0], ax[1], ax[2], ax[3]
		n, okN := p[near]
		f, okF := p[far]
		c, okC := p[ctr]
		w, okW := p[length]
		switch typ {
		case near:
			switch {
			case okF && okW:
				return f - w, true
			case okC && okW:
				return c - w/2, true
			case okC && okF:
				return 2*c - f, true
			}
		case far:
			switch {
			case okN && okW:
				return n + w, true
			case okC && okW:
				return c + w/2, true
			case okC && okN:
				return 2*c - n, true
			}
		case ctr:
			switch {
			case okN && okW:
				return n + w/2, true
			case okF && okW:
				return f - w/2, true
			case okN && okF:
				return (n + f) / 2, true
			}
		case length:
			switch {
			case okN && okF:
				return f - n, true
			case okC && okN:
				return 2 * (c - n), true
			case okC && okF:
				return 2 * (f - c), true
			}
		}
	}
	return 0, false
}

// connAlg turns a connector shape into the arrow its parameters name
// (begSty, endSty; a bent connector draws nothing) and sizes it by its
// layout node's constraints about its centre.
func connAlg(a *DiagramAtom, s *DiagramShape) {
	if s.sub == "conn" {
		typ := "rightArrow"
		switch beg, end := param(a, "begSty", ""), param(a, "endSty", ""); {
		case param(a, "connRout", "") == "bend":
			typ = ""
		case beg == "arr" && end == "arr":
			typ = "leftRightArrow"
		case beg == "arr":
			typ = "leftArrow"
		}
		s.sub = typ
		if s.tmpl != nil {
			s.tmpl.preset = typ
		}
	}
	props := layoutProps{}
	props.set("", "w", s.W)
	props.set("", "h", s.H)
	props.set("", "l", 0)
	props.set("", "t", 0)
	props.set("", "r", s.W)
	props.set("", "b", s.H)
	for _, ca := range a.Node.Kids {
		if ca.Kind != AtomConstraint {
			continue
		}
		c := ca.Constraint
		if (c.Op != "none" && c.Op != "equ") || c.Type == "none" {
			continue
		}
		if ref, ok := props[c.RefForName]; ok {
			if v, ok := ref[c.RefType]; ok {
				props.set(c.ForName, c.Type, trunc(float64(v)*c.Factor))
			}
		}
	}
	w, _ := props.get("", "w")
	h, _ := props.get("", "h")
	s.X += (s.W - w) / 2
	s.Y += (s.H - h) / 2
	s.W, s.H = w, h
}

// cycle places the kids on a circle (21.4.2.1 cycle): a quarter of the
// box each, from stAng over spanAng, the first in the centre for
// ctrShpMap fNode, connectors a twelfth of the box on a smaller radius,
// turned along the path.
func cycle(a *DiagramAtom, s *DiagramShape) {
	if len(s.Kids) == 0 {
		return
	}
	stAng := intParam(a, "stAng", 0)
	spanAng := intParam(a, "spanAng", 360)
	rotPath := param(a, "rotPath", "none")
	ctr := param(a, "ctrShpMap", "none")
	cw, ch := s.W/2, s.H/2
	kw, kh := s.W/4, s.H/4
	connW, connH := s.W/12, s.H/12
	radius := min((s.W-kw)/2, (s.H-kh)/2)
	kids := append([]*DiagramShape(nil), s.Kids...)
	if ctr == "fNode" {
		c := kids[0]
		kids = kids[1:]
		c.X, c.Y, c.W, c.H = cw-kw/2, ch-kh/2, kw, kh
	}
	n := len(kids)
	if n == 0 {
		return
	}
	connRadius := trunc(float64(radius) * detmath.Cos(float64(spanAng/int64(n))*math.Pi/180))
	connAngle := 0
	if spanAng <= 0 {
		connAngle = 180
	}
	for i, k := range kids {
		ang := float64(i)*float64(spanAng)/float64(n) + float64(stAng)
		w, h, r := kw, kh, radius
		if k.sub == "conn" {
			w, h, r = connW, connH, connRadius
		}
		rad := ang * math.Pi / 180
		k.X = trunc(float64(cw) + float64(float64(r)*detmath.Sin(rad)) - float64(w/2))
		k.Y = trunc(float64(ch) - float64(float64(r)*detmath.Cos(rad)) - float64(h/2))
		k.W, k.H = w, h
		if rotPath == "alongPath" {
			k.Rot = trunc(ang * perDegree)
		}
		if k.sub == "conn" {
			k.Rot = trunc((float64(connAngle) + ang) * perDegree)
		}
	}
}

// containsType reports a shape presenting, or holding one presenting, a
// data point of a type.
func containsType(s *DiagramShape, typ string, depth int) bool {
	if s.DataType == typ {
		return true
	}
	if depth >= maxDiagramDepth {
		return false
	}
	for _, k := range s.Kids {
		if containsType(k, typ, depth+1) {
			return true
		}
	}
	return false
}

// hierarchy places the kids of an organisation chart's levels
// (21.4.2.1 hierRoot, the manager above the employees, and hierChild,
// the employees side by side): equal shares along the direction with a
// tenth of a share between them across and three tenths down, a kid as
// tall as the shapes it stacks; connectors one EMU wide.
func hierarchy(a *DiagramAtom, s *DiagramShape) {
	if len(s.Kids) == 0 || s.W == 0 || s.H == 0 {
		return
	}
	dir := "fromL"
	if a.Alg == "hierRoot" {
		dir = "fromT"
	} else if v, ok := a.Params["linDir"]; ok {
		dir = v
	}
	sec := param(a, "secLinDir", "")
	count := int64(len(s.Kids))
	if a.Alg == "hierChild" {
		count = 0
		for _, k := range s.Kids {
			if k.sub != "conn" {
				count++
			}
		}
	}
	const spaceW, spaceH = 0.1, 0.3
	if a.Alg == "hierRoot" && count == 3 {
		k := s.Kids
		if !containsType(k[1], "asst", 0) && containsType(k[2], "asst", 0) {
			k[1], k[2] = k[2], k[1]
		}
	}
	horiz := int64(1)
	if sec == "fromT" {
		horiz = 2
	} else if dir == "fromL" || dir == "fromR" {
		horiz = count
	}
	vc := float64(s.vcount)
	cw := trunc(float64(s.W) / (float64(horiz) + float64(float64(horiz-1)*spaceW)))
	chh := trunc(float64(s.H) / (vc + float64((vc-1)*spaceH)))
	connW, connH := int64(1), chh
	var x, y int64
	if a.Alg == "hierChild" && horiz == 1 {
		const indent = 0.1
		x = trunc(float64(cw) * indent)
		cw = trunc(float64(cw) * (1 - 2*indent))
	}
	var rowH int64
	for i, k := range s.Kids {
		k.X, k.Y = x, y
		if a.Alg == "hierChild" && k.sub == "conn" {
			k.W, k.H = connW, connH
			continue
		}
		kv := float64(k.vcount)
		w, h := cw, trunc(float64(chh)*(kv+float64((kv-1)*spaceH)))
		k.W, k.H = w, h
		if dir == "fromT" || dir == "fromB" {
			y = trunc(float64(y) + float64(h) + float64(float64(chh)*spaceH))
		} else {
			x = trunc(float64(x) + float64(w) + float64(float64(w)*spaceW))
		}
		rowH = max(rowH, h)
		if sec == "fromT" && i%2 == 1 {
			x = 0
			y = trunc(float64(y) + float64(rowH) + float64(float64(chh)*spaceH))
			rowH = 0
		}
	}
}

// linear places the kids in a row or a column (21.4.2.1 lin, linDir):
// equal shares of the box, or the widths and heights the constraints on
// their names give, scaled down to fit, with the spacing the sp, space or
// sibTrans constraint gives; a kid centred across the direction; the
// rules say which kids shrink. A child order "t" reverses the stacking.
func (e *engine) linear(a *DiagramAtom, s *DiagramShape, cons []DiagramConstraint, rules []string) {
	if len(s.Kids) == 0 || s.W == 0 || s.H == 0 {
		return
	}
	dir := param(a, "linDir", "fromL")
	incX, incY := int64(0), int64(0)
	switch dir {
	case "fromL":
		incX = 1
	case "fromR":
		incX = -1
	case "fromT":
		incY = 1
	case "fromB":
		incY = -1
	}
	horizontal := dir == "fromL" || dir == "fromR"
	vertical := dir == "fromT" || dir == "fromB"
	count := float64(len(s.Kids))
	connAngle := map[string]int64{"fromL": 0, "fromR": 180, "fromT": 270, "fromB": 90}[dir]
	var spaceW, spaceH int64
	along, across := "w", "h"
	if vertical {
		along, across = "h", "w"
	}
	fl := e.fontLengths(s, cons, along, across)
	props := layoutProps{}
	for _, c := range cons {
		if c.ForName == "" {
			continue
		}
		if props[c.ForName] == nil {
			props[c.ForName] = map[string]int64{}
		}
		if !setsLength(c) {
			continue
		}
		if v, ok := s.user[c.RefType]; ok && c.RefForName == "" && (c.Type == "w" || c.Type == "h") {
			// A length that refers to a user-defined variable the
			// enclosing composite set on this layout (21.4.7.21 userA to
			// userZ) is its factor of it.
			props.set(c.ForName, c.Type, trunc(float64(v)*c.Factor))
			continue
		}
		if c.Type == "w" {
			props.set(c.ForName, "w", min(e.lengthOf(c, cons, fl, s.W), s.W))
		}
		if c.Type == "h" {
			props.set(c.ForName, "h", min(e.lengthOf(c, cons, fl, s.H), s.H))
		}
		if c.ForName == "sp" || c.ForName == "space" || c.ForName == "sibTrans" {
			if c.Type == "w" {
				spaceW = trunc(float64(s.W) * c.Factor)
			}
			if c.Type == "h" {
				spaceH = trunc(float64(s.H) * c.Factor)
			}
		}
	}
	shrink := map[string]bool{}
	for _, r := range rules {
		shrink[r] = true
	}
	if vertical {
		shrink = map[string]bool{}
	}
	var fitted map[*DiagramShape]int64
	if len(shrink) > 0 {
		deps := map[string]bool{}
		depFactor := map[string]float64{}
		for _, k := range s.Kids {
			if shrink[k.Name] || count <= 1 {
				continue
			}
			count--
			isDep := false
			factor := 0.0
			for _, c := range cons {
				if c.ForName != k.Name {
					continue
				}
				if horizontal && c.Type != "w" {
					continue
				}
				if horizontal && c.Type == "w" {
					factor = c.Factor
				}
				if vertical && c.Type != "h" {
					continue
				}
				if vertical && c.Type == "h" {
					factor = c.Factor
				}
				if !shrink[c.RefForName] {
					continue
				}
				count += c.Factor
				deps[k.Name] = true
				depFactor[k.Name] = c.Factor
				isDep = true
				break
			}
			if !isDep && k.Group {
				scale := false
				if horizontal {
					v, ok := props.get(k.Name, "w")
					scale = ok && v > 0
				}
				if !scale && vertical {
					v, ok := props.get(k.Name, "h")
					scale = ok && v > 0
				}
				if scale && len(k.Kids) == 0 {
					count += factor
					deps[k.Name] = true
				}
			}
		}
		for n := range deps {
			shrink[n] = true
		}
		spaceW, spaceH = 0, 0
		if horizontal {
			// Only a rule that shrinks a width shares the row (21.4.2.24).
			ruled := map[string]bool{}
			_, own := e.own(a.Node, s.pt)
			for _, r := range own {
				if r.Type == "w" && r.ForName != "" && r.Value == 0 {
					ruled[r.ForName] = true
				}
			}
			fitted = shrinkToFit(s, props, ruled, depFactor)
		}
	} else {
		s.Kids = dropEmptyGroupsBut(s.Kids, fl.spacers)
		count = float64(len(s.Kids))
	}
	cw, ch := s.W, s.H
	if horizontal {
		cw = trunc(float64(cw) / count)
	} else if vertical {
		ch = trunc(float64(ch) / count)
	}
	var x, y int64
	if incX == -1 {
		x = s.W - cw
	}
	if incY == -1 {
		y = s.H - ch
	}
	sizeOf := func(k *DiagramShape) (int64, int64) {
		w, h := cw, ch
		if v, ok := props.get(k.Name, "w"); ok {
			w = v
		}
		if v, ok := props.get(k.Name, "h"); ok {
			h = v
		}
		return w, h
	}
	var totW, totH int64
	for _, k := range s.Kids {
		w, h := sizeOf(k)
		totW += w
		totH += h
	}
	totW = trunc(float64(totW) + float64((count-1)*float64(spaceW)))
	totH = trunc(float64(totH) + float64((count-1)*float64(spaceH)))
	scaleW, scaleH := 1.0, 1.0
	if incX != 0 && totW > s.W {
		scaleW = float64(s.W) / float64(totW)
	}
	if incY != 0 && totH > s.H {
		scaleH = float64(s.H) / float64(totH)
	}
	spaceW = trunc(float64(spaceW) * scaleW)
	spaceH = trunc(float64(spaceH) * scaleH)
	// The set of kids sits along the direction where the definition's
	// horzAlign or vertAlign puts it, when it states one (21.4.2.3 alg lin).
	if horizontal {
		x += incX * alignOffset(param(a, "horzAlign", ""), s.W-trunc(float64(totW)*scaleW))
	} else if vertical {
		y += incY * alignOffset(param(a, "vertAlign", ""), s.H-trunc(float64(totH)*scaleH))
	}
	if fitted != nil {
		scaleW = 1
	}
	for _, k := range s.Kids {
		w, h := sizeOf(k)
		if v, ok := fitted[k]; ok {
			w = v
		}
		if (len(shrink) == 0 || shrink[k.Name]) && fitted == nil {
			w = trunc(float64(w) * scaleW)
			h = trunc(float64(h) * scaleH)
		} else if len(shrink) == 0 || shrink[k.Name] {
			h = trunc(float64(h) * scaleH)
		}
		k.W, k.H = w, h
		// Each kid sits across the direction where nodeVertAlign or
		// nodeHorzAlign puts it, in the middle by default.
		if incX != 0 {
			y = crossOffset(param(a, "nodeVertAlign", "mid"), s.H-h)
		}
		if incY != 0 {
			x = crossOffset(param(a, "nodeHorzAlign", "ctr"), s.W-w)
		}
		x, y = max(x, 0), max(y, 0)
		k.X, k.Y = x, y
		x += incX * (w + spaceW)
		y += incY * (h + spaceH)
		if k.sub == "conn" {
			k.Rot = connAngle * perDegree
		}
	}
	if a.Node != nil && a.Node.ChildOrder == "t" {
		for i, j := 0, len(s.Kids)-1; i < j; i, j = i+1, j-1 {
			s.Kids[i], s.Kids[j] = s.Kids[j], s.Kids[i]
		}
	}
}

// holdToBound holds the length an lte or gte constraint names to the bound it
// gives, when both are known.
func holdToBound(c DiagramConstraint, props layoutProps) {
	b := layoutProps{c.ForName: maps.Clone(props[c.ForName])}
	if c.RefForName != c.ForName {
		b[c.RefForName] = props[c.RefForName]
	}
	applyConstraint(c, b)
	v, okB := b.get(c.ForName, c.Type)
	cur, okC := props.get(c.ForName, c.Type)
	if okB && okC && (c.Op == "lte" && cur > v || c.Op == "gte" && cur < v) {
		props.set(c.ForName, c.Type, v)
	}
}

// shrinkToFit returns the widths a row gives the kids its rules shrink
// (21.4.2.24 rule type w, val 0) when they do not fit and it holds kids of
// a stated width or kids the user widened (prSet custScaleX), nil
// otherwise: the kids that shrink share what the stated widths leave of
// the row, each in proportion to its own scale, and a kid whose width is a
// factor of a shrinking kid's takes that factor of the share.
//
// # Corpus evidence
//
//   - libreoffice/pptx: smartart-linear-rule's hProcess3 states padding1
//     at 0.08 of the row, padding2 at userP (a quarter of the dummy's
//     height, at most 0.4 of the width), the spaces at 0.2 of a column,
//     and its third column's text is widened 1.42454 times: Office's
//     columns stand 145.02 points apart in the 563.2-point row, which the
//     share gives (1.2 x (0.92 x 563.2 - 56.3) / 3.82454 = 144.9); probe,
//     pptx: hprocess3-narrow and hprocess3-wide, 400 and 920 points wide,
//     103.0 and 243.4 points apart, given alike (102.9, 243.1).
func shrinkToFit(s *DiagramShape, props layoutProps, ruled map[string]bool, depFactor map[string]float64) map[*DiagramShape]int64 {
	var fixed, total, natural float64
	stated, scaled := false, false
	for _, k := range s.Kids {
		switch {
		case depFactor[k.Name] != 0:
			total += depFactor[k.Name]
		case ruled[k.Name]:
			f := customScale(k, 0)
			scaled = scaled || f != 1
			total += f
			v, ok := props.get(k.Name, "w")
			if !ok {
				return nil
			}
			natural += float64(float64(v) * f)
		default:
			v, ok := props.get(k.Name, "w")
			if !ok {
				return nil
			}
			fixed += float64(v)
			stated = stated || v != 0
		}
	}
	room := float64(s.W) - fixed
	// The rule shrinks the row to fit, and never widens it.
	if (!stated && !scaled) || !(total > 0) || !(room > 0) || natural <= room {
		return nil
	}
	share := room / total
	out := map[*DiagramShape]int64{}
	for _, k := range s.Kids {
		switch {
		case depFactor[k.Name] != 0:
			out[k] = trunc(float64(share * depFactor[k.Name]))
		case ruled[k.Name]:
			out[k] = trunc(float64(share * customScale(k, 0)))
		}
	}
	return out
}

// customSize stretches a composite's kid about its centre by the size the
// user gave its point (21.4.3.7 prSet custScaleX, custScaleY), once the
// composite has placed it: the kids whose edges refer to it keep the
// places its unstretched box gives them.
//
// # Corpus evidence
//
//   - libreoffice/pptx: smartart-composite-infer-right's vList4 picture
//     placeholder, custScaleX 70846 on a box 0.2 of a 612-point row wide
//     (122.4), prints 88.5 points wide in Office's page (its outline from
//     115.5 to 204), the text beside it where the unstretched box puts it.
func customSize(k *DiagramShape) {
	if k.pt == nil {
		return
	}
	if f := k.pt.ScaleX; f > 0 && f != 1 {
		w := trunc(float64(float64(k.W) * f))
		k.X -= (w - k.W) / 2
		k.W = w
	}
	if f := k.pt.ScaleY; f > 0 && f != 1 {
		h := trunc(float64(float64(k.H) * f))
		k.Y -= (h - k.H) / 2
		k.H = h
	}
}

// customScale returns the factor the user widened a shape by, or the
// widest a shape it holds was widened by (prSet custScaleX), 1 for none.
func customScale(s *DiagramShape, depth int) float64 {
	if s.pt != nil && s.pt.ScaleX > 0 {
		return s.pt.ScaleX
	}
	f := 1.0
	if depth >= maxDiagramDepth {
		return f
	}
	for _, k := range s.Kids {
		if v := customScale(k, depth+1); v > f {
			f = v
		}
	}
	return f
}

// setsLength reports a width or height constraint of a linear layout that
// states a length: an equality of no value (21.4.7.10 ST_BoolOperator equ)
// only ties the lengths others set, and an lte or gte one bounds a length
// without setting it.
func setsLength(c DiagramConstraint) bool {
	switch c.Op {
	case "lte", "gte":
		return false
	case "equ":
		return c.HasValue || c.RefType != "none"
	}
	return true
}

// fontKey names the size a font size constraint type starts a layout node
// at.
func fontKey(typ, name string) string { return typ + "\x00" + name }

// fontStart returns the size, in points, the constraints of cons start the
// font size typ of the layout node name at, bounded by the room earlier
// linear layouts left it; 0 for none.
func (e *engine) fontStart(cons []DiagramConstraint, typ, name string) float64 {
	for _, f := range cons {
		if f.Type == typ && f.ForName == name && f.HasValue && f.RefType == "none" && f.Value > 0 {
			v := min(f.Value, maxDiagramFontSize)
			if c, ok := e.fontCaps[fontKey(typ, name)]; ok {
				v = min(v, c)
			}
			return v
		}
	}
	return 0
}

// fontLength returns the length, in EMU, a width or height constraint that
// refers to a font size gives (refType primFontSz or secFontSz): its factor
// of that size read in millimetres, and the size's key; false for another
// constraint or a size no constraint starts.
func (e *engine) fontLength(c DiagramConstraint, cons []DiagramConstraint) (float64, string, bool) {
	if c.RefType != primFontSz && c.RefType != secFontSz {
		return 0, "", false
	}
	start := e.fontStart(cons, c.RefType, c.RefForName)
	if start <= 0 {
		return 0, "", false
	}
	key := fontKey(c.RefType, c.RefForName)
	if old, ok := e.fontUsed[key]; !ok || start < old {
		e.fontUsed[key] = start
	}
	return float64(float64(c.Factor*start) * 36000), key, true
}

// startLength returns the length a width or height constraint that refers
// to a font size gives at the size it starts at, as [engine.fontLength]
// does, without sizing the shape again at the size its text settles at.
func (e *engine) startLength(c DiagramConstraint, cons []DiagramConstraint) (float64, bool) {
	if c.RefType != primFontSz && c.RefType != secFontSz {
		return 0, false
	}
	start := e.fontStart(cons, c.RefType, c.RefForName)
	if start <= 0 {
		return 0, false
	}
	return float64(float64(c.Factor*start) * 36000), true
}

// fontLayout is what the font-sized lengths of a linear layout are: the
// kids whose length along the direction comes from a font size, which keep
// their room even when they draw nothing, and the lengths of the kids that
// grow without limit (val INF), the largest font-sized length of a layout
// node they hold.
type fontLayout struct {
	spacers   map[string]bool
	unbounded map[string]float64
}

// fontLengths bounds the font sizes the kids of a linear layout are sized
// from by the room the layout has, and returns their lengths. PowerPoint
// sizes the shapes from the font and the font from the shapes: a font size
// whose shapes along the direction, with the fixed lengths beside them,
// pass the layout's length falls by the share that makes them fit, and so
// does one whose shape across the direction passes the layout's width. The
// standard is silent on the unit; a font size reads as that many
// millimetres, as the evidence below measures.
//
// # Corpus evidence
//
//   - libreoffice/pptx: smartart-linear-rule's hProcess3 background arrow,
//     twice the primary font size tall, fills its 286-point layout, and
//     PowerPoint's reference prints the text at 50.5 points: 2 x 50.5 mm
//     is 286 points; its text sits where the column's spacers of half the
//     size put it, 0.5 x 50.5 mm under the column's top.
//   - libreoffice/pptx: smartart-linear-rule-vert's list1 items (0.82,
//     -0.41 and 0.7 of the size) repeat every 117.7 points in the
//     reference's 356-point frame: three items of 1.11 mm a point of size
//     fill it at 37.75 points, 118.8 points an item.
func (e *engine) fontLengths(s *DiagramShape, cons []DiagramConstraint, along, across string) fontLayout {
	out := fontLayout{spacers: map[string]bool{}, unbounded: map[string]float64{}}
	alongRoom, acrossRoom := s.W, s.H
	if along == "h" {
		alongRoom, acrossRoom = s.H, s.W
	}
	kids := map[string]*DiagramShape{}
	for _, k := range s.Kids {
		kids[k.Name] = k
	}
	// The unbounded kids take the largest font-sized length of a layout
	// node under them.
	for _, c := range cons {
		k, ok := kids[c.ForName]
		if !ok || !c.Unbounded || c.Type != along {
			continue
		}
		under := map[string]bool{}
		nodeNames(k.atom, under, 0)
		for _, f := range cons {
			if f.Type != along || !under[f.ForName] || !setsLength(f) {
				continue
			}
			if v, _, ok := e.fontLength(f, cons); ok {
				out.unbounded[k.Name] = max(out.unbounded[k.Name], v)
			}
		}
	}
	scale := 1.0
	keys := map[string]bool{}
	alongLen := map[string]float64{}
	fontAlong := map[string]bool{}
	for _, c := range cons {
		k, ok := kids[c.ForName]
		if !ok || !setsLength(c) {
			continue
		}
		v, key, font := e.fontLength(c, cons)
		if !font {
			if c.Type == along {
				alongLen[k.Name] = float64(alongRoom) * c.Factor
				if c.Unbounded {
					if u, ok := out.unbounded[k.Name]; ok {
						alongLen[k.Name], fontAlong[k.Name] = u, true
					}
				}
			}
			continue
		}
		keys[key] = true
		if c.Type == across && v > float64(acrossRoom) {
			scale = min(scale, float64(acrossRoom)/v)
		}
		if c.Type == along {
			alongLen[k.Name], fontAlong[k.Name] = v, true
			out.spacers[k.Name] = true
		}
	}
	if len(keys) == 0 {
		return out
	}
	var fixed, font float64
	all := true
	for _, k := range s.Kids {
		v, ok := alongLen[k.Name]
		if !ok {
			all = false
			break
		}
		if fontAlong[k.Name] {
			font += v
		} else {
			fixed += v
		}
	}
	if all && font > 0 && fixed+font > float64(alongRoom) && float64(alongRoom) > fixed {
		scale = min(scale, (float64(alongRoom)-fixed)/font)
	}
	if scale < 1 {
		for _, key := range sortedKeys(keys) {
			e.capFont(key, cons, scale)
		}
		for n, v := range out.unbounded {
			out.unbounded[n] = float64(v * scale)
		}
	}
	return out
}

// capFont bounds the size a font size constraint starts a layout node at
// by the share f of its present start that the shapes sized from it leave
// room for, in whole points: every size PowerPoint saves in its drawings
// is a whole point (see [engine.sizeText]), and the size search starts
// from this bound. The bound never rises and never falls under
// minDiagramFontSize.
func (e *engine) capFont(key string, cons []DiagramConstraint, f float64) {
	typ, name, _ := strings.Cut(key, "\x00")
	v := e.fontStart(cons, typ, name)
	if v <= 0 {
		return
	}
	c := max(math.Floor(float64(v*f)), minDiagramFontSize)
	if old, ok := e.fontCaps[key]; ok {
		c = min(c, old)
	}
	e.fontCaps[key] = c
}

// nodeNames adds the names of the layout nodes under a, at most
// maxDiagramDepth levels down.
func nodeNames(a *DiagramAtom, out map[string]bool, depth int) {
	if a == nil || depth >= maxDiagramDepth {
		return
	}
	for _, k := range a.Kids {
		if k.Kind == AtomLayoutNode {
			out[k.Name] = true
		}
		nodeNames(k, out, depth+1)
	}
}

// lengthOf returns the length a width or height constraint of a linear
// layout gives, in EMU: its factor of the font size it refers to, read in
// millimetres (see [engine.fontLengths]); the font-sized length of a layout
// node it holds for an unbounded one; else its factor of the side.
func (e *engine) lengthOf(c DiagramConstraint, cons []DiagramConstraint, fl fontLayout, side int64) int64 {
	if v, _, ok := e.fontLength(c, cons); ok {
		return trunc(v)
	}
	if v, ok := fl.unbounded[c.ForName]; ok && c.Unbounded {
		return trunc(v)
	}
	return trunc(float64(side) * c.Factor)
}

// alignOffset returns how far a set of kids that leaves room of a linear
// layout's length moves along the direction, for its horzAlign or
// vertAlign (21.4.7.49 ST_ParameterId): half the room for mid or ctr, all
// of it for b or r, none for t, l or no parameter.
func alignOffset(align string, room int64) int64 {
	if room <= 0 {
		return 0
	}
	switch align {
	case "mid", "ctr":
		return room / 2
	case "b", "r":
		return room
	}
	return 0
}

// crossOffset returns where a kid that leaves room across a linear
// layout's direction sits, for its nodeVertAlign or nodeHorzAlign: at the
// start for t or l, at the end for b or r, in the middle otherwise.
func crossOffset(align string, room int64) int64 {
	switch align {
	case "t", "l":
		return 0
	case "b", "r":
		return room
	}
	return room / 2
}

// dropEmptyGroupsBut returns kids without its group shapes that hold none,
// those keep names aside: their length still spaces the layout.
func dropEmptyGroupsBut(kids []*DiagramShape, keep map[string]bool) []*DiagramShape {
	out := kids[:0]
	for _, k := range kids {
		if k.Group && len(k.Kids) == 0 && !keep[k.Name] {
			continue
		}
		out = append(out, k)
	}
	return out
}

// pyramid stacks the kids as a pyramid's levels (21.4.2.1 pyra), each
// wider than the one above.
func pyramid(s *DiagramShape) {
	if len(s.Kids) == 0 || s.W == 0 || s.H == 0 {
		return
	}
	n := int64(len(s.Kids))
	const ratio = 0.32
	w, h := s.W/n, s.H/n
	x := trunc(ratio * float64(w) * float64(n-1))
	y := trunc(ratio * float64(h))
	for _, k := range s.Kids {
		k.X, k.Y = x, y
		if n > 1 {
			x -= h / (n - 1)
		}
		w += h
		k.W, k.H = w, h
		y += h
	}
}

// textAlg sets a text shape's text properties (21.4.2.1 tx): the margins
// the constraints give as a share of its width, the size of a run
// without one from a primFontSz constraint, the autofit unless the text
// was formatted by hand, the text's rotation against the shape's
// (autoTxRot, the turn of the layout shape's own rot counting with the
// algorithm's: upright text stays upright in a turned shape), the
// vertical anchor (txAnchorVert), the levels from the shallowest, the
// bullets from stBulletLvl and the alignment (parTxLTRAlign, else centred
// unless bulleted).
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/smartart-vertical-block-list's descendant text
//     sits in a round2SameRect of rot 90; PowerPoint prints its bullets
//     upright, one under the other.
//   - openxmlsdk/pptx: O12_templates/ListDiagram_TP10174331 (the same
//     vList5 layout) prints "Comments" upright beside each concept.
func (e *engine) textAlg(a *DiagramAtom, s *DiagramShape, cons []DiagramConstraint) {
	fontSize := 0.0
	for _, c := range cons {
		if c.RefType == "w" && c.ForName == "" {
			side := map[string]int{"lMarg": 0, "tMarg": 1, "rMarg": 2, "bMarg": 3}
			if i, ok := side[c.Type]; ok {
				// LibreOffice reads the factor in points as millimetres
				// of the width, then converts the width to 1/100 mm.
				mm := c.Factor * 25.4 / 72
				s.Insets[i] = trunc(float64(s.W)*mm/360) * 360
				s.Inset[i] = true
			}
		}
		if c.Type == "primFontSz" {
			fontSize = c.Value
		}
	}
	if len(s.Paragraphs) == 0 {
		return
	}
	s.text = true
	s.FontSize = fontSize
	s.Autofit = !e.customText(s)
	// The rotation of a cycle's kid grows with its stAng, which a
	// document sets: the turn is reduced by a remainder, not by steps.
	rot := s.Rot % (360 * perDegree)
	rot += trunc(math.Mod(s.DiagramRot, 360) * perDegree)
	rot %= 360 * perDegree
	if rot < 0 {
		rot += 360 * perDegree
	}
	switch param(a, "autoTxRot", "upr") {
	case "upr":
		n := 0
		switch {
		case rot >= 315*perDegree:
		case rot > 225*perDegree:
			n = -3
		case rot >= 135*perDegree:
			n = -2
		case rot > 45*perDegree:
			n = -1
		}
		s.TextRot = n * 90
	case "grav":
		if rot > 90*perDegree && rot < 270*perDegree {
			s.TextRot = -180
		}
	}
	switch param(a, "txAnchorVert", "mid") {
	case "t":
		s.Anchor = "t"
	case "b":
		s.Anchor = "b"
	default:
		s.Anchor = "ctr"
	}
	base := s.Paragraphs[0].Level
	for _, p := range s.Paragraphs {
		base = min(base, p.Level)
	}
	start := intParam(a, "stBulletLvl", 2) - 1
	bullets := false
	spacing := [2][2]int64{
		{spacingParam(a, "lnSpPar", 90), spacingParam(a, "lnSpAfParP", 35)},
		{spacingParam(a, "lnSpCh", 90), spacingParam(a, "lnSpAfChP", 15)},
	}
	for i := range s.Paragraphs {
		p := &s.Paragraphs[i]
		p.Level -= base
		sp := spacing[min(max(p.Level, 0), 1)]
		if p.PPr.Child(NSA, "lnSpc") == nil && p.Source.Child(NSA, "pPr").Child(NSA, "lnSpc") == nil {
			p.LineSpacing = sp[0]
		}
		if p.PPr.Child(NSA, "spcAft") == nil && p.Source.Child(NSA, "pPr").Child(NSA, "spcAft") == nil {
			p.SpaceAfter = sp[1]
		}
		if int64(p.Level) >= start {
			if _, ok := p.PPr.Attr("marL"); !ok {
				p.MarL, p.HasMarL = 285750*(int64(p.Level)-start+1), true
			}
			if _, ok := p.PPr.Attr("indent"); !ok {
				p.Indent, p.HasIndent = -285750, true
			}
			p.Bullet = true
			bullets = true
		}
	}
	if v, ok := a.Params["parTxLTRAlign"]; ok {
		for i := range s.Paragraphs {
			s.Paragraphs[i].Align = v
		}
	} else if !bullets {
		for i := range s.Paragraphs {
			s.Paragraphs[i].Align = "ctr"
		}
	}
}

// maxSpacingPercent caps a spacing parameter of the text algorithm at
// the largest percentage a DrawingML spacing holds (20.1.10.44
// ST_TextSpacingPercentOrPercentString, 13200%).
const maxSpacingPercent = 13200

// spacingParam returns a line spacing parameter of the text algorithm
// (21.4.7.49 ST_ParameterId: lnSpPar "line spacing of the parent", lnSpCh
// "line spacing of the children", lnSpAfParP "line spacing after the
// parent", lnSpAfChP "line spacing after children"), a percentage of the
// font size, in thousandths of a per cent; def when absent, not a number
// or negative. The standard gives no default: the defaults are the
// spacings PowerPoint saves for a text algorithm that states none.
//
// # Corpus evidence
//
//   - libreoffice/pptx, poi/pptx: the 19 PowerPoint drawings of 12
//     layout definitions whose tx algorithms state no spacing save every
//     paragraph of their 98 laid out texts at a:lnSpc 90%, the parent
//     paragraphs with a:spcAft 35% and the child paragraphs (n862510_2,
//     bnc862510_5) with 15%; orgChart1 nodes whose text was formatted by
//     hand (n819614) keep their own.
func spacingParam(a *DiagramAtom, typ string, def float64) int64 {
	v := def
	if p, ok := a.Params[typ]; ok {
		if f := parseDouble(p, -1); f >= 0 {
			v = min(f, maxSpacingPercent)
		}
	}
	return int64(math.Round(v * 1000))
}

// customText reports a text shape whose first data point's text was
// formatted by hand (prSet custT). LibreOffice takes the shape's first
// presentation point in its map's order, by address; the earliest in the
// data model stands for it here.
func (e *engine) customText(s *DiagramShape) bool {
	p, ok := e.shpPres[s]
	if !ok {
		return false
	}
	for _, it := range e.d.presOf[p.ModelID] {
		if dp := e.d.byID[it.src]; dp != nil {
			return dp.CustomText
		}
	}
	return false
}
