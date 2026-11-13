package css

// ShapeCommand is one command of a shape() (CSS Shapes 1, 3.2.1), as the
// SVG path command it draws: Op is the command letter, upper case for an
// absolute "to", lower case for a relative "by".
type ShapeCommand struct {
	// Op is one of M, L, H, V, C, Q, S, T, A and Z, or its lower-case
	// form.
	Op byte
	// Args are the command's lengths in SVG path order, each Points,
	// Percent or Calc: a horizontal one resolves against the reference
	// box's width, a vertical one against its height (Vertical).
	Args []Dimension
	// Vertical marks each of Args that resolves against the height.
	Vertical []bool
	// Rotate, Large and Sweep are an arc's rotation in degrees and its
	// flags.
	Rotate       float64
	Large, Sweep bool
	// OneRadius marks an arc written with one radius: both radii resolve
	// against the reference box's diagonal over the square root of 2
	// (CSS Shapes 1, 3.1.1, as circle() does).
	OneRadius bool
}

// maxShapeCommands caps the commands of a shape(). Anti-DoS guard, not
// spec-mandated: a drawn outline holds a few hundred; past the cap the
// value is invalid.
const maxShapeCommands = 10000

// shapeCmdSpec is a specified shape() command.
type shapeCmdSpec struct {
	op           byte
	args         []length
	vertical     []bool
	rotate       float64
	large, sweep bool
	oneRadius    bool
}

// parseShapeFunction parses the arguments of shape(): an optional fill
// rule, from and a position, then the commands, separated by commas (CSS
// Shapes 1, 3.2.1). It computes to the path its commands draw.
func parseShapeFunction(args []token) (*shapeSpec, bool) {
	parts := splitCommas(args)
	if len(parts) < 2 || len(parts) > maxShapeCommands {
		return nil, false
	}
	first := parts[0]
	s := &shapeSpec{kind: ShapeShape}
	if len(first) > 0 && first[0].typ == tIdent {
		switch lower(first[0].value) {
		case "nonzero":
			first = first[1:]
		case "evenodd":
			s.evenOdd, first = true, first[1:]
		}
	}
	if len(first) < 2 || !first[0].isIdent("from") {
		return nil, false
	}
	p, ok := parsePosition(first[1:])
	if !ok {
		return nil, false
	}
	s.cmds = append(s.cmds, shapeCmdSpec{op: 'M', args: p[:], vertical: []bool{false, true}})
	for _, part := range parts[1:] {
		c, ok := parseShapeCommand(part)
		if !ok {
			return nil, false
		}
		s.cmds = append(s.cmds, c)
	}
	return s, true
}

// parseShapeCommand parses one command of a shape(): move, line, hline,
// vline, curve, smooth, arc or close, with to and a position or by and a
// pair of lengths.
func parseShapeCommand(v []token) (shapeCmdSpec, bool) {
	if len(v) == 0 || v[0].typ != tIdent {
		return shapeCmdSpec{}, false
	}
	name := lower(v[0].value)
	if name == "close" {
		return shapeCmdSpec{op: 'Z'}, len(v) == 1
	}
	if len(v) < 3 {
		return shapeCmdSpec{}, false
	}
	var rel bool
	switch {
	case v[1].isIdent("by"):
		rel = true
	case !v[1].isIdent("to"):
		return shapeCmdSpec{}, false
	}
	letter := func(c byte) byte {
		if rel {
			return c + 'a' - 'A'
		}
		return c
	}
	rest := v[2:]
	switch name {
	case "move", "line":
		end, ok := shapePoint(rest, rel)
		if !ok {
			return shapeCmdSpec{}, false
		}
		op := byte('L')
		if name == "move" {
			op = 'M'
		}
		return shapeCmdSpec{op: letter(op), args: end[:], vertical: []bool{false, true}}, true
	case "hline", "vline":
		if len(rest) != 1 {
			return shapeCmdSpec{}, false
		}
		l, ok := shapeCoordinate(rest[0], name == "hline", rel)
		if !ok {
			return shapeCmdSpec{}, false
		}
		if name == "hline" {
			return shapeCmdSpec{op: letter('H'), args: []length{l}, vertical: []bool{false}}, true
		}
		return shapeCmdSpec{op: letter('V'), args: []length{l}, vertical: []bool{true}}, true
	case "curve", "smooth":
		return parseShapeCurve(rest, name == "smooth", rel, letter)
	case "arc":
		return parseShapeArc(rest, rel, letter)
	}
	return shapeCmdSpec{}, false
}

// shapePoint parses a <position> after to, or two lengths after by.
func shapePoint(v []token, rel bool) ([2]length, bool) {
	if rel {
		if len(v) != 2 {
			return [2]length{}, false
		}
		x, ok := parseLength(v[0], allowPct|allowMixed)
		if !ok {
			return [2]length{}, false
		}
		y, ok := parseLength(v[1], allowPct|allowMixed)
		return [2]length{x, y}, ok
	}
	return parsePosition(v)
}

