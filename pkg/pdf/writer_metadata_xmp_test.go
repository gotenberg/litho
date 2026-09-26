package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

// xmpTitledPDF is a one-page document whose catalog carries an XMP packet
// asserting a dc:title, alongside an /Info dictionary asserting the same title.
// Editing the title must move both.
func xmpTitledPDF(title string) []byte {
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
		`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">` + title + `</rdf:li></rdf:Alt></dc:title>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`

	return buildTestPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Title (%s) >>", title),
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(xmp), xmp),
	}, "/Info 4 0 R")
}

// buildTestPDF assembles objects into a PDF with a correct cross-reference
// table. The package's other fixtures hardcode their offsets, which a stream of
// variable length cannot do.
func buildTestPDF(objs []string, extraTrailer string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, o := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R %s >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, extraTrailer, xref)
	return buf.Bytes()
}

// TestSetMetadataSyncsXMPTitle pins that editing /Title moves the XMP dc:title
// with it.
//
// [Writer.SetMetadata] used to write only the /Info dictionary, so a document
// carrying XMP came out asserting two different titles. Every profile that
// requires the two to agree rejects that (ISO 19005-2/3, 6.6.3, worded "shall
// be equivalent to the document information dictionary entry"), and readers
// that prefer XMP, which most do, kept showing the old title.
func TestSetMetadataSyncsXMPTitle(t *testing.T) {
	t.Parallel()

	const oldTitle = "The Original Title"
	const newTitle = "The Replacement Title"

	data := xmpTitledPDF(oldTitle)
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})

	w := NewWriter(r)
	w.SetMetadata(Metadata{Title: newTitle})

	var out bytes.Buffer
	if err = w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	rr, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening written document: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := rr.Close(); closeErr != nil {
			t.Logf("closing written reader: %v", closeErr)
		}
	})

	meta, err := rr.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if got := meta["Title"]; got != newTitle {
		t.Errorf("/Info /Title = %q, want %q", got, newTitle)
	}

	packet := readXMPPacket(t, rr)
	if !strings.Contains(packet, newTitle) {
		t.Errorf("XMP packet does not carry the new title %q", newTitle)
	}
	if strings.Contains(packet, oldTitle) {
		t.Errorf("XMP packet still asserts the old title %q", oldTitle)
	}
}

// TestSetMetadataLeavesDocumentsWithoutXMPAlone pins that a document carrying
// no XMP packet does not gain one. Writing a packet would add a metadata claim
// the file never made, and the /Info dictionary alone is a complete answer.
func TestSetMetadataLeavesDocumentsWithoutXMPAlone(t *testing.T) {
	t.Parallel()

	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})
	if readXMPPacket(t, r) != "" {
		t.Skip("fixture already carries XMP; this test needs one without")
	}

	w := NewWriter(r)
	w.SetMetadata(Metadata{Title: "Any Title"})

	var out bytes.Buffer
	if err = w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	rr, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening written document: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := rr.Close(); closeErr != nil {
			t.Logf("closing written reader: %v", closeErr)
		}
	})

	if got := readXMPPacket(t, rr); got != "" {
		t.Errorf("a document with no XMP gained a %d-byte packet", len(got))
	}
}

// readXMPPacket returns the reader's XMP packet, or "" when it carries none.
// [Reader.XMPMetadata] hands back an opener rather than bytes, since the packet
// is a stream the reader does not materialise until asked.
func readXMPPacket(t *testing.T, r *Reader) string {
	t.Helper()

	ctx := context.Background()
	open, err := r.XMPMetadata(ctx)
	if err != nil || open == nil {
		return ""
	}
	rc, err := open(ctx)
	if err != nil || rc == nil {
		return ""
	}
	defer func() {
		if closeErr := rc.Close(); closeErr != nil {
			t.Logf("closing XMP stream: %v", closeErr)
		}
	}()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading XMP packet: %v", err)
	}
	return string(data)
}
