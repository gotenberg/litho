package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// nullEncryptPDF builds a one-page document whose trailer /Encrypt is value,
// with object 4 the null object for a reference to point at.
func nullEncryptPDF(value string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"null",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
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
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Encrypt %s >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, value, start)
	return b.Bytes()
}

// TestNullEncryptIsAbsent pins that a trailer /Encrypt written as the null
// object is no /Encrypt at all (ISO 32000-1, 7.3.7): the document opens
// unencrypted, as veraPDF opens it, where litho refused to open it. A /Encrypt
// naming a null object still fails, and veraPDF refuses that file too.
func TestNullEncryptIsAbsent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := nullEncryptPDF("null")
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("/Encrypt null: NewDocument: %v", err)
	}
	if doc.IsEncrypted || doc.Crypto != nil {
		t.Errorf("/Encrypt null: IsEncrypted=%v Crypto=%v, want an unencrypted document", doc.IsEncrypted, doc.Crypto)
	}

	data = nullEncryptPDF("4 0 R")
	if _, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil); err == nil {
		t.Errorf("/Encrypt naming a null object: NewDocument succeeded, want an error")
	}
}
