package intconv

import (
	goast "go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"testing"
)

// intconvPath is the import path of this package, whose parsers the guard
// counts as width-independent.
const intconvPath = modulePath + "/pkg/internal/intconv"

// TestParsesReadTheSameOnEveryWidth pins that no parse in the shipped code
// under pkg/ reads an integer at the platform's width: a value from 2^31 up
// parses where int is 64 bits and fails where it is 32, so the same document
// takes a default on one machine and its value on the other. The guard fails
// on, naming the file and line:
//
//   - strconv.Atoi;
//   - strconv.ParseInt and strconv.ParseUint with a bit size that is 0,
//     strconv.IntSize or not a constant;
//   - fmt.Sscan, Sscanf, Sscanln and their Fscan and Scan forms into an int,
//     a uint or a uintptr;
//   - encoding/xml and encoding/json decoding into a value whose type holds an
//     int, a uint or a uintptr field, or whose type the call does not state.
//
// The allowed forms parse at a stated width: [Atoi] (64 bits, narrowed by
// [Clamp]), [Atoi32], strconv.ParseInt or ParseUint with a constant bit size
// of 8, 16, 32 or 64, a scan or a decode into a sized integer type. A sum or
// a product of parsed values formed in int wraps on a 32-bit platform and not
// on a 64-bit one; the guard cannot see one, so CONTRIBUTING.md states it.
func TestParsesReadTheSameOnEveryWidth(t *testing.T) {
	t.Parallel()

	mod := checkedModule(t)
	var covered int
	var offenders []string
	for _, pkg := range mod.packages {
		n, found := widthDependentParses(mod.fset, pkg.files, pkg.info)
		covered += n
		offenders = append(offenders, found...)
	}

	slices.Sort(offenders)
	offenders = slices.Compact(offenders)
	for _, o := range offenders {
		t.Errorf("%s", o)
	}

	// A lower bound under what pkg/ holds, so a guard that stopped seeing
	// parses fails instead of passing.
	if covered < 150 {
		t.Fatalf("guard saw %d integer parses; it no longer inspects the module", covered)
	}
}

// widthDependentParses returns how many integer parses files hold, and the
// position and reason of each that reads at the platform's width.
func widthDependentParses(fset *token.FileSet, files []*goast.File, info *types.Info) (int, []string) {
	var count int
	var offenders []string
	report := func(pos token.Pos, what string) {
		offenders = append(offenders, fset.Position(pos).String()+": "+what)
	}
	for _, f := range files {
		goast.Inspect(f, func(n goast.Node) bool {
			call, ok := n.(*goast.CallExpr)
			if !ok {
				return true
			}
			fn := callee(info, call)
			if fn == nil {
				return true
			}
			switch name := fn.FullName(); name {
			case intconvPath + ".Atoi", intconvPath + ".Atoi32":
				count++
			case "strconv.Atoi":
				count++
				report(call.Pos(), "strconv.Atoi parses at the width of int; use intconv.Atoi, intconv.Atoi32 or strconv.ParseInt with a stated bit size")
			case "strconv.ParseInt", "strconv.ParseUint":
				count++
				if len(call.Args) == 3 && !statedBitSize(info, call.Args[2]) {
					report(call.Pos(), name+" with a bit size of the platform's width; state 8, 16, 32 or 64")
				}
			case "fmt.Sscan", "fmt.Sscanf", "fmt.Sscanln", "fmt.Fscan", "fmt.Fscanf", "fmt.Fscanln", "fmt.Scan", "fmt.Scanf", "fmt.Scanln":
				for _, arg := range call.Args {
					if p, ok := info.Types[arg].Type.(*types.Pointer); ok && platformWidth(p.Elem()) {
						count++
						report(arg.Pos(), name+" into an int, a uint or a uintptr; scan into a sized integer type")
					}
				}
			case "encoding/xml.Unmarshal", "encoding/json.Unmarshal":
				if len(call.Args) == 2 {
					count++
					offenders = append(offenders, decodeTargets(fset, info, call.Args[1], name)...)
				}
			case "(*encoding/xml.Decoder).Decode", "(*encoding/xml.Decoder).DecodeElement", "(*encoding/json.Decoder).Decode":
				if len(call.Args) >= 1 {
					count++
					offenders = append(offenders, decodeTargets(fset, info, call.Args[0], name)...)
				}
			}
			return true
		})
	}
	return count, offenders
}

