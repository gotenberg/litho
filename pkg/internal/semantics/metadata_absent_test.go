package semantics

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// bareDocPDF assembles a one-page document with no /Info dictionary, no XMP
// packet and no catalog /Lang, so metadata extraction finds nothing.
func bareDocPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}

	var buf []byte
	buf = append(buf, "%PDF-1.7\n"...)
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = len(buf)
		buf = append(buf, fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, body)...)
	}
	start := len(buf)
	buf = append(buf, fmt.Sprintf("xref\n0 %d\n", len(objs)+1)...)
	buf = append(buf, "0000000000 65535 f \n"...)
	for i := 1; i <= len(objs); i++ {
		buf = append(buf, fmt.Sprintf("%010d 00000 n \n", offsets[i])...)
	}
	buf = append(buf, fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, start)...)
	return buf
}

// TestAbsentMetadataReportsTheSentinel pins that a document carrying no
// metadata reports [ErrNoMetadata] rather than an anonymous error.
//
// Absence is not a defect, and the CLI must be able to tell it from a genuine
// extraction failure without matching on the message, which CONTRIBUTING
// forbids. Before this, `litho metadata` exited non-zero on a valid document
// while the sibling verbs printed "(no ...)" and exited 0.
func TestAbsentMetadataReportsTheSentinel(t *testing.T) {
	t.Parallel()

	data := bareDocPDF()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	meta, err := Metadata(context.Background(), doc)
	if !errors.Is(err, ErrNoMetadata) {
		t.Fatalf("err = %v, want ErrNoMetadata so callers can match it with errors.Is", err)
	}
	if len(meta) != 0 {
		t.Errorf("metadata = %v, want none alongside the sentinel", meta)
	}
}
