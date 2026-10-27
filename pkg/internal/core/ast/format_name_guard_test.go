package ast

import (
	"errors"
	goast "go/ast"
	"go/build"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// pkgRoot is the pkg directory of the module, from this package's directory.
const pkgRoot = "../../.."

// pkgImportPath is the import path of [pkgRoot].
const pkgImportPath = "github.com/gotenberg/litho/pkg"

// TestFormatQuoteNeverTakesPdfName pins that no format call under pkg/ quotes
// a PDF name with %q. [PdfName.String] writes the solidus, and %q goes through
// String, so a MIME type read as "/text/plain" and a missing entry, which
// resolves to the empty name, as "/", a value it does not have. A message
// quotes string(name) instead, and %s or %v writes the name as PDF syntax.
//
// The guard type-checks every package under pkg/ from source and inspects
// every call whose format argument is a constant, so it states a lower bound
// on what it saw: a failed type check leaves every operand untyped and would
// pass silently.
func TestFormatQuoteNeverTakesPdfName(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil)
	var packages, formatCalls, nameOperands int
	err := filepath.WalkDir(pkgRoot, func(dir string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if name := d.Name(); dir != pkgRoot && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
			return filepath.SkipDir
		}
		bp, err := build.Default.ImportDir(dir, 0)
		if _, ok := errors.AsType[*build.NoGoError](err); ok {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(pkgRoot, dir)
		if err != nil {
			return err
		}
		importPath := path.Join(pkgImportPath, filepath.ToSlash(rel))

		var files []*goast.File
		for _, name := range bp.GoFiles {
			f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				return err
			}
			files = append(files, f)
		}
		info := &types.Info{Types: map[goast.Expr]types.TypeAndValue{}}
		conf := types.Config{Importer: imp}
		if _, err := conf.Check(importPath, fset, files, info); err != nil {
			t.Fatalf("type-check %s: %v", importPath, err)
		}
		packages++
		calls, operands := checkFormatCalls(t, fset, files, info)
		formatCalls += calls
		nameOperands += operands
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", pkgRoot, err)
	}

	// Lower bounds under what pkg/ holds (46 packages, over 3000 format calls,
	// over 100 of them formatting a PdfName), so a guard that stopped seeing
	// packages, calls or types fails instead of passing.
	if packages < 40 || formatCalls < 2500 || nameOperands < 90 {
		t.Fatalf("guard saw %d packages, %d format calls and %d PdfName operands; it no longer inspects pkg/",
			packages, formatCalls, nameOperands)
	}
}

// checkFormatCalls reports each operand of a format call in files that quotes
// a PdfName, or a slice, array or map of them, with %q. It returns how many
// format calls it inspected and how many of their operands hold PdfNames.
func checkFormatCalls(t *testing.T, fset *token.FileSet, files []*goast.File, info *types.Info) (int, int) {
	t.Helper()
	var formatCalls, nameOperands int
	for _, f := range files {
		goast.Inspect(f, func(n goast.Node) bool {
			call, ok := n.(*goast.CallExpr)
			if !ok {
				return true
			}
			sig, ok := info.Types[call.Fun].Type.(*types.Signature)
			if !ok || !sig.Variadic() || sig.Params().Len() < 2 {
				return true
			}
			fi := sig.Params().Len() - 2
			if len(call.Args) <= fi {
				return true
			}
			format := info.Types[call.Args[fi]].Value
			if format == nil || format.Kind() != constant.String {
				return true
			}
			formatCalls++
			verbs, ok := formatVerbs(constant.StringVal(format))
			if !ok {
				t.Errorf("%s: the format uses an argument index, which the guard does not map to operands",
					fset.Position(call.Pos()))
				return true
			}
			args := call.Args[fi+1:]
			for i, verb := range verbs {
				if i >= len(args) || !holdsPdfName(info.Types[args[i]].Type) {
					continue
				}
				nameOperands++
				if verb == 'q' {
					t.Errorf("%s: %%q formats the PdfName %s; quote string(%s)",
						fset.Position(args[i].Pos()), types.ExprString(args[i]), types.ExprString(args[i]))
				}
			}
			return true
		})
	}
	return formatCalls, nameOperands
}

// holdsPdfName reports whether t is [PdfName], or a slice, array or map whose
// elements or keys are, each of which %q writes through [PdfName.String].
func holdsPdfName(t types.Type) bool {
	switch u := t.(type) {
	case *types.Named:
		if obj := u.Obj(); obj.Name() == "PdfName" && obj.Pkg() != nil &&
			obj.Pkg().Path() == pkgImportPath+"/internal/core/ast" {
			return true
		}
		return false
	case *types.Slice:
		return holdsPdfName(u.Elem())
	case *types.Array:
		return holdsPdfName(u.Elem())
	case *types.Map:
		return holdsPdfName(u.Key()) || holdsPdfName(u.Elem())
	}
	return false
}

// formatVerbs returns the verb of each operand-consuming directive in a fmt
// format string, in order, a * width or precision as the verb '*' it is, and
// false when a directive takes an argument index, which reorders the operands.
func formatVerbs(format string) ([]rune, bool) {
	var verbs []rune
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		for i < len(format) && strings.IndexByte("+-# 0123456789.*", format[i]) >= 0 {
			if format[i] == '*' {
				verbs = append(verbs, '*')
			}
			i++
		}
		if i >= len(format) {
			break
		}
		switch format[i] {
		case '%':
		case '[':
			return nil, false
		default:
			verbs = append(verbs, rune(format[i]))
		}
	}
	return verbs, true
}
