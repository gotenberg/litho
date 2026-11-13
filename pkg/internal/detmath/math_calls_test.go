package detmath

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// exactMath are the identifiers of the math package whose value does not
// depend on the machine: its constants, and the functions IEEE 754 rounds
// correctly or that move bits without rounding. Every other function of
// the package is computed by an algorithm of its own on some machine, or
// in pure Go the compiler fuses, and comes from this package instead.
var exactMath = map[string]bool{
	"Abs": true, "Ceil": true, "Copysign": true, "Dim": true, "FMA": true, "Floor": true, "Frexp": true,
	"Float32bits": true, "Float32frombits": true, "Float64bits": true, "Float64frombits": true,
	"Inf": true, "IsInf": true, "IsNaN": true, "Ldexp": true, "Max": true, "Min": true, "Mod": true, "Modf": true,
	"NaN": true, "Nextafter": true, "Nextafter32": true, "Pow10": true, "Remainder": true, "Round": true,
	"RoundToEven": true, "Signbit": true, "Sqrt": true, "Trunc": true,

	"E": true, "Pi": true, "Phi": true, "Sqrt2": true, "SqrtE": true, "SqrtPi": true, "SqrtPhi": true,
	"Ln2": true, "Log2E": true, "Ln10": true, "Log10E": true,
	"MaxFloat32": true, "SmallestNonzeroFloat32": true, "MaxFloat64": true, "SmallestNonzeroFloat64": true,
	"MaxInt": true, "MinInt": true, "MaxInt8": true, "MinInt8": true, "MaxInt16": true, "MinInt16": true,
	"MaxInt32": true, "MinInt32": true, "MaxInt64": true, "MinInt64": true,
	"MaxUint": true, "MaxUint8": true, "MaxUint16": true, "MaxUint32": true, "MaxUint64": true,
}

// TestPackages_TakeInexactFunctionsFromDetmath reads every source file of
// the public and internal packages, tests aside, and fails on a use of a
// math function outside [exactMath]: math.Sin, math.Pow, math.Exp and
// their like return bits that depend on the machine, and the PDF's bytes
// or a verdict then do too. This package is exempt: it is where they are
// written.
func TestPackages_TakeInexactFunctionsFromDetmath(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(root, "internal", "detmath")
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == own || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		name := mathName(file)
		if name == "" {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == name && !exactMath[sel.Sel.Name] {
				rel, _ := filepath.Rel(root, path) //nolint:errcheck // both are absolute: Rel cannot fail
				t.Errorf("pkg/%s:%d: math.%s depends on the machine: take it from detmath", filepath.ToSlash(rel), fset.Position(sel.Pos()).Line, sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// mathName returns the name file imports the math package under, empty
// when it does not import it.
func mathName(file *ast.File) string {
	for _, imp := range file.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err != nil || path != "math" {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "math"
	}
	return ""
}
