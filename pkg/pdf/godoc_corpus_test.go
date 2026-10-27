package pdf

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestPublicGodocNeverMentionsCorpus keeps corpus evidence out of the public
// surface. Public godoc speaks to callers, who have no corpus: the evidence
// behind a public behavior belongs on the unexported declaration that makes
// the decision, or on the test that locks it (see CONTRIBUTING.md, Godoc).
//
// It reads every package comment and every doc comment of an exported
// declaration, struct field and interface method in the public packages.
func TestPublicGodocNeverMentionsCorpus(t *testing.T) {
	t.Parallel()

	checked := 0
	for _, dir := range []string{".", "../models", "../plugins"} {
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing %s: %v", dir, err)
		}
		check := func(name string, doc *ast.CommentGroup) {
			if doc == nil {
				return
			}
			checked++
			if strings.Contains(strings.ToLower(doc.Text()), "corpus") {
				t.Errorf("%s: godoc of %s mentions the corpus", fset.Position(doc.Pos()), name)
			}
		}
		for _, pkg := range pkgs {
			for _, file := range pkg.Files {
				check("package "+pkg.Name, file.Doc)
				for _, decl := range file.Decls {
					checkPublicDecl(decl, check)
				}
			}
		}
	}
	// Without a lower bound the test would pass vacuously if the walk
	// stopped finding declarations.
	if checked < 500 {
		t.Fatalf("checked %d doc comments, want at least 500: the walk is broken", checked)
	}
}

func checkPublicDecl(decl ast.Decl, check func(string, *ast.CommentGroup)) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Name.IsExported() && (d.Recv == nil || exportedRecv(d.Recv)) {
			check(d.Name.Name, d.Doc)
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if !s.Name.IsExported() {
					continue
				}
				check(s.Name.Name, s.Doc)
				if len(d.Specs) == 1 {
					check(s.Name.Name, d.Doc)
				}
				var fields *ast.FieldList
				switch t := s.Type.(type) {
				case *ast.StructType:
					fields = t.Fields
				case *ast.InterfaceType:
					fields = t.Methods
				}
				if fields == nil {
					continue
				}
				for _, f := range fields.List {
					for _, n := range f.Names {
						if n.IsExported() {
							check(s.Name.Name+"."+n.Name, f.Doc)
						}
					}
				}
			case *ast.ValueSpec:
				for _, n := range s.Names {
					if n.IsExported() {
						check(n.Name, s.Doc)
						check(n.Name, d.Doc)
					}
				}
			}
		}
	}
}

func exportedRecv(recv *ast.FieldList) bool {
	t := recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if idx, ok := t.(*ast.IndexExpr); ok {
		t = idx.X
	}
	id, ok := t.(*ast.Ident)
	return ok && id.IsExported()
}
