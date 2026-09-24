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
// its Table 5 effective parent.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__7.2-t26-fail-a: the golden file for this shape.
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

// TestISO32005_PassThroughParent pins the Table 5 pass-through set on the
// parent side: Part, Div, and NonStruct are transparent, so an element wrapped
// in one is judged against the nearest enclosing element Table 5 constrains.
// Sect is the negative control: it is a grouping type Table 5 does constrain,
// so it stays visible as the parent.
//
// Verified against live veraPDF 1.30.2 at PDF/UA-2 on the equivalent object
// graph (/Document > wrapper > /Span holding marked content): veraPDF reports
// "Table 5. Document-Span" test 1 for each of Part, Div, and NonStruct, and
// "Table 5. Sect-Span" test 1 for Sect.
func TestISO32005_PassThroughParent(t *testing.T) {
	for _, tc := range []struct {
		wrapper string
		clause  string
	}{
		{"Div", "Table 5. Document-Span"},
		{"Part", "Table 5. Document-Span"},
		{"NonStruct", "Table 5. Document-Span"},
		{"Sect", "Table 5. Sect-Span"},
	} {
		t.Run(tc.wrapper, func(t *testing.T) {
			vs := iso32005Violations(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				"<< /Type /StructTreeRoot /K 5 0 R >>",
				"<< /Type /StructElem /S /Document /P 4 0 R /K 6 0 R >>",
				"<< /Type /StructElem /S /"+tc.wrapper+" /P 5 0 R /K 7 0 R >>",
				"<< /Type /StructElem /S /Span /P 6 0 R /K 0 >>",
			)
			if !iso32005HasClause(vs, tc.clause) {
				t.Errorf("Span under %s under Document: want %q; got %v", tc.wrapper, tc.clause, vs)
			}
		})
	}
}

// TestISO32005_PassThroughKids pins the pass-through set on the kid side: a
// wrapper contributes its own kids to the enclosing element's Table 5 kid
// counts. Two H elements behind separate wrappers still breach the
// at-most-one-H limit on Document. Sect is the negative control: it absorbs
// the H into its own count, so Document sees none.
//
// veraPDF 1.30.2 builds the same flattened list in
// GFPDStructTreeNode.getChildrenStandardTypes and reports
// "Table 5. Document-H" test 1 on the Part, Div, and NonStruct shapes only.
func TestISO32005_PassThroughKids(t *testing.T) {
	for _, tc := range []struct {
		wrapper string
		want    bool
	}{
		{"Div", true},
		{"Part", true},
		{"NonStruct", true},
		{"Sect", false},
	} {
		t.Run(tc.wrapper, func(t *testing.T) {
			vs := iso32005Violations(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				"<< /Type /StructTreeRoot /K 5 0 R >>",
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R 8 0 R] >>",
				"<< /Type /StructElem /S /"+tc.wrapper+" /P 5 0 R /K 7 0 R >>",
				"<< /Type /StructElem /S /H /P 6 0 R /K 0 >>",
				"<< /Type /StructElem /S /"+tc.wrapper+" /P 5 0 R /K 9 0 R >>",
				"<< /Type /StructElem /S /H /P 8 0 R /K 1 >>",
			)
			if got := iso32005HasClause(vs, "Table 5. Document-H"); got != tc.want {
				t.Errorf("two H behind %s wrappers: Table 5. Document-H fired=%v, want %v; got %v",
					tc.wrapper, got, tc.want, vs)
			}
		})
	}
}

// TestISO32005_PassThroughContent pins the pass-through set on the content
// side: marked content inside a wrapper counts as content of the enclosing
// element, so a Document whose only content sits inside a Part, Div, or
// NonStruct still contains content directly. Sect is the negative control.
//
// veraPDF 1.30.2 recurses through the same set in
// GFPDStructTreeNode.gethasContentItems and reports
// "Table 5. Document-content" test 1 on the three pass-through shapes.
func TestISO32005_PassThroughContent(t *testing.T) {
	for _, tc := range []struct {
		wrapper string
		clause  string
	}{
		{"Div", "Table 5. Document-content"},
		{"Part", "Table 5. Document-content"},
		{"NonStruct", "Table 5. Document-content"},
		{"Sect", "Table 5. Sect-content"},
	} {
		t.Run(tc.wrapper, func(t *testing.T) {
			vs := iso32005Violations(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				"<< /Type /StructTreeRoot /K 5 0 R >>",
				"<< /Type /StructElem /S /Document /P 4 0 R /K 6 0 R >>",
				"<< /Type /StructElem /S /"+tc.wrapper+" /P 5 0 R /K 0 >>",
			)
			if !iso32005HasClause(vs, tc.clause) {
				t.Errorf("content inside %s inside Document: want %q; got %v", tc.wrapper, tc.clause, vs)
			}
		})
	}
}

