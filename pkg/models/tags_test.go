package models

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// TestEveryFieldNamesItsJSONKey holds every exported field of every struct
// of the package to a json tag: a key that starts in lower case and is
// used once in its struct, or "-" for a value JSON cannot carry, an error
// among them. A struct declared without tags would write its Go field
// names, and the values of the package would then read two ways.
func TestEveryFieldNamesItsJSONKey(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	structs := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok || !spec.Name.IsExported() {
				return true
			}
			structs++
			keys := map[string]bool{}
			for _, f := range st.Fields.List {
				for _, id := range f.Names {
					if !id.IsExported() {
						continue
					}
					where := name + ": " + spec.Name.Name + "." + id.Name
					if len(f.Names) > 1 {
						t.Errorf("%s shares its line with another field, so a tag would name both", where)
					}
					key := jsonKey(f.Tag)
					switch {
					case key == "":
						t.Errorf("%s has no json tag", where)
					case key == "-":
					case key[0] < 'a' || key[0] > 'z':
						t.Errorf("%s has the json key %q, want one that starts in lower case", where, key)
					case keys[key]:
						t.Errorf("%s repeats the json key %q of its struct", where, key)
					default:
						keys[key] = true
					}
				}
			}
			return true
		})
	}
	if structs == 0 {
		t.Fatal("no struct found: the test read no source file")
	}
}

// jsonKey returns the key of the json tag in tag, "" when it has none.
func jsonKey(tag *ast.BasicLit) string {
	if tag == nil {
		return ""
	}
	raw, err := strconv.Unquote(tag.Value)
	if err != nil {
		return ""
	}
	key, _, _ := strings.Cut(reflect.StructTag(raw).Get("json"), ",")
	return key
}
