package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
)

// objsDoc builds a document whose objects are objs, numbered from 1, with
// object 1 as the catalog.
func objsDoc(t *testing.T, objs ...string) *Document {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	doc, err := NewDocument(context.Background(), bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// sharedPageKidsDoc builds a document whose page tree root lists its kids in
// object 4, an array holding the one page beside two direct page tree nodes
// that each name object 4 again as their /Kids. Walked anew wherever it is
// reached, the array fans out 2^depth up to maxPageTreeDepth.
func sharedPageKidsDoc(t *testing.T) *Document {
	t.Helper()
	return objsDoc(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids 4 0 R /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"[3 0 R << /Type /Pages /Kids 4 0 R /Count 1 >> << /Type /Pages /Kids 4 0 R /Count 1 >>]",
	)
}

// TestResolvePages_SharedKidsArrayFanOut pins that the page tree walk visits a
// /Kids array written as an object of its own once, and still finds the page
// it holds.
func TestResolvePages_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := sharedPageKidsDoc(t)
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if ctx.Err() != nil {
		t.Fatal("the walk did not end before the deadline")
	}
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	if len(pages) != 1 || pages[0].Ref.ObjectNum != 3 {
		t.Errorf("got %d pages, want the one page, object 3", len(pages))
	}
}

// TestPromoteDirectKids_SharedKidsArrayFanOut pins that the promotion of the
// direct page tree nodes walks a /Kids array written as an object of its own
// once, numbering each direct node it holds once.
func TestPromoteDirectKids_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := sharedPageKidsDoc(t)
	err := doc.PromoteDirectKids(ctx)
	if ctx.Err() != nil {
		t.Fatal("the walk did not end before the deadline")
	}
	if err != nil {
		t.Fatalf("PromoteDirectKids: %v", err)
	}
	// The array, restaged with references, and the two nodes it held.
	if n := len(doc.PromotedObjects()); n != 3 {
		t.Errorf("staged %d objects, want 3", n)
	}
}

// TestResolveDPartMap_SharedKArrayCycle pins that the DPart walk visits a /K
// array written as an object of its own once: its direct DPart nodes each name
// it again, a cycle through no node's object number that recursed until the
// goroutine stack was exhausted.
func TestResolveDPartMap_SharedKArrayCycle(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := objsDoc(t,
		"<< /Type /Catalog /Pages 2 0 R /DPartRoot << /Type /DPartRoot /K 4 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"[<< /Type /DPart /K 4 0 R /DPM << /A (a) >> >> << /Type /DPart /K [3 0 R] /DPM << /B (b) >> >>]",
	)
	got, err := ResolveDPartMap(ctx, doc)
	if ctx.Err() != nil {
		t.Fatal("the walk did not end before the deadline")
	}
	if err != nil {
		t.Fatalf("ResolveDPartMap: %v", err)
	}
	if got[3]["B"] != "b" {
		t.Errorf("page 3 carries %v, want the metadata of the DPart listing it", got[3])
	}
}
