package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// misorderedNameTreePDF builds a document whose /Names /Dests tree is sorted by
// decoded text rather than by byte value, so the root's /Limits does not bound
// the subtree below it.
//
// The UTF-16BE key begins 0xFE, which is greater than every byte of "section",
// yet the producer places it inside a subtree the root claims ends at
// "section*.4". This is the shape of pdfjs issue14864.pdf.
func misorderedNameTreePDF(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	// "\xfe\xff\x00H\x00i" is UTF-16BE "Hi" with a byte order mark.
	utf16Key := "<FEFF00480069>"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /Dests 7 0 R >> /Outlines 8 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		// Root claims a maximum of "second", but its child holds the 0xFE key.
		"<< /Kids [9 0 R] /Limits [(first) (second)] >>",
		"<< /Type /Outlines /First 10 0 R /Last 10 0 R /Count 1 >>",
		fmt.Sprintf("<< /Limits [(first) %s] /Names [(first) [3 0 R /Fit] %s [4 0 R /Fit]] >>",
			utf16Key, utf16Key),
		fmt.Sprintf("<< /Title (jump) /Parent 8 0 R /A << /S /GoTo /D %s >> >>", utf16Key),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestNamedDestFoundDespiteMisorderedLimits pins that a destination is still
// found when the tree's /Limits do not bound their subtree.
//
// ISO 32000-1, 7.9.6 orders name-tree keys by byte value and makes each /Limits
// pair the least and greatest key beneath it. Producers sort by decoded text
// instead: pdfjs issue14864.pdf puts UTF-16BE keys starting 0xFE under a root
// claiming a maximum of "section*.4", so pruning on that discarded the entire
// tree at the root and two bookmarks every other reader resolves came back with
// no destination at all.
func TestNamedDestFoundDespiteMisorderedLimits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := misorderedNameTreePDF(t)
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	arr, err := ResolveNamedDest(ctx, doc, "\xfe\xff\x00H\x00i")
	if err != nil {
		t.Fatalf("ResolveNamedDest: %v", err)
	}
	if arr == nil {
		t.Fatal("the UTF-16 destination was not found: the root /Limits pruned the subtree that holds it")
	}
	if len(arr) == 0 {
		t.Fatal("the destination resolved to an empty array")
	}
}

// TestNamedDestStillFoundWithHonestLimits is the control: the ordinary case,
// where /Limits does bound the subtree, must keep working through the fast path.
func TestNamedDestStillFoundWithHonestLimits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := misorderedNameTreePDF(t)
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	arr, err := ResolveNamedDest(ctx, doc, "first")
	if err != nil {
		t.Fatalf("ResolveNamedDest: %v", err)
	}
	if arr == nil {
		t.Error("the in-range destination was not found")
	}
}

// TestNamedDestAbsentKeyStillReportsNothing pins that the rescan does not
// invent a hit: a key the document does not have must stay absent.
func TestNamedDestAbsentKeyStillReportsNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := misorderedNameTreePDF(t)
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	arr, err := ResolveNamedDest(ctx, doc, "nothing-like-this")
	if err != nil {
		t.Fatalf("ResolveNamedDest: %v", err)
	}
	if arr != nil {
		t.Errorf("an absent key resolved to %v", arr)
	}
}
