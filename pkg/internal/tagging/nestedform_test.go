package tagging

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// nestedFormPDF builds a page whose /X0 form shows no text of its own and only
// invokes /Inner, which does. /Plain paints a rectangle and reaches no text.
func nestedFormPDF() []byte {
	outer := " q /Inner Do Q "
	inner := "BT /F1 9 Tf 0 0 Td (Louise Anne Farrar) Tj ET"
	plain := "0 0 10 10 re f"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /XObject << /X0 5 0 R /Plain 7 0 R >> >> >>",
		"<< /Length 12 >>\nstream\n/X0 Do\nendstream",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] "+
			"/Resources << /XObject << /Inner 6 0 R >> >> /Length %d >>\nstream\n%sendstream", len(outer), outer),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length %d >>\nstream\n%sendstream", len(inner), inner),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length %d >>\nstream\n%sendstream", len(plain), plain),
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// TestTextBearingXObjects_FollowsNestedForms pins that a form reaching text
// only through the forms it invokes still counts as painting text.
//
// A form that carries no text-showing operator and only invokes other forms
// looks decorative judged by its own bytes, so tagging swept it into an
// /Artifact and the filled-in field value it drew stopped extracting.
//
// # Corpus evidence
//
//   - pdfjs issue15629: draws its field values through /X0, which carries no
//     text-showing operator and does nothing but invoke three /Annot_* forms;
//     tagging swept /X0 into an /Artifact and the filled-in name stopped
//     extracting.
func TestTextBearingXObjects_FollowsNestedForms(t *testing.T) {
	ctx := context.Background()
	data := nestedFormPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}

	got := textBearingXObjects(ctx, nil, doc, pages[0])
	if !got["X0"] {
		t.Error("/X0 reaches text through /Inner but was reported as decoration, so tagging would artifact it")
	}
	if got["Plain"] {
		t.Error("/Plain paints only a rectangle and must stay eligible for an /Artifact")
	}
}

// TestTextBearingXObjects_CycleTerminates pins that two forms invoking each other do
// not spin the scan. A crafted file can nest forms arbitrarily, so the walk
// tracks visited object numbers and stops at the form budget.
func TestTextBearingXObjects_CycleTerminates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loopA := " /B Do "
	loopB := " /A Do "
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /XObject << /A 5 0 R >> >> >>",
		"<< /Length 8 >>\nstream\n/A Do\nendstream",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] "+
			"/Resources << /XObject << /B 6 0 R >> >> /Length %d >>\nstream\n%sendstream", len(loopA), loopA),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] "+
			"/Resources << /XObject << /A 5 0 R >> >> /Length %d >>\nstream\n%sendstream", len(loopB), loopB),
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	data := b.Bytes()

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}

	done := make(chan map[string]bool, 1)
	go func() { done <- textBearingXObjects(ctx, nil, doc, pages[0]) }()
	select {
	case got := <-done:
		if got["A"] {
			t.Error("a cycle painting no text reported /A as text-bearing")
		}
	case <-ctx.Done():
		t.Fatal("scan did not terminate on a form cycle")
	}
}
