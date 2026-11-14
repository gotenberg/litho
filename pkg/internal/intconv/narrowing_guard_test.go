package intconv

import (
	"errors"
	"fmt"
	goast "go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// moduleRoot is the module directory, from this package's directory.
const moduleRoot = "../../.."

// modulePath is the import path of [moduleRoot].
const modulePath = "github.com/gotenberg/litho"

// fitsMarker is the comment that vouches for a narrowing conversion: it names
// the bound that keeps the value within a 32-bit int or uint.
var fitsMarker = regexp.MustCompile(`//.*\bfits u?int:`)

// TestNarrowingConversionsAreClampedOrBounded pins that no conversion in the
// shipped code under pkg/ silently wraps on a 32-bit platform: every int64, uint64,
// uint32 or uint value converted to int, and every int64 or uint64 value
// converted to uint, goes through [Clamp] or carries a same-line comment
// "fits int: <bound>" (or "fits uint:") that names what keeps it in range.
// A 64-bit build never wraps those conversions, so neither the tests nor the
// linters see a 32-bit one go wrong. The cmd/gen-* generators run on a
// developer's machine only, and cannot import this package.
//
// The guard type-checks every package from source, so it states a lower bound
// on what it saw: a failed type check leaves every operand untyped and would
// pass silently.
func TestNarrowingConversionsAreClampedOrBounded(t *testing.T) {
	t.Parallel()

	mod := checkedModule(t)
	var covered int
	var offenders []string
	for _, pkg := range mod.packages {
		if pkg.name == "intconv" {
			continue
		}
		n, found := narrowingConversions(mod.fset, pkg.files, pkg.info)
		covered += n
		offenders = append(offenders, found...)
	}

	slices.Sort(offenders)
	for _, o := range offenders {
		t.Errorf("%s: narrows on a 32-bit platform; use intconv.Clamp or state the bound in a same-line \"fits int:\" comment", o)
	}

	// Lower bounds under what pkg/ holds, so a guard that stopped seeing
	// packages or conversions fails instead of passing.
	if len(mod.packages) < 60 || covered < 300 {
		t.Fatalf("guard saw %d packages and %d clamped or narrowing conversions; it no longer inspects the module", len(mod.packages), covered)
	}
}

// checkedPackage is one package of the shipped code under pkg/, parsed with
// its comments and type-checked from source.
type checkedPackage struct {
	name  string
	files []*goast.File
	info  *types.Info
}

// module is every package under pkg/, type-checked once for the guards of
// this package.
type module struct {
	fset     *token.FileSet
	packages []checkedPackage
	err      error
}

var (
	moduleOnce sync.Once
	moduleMemo module
)

// checkedModule type-checks the non-test files of every package under pkg/,
// once for all the guards that read them. The cmd/gen-* generators run on a
// developer's machine only, and cannot import this package.
//
// A failed type check fails the guard: it would leave every operand untyped
// and let the guards pass silently.
func checkedModule(t *testing.T) *module {
	t.Helper()
	moduleOnce.Do(func() {
		moduleMemo.fset = token.NewFileSet()
		moduleMemo.err = typeCheckModule(&moduleMemo)
	})
	if moduleMemo.err != nil {
		t.Fatal(moduleMemo.err)
	}
	return &moduleMemo
}

// typeCheckModule fills m with the packages under pkg/.
func typeCheckModule(m *module) error {
	imp := importer.ForCompiler(m.fset, "source", nil)
	root := filepath.Join(moduleRoot, "pkg")
	return filepath.WalkDir(root, func(dir string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if name := d.Name(); dir != root && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
			return filepath.SkipDir
		}
		bp, err := build.Default.ImportDir(dir, 0)
		if _, ok := errors.AsType[*build.NoGoError](err); ok {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(moduleRoot, dir)
		if err != nil {
			return err
		}
		importPath := path.Join(modulePath, filepath.ToSlash(rel))

		var files []*goast.File
		for _, name := range bp.GoFiles {
			f, err := parser.ParseFile(m.fset, filepath.Join(dir, name), nil, parser.ParseComments)
			if err != nil {
				return err
			}
			files = append(files, f)
		}
		info := &types.Info{
			Types:      map[goast.Expr]types.TypeAndValue{},
			Uses:       map[*goast.Ident]types.Object{},
			Selections: map[*goast.SelectorExpr]*types.Selection{},
		}
		conf := types.Config{Importer: imp}
		if _, err := conf.Check(importPath, m.fset, files, info); err != nil {
			return fmt.Errorf("type-check %s: %w", importPath, err)
		}
		m.packages = append(m.packages, checkedPackage{name: bp.Name, files: files, info: info})
		return nil
	})
}

// narrowingConversions returns how many narrowing conversions and
// [Clamp] calls files hold, and the position and text of each conversion
// neither marked nor clamped.
func narrowingConversions(fset *token.FileSet, files []*goast.File, info *types.Info) (int, []string) {
	var count int
	var offenders []string
	for _, f := range files {
		marked := map[int]bool{}
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if fitsMarker.MatchString(c.Text) {
					marked[fset.Position(c.Pos()).Line] = true
				}
			}
		}
		goast.Inspect(f, func(n goast.Node) bool {
			call, ok := n.(*goast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			if types.ExprString(call.Fun) == "intconv.Clamp" {
				count++
				return true
			}
			if !narrows(info, call) {
				return true
			}
			count++
			pos := fset.Position(call.Pos())
			if !marked[pos.Line] {
				offenders = append(offenders, pos.String()+": "+types.ExprString(call))
			}
			return true
		})
	}
	return count, offenders
}

// narrows reports whether call converts a non-constant operand to int or uint
// from a type a 32-bit int or uint cannot hold every value of.
func narrows(info *types.Info, call *goast.CallExpr) bool {
	fun, ok := info.Types[call.Fun]
	if !ok || !fun.IsType() {
		return false
	}
	to, ok := fun.Type.Underlying().(*types.Basic)
	if !ok {
		return false
	}
	arg := info.Types[call.Args[0]]
	if arg.Value != nil || arg.Type == nil {
		return false
	}
	from, ok := arg.Type.Underlying().(*types.Basic)
	if !ok {
		return false
	}
	switch to.Kind() {
	case types.Int:
		return from.Kind() == types.Int64 || from.Kind() == types.Uint64 || from.Kind() == types.Uint32 || from.Kind() == types.Uint
	case types.Uint:
		return from.Kind() == types.Int64 || from.Kind() == types.Uint64
	}
	return false
}
