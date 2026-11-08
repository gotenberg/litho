package ooxml

import (
	"math"
	"strconv"
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
// not an integer.
func intParam(a *DiagramAtom, typ string, def int) int {
	v, ok := a.Params[typ]
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
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
		composite(a, s, cons)
	case "conn":
		connAlg(a, s)
	case "cycle":
		cycle(a, s)
	case "hierChild", "hierRoot":
		hierarchy(a, s)
	case "lin":
		linear(a, s, cons, rules)
	case "pyra":
		pyramid(s)
	case "snake":
		snake(a, s, cons)
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
// edges, each a factor of a reference property or a value in millimetres,
// then centres the kids vertically in the free height.
func composite(a *DiagramAtom, s *DiagramShape, cons []DiagramConstraint) {
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
		applyConstraint(c, props)
	}
	for _, k := range s.Kids {
		for _, c := range cons {
			if c.ForName == k.Name {
				applyConstraint(c, props)
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
				applyConstraint(c, props)
			}
		}
		w, h := s.W, s.H
		var x, y int64
		if p, ok := props[k.Name]; ok {
			if v, ok := p["w"]; ok {
				w = min(v, s.W)
			}
			if v, ok := p["h"]; ok {
				h = min(v, s.H)
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
				if r, ok := p["r"]; ok {
					w = r - l
				}
			}
			if t, ok := p["t"]; ok {
				if b, ok := p["b"]; ok {
					h = b - t
				}
			}
			x += xOff
			w = min(w, s.W-x)
			h = min(h, s.H-y)
		}
		k.W, k.H, k.X, k.Y = w, h, x, y
		vmin = min(vmin, y)
		vmax = max(vmax, y+h)
	}
	ph, _ := props.get("", "h")
	if !(vmin >= 0 && vmin <= vmax && vmax <= ph) {
		return
	}
	if diff := ph - (vmax - vmin); diff > 0 {
		for _, k := range s.Kids {
			k.Y += diff / 2
		}
	}
}

// applyConstraint applies one constraint to the layout properties: its
// reference property times its factor, the right edge inferred from the
// left and the width, else its value in millimetres (points for a font
// size reference).
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
	if c.RefType == "r" {
		l, okL := ref["l"]
		w, okW := ref["w"]
		if okL && okW {
			props.set(c.ForName, c.Type, trunc(float64(l+w)*c.Factor))
			return
		}
	}
	unit := 36000.0
	if c.RefType == "primFontSz" || c.RefType == "secFontSz" {
		unit = 12700
	}
	props.set(c.ForName, c.Type, trunc(c.Value*unit))
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
		if !((c.Op == "none" || c.Op == "equ") && c.Type != "none") {
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
	connRadius := trunc(float64(radius) * math.Cos(float64(spanAng/n)*math.Pi/180))
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
		k.X = trunc(float64(cw) + float64(r)*math.Sin(rad) - float64(w/2))
		k.Y = trunc(float64(ch) - float64(r)*math.Cos(rad) - float64(h/2))
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
	cw := trunc(float64(s.W) / (float64(horiz) + float64(horiz-1)*spaceW))
	chh := trunc(float64(s.H) / (vc + (vc-1)*spaceH))
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
		w, h := cw, trunc(float64(chh)*(kv+(kv-1)*spaceH))
		k.W, k.H = w, h
		if dir == "fromT" || dir == "fromB" {
			y = trunc(float64(y) + float64(h) + float64(chh)*spaceH)
		} else {
			x = trunc(float64(x) + float64(w) + float64(w)*spaceW)
		}
		rowH = max(rowH, h)
		if sec == "fromT" && i%2 == 1 {
			x = 0
			y = trunc(float64(y) + float64(rowH) + float64(chh)*spaceH)
			rowH = 0
		}
	}
}

