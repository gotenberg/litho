package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestMatchTableCells(t *testing.T) {
	t.Parallel()
	if !matchTableCells(compliance.Violation{Rule: compliance.Rule_14289_1_7_2_t04}) {
		t.Error("must match §7.2 t04 (TD/TH not in TR)")
	}
	// t03 (wrong child) and t05 (TR misplaced) share clause 7.2 but are other fixers' concern.
	if matchTableCells(compliance.Violation{Rule: compliance.Rule_14289_1_7_2_t03}) {
		t.Error("must not match §7.2 t03")
	}
	if matchTableCells(compliance.Violation{Rule: compliance.Rule_14289_1_7_2_t05}) {
		t.Error("must not match §7.2 t05")
	}
}

func TestFixTableCells_WrapsCellsUnderTBody(t *testing.T) {
	// TBody /K = [TD, TD] with the cells directly under the TBody (no TR). Both
	// cells must move into a new TR.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /TBody /P 5 0 R /K [7 0 R 8 0 R] >>",
		"<< /Type /StructElem /S /TD /P 6 0 R >>",
		"<< /Type /StructElem /S /TD /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableCells(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableCells: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	tbodyK, ok := pendingDict(t, alloc, 6)["K"].(ast.PdfArray)
	if !ok || len(tbodyK) != 1 {
		t.Fatalf("TBody /K: got %v, want [TR]", pendingDict(t, alloc, 6)["K"])
	}
	trRef, ok := tbodyK[0].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("TBody /K[0]: got %T, want a TR indirect ref", tbodyK[0])
	}
	tr := pendingDict(t, alloc, trRef.ObjectNum)
	if tr["S"] != ast.PdfName("TR") {
		t.Errorf("TR /S: got %v, want TR", tr["S"])
	}
	if tr["P"] != (ast.PdfIndirectRef{ObjectNum: 6}) {
		t.Errorf("TR /P: got %v, want TBody 6 0 R", tr["P"])
	}
	if trK, ok := tr["K"].(ast.PdfArray); !ok || len(trK) != 2 || trK[0] != (ast.PdfIndirectRef{ObjectNum: 7}) || trK[1] != (ast.PdfIndirectRef{ObjectNum: 8}) {
		t.Fatalf("TR /K: got %v, want [TD@7 TD@8]", tr["K"])
	}
	for _, n := range []int{7, 8} {
		if pendingDict(t, alloc, n)["P"] != trRef {
			t.Errorf("TD %d /P: got %v, want TR %v", n, pendingDict(t, alloc, n)["P"], trRef)
		}
	}
}

func TestFixTableCells_WrapsCellsUnderTable(t *testing.T) {
	// Cells directly under the Table itself.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R 7 0 R] >>",
		"<< /Type /StructElem /S /TH /P 5 0 R >>",
		"<< /Type /StructElem /S /TD /P 5 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableCells(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableCells: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	tableK, ok := pendingDict(t, alloc, 5)["K"].(ast.PdfArray)
	if !ok || len(tableK) != 1 {
		t.Fatalf("Table /K: got %v, want [TR]", pendingDict(t, alloc, 5)["K"])
	}
	trRef := tableK[0].(ast.PdfIndirectRef)
	if trK, ok := pendingDict(t, alloc, trRef.ObjectNum)["K"].(ast.PdfArray); !ok || len(trK) != 2 {
		t.Fatalf("TR /K: got %v, want [TH TD]", pendingDict(t, alloc, trRef.ObjectNum)["K"])
	}
}

func TestFixTableCells_MixedRowAndCells(t *testing.T) {
	// TBody /K = [TR, TD, TD]: the existing TR is preserved, the loose cells form
	// a new TR placed at the first loose cell's slot.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /TBody /P 5 0 R /K [7 0 R 8 0 R 9 0 R] >>",
		"<< /Type /StructElem /S /TR /P 6 0 R /K [10 0 R] >>",
		"<< /Type /StructElem /S /TD /P 6 0 R >>",
		"<< /Type /StructElem /S /TD /P 6 0 R >>",
		"<< /Type /StructElem /S /TD /P 7 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableCells(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableCells: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	tbodyK, ok := pendingDict(t, alloc, 6)["K"].(ast.PdfArray)
	if !ok || len(tbodyK) != 2 {
		t.Fatalf("TBody /K: got %v, want [TR(existing) TR(new)]", pendingDict(t, alloc, 6)["K"])
	}
	if tbodyK[0] != (ast.PdfIndirectRef{ObjectNum: 7}) {
		t.Errorf("TBody /K[0]: got %v, want existing TR 7 0 R", tbodyK[0])
	}
	newTR, ok := tbodyK[1].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("TBody /K[1]: got %T, want a new TR indirect ref", tbodyK[1])
	}
	if trK, ok := pendingDict(t, alloc, newTR.ObjectNum)["K"].(ast.PdfArray); !ok || len(trK) != 2 || trK[0] != (ast.PdfIndirectRef{ObjectNum: 8}) || trK[1] != (ast.PdfIndirectRef{ObjectNum: 9}) {
		t.Fatalf("new TR /K: got %v, want [TD@8 TD@9]", pendingDict(t, alloc, newTR.ObjectNum)["K"])
	}
	if _, queued := alloc.Pending()[7]; queued {
		t.Error("the existing TR (obj 7) must not be rewritten")
	}
}

func TestFixTableCells_SkipsCellUnderNonContainer(t *testing.T) {
	// A TD under a P is NOT a row-group child; wrapping it in a TR would only move
	// the violation, so the fixer leaves it for manual repair.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /P /P 5 0 R /K [7 0 R] >>",
		"<< /Type /StructElem /S /TD /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableCells(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableCells: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a cell under a non-row-group element must be left alone: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixTableCells_SkipsInlineCell(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /TBody /P 5 0 R /K [7 0 R << /Type /StructElem /S /TD >>] >>",
		"<< /Type /StructElem /S /TD /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableCells(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableCells: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a container with an inline cell must be left untouched: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixTableCells_NoopWhenWellFormed(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /TBody /P 5 0 R /K [7 0 R] >>",
		"<< /Type /StructElem /S /TR /P 6 0 R /K [8 0 R] >>",
		"<< /Type /StructElem /S /TD /P 7 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixTableCells(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableCells: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a well-formed table needs no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixTableCells_Idempotent(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Table /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /TBody /P 5 0 R /K [7 0 R 8 0 R] >>",
		"<< /Type /StructElem /S /TD /P 6 0 R >>",
		"<< /Type /StructElem /S /TD /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixTableCells(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixTableCells (first pass): %v", err)
	}
	pendingAfterFirst := len(alloc.Pending())

	descs, err := fixTableCells(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixTableCells (second pass): %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("second pass must be a no-op, got %v", descs)
	}
	if len(alloc.Pending()) != pendingAfterFirst {
		t.Errorf("second pass changed pending objects: was %d, now %d", pendingAfterFirst, len(alloc.Pending()))
	}
}
