package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

func iso32005Violations(t *testing.T, objs ...string) []Violation {
	t.Helper()
	data := cyclePDF(objs...)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	errs, err := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	var out []Violation
	for _, e := range errs {
		if v, ok := e.(Violation); ok && v.Rule.Specification == "ISO_32005" {
			out = append(out, v)
		}
	}
	return out
}

// TestISO32005_DocumentContent pins "Table 5. Document-content": a
// Document element carrying direct marked-content items fails under
// PDF/UA-2.
func TestISO32005_DocumentContent(t *testing.T) {
	vs := iso32005Violations(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K 0 >>",
	)
	found := false
	for _, v := range vs {
		if v.Rule.Clause == "Table 5. Document-content" {
			found = true
		}
	}
	if !found {
		t.Errorf("Document with direct content items not flagged; got %v", vs)
	}
}

// TestISO32005_RootTwoDocuments pins "Table 5. StructTreeRoot-Document":
// the root shall contain exactly one Document.
func TestISO32005_RootTwoDocuments(t *testing.T) {
	vs := iso32005Violations(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R 6 0 R] >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
	)
	found := false
	for _, v := range vs {
		if v.Rule.Clause == "Table 5. StructTreeRoot-Document" {
			found = true
		}
	}
	if !found {
		t.Errorf("two Document roots not flagged; got %v", vs)
	}
}

// TestISO32005_LegalPairingSilent pins the negative: Document > Sect > P
// with content inside P is a legal inclusion chain and emits no Table 5
// violations.
func TestISO32005_LegalPairingSilent(t *testing.T) {
	vs := iso32005Violations(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K 6 0 R >>",
		"<< /Type /StructElem /S /Sect /P 5 0 R /K 7 0 R >>",
		"<< /Type /StructElem /S /P /P 6 0 R /K 0 >>",
	)
	if len(vs) != 0 {
		t.Errorf("legal Document>Sect>P chain flagged: %v", vs)
	}
}

// TestISO32005_NotEmittedUnderUA1 pins the profile gate.
func TestISO32005_NotEmittedUnderUA1(t *testing.T) {
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K 0 >>",
	)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	errs, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	for _, e := range errs {
		if v, ok := e.(Violation); ok && v.Rule.Specification == "ISO_32005" {
			t.Errorf("ISO 32005 rule emitted under PDF/UA-1: %v", v)
		}
	}
}