// linear places the kids in a row or a column (21.4.2.1 lin, linDir):
// equal shares of the box, or the widths and heights the constraints on
// their names give, scaled down to fit, with the spacing the sp, space or
// sibTrans constraint gives; a kid centred across the direction; the
// rules say which kids shrink. A child order "t" reverses the stacking.
func linear(a *DiagramAtom, s *DiagramShape, cons []DiagramConstraint, rules []string) {
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
	props := layoutProps{}
	for _, c := range cons {
		if c.ForName == "" {
			continue
		}
		if props[c.ForName] == nil {
			props[c.ForName] = map[string]int64{}
		}
		if c.Type == "w" {
			props.set(c.ForName, "w", min(trunc(float64(s.W)*c.Factor), s.W))
		}
		if c.Type == "h" {
			props.set(c.ForName, "h", min(trunc(float64(s.H)*c.Factor), s.H))
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
	if len(shrink) > 0 {
		deps := map[string]bool{}
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
	} else {
		s.Kids = dropEmptyGroups(s.Kids)
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
	totW = trunc(float64(totW) + (count-1)*float64(spaceW))
	totH = trunc(float64(totH) + (count-1)*float64(spaceH))
	scaleW, scaleH := 1.0, 1.0
	if incX != 0 && totW > s.W {
		scaleW = float64(s.W) / float64(totW)
	}
	if incY != 0 && totH > s.H {
		scaleH = float64(s.H) / float64(totH)
	}
	spaceW = trunc(float64(spaceW) * scaleW)
	spaceH = trunc(float64(spaceH) * scaleH)
	for _, k := range s.Kids {
		w, h := sizeOf(k)
		if len(shrink) == 0 || shrink[k.Name] {
			w = trunc(float64(w) * scaleW)
			h = trunc(float64(h) * scaleH)
		}
		k.W, k.H = w, h
		if incX != 0 {
			y = (s.H - h) / 2
		}
		if incY != 0 {
			x = (s.W - w) / 2
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

// snake places the kids in rows (21.4.2.1 snake, grDir, contDir): as few
// rows as keep the shapes at least 0.54 as tall as wide, the spacing the
// sp constraint gives (0.3 of a shape by default), the last row centred.
func snake(a *DiagramAtom, s *DiagramShape, cons []DiagramConstraint) {
	if len(s.Kids) == 0 || s.W == 0 || s.H == 0 {
		return
	}
	childAR := s.Kids[0].ar
	shH, shW := float64(s.H), float64(s.W)
	if childAR != 0 && shH != 0 && childAR < shW/shH {
		shW = shH * childAR
	}
	spaceFromCons := 1.0
	byName := layoutProps{}
	byType := layoutProps{}
	byName.set("", "w", trunc(shW))
	byName.set("", "h", trunc(shH))
	for _, c := range cons {
		if c.RefType == "w" || c.RefType == "h" {
			if c.Type == "sp" && c.ForName == "" {
				spaceFromCons = c.Factor
			}
		}
		ref, ok := byName[c.RefForName]
		if !ok {
			continue
		}
		v, ok := ref[c.RefType]
		if !ok || c.Value != 0 {
			continue
		}
		val := trunc(float64(v) * c.Factor)
		if c.PtType == "none" {
			byName.set(c.ForName, c.Type, val)
		} else {
			byType.set(c.PtType, c.Type, val)
		}
	}
	widths := make([]int64, len(s.Kids))
	for i, k := range s.Kids {
		widths[i] = trunc(shW)
		if k.DataType == "" {
			continue
		}
		if v, ok := byType.get(k.DataType, "w"); ok {
			widths[i] = v
		}
	}
	spaceSet := spaceFromCons != 1
	grDir := param(a, "grDir", "tL")
	incX, incY := int64(1), int64(1)
	horizontal := true
	switch grDir {
	case "tR":
		incX = -1
	case "bL":
		incY = -1
		horizontal = false
	case "bR":
		incX, incY = -1, -1
		horizontal = false
	}
	count := int64(len(s.Kids))
	space := 0.3
	if spaceSet {
		space = spaceFromCons
	}
	const ratio = 0.54
	cols, rows := int64(1), int64(1)
	var maxRowW int64
	if float64(count) <= childAR {
		rows = count
	} else {
		for ; rows < count; rows++ {
			cols = int64(math.Ceil(float64(count) / float64(rows)))
			var rowW int64
			for i := int64(0); i < cols && i < count; i++ {
				rowW += widths[i]
			}
			total := shH * float64(rows)
			if rowW != 0 && total/float64(rowW) >= ratio {
				maxRowW = max(maxRowW, rowW)
				break
			}
		}
	}
	width := trunc(float64(s.W) / (float64(cols) + float64(cols-1)*space))
	cw, ch := width, trunc(float64(width)*ratio)
	if cols == 1 && rows > 1 {
		spaces := float64(-1)
		if spaceSet {
			spaces += 4
		}
		height := trunc(float64(s.H) / (float64(rows) + (float64(rows)+spaces)*space))
		if childAR > 1 {
			width = min(s.W, trunc(float64(height)*childAR))
			cw, ch = width, height
		}
		horizontal = false
	}
	var x, y int64
	if incX == -1 {
		x = s.W - cw
	}
	if incY == -1 {
		y = s.H - ch
	} else if spaceSet && !horizontal {
		y = trunc(float64(ch) * space * 2)
	}
	startX := x
	var colIdx, index int64
	switch param(a, "contDir", "sameDir") {
	case "sameDir":
		var rowH int64
		for _, k := range s.Kids {
			k.X, k.Y = x, y
			w, h := cw, ch
			fromCons := count >= 2 && s.Kids[1].DataType == "sibTrans"
			if fromCons && maxRowW != 0 {
				w = trunc(float64(s.W) * (float64(widths[index]) / float64(maxRowW)))
			}
			if childAR != 0 {
				h = trunc(float64(w) / childAR)
				h = min(h, trunc(float64(s.H)/(float64(rows)+float64(rows-1)*space)))
			}
			rowH = max(rowH, h)
			k.W, k.H = w, h
			index++
			if index%cols == 0 || index/cols+1 != rows {
				x = trunc(float64(x) + float64(incX)*(float64(w)+space*float64(w)))
			}
			colIdx++
			if colIdx == cols {
				if (index+1)%cols != 0 && index+1 >= 3 && (index+1)/cols+1 == rows && count != rows*cols {
					if fromCons {
						x = startX
					} else {
						x = trunc(float64(startX) + float64(incX)*(float64(w)+space*float64(w))/2)
					}
				} else {
					x = startX
				}
				y = trunc(float64(y) + float64(incY)*(float64(rowH)+space*float64(rowH)))
				colIdx, rowH = 0, 0
			}
			if index%cols != 0 && index >= 3 && index/cols+1 == rows {
				x = trunc(float64(x) + float64(incX)*(float64(w)+space*float64(w)))
			}
		}
	case "revDir":
		step := trunc(float64(cw) + space*float64(cw))
		for _, k := range s.Kids {
			k.X, k.Y = x, y
			k.W, k.H = cw, ch
			index++
			switch {
			case (index%cols == 0 || index/cols+1 != rows) && (index/cols+1)%2 != 0:
				x = trunc(float64(x) + float64(cw) + space*float64(cw))
			case index%cols != 0 && index/cols+1 != rows:
				x = trunc(float64(x) - (float64(cw) + space*float64(cw)))
			}
			colIdx++
			if colIdx == cols {
				lastRow := (index+1)%cols != 0 && index+1 >= 4 && (index+1)/cols+1 == rows && count != rows*cols
				switch {
				case lastRow && (index/cols+1)%2 == 0:
					x -= cw * 3 / 2
				case lastRow && (index/cols+1)%2 != 0:
					x = startX + incX*step/2
				case (index/cols+1)%2 != 0:
					x = startX
				}
				y = trunc(float64(y) + float64(incY)*(float64(ch)+space*float64(ch)))
				colIdx = 0
			}
			if index%cols != 0 && index >= 3 && index/cols+1 == rows {
				if (index/cols+1)%2 == 0 {
					x -= incX * step
				} else {
					x += incX * step
				}
			}
		}
	}
}

// textAlg sets a text shape's text properties (21.4.2.1 tx): the margins
// the constraints give as a share of its width, the size of a run
// without one from a primFontSz constraint, the autofit unless the text
// was formatted by hand, the text's rotation against the shape's
// (autoTxRot), the vertical anchor (txAnchorVert), the levels from the
// shallowest, the bullets from stBulletLvl and the alignment
// (parTxLTRAlign, else centred unless bulleted).
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
	s.FontSize = fontSize
	s.Autofit = !e.customText(s)
	rot := s.Rot
	for rot < 0 {
		rot += 360 * perDegree
	}
	for rot > 360*perDegree {
		rot -= 360 * perDegree
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
	for i := range s.Paragraphs {
		p := &s.Paragraphs[i]
		p.Level -= base
		if p.Level >= start {
			if _, ok := p.PPr.Attr("marL"); !ok {
				p.MarL, p.HasMarL = int64(285750*(p.Level-start+1)), true
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
