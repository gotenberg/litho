package shape

import "slices"

//go:generate go run ../../../cmd/gen-shape-presets -out presets_gen.go

// PresetNames returns the names of the presets the engine draws, sorted.
func PresetNames() []string {
	names := make([]string, 0, len(presetTable))
	for n := range presetTable {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// lit is a literal operand of the generated table.
func lit(v float64) Arg { return Arg{Kind: ArgConst, Value: v} }

// ref is a guide operand.
func ref(i int) Arg { return Arg{Kind: ArgGuide, Ref: i} }

// bin is a built-in operand.
func bin(b Builtin) Arg { return Arg{Kind: ArgBuiltin, Ref: int(b)} }

// gd is a guide.
func gd(name string, op Op, args ...Arg) Guide {
	g := Guide{Name: name, Op: op}
	copy(g.Args[:], args)
	return g
}

// cmd is a path command.
func cmd(k CmdKind, args ...Arg) Cmd { return Cmd{Kind: k, Args: args} }

// angleAdjusts lists, by preset, the adjust values that are angles in
// 60000ths of a degree (ECMA-376 Part 1, Annex D: the adjust values a
// polar handle's angAdj moves, and mathNotEqual's slant); every other
// adjust value is a share in 100000ths.
var angleAdjusts = map[string][]bool{
	"arc":                    {true, true},
	"blockArc":               {true, true, false},
	"chord":                  {true, true},
	"pie":                    {true, true},
	"circularArrow":          {false, true, true, true, false},
	"leftCircularArrow":      {false, true, true, true, false},
	"leftRightCircularArrow": {false, true, true, true, false},
	"mathNotEqual":           {false, true, false},
}

// AdjustGuides returns the names of a preset's adjust values in order
// (avLst gd names: "adj" or "adj1", "adj2", ...) and, for each, whether it
// is an angle. Both are nil for a preset the engine does not draw or one
// without adjust values.
func AdjustGuides(preset string) ([]string, []bool) {
	c, ok := presetTable[preset]
	if !ok || len(c.Adjust) == 0 {
		return nil, nil
	}
	names := make([]string, len(c.Adjust))
	angles := make([]bool, len(c.Adjust))
	for i, g := range c.Adjust {
		names[i] = g.Name
		if a := angleAdjusts[preset]; i < len(a) {
			angles[i] = a[i]
		}
	}
	return names, angles
}
