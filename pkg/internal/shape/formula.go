package shape

import (
	"math"
	"strconv"
	"strings"
)

// Op is the operator of a guide formula. The DrawingML operators are
// those of ECMA-376 Part 1, 20.1.9.11 gd (fmla), whose angles are in
// 60,000ths of a degree; the OpenDocument operators are the infix
// operators and functions of ODF 1.3 Part 3, 19.141 draw:formula, whose
// angles are in radians. An OpenDocument formula is compiled into a chain
// of guides over these operators, one guide per node of its tree.
type Op uint8

// The guide operators. Each takes up to three operands x, y and z.
const (
	// OpVal is "val x": x.
	OpVal Op = iota
	// OpMulDiv is "*/ x y z": x * y / z.
	OpMulDiv
	// OpAddSub is "+- x y z": x + y - z.
	OpAddSub
	// OpAddDiv is "+/ x y z": (x + y) / z.
	OpAddDiv
	// OpIfElse is "?: x y z": y when x > 0, else z.
	OpIfElse
	// OpAbs is "abs x": |x|.
	OpAbs
	// OpAt2 is "at2 x y": the angle of the vector (x, y), atan2(y, x), in
	// 60,000ths of a degree.
	OpAt2
	// OpCat2 is "cat2 x y z": x * cos(atan2(z, y)).
	OpCat2
	// OpCos is "cos x y": x * cos(y), y in 60,000ths of a degree.
	OpCos
	// OpMax is "max x y".
	OpMax
	// OpMin is "min x y".
	OpMin
	// OpMod is "mod x y z": sqrt(x² + y² + z²).
	OpMod
	// OpPin is "pin x y z": y clamped to [x, z].
	OpPin
	// OpSat2 is "sat2 x y z": x * sin(atan2(z, y)).
	OpSat2
	// OpSin is "sin x y": x * sin(y), y in 60,000ths of a degree.
	OpSin
	// OpSqrt is "sqrt x": the square root of x, 0 for a negative x.
	OpSqrt
	// OpTan is "tan x y": x * tan(y), y in 60,000ths of a degree.
	OpTan
	// OpAdd is the infix x + y.
	OpAdd
	// OpSub is the infix x - y.
	OpSub
	// OpMul is the infix x * y.
	OpMul
	// OpDiv is the infix x / y, 0 when y is 0.
	OpDiv
	// OpNeg is the unary -x.
	OpNeg
	// OpSinRad is sin(x), x in radians.
	OpSinRad
	// OpCosRad is cos(x), x in radians.
	OpCosRad
	// OpTanRad is tan(x), x in radians.
	OpTanRad
	// OpAtanRad is atan(x), in radians.
	OpAtanRad
	// OpAtan2Rad is atan2(x, y), in radians: the angle of the vector
	// (y, x), as ODF's atan2(y, x) names its arguments.
	OpAtan2Rad
)

// ArgKind is the kind of a formula operand.
type ArgKind uint8

// The operand kinds.
const (
	// ArgConst is a literal: Value.
	ArgConst ArgKind = iota
	// ArgGuide names a guide by Ref, its index in the geometry's
	// adjustments followed by its guides.
	ArgGuide
	// ArgBuiltin names a built-in value by Ref, a [Builtin].
	ArgBuiltin
)

// Arg is one operand of a formula or a path command: a literal, a guide
// or a built-in value.
type Arg struct {
	Kind  ArgKind
	Value float64
	Ref   int
}

// Builtin is a value every guide formula can name without defining it
// (ECMA-376 Part 1, 20.1.9.11 gd, the shape guide built-ins; ODF 1.3 Part
// 3, 19.141 draw:formula, the identifiers).
type Builtin uint8

