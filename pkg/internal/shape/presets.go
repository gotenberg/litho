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

// The constructors of the generated table.

// lit is a literal operand.
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
