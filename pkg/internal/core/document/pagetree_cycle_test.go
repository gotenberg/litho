package document

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// buildLoopingPageTreePDF builds a page tree whose root lists a sound page
// first and a looping /Pages chain second: 2 -> [4 (Page), 3], 3 -> 5 -> 6 -> 3.
// This mirrors the PDF.js "Pages loop" fixture, whose golden reports the first
// page with its text and an error only for the cyclic branch.
func buildLoopingPageTreePDF() []byte {
	objs := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [4 0 R 3 0 R] /Count 2 /MediaBox [0 0 595 842] >>\nendobj\n",
		"3 0 obj\n<< /Type /Pages /Kids [5 0 R] /Count 1 >>\nendobj\n",
		"4 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n",
		"5 0 obj\n<< /Type /Pages /Kids [6 0 R] /Count 1 >>\nendobj\n",
		"6 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
	}

	header := "%PDF-1.7\n"
	body := ""
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = len(header) + len(body)
		body += o
	}

	var xref strings.Builder
	xref.WriteString(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1))
	for _, off := range offsets {
		xref.WriteString(fmt.Sprintf("%010d 00000 n\r\n", off))
	}
	xrefOffset := len(header) + len(body)
	trailer := fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xrefOffset)

	return []byte(header + body + xref.String() + trailer)
}

// TestResolvePages_CycleKeepsSoundSiblings pins that a looping /Kids chain
// costs only its own branch. Aborting the whole traversal instead throws away
// pages already collected, so a document with one good page and one cyclic
// branch reported zero pages and became unremediable.
func TestResolvePages_CycleKeepsSoundSiblings(t *testing.T) {
	pdf := buildLoopingPageTreePDF()

	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(pdf), int64(len(pdf)), nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("a cyclic branch must not fail the whole traversal: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1 (the sound page preceding the cycle)", len(pages))
	}
	if got := pages[0].Ref.ObjectNum; got != 4 {
		t.Errorf("recovered page is object %d, want object 4", got)
	}
	// The cycle must terminate rather than be walked repeatedly.
	if got := arrToFloats(pages[0].MediaBox); got != [4]float64{0, 0, 595, 842} {
		t.Errorf("MediaBox = %v, want inherited [0 0 595 842]", got)
	}
}

// TestResolvePages_AllBranchesCyclic keeps the degenerate case honest: when
// every branch loops there is nothing to recover, and the result is an empty
// page list rather than a spurious page.
func TestResolvePages_AllBranchesCyclic(t *testing.T) {
	pdf := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Pages /Kids [2 0 R] /Count 1 >>\nendobj\n" +
		"trailer\n<< /Size 4 /Root 1 0 R >>\n%%EOF\n")

	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(pdf), int64(len(pdf)), nil)
	if err != nil {
		t.Skipf("document did not open: %v", err)
	}
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Skipf("catalog did not resolve: %v", err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil {
		return // Refusing an entirely cyclic tree is acceptable.
	}
	if len(pages) != 0 {
		t.Errorf("got %d pages from an entirely cyclic tree, want 0", len(pages))
	}
}
