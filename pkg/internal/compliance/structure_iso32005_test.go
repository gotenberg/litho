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

// TestISO32005_RootForbiddenChild pins the StructTreeRoot sentinel: the
// Table 5 rows "StructTreeRoot shall not contain X" key on the root
// literally, so a P element directly under the root fails. ISO 32005
// Table 5 lists no StructTreeRoot-P inclusion, so P is not a permitted
// direct child of the root.
func TestISO32005_RootForbiddenChild(t *testing.T) {
	vs := iso32005Violations(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R 6 0 R] >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
		"<< /Type /StructElem /S /P /P 4 0 R >>",
	)
	found := false
	for _, v := range vs {
		if v.Rule.Clause == "Table 5. StructTreeRoot-P" {
			found = true
		}
	}
	if !found {
		t.Errorf("P directly under StructTreeRoot not flagged; got %v", vs)
	}
}

// TestISO32005_NonStructTransparent pins NonStruct transparency: a
// Caption wrapped in a NonStruct inside Document still has Document as
// its Table 5 effective parent (PDF_UA-1__7.2-t26-fail-a golden).
func TestISO32005_NonStructTransparent(t *testing.T) {
	vs := iso32005Violations(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K 6 0 R >>",
		"<< /Type /StructElem /S /NonStruct /P 5 0 R /K 7 0 R >>",
		"<< /Type /StructElem /S /Caption /P 6 0 R >>",
	)
	found := false
	for _, v := range vs {
		if v.Rule.Clause == "Table 5. Document-Caption" {
			found = true
		}
	}
	if !found {
		t.Errorf("Caption under NonStruct under Document not flagged; got %v", vs)
	}
}

// iso32005HasClause reports whether the Table 5 clause fired.
func iso32005HasClause(vs []Violation, clause string) bool {
	for _, v := range vs {
		if v.Rule.Clause == clause {
			return true
		}
	}
	return false
}

// TestISO32005_RoleMapResolvedBeforeTable5 pins role-map-first resolution
// (ISO 32000-2:2020, 14.8.6): a structure type stands for the type its role
// map names, so a remapped standard type no longer denotes that standard
// type and drops out of the ISO/TS 32005 Table 5 matrix.
//
// Both shapes are verified against live veraPDF 1.30.2 at PDF/UA-2:
//
//   - /RoleMap << /Document /Book >> over a single /Document root child is
//     the golden PDF_UA-1__7.1-t07-fail-a shape. Book is not a standard type,
//     so the root holds zero Document elements and the root-count rule FIRES.
//     veraPDF reports "Table 5. StructTreeRoot-Document" test 1 on both the
//     golden and this exact object graph.
//   - /RoleMap << /Span /Custom >> over a Span inside a Document makes the
//     Span a Custom, which Table 5 does not govern, so "Document shall not
//     contain Span" does NOT fire. veraPDF is silent on that graph.
func TestISO32005_RoleMapResolvedBeforeTable5(t *testing.T) {
	const rootCount = "Table 5. StructTreeRoot-Document"
	const docSpan = "Table 5. Document-Span"

	remappedDoc := iso32005Violations(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /RoleMap << /Document /Book >> >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
	)
	if !iso32005HasClause(remappedDoc, rootCount) {
		t.Errorf("Document remapped to /Book should leave the root without a Document; got %v", remappedDoc)
	}

	// A StructTreeRoot whose only child is a non-Document standard type also
	// fails the root-count rule (zero Document elements).
	missing := iso32005Violations(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Part /P 4 0 R >>",
	)
	if !iso32005HasClause(missing, rootCount) {
		t.Errorf("StructTreeRoot with no Document child should fail root-count; got %v", missing)
	}

	remappedSpan := iso32005Violations(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /RoleMap << /Span /Custom >> >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /Span /P 5 0 R >>",
	)
	if iso32005HasClause(remappedSpan, docSpan) {
		t.Errorf("Span remapped to /Custom is not governed by Table 5; got %v", remappedSpan)
	}

	// The same shape without the role map keeps the Table 5 row: the guard
	// above must not silence the matrix wholesale.
	plainSpan := iso32005Violations(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /Span /P 5 0 R >>",
	)
	if !iso32005HasClause(plainSpan, docSpan) {
		t.Errorf("Span directly inside Document should fail Table 5; got %v", plainSpan)
	}
}

// TestISO32005_RoleMapInertUnderExplicitNamespace pins the namespace gate on
// role-map resolution (ISO 32000-2:2020, 14.8.6): an element that declares
// its own /NS resolves through that namespace, so the structure tree root's
// legacy /RoleMap does not reach it. veraPDF 1.30.2 reports "Table 5.
// Document-Span" test 1 on this graph even though /RoleMap maps Span away.
func TestISO32005_RoleMapInertUnderExplicitNamespace(t *testing.T) {
	vs := iso32005Violations(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /Namespaces [7 0 R] /RoleMap << /Span /Custom >> >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /NS 7 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /Span /P 5 0 R /NS 7 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
	)
	if !iso32005HasClause(vs, "Table 5. Document-Span") {
		t.Errorf("legacy /RoleMap must not reach an element with an explicit /NS; got %v", vs)
	}
}

// TestISO32005_OBJRIsContent pins OBJR counting as a content item for
// "Table 5. Document-content" (PDF_UA-1__7.18.4-t01-fail-a golden: the
// rule fails with a single OBJR kid).
func TestISO32005_OBJRIsContent(t *testing.T) {
	vs := iso32005Violations(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [7 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K 6 0 R >>",
		"<< /Type /OBJR /Obj 7 0 R >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] >>",
	)
	found := false
	for _, v := range vs {
		if v.Rule.Clause == "Table 5. Document-content" {
			found = true
		}
	}
	if !found {
		t.Errorf("OBJR kid not counted as Document content item; got %v", vs)
	}
}
