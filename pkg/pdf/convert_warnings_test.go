package pdf

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestConvertWarningCodes_MirrorTheInternalSet holds the ConvertWarningCode
// constants declared in convert_warnings.go equal to the internal codes,
// one for one, in order and value for value, so that a code added on one
// side alone fails.
func TestConvertWarningCodes_MirrorTheInternalSet(t *testing.T) {
	t.Parallel()

	f, err := parser.ParseFile(token.NewFileSet(), "convert_warnings.go", nil, 0)
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
			if id, ok := v.Type.(*ast.Ident); !ok || id.Name != "ConvertWarningCode" {
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

	internal := warn.All()
	if len(public) != len(internal) {
		t.Fatalf("%d public codes, %d internal ones", len(public), len(internal))
	}
	for i, c := range internal {
		if public[i].value != c.String() {
			t.Errorf("public code %d, %s, is %q; the internal one is %q", i, public[i].name, public[i].value, c)
		}
	}
}
