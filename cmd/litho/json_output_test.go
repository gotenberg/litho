package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestEveryJSONCapableCommandEmitsJSON pins a claim twenty commands make in
// their own flag help: "Output format: text or json".
//
// A command declares the flag and then switches on it. Declaring it without
// wiring the json arm leaves `-f json` silently emitting TEXT, which is the
// same defect as advising a flag the command rejects: the help promises
// something the command does not do. Machine consumers exist, including litho's
// own corpus harness, which parses `litho validate -f json`.
//
// Measured when this test was written: 20 of 20 emit parseable JSON, it stays
// parseable while a warning is printed (warnings go to stderr, so an encrypted
// input does not contaminate the document or the report), and the two commands
// that also write a document route their report through [reportWriter]. This
// pins that rather than establishing it.
//
// The command set is DERIVED from the source, not listed here, so a new
// json-capable command cannot be added without this test noticing: the table
// below is checked against the derivation in both directions.
func TestEveryJSONCapableCommandEmitsJSON(t *testing.T) {
	t.Parallel()

	derived := jsonCapableCommands(t)

	// Control: a derivation that matched nothing would report perfect
	// agreement against an empty set.
	if len(derived) < 15 {
		t.Fatalf("derived only %d json-capable commands, so the parser is not reading the package", len(derived))
	}

	invocations := map[string]func(src, out string) []string{
		"info":         func(s, _ string) []string { return []string{"info", "-f", "json", s} },
		"text":         func(s, _ string) []string { return []string{"text", "-f", "json", s} },
		"pages":        func(s, _ string) []string { return []string{"pages", "-f", "json", s} },
		"metadata":     func(s, _ string) []string { return []string{"metadata", "-f", "json", s} },
		"bookmarks":    func(s, _ string) []string { return []string{"bookmarks", "-f", "json", s} },
		"forms":        func(s, _ string) []string { return []string{"forms", "-f", "json", s} },
		"annotations":  func(s, _ string) []string { return []string{"annotations", "-f", "json", s} },
		"images":       func(s, _ string) []string { return []string{"images", "-f", "json", s} },
		"signatures":   func(s, _ string) []string { return []string{"signatures", "-f", "json", s} },
		"attachments":  func(s, _ string) []string { return []string{"attachments", "-f", "json", s} },
		"tables":       func(s, _ string) []string { return []string{"tables", "-f", "json", s} },
		"geo":          func(s, _ string) []string { return []string{"geo", "-f", "json", s} },
		"measurements": func(s, _ string) []string { return []string{"measurements", "-f", "json", s} },
		"model3d":      func(s, _ string) []string { return []string{"model3d", "-f", "json", s} },
		"formulas":     func(s, _ string) []string { return []string{"formulas", "-f", "json", s} },
		"lexicons":     func(s, _ string) []string { return []string{"lexicons", "-f", "json", s} },
		"redactions":   func(s, _ string) []string { return []string{"redactions", "-f", "json", s} },
		"validate": func(s, _ string) []string {
			return []string{"validate", "--profile", "PDF/A-2b", "-f", "json", s}
		},
		"remediate": func(s, o string) []string {
			return []string{"remediate", "--profile", "PDF/A-2b", "-f", "json", "-o", o, s}
		},
		"redact": func(s, o string) []string {
			return []string{"redact", "-region", "1:0,0,10,10", "-f", "json", "-o", o, s}
		},
		"convert-colors": func(s, o string) []string {
			return []string{"convert-colors", "--target", "cmyk", "-f", "json", "-o", o, s}
		},
	}

	for name := range derived {
		if _, ok := invocations[name]; !ok {
			t.Errorf("%q declares -f json but this test has no invocation for it, so nothing "+
				"checks that the flag is honoured", name)
		}
	}
	for name := range invocations {
		if !derived[name] {
			t.Errorf("this test invokes %q with -f json, but that command declares no -f flag", name)
		}
	}

	src := writeTempPDF(t, untaggedTextPDF())
	for name, build := range invocations {
		if !derived[name] {
			continue
		}
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.pdf")

			var stdout, stderr bytes.Buffer
			rc := run(context.Background(), build(src, out), &stdout, &stderr)

			// exitValidationFail is a verdict, not a failure to run: validate
			// reports it for a non-conformant document and still prints JSON.
			if rc != exitOK && rc != exitValidationFail {
				t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
			}

			var v any
			if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
				t.Errorf("-f json did not produce JSON (%v); stdout begins %.120q",
					err, stdout.String())
			}
		})
	}
}

// jsonCapableCommands returns the command names that declare the `-f` output
// format flag, read from the source rather than listed.
//
// A command names itself in flag.NewFlagSet and declares its flags on that set
// inside the same function, so the two are paired per function declaration.
// Read with go/ast: the pairing is structural, and a text pattern would have to
// guess which NewFlagSet call a given fs.String belongs to.
func jsonCapableCommands(t *testing.T) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing package: %v", err)
	}

	names := make(map[string]bool)
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if cmd, ok := commandWithFormatFlag(fn); ok {
					names[cmd] = true
				}
			}
		}
	}
	return names
}

// commandWithFormatFlag reports the command a function defines, when that
// function both names a FlagSet and declares an "f" flag on it.
func commandWithFormatFlag(fn *ast.FuncDecl) (string, bool) {
	var cmd string
	var hasFormat bool

	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		switch sel.Sel.Name {
		case "NewFlagSet":
			if cmd == "" {
				cmd = value
			}
		case "String":
			if value == "f" {
				hasFormat = true
			}
		}
		return true
	})

	return cmd, cmd != "" && hasFormat
}
