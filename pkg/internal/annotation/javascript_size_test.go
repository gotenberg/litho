package annotation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// jsLeaf builds a name-tree leaf whose /Names array alternates a name with a
// JavaScript action dictionary carrying the given /JS value.
func jsLeaf(values ...ast.PdfObject) ast.PdfDict {
	var names ast.PdfArray
	for _, v := range values {
		names = append(names,
			ast.PdfString("script"),
			ast.PdfDict{"S": ast.PdfName("JavaScript"), "JS": v})
	}
	return ast.PdfDict{"Names": names}
}

// TestExtractJSTree_BoundsTotalScriptSize pins maxDocumentScriptsSize.
//
// The existing tests in javascript_cycle_test.go pin how many NODES the walk
// visits (the cycle guard and the repeated-kid case). None pinned how many
// BYTES it collects, which is the quantity a crafted file actually controls:
// measured without the bound, a leaf of 20 compressed streams turned 328 KB of
// input into 160 MB of scripts.
func TestExtractJSTree_BoundsTotalScriptSize(t *testing.T) {
	const entries = 3
	const each = 2 << 20 // 6 MB total, past the 4 MB bound

	values := make([]ast.PdfObject, 0, entries)
	for range entries {
		values = append(values, xfaFlateStream(t, each))
	}

	var scripts []string
	err := extractJSTree(context.Background(), xfaTestDoc(), jsLeaf(values...), &scripts)
	if err == nil {
		t.Fatalf("collected %d scripts with no error; %d entries of %d bytes must exceed the %d byte bound",
			len(scripts), entries, each, maxDocumentScriptsSize)
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %q, want it to name the limit it hit", err)
	}
}

// TestExtractJSTree_BoundsAcrossNodes pins that the bound is on the SUM across
// the whole walk, not per node.
//
// Each leaf here is comfortably under the limit on its own; only their total
// exceeds it. A bound applied per node would pass this and still let a file
// with many small leaves collect without limit.
func TestExtractJSTree_BoundsAcrossNodes(t *testing.T) {
	const perLeaf = 3 << 20 // under the bound alone, over it in pairs

	kids := ast.PdfArray{
		jsLeaf(xfaFlateStream(t, perLeaf)),
		jsLeaf(xfaFlateStream(t, perLeaf)),
	}

	var scripts []string
	err := extractJSTree(context.Background(), xfaTestDoc(),
		ast.PdfDict{"Kids": kids}, &scripts)
	if err == nil {
		t.Fatalf("collected %d scripts with no error; two leaves of %d bytes must exceed the %d byte bound",
			len(scripts), perLeaf, maxDocumentScriptsSize)
	}
}

// TestExtractJSTree_AcceptsARealisticDocument is the control. The bound tests
// above would pass just as well on a build that refused every script.
//
// 128 KB is above the largest document-level JavaScript measured anywhere in
// the corpora (60,881 bytes across 65 scripts), so a bound that rejected this
// would be rejecting real documents.
func TestExtractJSTree_AcceptsARealisticDocument(t *testing.T) {
	const realistic = 128 << 10

	var scripts []string
	err := extractJSTree(context.Background(), xfaTestDoc(),
		jsLeaf(xfaFlateStream(t, realistic)), &scripts)
	if err != nil {
		t.Fatalf("refused a %d byte document: %v", realistic, err)
	}
	if len(scripts) != 1 || len(scripts[0]) != realistic {
		t.Errorf("got %d scripts (first %d bytes), want 1 of %d",
			len(scripts), len(scripts[0]), realistic)
	}
}

// TestExtractJSTree_CollectsLiteralStrings pins that a /JS given as a string
// rather than a stream still counts toward the bound and is still collected.
func TestExtractJSTree_CollectsLiteralStrings(t *testing.T) {
	var scripts []string
	err := extractJSTree(context.Background(), xfaTestDoc(),
		jsLeaf(ast.PdfString("app.alert(1);")), &scripts)
	if err != nil {
		t.Fatalf("extractJSTree: %v", err)
	}
	if len(scripts) != 1 || scripts[0] != "app.alert(1);" {
		t.Errorf("got %v, want one script %q", scripts, "app.alert(1);")
	}
}
