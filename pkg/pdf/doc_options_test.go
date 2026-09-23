package pdf

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestDocGoListsEveryFunctionalOption pins the package documentation against
// the package itself.
//
// doc.go's "Functional options" section enumerates the options each entry
// point accepts, one bullet per option type, with no hedging: it reads as the
// list, not as examples. It had drifted. Measured at the time this test was
// written, [WriteOption] listed six of its nine constructors and
// [RemediateOption] three of its five, so [WithEncryption], [WithObjectStreams],
// [WithDedup], [WithFigureAltText] and [WithDocumentID] were reachable,
// exported and absent from the only place that enumerates them.
//
// [WithEncryption] is the costly omission. Nothing preserves a source's
// encryption, so it is the sole way to obtain an encrypted output, and a reader
// of the package documentation could not learn it exists.
//
// The declarations are read with go/ast rather than matched with a regular
// expression. A pattern keyed on the "With" prefix would have passed a
// constructor named otherwise, and this test exists precisely because an
// enumeration drifted from what it enumerates.
func TestDocGoListsEveryFunctionalOption(t *testing.T) {
	t.Parallel()

	doc, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatalf("reading doc.go: %v", err)
	}
	// Without this the assertions below would pass vacuously if doc.go were
	// renamed or its options section deleted.
	if !strings.Contains(string(doc), "# Functional options") {
		t.Fatal("doc.go has no Functional options section, so this test proves nothing")
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing package: %v", err)
	}

	found := 0
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !fn.Name.IsExported() {
					continue
				}
				optType, ok := singleOptionResult(fn)
				if !ok {
					continue
				}
				found++
				if !strings.Contains(string(doc), "["+fn.Name.Name+"]") {
					t.Errorf("%s returns %s but doc.go never names it, so a reader of the "+
						"package documentation cannot discover it", fn.Name.Name, optType)
				}
			}
		}
	}

	// The second control: a parser that silently matched nothing would report
	// no drift. Measured at 26 constructors across four option types.
	if found < 20 {
		t.Errorf("found only %d option constructors, so the parser is not reading the package", found)
	}
}

// singleOptionResult reports the option type a constructor returns, if it
// returns exactly one result whose type name ends in "Option".
func singleOptionResult(fn *ast.FuncDecl) (string, bool) {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return "", false
	}
	ident, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
	if !ok || !strings.HasSuffix(ident.Name, "Option") {
		return "", false
	}
	return ident.Name, true
}
