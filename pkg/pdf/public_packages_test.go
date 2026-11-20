package pdf

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// pdfDataTypes lists the exported struct and basic types package pdf
// declares: the handles, the error types beside the sentinels, and the
// values that hold a [Reader], a plugin or an internal value. Every other
// data type that crosses the public boundary is declared once, in
// pkg/models (see CONTRIBUTING.md, Encapsulation).
var pdfDataTypes = []string{
	"BookmarkGenerationOptions",
	"BookmarkOptions",
	"ColorTarget",
	"Conversion",
	"EditError",
	"FontSource",
	"ImageStamp",
	"PDFStamp",
	"Pipeline",
	"PipelineStageError",
	"Placement",
	"Reader",
	"RedactionPixelError",
	"StampFont",
	"TextStamp",
	"Writer",
}

// TestPublicPackagesKeepTheirPlace holds the three public packages to the
// layout CONTRIBUTING.md gives them:
//
//   - pkg/models imports the standard library alone;
//   - pkg/plugins imports the standard library and pkg/models alone;
//   - pkg/pdf declares no exported struct, integer, string or other basic
//     type outside [pdfDataTypes], so a new data type is declared in
//     pkg/models;
//   - no exported declaration of the three names a type of pkg/internal.
func TestPublicPackagesKeepTheirPlace(t *testing.T) {
	t.Parallel()

	const module = "github.com/gotenberg/litho/"
	models := module + "pkg/models"
	for _, tc := range []struct {
		dir     string
		allowed func(path string) bool
	}{
		{"../models", func(path string) bool { return isStandard(path) }},
		{"../plugins", func(path string) bool { return isStandard(path) || path == models }},
	} {
		bp, err := build.ImportDir(tc.dir, 0)
		if err != nil {
			t.Fatalf("listing %s: %v", tc.dir, err)
		}
		if len(bp.GoFiles) == 0 {
			t.Fatalf("%s: no Go file listed", tc.dir)
		}
		for _, path := range bp.Imports {
			if !tc.allowed(path) {
				t.Errorf("package %s imports %s", bp.Name, path)
			}
		}
	}

	declared := 0
	for _, dir := range []string{".", "../models", "../plugins"} {
		bp, err := build.ImportDir(dir, 0)
		if err != nil {
			t.Fatalf("listing %s: %v", dir, err)
		}
		fset := token.NewFileSet()
		for _, name := range bp.GoFiles {
			file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parsing %s: %v", name, err)
			}
			internal := internalImports(file)
			for _, decl := range file.Decls {
				for _, sig := range exportedSignatures(decl) {
					ast.Inspect(sig, func(n ast.Node) bool {
						sel, ok := n.(*ast.SelectorExpr)
						if !ok {
							return true
						}
						if x, ok := sel.X.(*ast.Ident); ok && internal[x.Name] {
							t.Errorf("%s: an exported declaration names %s.%s, a type of pkg/internal", fset.Position(sel.Pos()), x.Name, sel.Sel.Name)
						}
						return false
					})
				}
				if dir != "." {
					continue
				}
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.TYPE {
					continue
				}
				for _, spec := range gen.Specs {
					ts := spec.(*ast.TypeSpec)
					if !ts.Name.IsExported() || !isDataType(ts.Type) {
						continue
					}
					declared++
					if !slices.Contains(pdfDataTypes, ts.Name.Name) {
						t.Errorf("%s: package pdf declares the data type %s: declare it in pkg/models", fset.Position(ts.Pos()), ts.Name.Name)
					}
				}
			}
		}
	}
	// Without a lower bound the test would pass vacuously if the walk
	// stopped finding declarations.
	if declared < len(pdfDataTypes) {
		t.Fatalf("found %d data types in package pdf, want at least %d: the walk is broken", declared, len(pdfDataTypes))
	}
}

// isStandard reports whether path is a package of the standard library,
// whose first element holds no dot.
func isStandard(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// internalImports returns the names file imports packages of pkg/internal
// under.
func internalImports(file *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !strings.Contains(path, "/pkg/internal/") {
			continue
		}
		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		out[name] = true
	}
	return out
}

// exportedSignatures returns the parts of decl a caller sees: the type of an
// exported function or method of an exported type, the type of an exported
// type with its unexported struct fields left out, and the type of an
// exported variable or constant.
func exportedSignatures(decl ast.Decl) []ast.Node {
	var out []ast.Node
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Name.IsExported() && (d.Recv == nil || exportedRecv(d.Recv)) {
			out = append(out, d.Type)
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if !s.Name.IsExported() {
					continue
				}
				st, ok := s.Type.(*ast.StructType)
				if !ok {
					out = append(out, s.Type)
					continue
				}
				for _, f := range st.Fields.List {
					if len(f.Names) == 0 || slices.ContainsFunc(f.Names, func(n *ast.Ident) bool { return n.IsExported() }) {
						out = append(out, f.Type)
					}
				}
			case *ast.ValueSpec:
				if s.Type != nil && slices.ContainsFunc(s.Names, func(n *ast.Ident) bool { return n.IsExported() }) {
					out = append(out, s.Type)
				}
			}
		}
	}
	return out
}

// isDataType reports whether a type declaration declares a struct, a basic
// type such as an integer or a string, or a type defined on another named
// type: the kinds of type pkg/models holds.
func isDataType(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.StructType, *ast.SelectorExpr:
		return true
	case *ast.Ident:
		switch t.Name {
		case "bool", "string", "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
			"float32", "float64", "complex64", "complex128", "byte", "rune":
			return true
		}
	}
	return false
}
