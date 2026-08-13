package mutations

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// unreadablePageTreePDF builds a document whose page tree lives in an object
// stream that will not decode, and whose cross-reference stream nonetheless
// lists that page tree as live.
//
// This is the shape an encrypted file takes when litho authenticates but cannot
// decrypt the object stream holding the page tree: the file states where the
// pages are, and litho cannot read them. It is deliberately not a dangling
// reference, which ISO 32000-1, 7.3.9 makes null and which
// TestFullRewriteNeverWritesNullPageTree covers instead.
func unreadablePageTreePDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")

	catalogOffset := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	// /Filter /FlateDecode over bytes that are not a zlib stream, so object 2
	// can never be extracted from it.
	objStmOffset := buf.Len()
	garbage := "not a flate stream at all"
	fmt.Fprintf(&buf, "3 0 obj\n<< /Type /ObjStm /N 1 /First 0 /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream\nendobj\n",
		len(garbage), garbage)

	// Cross-reference stream, unfiltered so the fixture stays readable. Per
	// ISO 32000-1, 7.5.8.3 a type 2 entry names the object stream holding the
	// object and its index within it.
	xrefOffset := buf.Len()
	entry := func(t, a, b int) []byte {
		row := make([]byte, 4)
		row[0] = byte(t)
		binary.BigEndian.PutUint16(row[1:3], uint16(a))
		row[3] = byte(b)
		return row
	}
	var rows bytes.Buffer
	rows.Write(entry(0, 0, 255))           // object 0, free
	rows.Write(entry(1, catalogOffset, 0)) // object 1, the catalog
	rows.Write(entry(2, 3, 0))             // object 2, inside object stream 3
	rows.Write(entry(1, objStmOffset, 0))  // object 3, the object stream
	rows.Write(entry(1, xrefOffset, 0))    // object 4, this stream
	fmt.Fprintf(&buf, "4 0 obj\n<< /Type /XRef /Size 5 /W [1 2 1] /Root 1 0 R /Length %d >>\nstream\n",
		rows.Len())
	buf.Write(rows.Bytes())
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefOffset)
	return buf.Bytes()
}

// TestFullRewriteRefusesToDropADefinedPageTree pins that the rewrite reports a
// page tree it could not reproduce instead of substituting an empty one.
//
// ISO 32000-1, 7.7.3.1 makes /Pages the page tree root, so the rewrite must give
// the output catalog one. Doing that unconditionally emptied documents in
// silence: `litho attach` on an encrypted pdfjs file whose page tree sits in an
// undecodable object stream turned a 47 KB one-page document into a 1.9 KB
// zero-page one and exited 0, while `litho rotate` on the same file failed.
//
// The empty tree stays correct for a catalog naming an object the file never
// defines: that document has no pages to lose.
func TestFullRewriteRefusesToDropADefinedPageTree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := unreadablePageTreePDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if !doc.DefinesObject(2) {
		t.Fatal("fixture is wrong: the cross-reference stream must list object 2 as live")
	}

	var out bytes.Buffer
	err = FullRewrite(ctx, &out, FullRewriteConfig{Doc: doc})
	if !errors.Is(err, ErrPageTreeNotReproduced) {
		t.Fatalf("FullRewrite error = %v, want ErrPageTreeNotReproduced; wrote %d bytes:\n%s",
			err, out.Len(), out.String())
	}
}