// TestISO32005_PassThroughRootKids pins the pass-through set at the structure
// tree root, where it suppresses a firing rather than adding one: a Document
// reached through a wrapper still counts as the root's one Document, so the
// exactly-one-Document rule shall stay silent. veraPDF 1.30.2 is silent on
// this graph because getChildrenStandardTypes flattens the wrapper away.
func TestISO32005_PassThroughRootKids(t *testing.T) {
	for _, wrapper := range []string{"Div", "Part", "NonStruct"} {
		t.Run(wrapper, func(t *testing.T) {
			vs := iso32005Violations(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				"<< /Type /StructTreeRoot /K 5 0 R >>",
				"<< /Type /StructElem /S /"+wrapper+" /P 4 0 R /K 6 0 R >>",
				"<< /Type /StructElem /S /Document /P 5 0 R /K 7 0 R >>",
				"<< /Type /StructElem /S /P /P 6 0 R /K 0 >>",
			)
			if iso32005HasClause(vs, "Table 5. StructTreeRoot-Document") {
				t.Errorf("Document behind a %s wrapper should still count as the root's Document; got %v",
					wrapper, vs)
			}
		})
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
//   - /RoleMap << /Document /Book >> over a single /Document root child. Book
//     is not a standard type, so the root holds zero Document elements and
//     the root-count rule FIRES. veraPDF reports "Table 5.
//     StructTreeRoot-Document" test 1 on this exact object graph.
//   - /RoleMap << /Span /Custom >> over a Span inside a Document makes the
//     Span a Custom, which Table 5 does not govern, so "Document shall not
//     contain Span" does NOT fire. veraPDF is silent on that graph.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__7.1-t07-fail-a: the golden file with the
//     /RoleMap << /Document /Book >> shape, on which veraPDF also reports
//     "Table 5. StructTreeRoot-Document" test 1.
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

// TestISO32005_NamespaceVocabularyGatesTable5 pins the namespace gate on the
// Table 5 key. A type is standard only in the namespace that defines it
// (ISO 32000-2:2020, 14.8.6), so TOC and BlockQuote key into Table 5 from the
// PDF 1.7 namespace and key into nothing from the PDF 2.0 one, where neither is
// a standard structure type. veraPDF 1.30.2 reports the pairing on the first
// graph and reports ISO 14289-2 §8.2.4 t1 instead of any Table 5 rule on the
// second, because both elements become SENonStandard there.
func TestISO32005_NamespaceVocabularyGatesTable5(t *testing.T) {
	build := func(t *testing.T, ns string) []Violation {
		t.Helper()
		return iso32005Violations(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /StructTreeRoot /K 5 0 R /Namespaces [8 0 R 9 0 R] >>",
			"<< /Type /StructElem /S /Document /P 4 0 R /NS 8 0 R /K [6 0 R] >>",
			"<< /Type /StructElem /S /TOC /P 5 0 R /NS "+ns+" /K [7 0 R] >>",
			"<< /Type /StructElem /S /BlockQuote /P 6 0 R /NS "+ns+" >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) >>",
		)
	}

	if vs := build(t, "9 0 R"); !iso32005HasClause(vs, "Table 5. TOC-BlockQuote") {
		t.Errorf("BlockQuote inside TOC in the PDF 1.7 namespace must fail Table 5; got %v", vs)
	}
	if vs := build(t, "8 0 R"); iso32005HasClause(vs, "Table 5. TOC-BlockQuote") {
		t.Errorf("neither TOC nor BlockQuote is standard in the PDF 2.0 namespace, so Table 5 must not judge the pairing; got %v", vs)
	}
}

// TestISO32005_OBJRIsContent pins OBJR counting as a content item for
// "Table 5. Document-content": the rule fails with a single OBJR kid.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__7.18.4-t01-fail-a: the golden file, where the rule
//     fails with a single OBJR kid.
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
