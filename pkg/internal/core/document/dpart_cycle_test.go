package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
)

// dpartCyclePDF builds a /DPartRoot node whose only kid is itself.
//
// 425 bytes. Before the visited set this killed the process: traverseDPart
// resolves every kid through ResolveDict, whose cycle guard is per-resolution,
// so re-resolving the same reference from a fresh frame succeeds every time and
// the walk recurses forever. Measured with the max stack lowered to 48 MiB:
// "runtime: goroutine stack exceeds 50331648-byte limit / fatal error: stack
// overflow". A stack overflow is a fatal runtime error, not a panic, so the
// recover() every pkg/pdf entry point installs cannot convert it.
func dpartCyclePDF() []byte {
	return buildDestPDF(
		"<< /Type /Catalog /Pages 2 0 R /DPartRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /DPM << /Cyc (yes) >> /K [4 0 R] >>",
	)
}

// dpartChainPDF builds an acyclic /DPartRoot chain `depth` nodes long whose
// last node parents the page, so the DPM reaches it.
func dpartChainPDF(depth int) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /DPartRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	for i := range depth {
		if i == depth-1 {
			objs = append(objs, "<< /DPM << /Leaf (yes) >> /K [3 0 R] >>")
		} else {
			objs = append(objs, fmt.Sprintf("<< /K [%d 0 R] >>", 4+i+1))
		}
	}
	return buildDestPDF(objs...)
}

// resolvePagesWithin runs the page walk on its own goroutine so a hang fails the
// test instead of stalling the package, mirroring the deadline idiom used by
// destination_test.go, xref_test.go and nametree_test.go.
//
// It cannot rescue a stack overflow: that kills the process outright, which is
// exactly why removing the guard shows up as a non-zero `go test` exit rather
// than a failed assertion.
func resolvePagesWithin(t *testing.T, data []byte, d time.Duration) []*Page {
	t.Helper()

	var pages []*Page
	var err error
	done := make(chan struct{})
	go func() {
		defer close(done)
		doc, derr := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if derr != nil {
			err = derr
			return
		}
		cat, cerr := ResolveCatalog(context.Background(), doc)
		if cerr != nil {
			err = cerr
			return
		}
		pages, err = ResolvePages(context.Background(), doc, cat)
	}()

	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("page walk did not finish within %v: the DPart walk is not terminating", d)
	}
	if err != nil {
		t.Fatalf("resolving pages: %v", err)
	}
	return pages
}

// TestResolveDPartMap_SelfReferencingKidTerminates pins that a /DPart node
// naming itself as its own kid is walked once rather than forever.
func TestResolveDPartMap_SelfReferencingKidTerminates(t *testing.T) {
	t.Parallel()

	pages := resolvePagesWithin(t, dpartCyclePDF(), 20*time.Second)
	if len(pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(pages))
	}
}

// TestResolveDPartMap_AcyclicChainStillCarriesDPM is the control. A guard that
// refused to recurse at all would satisfy the test above while silently
// dropping every document part's metadata.
func TestResolveDPartMap_AcyclicChainStillCarriesDPM(t *testing.T) {
	t.Parallel()

	for _, depth := range []int{1, 2, 500} {
		pages := resolvePagesWithin(t, dpartChainPDF(depth), 20*time.Second)
		if len(pages) != 1 {
			t.Fatalf("depth %d: pages = %d, want 1", depth, len(pages))
		}
		if got := pages[0].DPM["Leaf"]; got != "yes" {
			t.Errorf("depth %d: DPM[Leaf] = %q, want \"yes\": the walk stopped short", depth, got)
		}
	}
}

// TestResolveDPartMap_RepeatedKidIsNotAnError covers the shape a visited set
// could plausibly break: the same node legitimately appearing twice under
// different parents. It must still be walked, just not re-walked below itself.
func TestResolveDPartMap_RepeatedKidIsNotAnError(t *testing.T) {
	t.Parallel()

	// Root names node 5 twice. Node 5 parents the page.
	data := buildDestPDF(
		"<< /Type /Catalog /Pages 2 0 R /DPartRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /K [5 0 R 5 0 R] >>",
		"<< /DPM << /Leaf (yes) >> /K [3 0 R] >>",
	)

	pages := resolvePagesWithin(t, data, 20*time.Second)
	if len(pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(pages))
	}
	if got := pages[0].DPM["Leaf"]; got != "yes" {
		t.Errorf("DPM[Leaf] = %q, want \"yes\": a repeated kid was dropped entirely", got)
	}
}

// TestResolveDPartMap_SharedNodeResolvesDeterministically pins the one
// behaviour the visited set genuinely changes: a node reachable by two paths is
// walked on the first and skipped on the second, where the unguarded walk took
// the last. ISO 32000-2, 14.12 describes the document part hierarchy as a tree,
// so two parents naming one node is malformed either way. What must hold is that
// the page keeps its metadata: the guard may not cost a page its DPM.
func TestResolveDPartMap_SharedNodeResolvesDeterministically(t *testing.T) {
	t.Parallel()

	// Nodes 5 and 6 both name node 7, which parents the page.
	data := buildDestPDF(
		"<< /Type /Catalog /Pages 2 0 R /DPartRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /K [5 0 R 6 0 R] >>",
		"<< /DPM << /Branch (first) >> /K [7 0 R] >>",
		"<< /DPM << /Branch (second) >> /K [7 0 R] >>",
		"<< /DPM << /Leaf (yes) >> /K [3 0 R] >>",
	)

	pages := resolvePagesWithin(t, data, 20*time.Second)
	if len(pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(pages))
	}
	if got := pages[0].DPM["Leaf"]; got != "yes" {
		t.Fatalf("DPM[Leaf] = %q, want \"yes\": the shared node was skipped outright", got)
	}
	if got := pages[0].DPM["Branch"]; got != "first" {
		t.Errorf("DPM[Branch] = %q, want \"first\": the walk is no longer deterministic", got)
	}
}
