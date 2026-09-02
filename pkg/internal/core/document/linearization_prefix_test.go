package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// prefixedLinearizedPDF builds a linearized document preceded by prefixBytes of
// junk, the shape of pdfjs issue11330.pdf, whose first 27 bytes are Java
// serialization. /L counts from the header, so it excludes the prefix.
func prefixedLinearizedPDF(prefix string) []byte {
	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"

	objs := []string{
		"<< /Type /Catalog /Pages 3 0 R >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 3 0 R /MediaBox [0 0 612 792] /Contents 5 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}

	// Build the body once to learn its length, then write the linearization
	// dictionary with the /L the finished file will have.
	build := func(fileLength int) []byte {
		var buf bytes.Buffer
		buf.WriteString(prefix)
		headerAt := buf.Len()
		buf.WriteString("%PDF-1.5\n")

		offs := make([]int, len(objs)+2)
		offs[1] = buf.Len()
		fmt.Fprintf(&buf, "1 0 obj\n<< /Linearized 1 /L %d /O 4 /N 1 /E 400 /T 300 >>\nendobj\n",
			fileLength)
		for i, o := range objs {
			offs[i+2] = buf.Len()
			fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+2, o)
		}
		xref := buf.Len()
		fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+2)
		for i := 1; i <= len(objs)+1; i++ {
			fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
		}
		fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 2 0 R >>\nstartxref\n%d\n%%%%EOF\n",
			len(objs)+2, xref)

		// /L is measured from the header, so the prefix is not counted.
		_ = headerAt
		return buf.Bytes()
	}

	// /L states the length of the file that contains it, so writing a longer
	// number lengthens the file. Iterate to the fixed point where the declared
	// value equals the length actually produced.
	data := build(0)
	for range 8 {
		want := len(data) - len(prefix)
		next := build(want)
		if len(next)-len(prefix) == want {
			return next
		}
		data = next
	}
	return data
}

// TestLinearizedBehindALeadingPrefix pins that a linearized file is still
// recognised when something precedes its header.
//
// litho already accepts a leading prefix when locating %PDF-, and xref offsets
// stay absolute from byte 0. The linearization probe read from byte 0 instead of
// the header, so it parsed the prefix rather than the dictionary, and /L was
// compared against the raw file size rather than the length from the header.
// pdfjs issue11330.pdf carries 27 bytes of Java serialization and declares
// /L 129725 in a 129752 byte file; pdf.js reports it linearized and litho did
// not.
func TestLinearizedBehindALeadingPrefix(t *testing.T) {
	t.Parallel()

	const prefix = "\xac\xed\x00\x05ur\x00\x02[B\xac\xf3\x17\xf8\x06\x08T\xe0\x02\x00\x00xp\x00\x01\xfa\xbd"
	data := prefixedLinearizedPDF(prefix)

	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.HeaderOffset != int64(len(prefix)) {
		t.Fatalf("header offset = %d, want %d", doc.HeaderOffset, len(prefix))
	}
	if !doc.IsLinearized() {
		t.Error("the document was not reported as linearized: the probe read the prefix")
	}
}

// TestLinearizedWithoutPrefixStillWorks is the control for the ordinary case,
// where the header sits at byte 0.
func TestLinearizedWithoutPrefixStillWorks(t *testing.T) {
	t.Parallel()

	data := prefixedLinearizedPDF("")
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.HeaderOffset != 0 {
		t.Fatalf("header offset = %d, want 0", doc.HeaderOffset)
	}
	if !doc.IsLinearized() {
		t.Error("a linearized document with no prefix was not reported as linearized")
	}
}

// TestStaleLinearizationStillRejected pins that the /L check keeps doing its
// job: a dictionary left behind by a re-save must not be believed.
func TestStaleLinearizationStillRejected(t *testing.T) {
	t.Parallel()

	data := prefixedLinearizedPDF("")
	// Append a byte so the file no longer matches its declared /L.
	data = append(data, '\n')

	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.IsLinearized() {
		t.Error("a stale linearization dictionary was accepted: /L no longer matches the file")
	}
}
