package pdf

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestConvertWarningCodes_MirrorTheInternalSet holds the ConvertWarningCode
// constants declared in pkg/models/warnings.go equal to the internal codes,
// one for one, in order and value for value, so that a code added on one
// side alone fails.
func TestConvertWarningCodes_MirrorTheInternalSet(t *testing.T) {
	t.Parallel()

	var internal []string
	for _, c := range warn.All() {
		internal = append(internal, c.String())
	}
	mirrors(t, "ConvertWarningCode", internal)
}

// TestConvertWarningEffects_MirrorTheInternalSet holds the
// ConvertWarningEffect constants declared in pkg/models/warnings.go equal to
// the internal effects, one for one, in order and value for value.
func TestConvertWarningEffects_MirrorTheInternalSet(t *testing.T) {
	t.Parallel()

	var internal []string
	for _, e := range warn.Effects() {
		internal = append(internal, e.String())
	}
	mirrors(t, "ConvertWarningEffect", internal)
}

// mirrors checks that the string constants of type typ declared in
// pkg/models/warnings.go hold the values internal, in order.
func mirrors(t *testing.T, typ string, internal []string) {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "models", "warnings.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	type constant struct{ name, value string }
	var public []constant
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			v := s.(*ast.ValueSpec)
			if id, ok := v.Type.(*ast.Ident); !ok || id.Name != typ {
				continue
			}
			for i, name := range v.Names {
				lit, ok := v.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Fatalf("%s is not a string literal", name.Name)
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				public = append(public, constant{name.Name, value})
			}
		}
	}

	if len(public) != len(internal) {
		t.Fatalf("%d public %s constants, %d internal ones", len(public), typ, len(internal))
	}
	for i, c := range internal {
		if public[i].value != c {
			t.Errorf("public %s %d, %s, is %q; the internal one is %q", typ, i, public[i].name, public[i].value, c)
		}
	}
}
