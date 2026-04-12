package remediation

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestFixXMP_GeneratesFromScratch_PDFA2b(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))

	descs, err := fixXMP(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixXMP: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if descs[0] != "generated XMP metadata stream" {
		t.Errorf("unexpected description: %q", descs[0])
	}

	newCatalog := pendingDict(t, alloc, 1)
	metaRef, ok := newCatalog["Metadata"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("catalog /Metadata: got %T, want PdfIndirectRef", newCatalog["Metadata"])
	}

	metaStream, ok := alloc.Pending()[metaRef.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("XMP stream: got %T, want PdfStream", alloc.Pending()[metaRef.ObjectNum])
	}
	if metaStream.Dict["Type"] != ast.PdfName("Metadata") {
		t.Errorf("XMP stream /Type: got %v, want Metadata", metaStream.Dict["Type"])
	}
	if metaStream.Dict["Subtype"] != ast.PdfName("XML") {
		t.Errorf("XMP stream /Subtype: got %v, want XML", metaStream.Dict["Subtype"])
	}

	xmpBytes, err := metaStream.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading XMP bytes: %v", err)
	}
	// Verify the PDF/A-2b part + conformance land in the XMP body via the
	// canonical compliance.PDFAPart / PDFAConformance helpers.
	xmpStr := string(xmpBytes)
	if !strings.Contains(xmpStr, "<pdfaid:part>2</pdfaid:part>") {
		t.Errorf("XMP missing pdfaid:part=2:\n%s", xmpStr)
	}
	if !strings.Contains(xmpStr, "<pdfaid:conformance>B</pdfaid:conformance>") {
		t.Errorf("XMP missing pdfaid:conformance=B:\n%s", xmpStr)
	}
}

func TestFixXMP_GeneratesFromScratch_PDFA4(t *testing.T) {
	// Bare PDF/A-4 has no explicit conformance level. The fixer should
	// default to "B" via compliance.PDFAConformance.
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))

	_, err := fixXMP(context.Background(), alloc, doc, Options{}, nil, "PDF/A-4")
	if err != nil {
		t.Fatalf("fixXMP: %v", err)
	}

	newCatalog := pendingDict(t, alloc, 1)
	metaRef := newCatalog["Metadata"].(ast.PdfIndirectRef)
	metaStream := alloc.Pending()[metaRef.ObjectNum].(ast.PdfStream)
	xmpBytes, _ := metaStream.Bytes(context.Background())
	xmpStr := string(xmpBytes)

	if !strings.Contains(xmpStr, "<pdfaid:part>4</pdfaid:part>") {
		t.Errorf("XMP missing pdfaid:part=4:\n%s", xmpStr)
	}
}

func TestFixXMP_GeneratesFromScratch_PDFUA1(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))

	_, err := fixXMP(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixXMP: %v", err)
	}

	newCatalog := pendingDict(t, alloc, 1)
	metaRef := newCatalog["Metadata"].(ast.PdfIndirectRef)
	metaStream := alloc.Pending()[metaRef.ObjectNum].(ast.PdfStream)
	xmpBytes, _ := metaStream.Bytes(context.Background())

	// Per ISO 14289-1, the UA identifier lives under pdfuaid:part.
	if !bytes.Contains(xmpBytes, []byte("pdfuaid:part>1")) {
		t.Errorf("XMP missing pdfuaid:part=1:\n%s", xmpBytes)
	}
}