// The built-in values. The lengths are in the geometry's guide space: the
// shape's size in EMUs for DrawingML, its svg:viewBox for OpenDocument.
const (
	BuiltinL Builtin = iota
	BuiltinT
	BuiltinR
	BuiltinB
	BuiltinW
	BuiltinH
	BuiltinHC
	BuiltinVC
	BuiltinSS
	BuiltinLS
	BuiltinCD2
	BuiltinCD4
	BuiltinCD8
	Builtin3CD4
	Builtin3CD8
	Builtin5CD8
	Builtin7CD8
	BuiltinWD2
	BuiltinWD3
	BuiltinWD4
	BuiltinWD5
	BuiltinWD6
	BuiltinWD8
	BuiltinWD10
	BuiltinWD12
	BuiltinWD32
	BuiltinHD2
	BuiltinHD3
	BuiltinHD4
	BuiltinHD5
	BuiltinHD6
	BuiltinHD8
	BuiltinSSD2
	BuiltinSSD4
	BuiltinSSD6
	BuiltinSSD8
	BuiltinSSD16
	BuiltinSSD32
	// BuiltinLogWidth and BuiltinLogHeight are the shape's size in
	// 1/100 mm (ODF logwidth, logheight).
	BuiltinLogWidth
	BuiltinLogHeight
	// BuiltinHasFill and BuiltinHasStroke are 1 (ODF hasfill,
	// hasstroke): the engine draws every geometry as if both were set
	// and leaves the painting to its caller.
	BuiltinHasFill
	BuiltinHasStroke
	builtinCount
)

// drawingMLBuiltins maps the built-in names of ECMA-376 Part 1, 20.1.9.11
// to their values.
var drawingMLBuiltins = map[string]Builtin{ //nolint:gochecknoglobals // a read-only spec table, ECMA-376 Part 1, 20.1.9.11.
	"l": BuiltinL, "t": BuiltinT, "r": BuiltinR, "b": BuiltinB, "w": BuiltinW, "h": BuiltinH,
	"hc": BuiltinHC, "vc": BuiltinVC, "ss": BuiltinSS, "ls": BuiltinLS,
	"cd2": BuiltinCD2, "cd4": BuiltinCD4, "cd8": BuiltinCD8,
	"3cd4": Builtin3CD4, "3cd8": Builtin3CD8, "5cd8": Builtin5CD8, "7cd8": Builtin7CD8,
	"wd2": BuiltinWD2, "wd3": BuiltinWD3, "wd4": BuiltinWD4, "wd5": BuiltinWD5, "wd6": BuiltinWD6,
	"wd8": BuiltinWD8, "wd10": BuiltinWD10, "wd12": BuiltinWD12, "wd32": BuiltinWD32,
	"hd2": BuiltinHD2, "hd3": BuiltinHD3, "hd4": BuiltinHD4, "hd5": BuiltinHD5, "hd6": BuiltinHD6, "hd8": BuiltinHD8,
	"ssd2": BuiltinSSD2, "ssd4": BuiltinSSD4, "ssd6": BuiltinSSD6, "ssd8": BuiltinSSD8,
	"ssd16": BuiltinSSD16, "ssd32": BuiltinSSD32,
}

// DrawingMLBuiltin returns the built-in the DrawingML guide name s names
// (ECMA-376 Part 1, 20.1.9.11), and whether it names one.
func DrawingMLBuiltin(s string) (Builtin, bool) {
	b, ok := drawingMLBuiltins[s]
	return b, ok
}

// drawingMLOps maps the formula operator names of ECMA-376 Part 1,
// 20.1.9.11 to their operators and operand counts.
var drawingMLOps = map[string]struct { //nolint:gochecknoglobals // a read-only spec table, ECMA-376 Part 1, 20.1.9.11.
	op Op
	n  int
}{
	"val": {OpVal, 1}, "*/": {OpMulDiv, 3}, "+-": {OpAddSub, 3}, "+/": {OpAddDiv, 3},
	"?:": {OpIfElse, 3}, "abs": {OpAbs, 1}, "at2": {OpAt2, 2}, "cat2": {OpCat2, 3},
	"cos": {OpCos, 2}, "max": {OpMax, 2}, "min": {OpMin, 2}, "mod": {OpMod, 3},
	"pin": {OpPin, 3}, "sat2": {OpSat2, 3}, "sin": {OpSin, 2}, "sqrt": {OpSqrt, 1},
	"tan": {OpTan, 2},
}

