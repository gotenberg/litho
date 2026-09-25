package document

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestXRefWritersNoteTheChange pins the contract [Document.noteXRefChange]
// states: every function in the package that writes xrefEntries also records
// the change. A writer that does not leaves every cache derived from the index
// stale, which is how the offset cache behind [Document.nextNormalObjectOffset]
// kept the offsets the table declared after the augmentation moved one.
//
// A write is an assignment to the field or to one of its keys, a delete from
// it, or passing it to a callee that fills it. A callee is taken to fill the
// map unless it is listed as a reader below, so a new call site fails here
// until someone decides which it is. The check is per function: it catches a
// writer that never records a change, not one that records one write of two.
func TestXRefWritersNoteTheChange(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	writers := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			write, notes := xrefWriteAndNote(fn.Body)
			if !write.IsValid() {
				continue
			}
			writers++
			if !notes {
				t.Errorf("%s writes xrefEntries at %s without calling noteXRefChange", fn.Name.Name, fset.Position(write))
			}
		}
	}

	// Control. A scan that matched nothing would report no missing call, which
	// is the failure this test exists to catch. When it was written, six
	// functions wrote the index: parseXRefChain, pruneUnrepresentableEntries,
	// parseHybridXRefStream, rebuildXRef, NewDocument and
	// augmentXrefFromRawScan.
	if writers < 6 {
		t.Fatalf("found only %d functions writing xrefEntries, so the scan is not reading the package", writers)
	}
}

// xrefReaders lists the callees that only read an xrefEntries map passed to
// them, with the argument positions they write, if any: maps.Copy writes its
// first argument and reads its second.
var xrefReaders = map[string][]int{
	"len":                  nil,
	"declaredObjectStarts": nil,
	"Copy":                 {0},
}

// xrefWriteAndNote returns the position of the first write to xrefEntries in
// body, or [token.NoPos], and whether body calls noteXRefChange.
func xrefWriteAndNote(body *ast.BlockStmt) (token.Pos, bool) {
	write := token.NoPos
	notes := false
	mark := func(pos token.Pos) {
		if !write.IsValid() {
			write = pos
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if index, ok := lhs.(*ast.IndexExpr); ok {
					lhs = index.X
				}
				if isXRefEntries(lhs) {
					mark(lhs.Pos())
				}
			}
		case *ast.CallExpr:
			callee := calleeName(n.Fun)
			if callee == "noteXRefChange" {
				notes = true
			}
			written, reader := xrefReaders[callee]
			for i, arg := range n.Args {
				if isXRefEntries(arg) && (!reader || slices.Contains(written, i)) {
					mark(arg.Pos())
				}
			}
		}
		return true
	})
	return write, notes
}

// isXRefEntries reports whether e names the xrefEntries field.
func isXRefEntries(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "xrefEntries"
}

// calleeName returns the name a call expression calls, without its receiver
// or package.
func calleeName(fun ast.Expr) string {
	switch fun := fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}
