package warn

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestAll(t *testing.T) {
	t.Parallel()

	kebab := regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)
	seen := map[string]bool{}
	for _, c := range All() {
		name := c.String()
		if !kebab.MatchString(name) {
			t.Errorf("code %q is not kebab-case", name)
		}
		if seen[name] {
			t.Errorf("code %q is listed twice", name)
		}
		seen[name] = true
	}
	if seen[Code{}.String()] {
		t.Error("the zero Code is listed")
	}
}

func TestAllReturnsACopy(t *testing.T) {
	t.Parallel()

	a := All()
	a[0] = LimitReached
	if All()[0] != MissingGlyph {
		t.Error("changing the slice All returned changed the list")
	}
}

func TestEffects(t *testing.T) {
	t.Parallel()

	kebab := regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)
	seen := map[string]bool{}
	for _, e := range Effects() {
		name := e.String()
		if !kebab.MatchString(name) {
			t.Errorf("effect %q is not kebab-case", name)
		}
		if seen[name] {
			t.Errorf("effect %q is listed twice", name)
		}
		seen[name] = true
	}
	if seen[Effect{}.String()] {
		t.Error("the zero Effect is listed")
	}
}

// TestEveryWarningStatesItsEffect reads the source of every package under
// pkg and holds each composite literal of a warning type, a type whose
// name ends in Warning, to keyed fields that name the effect. A helper
// that makes warnings takes the effect as a parameter, which the compiler
// holds every call to; this test holds the literals the compiler does not,
// so that the zero Effect never reaches a caller.
func TestEveryWarningStatesItsEffect(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	literals := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !strings.HasSuffix(typeName(lit.Type), "Warning") {
				return true
			}
			literals++
			named := false
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					t.Errorf("%s: a warning literal with unkeyed fields", fset.Position(lit.Pos()))
					return true
				}
				if id, ok := kv.Key.(*ast.Ident); ok && strings.EqualFold(id.Name, "effect") {
					named = true
				}
			}
			if !named {
				t.Errorf("%s: a warning literal names no effect", fset.Position(lit.Pos()))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if literals == 0 {
		t.Fatal("no warning literal found: the walk read no source")
	}
}

// typeName returns the name of the type expression of a composite literal,
// without its package, "" for a literal of no named type.
func typeName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}
