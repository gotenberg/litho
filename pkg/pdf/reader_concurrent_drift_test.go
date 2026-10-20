package pdf

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestConcurrentSurfaceCoversEveryUniformReader pins the concurrency tests
// against the type they promise to cover.
//
// [Reader] documents itself as safe for concurrent use, and -race only reports
// a race on a path some test actually runs concurrently, so the promise is
// worth exactly the surface the tests reach. That surface is enumerated BY HAND
// in the concurrent tests, and a hand-written list does not grow when the type
// does: nothing fails when a new reader is added, which is how it drifted.
//
// Measured when this test was written: the surface had grown from 44 exported
// methods to 48, and two context-only readers added since the lists were
// written were exercised nowhere. [Reader.HasXFAForm] arrived with the XFA
// warning work and [Reader.DeclaredPDFX] with PDF/X claim detection.
//
// Scope is deliberately the UNIFORM shapes, the ones a fan-out can call without
// a fixture of their own: methods taking only a context, and page-scoped
// methods taking (context, int). Readers taking options or varargs are out of
// scope here for the reason the earlier work recorded, that they need
// per-method fixtures rather than a uniform fan-out.
func TestConcurrentSurfaceCoversEveryUniformReader(t *testing.T) {
	t.Parallel()

	ctxOnly, pageScoped, total := readerMethodsByShape(t)

	// Controls. A parser that silently matched nothing would report no drift,
	// which is the failure mode this whole test exists to prevent.
	if total < 40 {
		t.Fatalf("found only %d exported Reader methods, so the parser is not reading the package", total)
	}
	if len(ctxOnly) < 15 {
		t.Fatalf("found only %d context-only readers, so the shape filter is wrong", len(ctxOnly))
	}

	exercised := concurrentlyExercised(t)
	if len(exercised) < 20 {
		t.Fatalf("found only %d methods called in concurrent tests, so the scan is not reading them", len(exercised))
	}

	for _, name := range ctxOnly {
		if !exercised[name] {
			t.Errorf("Reader.%s takes only a context and is exercised by no concurrent test, "+
				"so -race never checks it against the documented contract", name)
		}
	}
	for _, name := range pageScoped {
		if !exercised[name] {
			t.Errorf("Reader.%s is page-scoped and is exercised by no concurrent test, "+
				"so -race never checks it against the documented contract", name)
		}
	}
	for _, name := range racedOptionReaders {
		if !exercised[name] {
			t.Errorf("Reader.%s has a fixture-backed race test that no concurrent test file "+
				"calls any more, so -race no longer checks it", name)
		}
	}
}

// racedOptionReaders lists the readers outside the uniform shapes that a race
// test of their own covers, with a fixture that makes them do real work. The
// shape scan cannot require them, so this list keeps a deleted or renamed
// test from silently dropping them.
var racedOptionReaders = []string{
	// TestConcurrentGenerateBookmarks, over a document with headings.
	"GenerateBookmarks",
	// TestConcurrentVerifySignatures, over a document litho signed.
	"VerifySignatures",
	// TestRenderPageConcurrent, over a page that paints.
	"RenderPage",
}

// readerMethodsByShape parses the package and groups exported *Reader methods
// by parameter shape, returning the context-only names, the (context, int)
// names, and the total number of exported methods found.
//
// The declarations are read with go/ast rather than matched with a regular
// expression: a signature spans lines, and the question is the parameter TYPES,
// which a text pattern answers only by accident.
func readerMethodsByShape(t *testing.T) (ctxOnly, pageScoped []string, total int) {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing package: %v", err)
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || !fn.Name.IsExported() {
					continue
				}
				star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				if recv, ok := star.X.(*ast.Ident); !ok || recv.Name != "Reader" {
					continue
				}
				total++
				switch shape := paramShape(fn); shape {
				case "Context":
					ctxOnly = append(ctxOnly, fn.Name.Name)
				case "Context,int":
					pageScoped = append(pageScoped, fn.Name.Name)
				}
			}
		}
	}
	return ctxOnly, pageScoped, total
}

// paramShape renders a function's parameter types as a comma-separated string,
// expanding grouped names so (ctx context.Context, a, b int) reads
// "Context,int,int".
func paramShape(fn *ast.FuncDecl) string {
	var parts []string
	for _, p := range fn.Type.Params.List {
		var name string
		switch typ := p.Type.(type) {
		case *ast.SelectorExpr:
			name = typ.Sel.Name
		case *ast.Ident:
			name = typ.Name
		default:
			name = "other"
		}
		n := len(p.Names)
		if n == 0 {
			n = 1
		}
		for range n {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ",")
}

// concurrentlyExercised reports which Reader methods are called inside a test
// file that spawns goroutines, with a go statement or [sync.WaitGroup.Go]. A
// method named only by a sequential test does not count: -race reports a race
// on concurrent paths.
func concurrentlyExercised(t *testing.T) map[string]bool {
	t.Helper()

	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("globbing test files: %v", err)
	}

	call := regexp.MustCompile(`\br\.([A-Z][A-Za-z0-9_]*)\(`)
	exercised := make(map[string]bool)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		if !strings.Contains(string(src), "go func") && !strings.Contains(string(src), ".Go(func") {
			continue
		}
		for _, m := range call.FindAllStringSubmatch(string(src), -1) {
			exercised[m[1]] = true
		}
	}
	return exercised
}
