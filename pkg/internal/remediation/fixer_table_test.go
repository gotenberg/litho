package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestMatchTableSections(t *testing.T) {
	t.Parallel()
	if !matchTableSections(compliance.Violation{Rule: compliance.Rule_14289_1_7_2_t13}) {
		t.Error("must match §7.2 t13 (TFoot but no TBody)")
	}
	if !matchTableSections(compliance.Violation{Rule: compliance.Rule_14289_1_7_2_t14}) {
		t.Error("must match §7.2 t14 (THead but no TBody)")
	}
	if matchTableSections(compliance.Violation{Rule: compliance.Rule{Clause: "7.2", TestNumber: 5}}) {
		t.Error("must not match other §7.2 rules (the clause is shared with /Lang)")
	}
	if matchTableSections(compliance.Violation{Rule: compliance.Rule{Clause: "8.4.4", TestNumber: 1}}) {
		t.Error("must not match non-table clauses")
	}
}

func TestFixTableSections_WrapsBodyRows(t *testing.T) {
	// Table /K = [THead, TR, TR] with no TBody. The body rows must move into a
	// new TBody, with /P pointers and the Table /K rebuilt.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R 7 0 R 8 0 R] >>",
		"<< /Type /StructElem /S /THead /P 5 0 R >>",
		"<< /Type /StructElem /S /TR /P 5 0 R >>",
		"<< /Type /StructElem /S /TR /P 5 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableSections(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableSections: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	// The Table /K is now [THead, TBody].
	tableK, ok := pendingDict(t, alloc, 5)["K"].(ast.PdfArray)
	if !ok || len(tableK) != 2 {
		t.Fatalf("table /K: got %v, want [THead TBody]", pendingDict(t, alloc, 5)["K"])
	}
	if tableK[0] != (ast.PdfIndirectRef{ObjectNum: 6}) {
		t.Errorf("table /K[0]: got %v, want THead 6 0 R", tableK[0])
	}
	tbodyRef, ok := tableK[1].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("table /K[1]: got %T, want a TBody indirect ref", tableK[1])
	}

	// The new TBody element.
	tbody := pendingDict(t, alloc, tbodyRef.ObjectNum)
	if tbody["S"] != ast.PdfName("TBody") {
		t.Errorf("TBody /S: got %v, want TBody", tbody["S"])
	}
	if tbody["P"] != (ast.PdfIndirectRef{ObjectNum: 5}) {
		t.Errorf("TBody /P: got %v, want Table 5 0 R", tbody["P"])
	}
	if tbodyK, ok := tbody["K"].(ast.PdfArray); !ok || len(tbodyK) != 2 {
		t.Fatalf("TBody /K: got %v, want [TR TR]", tbody["K"])
	}

	// Each moved TR now points at the TBody.
	for _, n := range []int{7, 8} {
		if pendingDict(t, alloc, n)["P"] != tbodyRef {
			t.Errorf("TR %d /P: got %v, want TBody %v", n, pendingDict(t, alloc, n)["P"], tbodyRef)
		}
	}
}

func TestFixTableSections_NonContiguousRows(t *testing.T) {
	// Body rows straddle a TFoot: /K = [THead, TR, TFoot, TR]. Both TRs must move
	// into the TBody even though they are not adjacent, the TBody must take the
	// first row's slot, and the TFoot must be preserved in place. This guards the
	// rebuild against the "drop the second, non-contiguous row" failure mode.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R 7 0 R 8 0 R 9 0 R] >>",
		"<< /Type /StructElem /S /THead /P 5 0 R >>",
		"<< /Type /StructElem /S /TR /P 5 0 R >>",
		"<< /Type /StructElem /S /TFoot /P 5 0 R >>",
		"<< /Type /StructElem /S /TR /P 5 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableSections(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableSections: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	// Table /K is [THead, TBody, TFoot]: both TRs gone, TFoot kept after the TBody.
	tableK, ok := pendingDict(t, alloc, 5)["K"].(ast.PdfArray)
	if !ok || len(tableK) != 3 {
		t.Fatalf("table /K: got %v, want [THead TBody TFoot]", pendingDict(t, alloc, 5)["K"])
	}
	if tableK[0] != (ast.PdfIndirectRef{ObjectNum: 6}) {
		t.Errorf("table /K[0]: got %v, want THead 6 0 R", tableK[0])
	}
	if tableK[2] != (ast.PdfIndirectRef{ObjectNum: 8}) {
		t.Errorf("table /K[2]: got %v, want TFoot 8 0 R", tableK[2])
	}
	tbodyRef, ok := tableK[1].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("table /K[1]: got %T, want a TBody indirect ref", tableK[1])
	}

	// Both non-adjacent rows landed in the TBody, in document order.
	tbodyK, ok := pendingDict(t, alloc, tbodyRef.ObjectNum)["K"].(ast.PdfArray)
	if !ok || len(tbodyK) != 2 {
		t.Fatalf("TBody /K: got %v, want [TR@7 TR@9]", pendingDict(t, alloc, tbodyRef.ObjectNum)["K"])
	}
	if tbodyK[0] != (ast.PdfIndirectRef{ObjectNum: 7}) || tbodyK[1] != (ast.PdfIndirectRef{ObjectNum: 9}) {
		t.Errorf("TBody /K: got %v, want [7 0 R 9 0 R]", tbodyK)
	}
	for _, n := range []int{7, 9} {
		if pendingDict(t, alloc, n)["P"] != tbodyRef {
			t.Errorf("TR %d /P: got %v, want TBody %v", n, pendingDict(t, alloc, n)["P"], tbodyRef)
		}
	}
}

