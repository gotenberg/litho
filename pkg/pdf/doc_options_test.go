package pdf

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
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
	// no drift. The floor sits well below the package's constructor count, so
	// it catches a parser that reads nothing without needing an update each
	// time an option is added.
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

// TestDocGoListsEveryOptionEntryPoint pins the entry points doc.go's
// "Functional options" section names against the parameters the package
// declares.
//
// Each bullet of that section names the calls that take one option type. It
// had drifted: [Reader.ExtractAllText], [Pipeline.Split], [Pipeline.Merge],
// [Writer.RemediateWTPDF] and [Writer.RepairFacturX] took an option type no
// bullet named them under. The test reads every variadic option parameter of
// every exported function and method with go/ast, and requires the bullet of
// that option type to name the entry point. It also requires each option
// constructor in the bullet of the type it returns, which
// [TestDocGoListsEveryFunctionalOption] does not: a constructor listed under
// the wrong type passes it.
func TestDocGoListsEveryOptionEntryPoint(t *testing.T) {
	t.Parallel()

	doc, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatalf("reading doc.go: %v", err)
	}
	bullets := optionBullets(t, string(doc))
	// Measured at six bullets, one per option type.
	if len(bullets) < 6 {
		t.Fatalf("found only %d option bullets in doc.go, so the parser is not reading the section", len(bullets))
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing package: %v", err)
	}

	entryPoints, constructors := 0, 0
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || !fn.Name.IsExported() {
					continue
				}
				name := fn.Name.Name
				if fn.Recv != nil {
					recv, ok := receiverName(fn)
					if !ok || !ast.IsExported(recv) {
						continue
					}
					name = recv + "." + name
				}
				if optType, ok := variadicOptionParam(fn); ok {
					entryPoints++
					if !strings.Contains(bullets[optType], "["+name+"]") {
						t.Errorf("%s takes ...%s but doc.go's %s bullet does not name it", name, optType, optType)
					}
				}
				if fn.Recv != nil {
					continue
				}
				if optType, ok := singleOptionResult(fn); ok {
					constructors++
					if !strings.Contains(bullets[optType], "["+name+"]") {
						t.Errorf("%s returns %s but doc.go's %s bullet does not list it", name, optType, optType)
					}
				}
			}
		}
	}

	// A parser that silently matched nothing would report no drift. Measured
	// at 18 entry points and 46 constructors.
	if entryPoints < 15 {
		t.Errorf("found only %d entry points taking options, so the parser is not reading the package", entryPoints)
	}
	if constructors < 40 {
		t.Errorf("found only %d option constructors, so the parser is not reading the package", constructors)
	}
}

// optionBulletType matches the phrase that names a bullet's option type.
var optionBulletType = regexp.MustCompile(`takes? \[(\w+Option)\]`)

// optionBullets maps each option type to the text of the doc.go bullet that
// names the entry points taking it.
func optionBullets(t *testing.T, doc string) map[string]string {
	t.Helper()
	start := strings.Index(doc, "# Functional options")
	if start < 0 {
		t.Fatal("doc.go has no Functional options section, so this test proves nothing")
	}
	section := doc[start:]
	if end := strings.Index(section, "\n// # "); end >= 0 {
		section = section[:end]
	}
	bullets := map[string]string{}
	var current []string
	flush := func() {
		text := strings.Join(current, " ")
		if m := optionBulletType.FindStringSubmatch(text); m != nil {
			bullets[m[1]] = text
		}
		current = nil
	}
	for line := range strings.SplitSeq(section, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "//"))
		switch {
		case strings.HasPrefix(line, "- "):
			flush()
			current = append(current, line)
		case line == "":
			flush()
		case current != nil:
			current = append(current, line)
		}
	}
	flush()
	return bullets
}

// receiverName returns the type name of a method's receiver.
func receiverName(fn *ast.FuncDecl) (string, bool) {
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// variadicOptionParam reports the option type of a function's final variadic
// parameter, if its element type name ends in "Option".
func variadicOptionParam(fn *ast.FuncDecl) (string, bool) {
	params := fn.Type.Params.List
	if len(params) == 0 {
		return "", false
	}
	ellipsis, ok := params[len(params)-1].Type.(*ast.Ellipsis)
	if !ok {
		return "", false
	}
	ident, ok := ellipsis.Elt.(*ast.Ident)
	if !ok || !strings.HasSuffix(ident.Name, "Option") {
		return "", false
	}
	return ident.Name, true
}
