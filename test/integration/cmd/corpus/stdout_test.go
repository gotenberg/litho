package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestFeaturesNeverWriteToStdout locks that `corpus run --format=json`
// prints JSON alone: a feature runs inside run, check and baseline, whose
// stdout carries the report, so its progress lines and notices go to
// stderr. It fails on a feature file that calls fmt.Print, fmt.Printf or
// fmt.Println, or names os.Stdout.
func TestFeaturesNeverWriteToStdout(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("feature*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		checked++
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch {
			case pkg.Name == "fmt" && (sel.Sel.Name == "Print" || sel.Sel.Name == "Printf" || sel.Sel.Name == "Println"),
				pkg.Name == "os" && sel.Sel.Name == "Stdout":
				t.Errorf("%s: %s.%s writes to stdout, which carries the report", fset.Position(sel.Pos()), pkg.Name, sel.Sel.Name)
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no feature file found")
	}
}
