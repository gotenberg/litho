package annotation

import (
	"fmt"
	"testing"
)

// jsTreeObjs builds a document whose /Names /JavaScript tree is the chain of
// node bodies given, rooted at object 5. Object 4 is the /Names dictionary.
func jsTreeObjs(nodes ...string) []string {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /JavaScript 5 0 R >>",
	}
	return append(objs, nodes...)
}

// jsChain builds an acyclic /Kids chain n nodes long ending in a leaf that
// carries one script.
func jsChain(n int) []string {
	var nodes []string
	for i := range n {
		nodes = append(nodes, fmt.Sprintf("<< /Kids [%d 0 R] >>", 5+i+1))
	}
	// Leaf node, then the action dictionary it names.
	nodes = append(nodes,
		fmt.Sprintf("<< /Names [(a) %d 0 R] >>", 5+n+1),
		"<< /S /JavaScript /JS (app.alert\\(1\\);) >>",
	)
	return jsTreeObjs(nodes...)
}

// TestExtractDocumentScripts_SelfReferencingKidTerminates pins that a JavaScript
// name-tree node naming itself as its own kid is walked once rather than
// forever.
//
// Measured before the visited set, through the public Reader.FormData path that
// reaches this walk: "runtime: goroutine stack exceeds 67108864-byte limit /
// fatal error: stack overflow". That is a fatal runtime error, not a panic, so
// the recover() installed at every pkg/pdf entry point cannot convert it.
func TestExtractDocumentScripts_SelfReferencingKidTerminates(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, jsTreeObjs("<< /Kids [5 0 R] >>"))

	if _, err := ExtractDocumentScripts(ctx, doc); err != nil {
		t.Fatalf("extracting scripts from a cyclic tree: %v", err)
	}
}

// TestExtractDocumentScripts_AcyclicTreeStillCollects is the control. A guard
// that refused to recurse would satisfy the test above while silently
// collecting no script from any real document.
func TestExtractDocumentScripts_AcyclicTreeStillCollects(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, jsChain(3))

	scripts, err := ExtractDocumentScripts(ctx, doc)
	if err != nil {
		t.Fatalf("extracting scripts: %v", err)
	}
	if len(scripts) != 1 {
		t.Fatalf("scripts = %d, want 1: the walk no longer reaches the leaf", len(scripts))
	}
	if scripts[0] != "app.alert(1);" {
		t.Errorf("script = %q, want %q", scripts[0], "app.alert(1);")
	}
}

// TestExtractDocumentScripts_RepeatedKidCollectsOnce pins the fan-out shape the
// visited set also closes: a node whose /Kids lists the same child twice is
// malformed for a name tree, and the unguarded walk both duplicated the script
// and, on a deeper tree, branched 2^depth.
func TestExtractDocumentScripts_RepeatedKidCollectsOnce(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, jsTreeObjs(
		"<< /Kids [6 0 R 6 0 R] >>",
		"<< /Names [(a) 7 0 R] >>",
		"<< /S /JavaScript /JS (app.alert\\(1\\);) >>",
	))

	scripts, err := ExtractDocumentScripts(ctx, doc)
	if err != nil {
		t.Fatalf("extracting scripts: %v", err)
	}
	if len(scripts) != 1 {
		t.Errorf("scripts = %d, want 1: the same node was walked twice", len(scripts))
	}
}

// TestExtractDocumentScripts_DeepChainTerminates pins that a chain past the
// depth bound returns rather than running away. The visited set alone already
// bounds this walk by the file's distinct nodes; the bound is what keeps a
// merely deep tree from costing the whole stack.
func TestExtractDocumentScripts_DeepChainTerminates(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, jsChain(maxJSNameTreeDepth+40))

	if _, err := ExtractDocumentScripts(ctx, doc); err != nil {
		t.Fatalf("extracting scripts from a deep tree: %v", err)
	}
}