// Guide is one named value of a geometry: an adjustment's default or a
// guide formula over literals, earlier guides and built-ins.
type Guide struct {
	Name string
	Op   Op
	Args [3]Arg
}

// ParseFormula parses a DrawingML guide formula (ECMA-376 Part 1,
// 20.1.9.11 gd fmla: an operator name and its operands, space separated).
// resolve returns the operand a name stands for; a literal operand is an
// integer. A missing operand is 0. It reports false for an unknown
// operator; the guide is then "val 0".
func ParseFormula(fmla string, resolve func(name string) Arg) (Op, [3]Arg, bool) {
	var args [3]Arg
	f := strings.Fields(fmla)
	if len(f) == 0 {
		return OpVal, args, false
	}
	def, ok := drawingMLOps[f[0]]
	if !ok {
		return OpVal, args, false
	}
	for i := 0; i < def.n && i+1 < len(f); i++ {
		args[i] = Operand(f[i+1], resolve)
	}
	return def.op, args, true
}

// Operand returns the operand s: a literal when s is a number (20.1.10.3
// ST_AdjCoordinate and 20.1.10.2 ST_AdjAngle accept an integer or a guide
// name), else what resolve says the name stands for.
func Operand(s string, resolve func(name string) Arg) Arg {
	if v, ok := parseNumber(s); ok {
		return Arg{Kind: ArgConst, Value: v}
	}
	if resolve == nil {
		return Arg{}
	}
	return resolve(s)
}

// parseNumber reads a decimal number, with an optional sign, fraction and
// exponent, refusing what is not finite.
func parseNumber(s string) (float64, bool) {
	if s == "" || len(s) > maxNumberLen {
		return 0, false
	}
	digits := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits = true
		case c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E':
		default:
			return 0, false
		}
	}
	if !digits {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// maxNumberLen caps the length of a numeric literal. Anti-DoS guard, not
// spec-mandated: no real coordinate needs more than a few dozen
// characters.
const maxNumberLen = 64

// angle60k turns 60,000ths of a degree into radians (ECMA-376 Part 1,
// 20.1.10.3 ST_Angle).
func angle60k(v float64) float64 { return v / 60000 * math.Pi / 180 }

// apply computes op over the operands x, y and z.
func apply(op Op, x, y, z float64) float64 {
	div := func(a, b float64) float64 {
		if b == 0 {
			return 0
		}
		return a / b
	}
	switch op {
	case OpVal:
		return x
	case OpMulDiv:
		return div(x*y, z)
	case OpAddSub:
		return x + y - z
	case OpAddDiv:
		return div(x+y, z)
	case OpIfElse:
		if x > 0 {
			return y
		}
		return z
	case OpAbs:
		return math.Abs(x)
	case OpAt2:
		return math.Atan2(y, x) * 180 / math.Pi * 60000
	case OpCat2:
		return x * math.Cos(math.Atan2(z, y))
	case OpCos:
		return x * math.Cos(angle60k(y))
	case OpMax:
		return math.Max(x, y)
	case OpMin:
		return math.Min(x, y)
	case OpMod:
		return math.Sqrt(x*x + y*y + z*z)
	case OpPin:
		if y < x {
			return x
		}
		if y > z {
			return z
		}
		return y
	case OpSat2:
		return x * math.Sin(math.Atan2(z, y))
	case OpSin:
		return x * math.Sin(angle60k(y))
	case OpSqrt:
		if x < 0 {
			return 0
		}
		return math.Sqrt(x)
	case OpTan:
		return x * math.Tan(angle60k(y))
	case OpAdd:
		return x + y
	case OpSub:
		return x - y
	case OpMul:
		return x * y
	case OpDiv:
		return div(x, y)
	case OpNeg:
		return -x
	case OpSinRad:
		return math.Sin(x)
	case OpCosRad:
		return math.Cos(x)
	case OpTanRad:
		return math.Tan(x)
	case OpAtanRad:
		return math.Atan(x)
	case OpAtan2Rad:
		return math.Atan2(x, y)
	}
	return 0
}