// shapeCoordinate parses the one coordinate of hline or vline: a length,
// or after to a keyword of its axis.
func shapeCoordinate(t token, horizontal, rel bool) (length, bool) {
	if !rel && t.typ == tIdent {
		kw := map[string]float64{"center": 50}
		if horizontal {
			kw["left"], kw["right"] = 0, 100
		} else {
			kw["top"], kw["bottom"] = 0, 100
		}
		if p, ok := kw[lower(t.value)]; ok {
			return pctLength(p), true
		}
		return length{}, false
	}
	return parseLength(t, allowPct|allowMixed)
}

// parseShapeCurve parses the rest of curve, "<end> with <control> [/
// <control>]", or of smooth, "<end> [with <control>]": one control point
// draws a quadratic curve, two a cubic.
func parseShapeCurve(v []token, smooth, rel bool, letter func(byte) byte) (shapeCmdSpec, bool) {
	with := -1
	for i, t := range v {
		if t.isIdent("with") {
			with = i
			break
		}
	}
	endToks := v
	if with >= 0 {
		endToks = v[:with]
	}
	end, ok := shapePoint(endToks, rel)
	if !ok {
		return shapeCmdSpec{}, false
	}
	var ctrls [][2]length
	if with >= 0 {
		var cur []token
		groups := [][]token{}
		for _, t := range v[with+1:] {
			if t.isDelim('/') {
				groups = append(groups, cur)
				cur = nil
				continue
			}
			cur = append(cur, t)
		}
		groups = append(groups, cur)
		for _, g := range groups {
			p, ok := shapePoint(g, rel)
			if !ok {
				return shapeCmdSpec{}, false
			}
			ctrls = append(ctrls, p)
		}
	}
	var op byte
	switch {
	case smooth && len(ctrls) == 0:
		op = 'T'
	case smooth && len(ctrls) == 1:
		op = 'S'
	case !smooth && len(ctrls) == 1:
		op = 'Q'
	case !smooth && len(ctrls) == 2:
		op = 'C'
	default:
		return shapeCmdSpec{}, false
	}
	out := shapeCmdSpec{op: letter(op)}
	for _, p := range append(ctrls, end) {
		out.args = append(out.args, p[0], p[1])
		out.vertical = append(out.vertical, false, true)
	}
	return out, true
}

// parseShapeArc parses the rest of arc: "<end> of <radius>{1,2}" then, in
// any order, cw or ccw, large or small, and rotate and an angle.
func parseShapeArc(v []token, rel bool, letter func(byte) byte) (shapeCmdSpec, bool) {
	of := -1
	for i, t := range v {
		if t.isIdent("of") {
			of = i
			break
		}
	}
	if of < 0 {
		return shapeCmdSpec{}, false
	}
	end, ok := shapePoint(v[:of], rel)
	if !ok {
		return shapeCmdSpec{}, false
	}
	out := shapeCmdSpec{op: letter('A')}
	var radii []length
	rest := v[of+1:]
	for len(rest) > 0 && rest[0].typ != tIdent && len(radii) < 2 {
		r, ok := parseLength(rest[0], allowPct|allowMixed|nonNeg)
		if !ok {
			return shapeCmdSpec{}, false
		}
		radii = append(radii, r)
		rest = rest[1:]
	}
	switch len(radii) {
	case 0:
		return shapeCmdSpec{}, false
	case 1:
		radii = append(radii, radii[0])
		out.oneRadius = true
	}
	for len(rest) > 0 {
		switch lower(rest[0].value) {
		case "cw":
			out.sweep = true
		case "ccw":
		case "large":
			out.large = true
		case "small":
		case "rotate":
			if len(rest) < 2 {
				return shapeCmdSpec{}, false
			}
			a, ok := parseAngle(rest[1])
			if !ok {
				return shapeCmdSpec{}, false
			}
			out.rotate = a
			rest = rest[1:]
		default:
			return shapeCmdSpec{}, false
		}
		rest = rest[1:]
	}
	out.args = []length{radii[0], radii[1], end[0], end[1]}
	out.vertical = []bool{false, true, false, true}
	return out, true
}

// compute returns the computed command.
func (c shapeCmdSpec) compute(lc lenCtx) ShapeCommand {
	out := ShapeCommand{Op: c.op, Vertical: c.vertical, Rotate: c.rotate, Large: c.large, Sweep: c.sweep, OneRadius: c.oneRadius}
	if len(c.args) > 0 {
		out.Args = make([]Dimension, len(c.args))
		for i, a := range c.args {
			out.Args[i] = a.dim(lc, false)
		}
	}
	return out
}
