package mutations

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestAppendPageContentsDropsNonStreamEntries pins that rewriting a /Contents
// array leaves out the entries that name no stream.
//
// ISO 32000-1, 7.8.2 makes a page's content the concatenation of the streams the
// array holds, and 7.3.9 makes a null equivalent to an absent entry, so dropping
// one loses nothing. Keeping it does: appending a flattened appearance after a
// leading null left a page poppler rendered as nothing.
//
// # Corpus evidence
//
//   - pdfjs issue18973.pdf: page 45 opens its /Contents with a null. The page
//     had shown 163 characters of signature text through the annotation before
//     the flatten and 0 after; without the null it shows 542, because the same
//     null was already hiding the page's own body text in the source.
func TestAppendPageContentsDropsNonStreamEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	alloc := writer.NewObjAllocator(20)
	brackets := newContentBrackets(alloc)
	stamp := ast.PdfIndirectRef{ObjectNum: 99}

	base := ast.PdfDict{
		"Contents": ast.PdfArray{
			ast.PdfNull{},
			ast.PdfIndirectRef{ObjectNum: 5},
			ast.PdfNull{},
		},
	}

	// A document whose object 5 is a real content stream, so the entry that
	// names it survives while the nulls do not.
	data := contentsArrayPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	got, ok := appendPageContents(ctx, doc, alloc, base, brackets, stamp).(ast.PdfArray)
	if !ok {
		t.Fatalf("appendPageContents returned %T, want ast.PdfArray", got)
	}
	for i, elem := range got {
		if _, isNull := elem.(ast.PdfNull); isNull {
			t.Errorf("element %d is a null: a reader drops the page's content at it", i)
		}
	}
	// save + the one real stream + restore + stamp.
	if len(got) != 4 {
		t.Errorf("got %d elements %v, want 4 (bracket, stream, bracket, stamp)", len(got), got)
	}
	if got[len(got)-1] != stamp {
		t.Errorf("last element = %v, want the stamp %v", got[len(got)-1], stamp)
	}
}

// contentsArrayPDF builds a one-page document whose object 5 is a content
// stream, so a /Contents entry naming it resolves.
func contentsArrayPDF() []byte {
	const content = "BT /F1 12 Tf 10 10 Td (body) Tj ET"

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Length 34 >>\nstream\n" + content + "\nendstream",
	}
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		b.WriteString(itoa(i+1) + " 0 obj\n" + o + "\nendobj\n")
	}
	start := b.Len()
	b.WriteString("xref\n0 " + itoa(len(objs)+1) + "\n0000000000 65535 f \n")
	for _, off := range offsets {
		b.WriteString(pad10(off) + " 00000 n \n")
	}
	b.WriteString("trailer\n<< /Size " + itoa(len(objs)+1) + " /Root 1 0 R >>\nstartxref\n" + itoa(start) + "\n%%EOF\n")
	return b.Bytes()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func pad10(n int) string {
	s := itoa(n)
	for len(s) < 10 {
		s = "0" + s
	}
	return s
}
