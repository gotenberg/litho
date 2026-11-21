package ooxml

import (
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
		composite(a, s, cons)
	case "conn":
		connAlg(a, s)
	case "cycle":
		cycle(a, s)
	case "hierChild", "hierRoot":
		hierarchy(a, s)
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
	if vmin < 0 || vmin > vmax || vmax > ph {
		return
	}
	if diff := ph - (vmax - vmin); diff > 0 {
		for _, k := range s.Kids {
			k.Y += diff / 2
		}
	}
}

// applyConstraint applies one constraint to the layout properties: its
// reference property times its factor, the right or bottom edge inferred
// from the left or top edge and the length, else its value in millimetres
// (points for a font size reference).
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
	// A far edge no constraint states is the near edge plus the length
	// (21.4.7.21 ST_ConstraintType r "right", b "bottom").
	for _, e := range [][3]string{{"r", "l", "w"}, {"b", "t", "h"}} {
		if c.RefType != e[0] {
			continue
		}
		near, okN := ref[e[1]]
		length, okL := ref[e[2]]
		if okN && okL {
			props.set(c.ForName, c.Type, trunc(float64(near+length)*c.Factor))
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
	for _, k := range s.Kids {
		w, h := sizeOf(k)
		if len(shrink) == 0 || shrink[k.Name] {
			w = trunc(float64(w) * scaleW)
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
	return float64(float64(c.Factor*start) * 36000), fontKey(c.RefType, c.RefForName), true
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
		for key := range keys {
			typ, name, _ := strings.Cut(key, "\x00")
			if v := e.fontStart(cons, typ, name); v > 0 {
				e.fontCaps[key] = float64(v * scale)
			}
		}
		for n, v := range out.unbounded {
			out.unbounded[n] = float64(v * scale)
		}
	}
	return out
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
