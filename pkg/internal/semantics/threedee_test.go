package semantics

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func testDoc(t *testing.T) *document.Document {
	t.Helper()
	// Build a minimal valid PDF so NewDocument doesn't panic.
	pdf := "%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\nxref\n0 2\n0000000000 65535 f \n0000000009 00000 n \ntrailer\n<< /Root 1 0 R /Size 2 >>\nstartxref\n52\n%%EOF"
	r := strings.NewReader(pdf)
	doc, err := document.NewDocument(context.Background(), r, int64(len(pdf)), nil)
	if err != nil {
		t.Fatalf("creating test document: %v", err)
	}
	return doc
}

func TestDetect3DFromStream_PRC(t *testing.T) {
	doc := testDoc(t)
	stream := ast.NewStream(ast.PdfDict{}, []byte("PRC\x00\x01\x02\x03"))
	got, err := detect3DFromStream(context.Background(), doc, stream)
	if err != nil {
		t.Fatal(err)
	}
	if got != "PRC" {
		t.Errorf("got %q, want PRC", got)
	}
}

func TestDetect3DFromStream_U3D(t *testing.T) {
	doc := testDoc(t)
	stream := ast.NewStream(ast.PdfDict{}, []byte("\x55\x33\x44\x00\x01\x02\x03"))
	got, err := detect3DFromStream(context.Background(), doc, stream)
	if err != nil {
		t.Fatal(err)
	}
	if got != "U3D" {
		t.Errorf("got %q, want U3D", got)
	}
}

func TestDetect3DFromStream_TooShort(t *testing.T) {
	doc := testDoc(t)
	stream := ast.NewStream(ast.PdfDict{}, []byte("\x00\x01"))
	got, err := detect3DFromStream(context.Background(), doc, stream)
	if err != nil {
		t.Fatal(err)
	}
	if got != "U3D" {
		t.Errorf("got %q, want U3D (default for short data)", got)
	}
}

func TestDetect3DFromStream_Empty(t *testing.T) {
	doc := testDoc(t)
	stream := ast.NewStream(ast.PdfDict{}, nil)
	got, err := detect3DFromStream(context.Background(), doc, stream)
	if err != nil {
		t.Fatal(err)
	}
	if got != "U3D" {
		t.Errorf("got %q, want U3D (default for empty data)", got)
	}
}