// callee returns the function or method call names, or nil for a conversion,
// a builtin or a function value.
func callee(info *types.Info, call *goast.CallExpr) *types.Func {
	var id *goast.Ident
	switch fun := goast.Unparen(call.Fun).(type) {
	case *goast.Ident:
		id = fun
	case *goast.SelectorExpr:
		if sel, ok := info.Selections[fun]; ok {
			fn, _ := sel.Obj().(*types.Func)
			return fn
		}
		id = fun.Sel
	default:
		return nil
	}
	fn, _ := info.Uses[id].(*types.Func)
	return fn
}

// statedBitSize reports whether arg is a constant bit size other than 0 that
// does not name strconv.IntSize.
func statedBitSize(info *types.Info, arg goast.Expr) bool {
	tv := info.Types[arg]
	if tv.Value == nil {
		return false
	}
	if v, ok := constant.Int64Val(tv.Value); !ok || v == 0 {
		return false
	}
	namesIntSize := false
	goast.Inspect(arg, func(n goast.Node) bool {
		if id, ok := n.(*goast.Ident); ok {
			if c, ok := info.Uses[id].(*types.Const); ok && c.Pkg() != nil && c.Pkg().Path() == "strconv" && c.Name() == "IntSize" {
				namesIntSize = true
			}
		}
		return !namesIntSize
	})
	return !namesIntSize
}

// platformWidth reports whether t is int, uint or uintptr, or a type defined
// over one of them.
func platformWidth(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return false
	}
	switch b.Kind() {
	case types.Int, types.Uint, types.Uintptr:
		return true
	}
	return false
}

// decodeTargets returns one offender for each int, uint or uintptr field a
// decode into target fills, or one for target itself when its static type is
// an interface, which hides what the decoder fills.
func decodeTargets(fset *token.FileSet, info *types.Info, target goast.Expr, call string) []string {
	t := info.Types[target].Type
	if t == nil {
		return nil
	}
	if types.IsInterface(t) {
		return []string{fset.Position(target.Pos()).String() + ": " + call + " into an interface; decode into a concrete type the guard can read"}
	}
	var offenders []string
	seen := map[types.Type]bool{}
	var walk func(t types.Type, pos token.Pos)
	walk = func(t types.Type, pos token.Pos) {
		if platformWidth(t) && !decodesItself(t) {
			offenders = append(offenders, fset.Position(pos).String()+": "+call+" fills an int, a uint or a uintptr; declare the field with a sized integer type")
			return
		}
		if seen[t] || decodesItself(t) {
			return
		}
		seen[t] = true
		switch u := t.Underlying().(type) {
		case *types.Pointer:
			walk(u.Elem(), pos)
		case *types.Slice:
			walk(u.Elem(), pos)
		case *types.Array:
			walk(u.Elem(), pos)
		case *types.Map:
			walk(u.Key(), pos)
			walk(u.Elem(), pos)
		case *types.Struct:
			for i := range u.NumFields() {
				if f := u.Field(i); f.Exported() || f.Embedded() {
					walk(f.Type(), f.Pos())
				}
			}
		}
	}
	walk(t, target.Pos())
	return offenders
}

// decodesItself reports whether a pointer to t has a method through which
// encoding/xml or encoding/json hands it its bytes instead of filling it.
func decodesItself(t types.Type) bool {
	ms := types.NewMethodSet(types.NewPointer(t))
	for _, name := range []string{"UnmarshalXML", "UnmarshalXMLAttr", "UnmarshalJSON", "UnmarshalText"} {
		if ms.Lookup(nil, name) != nil {
			return true
		}
	}
	return false
}