func TestFixTableSections_TFootCase(t *testing.T) {
	// A Table with a TFoot but no TBody (ISO 14289-1 §7.2 t13). The body rows
	// before the TFoot must wrap into a TBody.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R 7 0 R 8 0 R] >>",
		"<< /Type /StructElem /S /TR /P 5 0 R >>",
		"<< /Type /StructElem /S /TR /P 5 0 R >>",
		"<< /Type /StructElem /S /TFoot /P 5 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableSections(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableSections: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	// Table /K is [TBody, TFoot].
	tableK, ok := pendingDict(t, alloc, 5)["K"].(ast.PdfArray)
	if !ok || len(tableK) != 2 {
		t.Fatalf("table /K: got %v, want [TBody TFoot]", pendingDict(t, alloc, 5)["K"])
	}
	if tableK[1] != (ast.PdfIndirectRef{ObjectNum: 8}) {
		t.Errorf("table /K[1]: got %v, want TFoot 8 0 R", tableK[1])
	}
	tbodyRef, ok := tableK[0].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("table /K[0]: got %T, want a TBody indirect ref", tableK[0])
	}
	if tbodyK, ok := pendingDict(t, alloc, tbodyRef.ObjectNum)["K"].(ast.PdfArray); !ok || len(tbodyK) != 2 {
		t.Fatalf("TBody /K: got %v, want [TR TR]", pendingDict(t, alloc, tbodyRef.ObjectNum)["K"])
	}
}

func TestFixTableSections_SkipsInlineTR(t *testing.T) {
	// A Table whose /K mixes an inline TR (a direct dict, not an indirect ref)
	// must be left untouched: the rebuild only re-parents indirect rows, and a
	// partial wrap would strand the inline row at the Table level.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R << /Type /StructElem /S /TR >> 7 0 R] >>",
		"<< /Type /StructElem /S /THead /P 5 0 R >>",
		"<< /Type /StructElem /S /TR /P 5 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableSections(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableSections: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a table with an inline TR must be left untouched: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixTableSections_PreservesCaption(t *testing.T) {
	// A Caption is neither a section nor a row; it must survive the rebuild in
	// its original leading slot, ahead of the THead and the new TBody.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R 7 0 R 8 0 R 9 0 R] >>",
		"<< /Type /StructElem /S /Caption /P 5 0 R >>",
		"<< /Type /StructElem /S /THead /P 5 0 R >>",
		"<< /Type /StructElem /S /TR /P 5 0 R >>",
		"<< /Type /StructElem /S /TR /P 5 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableSections(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableSections: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	// Table /K is [Caption, THead, TBody]: the Caption keeps its leading slot.
	tableK, ok := pendingDict(t, alloc, 5)["K"].(ast.PdfArray)
	if !ok || len(tableK) != 3 {
		t.Fatalf("table /K: got %v, want [Caption THead TBody]", pendingDict(t, alloc, 5)["K"])
	}
	if tableK[0] != (ast.PdfIndirectRef{ObjectNum: 6}) {
		t.Errorf("table /K[0]: got %v, want Caption 6 0 R", tableK[0])
	}
	if tableK[1] != (ast.PdfIndirectRef{ObjectNum: 7}) {
		t.Errorf("table /K[1]: got %v, want THead 7 0 R", tableK[1])
	}
	if _, ok := tableK[2].(ast.PdfIndirectRef); !ok {
		t.Errorf("table /K[2]: got %T, want a TBody indirect ref", tableK[2])
	}
}

func TestFixTableSections_SkipsIfTBodyPresent(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R 7 0 R] >>",
		"<< /Type /StructElem /S /THead /P 5 0 R >>",
		"<< /Type /StructElem /S /TBody /P 5 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableSections(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableSections: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a table that already has a TBody needs no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixTableSections_SkipsIfNoBodyRows(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /THead /P 5 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableSections(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableSections: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a table with no direct body rows needs no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}
