package semantics

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// efPDF assembles a document whose catalog names an /EmbeddedFiles tree rooted
// at object 4 (objs[0] becomes object 1, the catalog).
func efPDF(objs ...string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

const (
	efCatalog = "<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 4 0 R >> >>"
	efPages   = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
	efPage    = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>"
)

// TestExtractEmbeddedFiles_SelfReferencingKidTerminates pins that an
// /EmbeddedFiles name-tree node naming itself as its own kid is walked once
// rather than forever.
//
// Measured before the visited set, through the public ExtractEmbeddedFiles
// path: "fatal error: stack overflow", frames all collectEmbeddedFiles. That is
// a fatal runtime error, not a panic, so the recover() at every pkg/pdf entry
// point cannot convert it.
func TestExtractEmbeddedFiles_SelfReferencingKidTerminates(t *testing.T) {
	t.Parallel()

	doc := openBytes(t, efPDF(efCatalog, efPages, efPage, "<< /Kids [4 0 R] >>"))
	if _, err := ExtractEmbeddedFiles(context.Background(), doc); err != nil {
		t.Fatalf("extracting from a cyclic tree: %v", err)
	}
}

// TestExtractEmbeddedFiles_AcyclicStillCollects is the control. A guard that
// refused to descend would satisfy the test above while reporting no
// attachments for any real document.
func TestExtractEmbeddedFiles_AcyclicStillCollects(t *testing.T) {
	t.Parallel()

	doc := openBytes(t, efPDF(
		efCatalog, efPages, efPage,
		"<< /Kids [5 0 R] >>",
		"<< /Names [(a.txt) 6 0 R] >>",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 7 0 R >> >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
	))

	files, err := ExtractEmbeddedFiles(context.Background(), doc)
	if err != nil {
		t.Fatalf("extracting: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1: the walk no longer reaches the leaf", len(files))
	}
}

// TestExtractEmbeddedFiles_OneStreamUnderTwoNamesStillTwoItems pins the
// contract collectEmbeddedFiles documents: "a portfolio may list one stream
// under several names, and each is its own item".
//
// This is the behaviour the visited set must NOT break. It cannot: the set is
// keyed on the tree NODES being descended, while both names live in a single
// leaf's /Names array, which no recursion visits twice. Reusing the existing
// seen parameter as the cycle guard, which is keyed on file STREAM numbers,
// would have suppressed the second entry and broken this silently.
func TestExtractEmbeddedFiles_OneStreamUnderTwoNamesStillTwoItems(t *testing.T) {
	t.Parallel()

	doc := openBytes(t, efPDF(
		efCatalog, efPages, efPage,
		"<< /Names [(a.txt) 5 0 R (b.txt) 5 0 R] >>",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 6 0 R >> >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
	))

	files, err := ExtractEmbeddedFiles(context.Background(), doc)
	if err != nil {
		t.Fatalf("extracting: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("files = %d, want 2: one stream under two names is two items", len(files))
	}
}

// TestExtractEmbeddedFiles_RepeatedKidWalkedOnce pins the behaviour that DOES
// change: a /Kids array listing the same node twice is malformed for a name
// tree, and the node is now descended once rather than twice. Unguarded, that
// shape fans out 2^depth on a deep tree.
func TestExtractEmbeddedFiles_RepeatedKidWalkedOnce(t *testing.T) {
	t.Parallel()

	doc := openBytes(t, efPDF(
		efCatalog, efPages, efPage,
		"<< /Kids [5 0 R 5 0 R] >>",
		"<< /Names [(a.txt) 6 0 R] >>",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 7 0 R >> >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
	))

	files, err := ExtractEmbeddedFiles(context.Background(), doc)
	if err != nil {
		t.Fatalf("extracting: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("files = %d, want 1: the repeated kid was descended twice", len(files))
	}
}
