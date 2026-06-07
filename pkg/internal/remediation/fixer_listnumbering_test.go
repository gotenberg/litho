package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// labelledListPDF builds a tagged single-page PDF: StructTreeRoot -> L -> LI ->
// {Lbl, LBody}. The L element carries the given extra entries (e.g. an existing
// /A attribute), letting tests vary the ListNumbering state.
func labelledListPDF(lExtra string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R]"+lExtra+" >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [7 0 R 8 0 R] >>",
		"<< /Type /StructElem /S /Lbl /P 6 0 R >>",
		"<< /Type /StructElem /S /LBody /P 6 0 R >>",
	)
}

func TestMatchListNumbering(t *testing.T) {
	t.Parallel()
	if !matchListNumbering(compliance.Violation{Rule: compliance.Rule_14289_2_8_2_5_25_t01}) {
		t.Error("must match §8.2.5.25 t01")
	}
	if matchListNumbering(compliance.Violation{Rule: compliance.Rule_14289_2_8_2_5_25_t02}) {
		t.Error("must not match §8.2.5.25 t02 (LBody, a different fixer's concern)")
	}
}

func TestFixListNumbering_AddsInAggressive(t *testing.T) {
	doc, alloc := newTestDoc(t, labelledListPDF(""))

	// Options{} has PreserveContent == false: aggressive mode.
	descs, err := fixListNumbering(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixListNumbering: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	a, ok := pendingDict(t, alloc, 5)["A"].(ast.PdfDict)
	if !ok {
		t.Fatalf("L /A: got %T, want attribute dict", pendingDict(t, alloc, 5)["A"])
	}
	if a["ListNumbering"] != defaultListNumbering {
		t.Errorf("/ListNumbering: got %v, want %v", a["ListNumbering"], defaultListNumbering)
	}
	if a["O"] != ast.PdfName("List") {
		t.Errorf("/O: got %v, want /List", a["O"])
	}
}

func TestFixListNumbering_NoopSafeMode(t *testing.T) {
	doc, alloc := newTestDoc(t, labelledListPDF(""))

	descs, err := fixListNumbering(context.Background(), alloc, doc, Options{PreserveContent: true}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixListNumbering: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("safe mode must leave the list for a human: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixListNumbering_NoopWhenPresent(t *testing.T) {
	// L already declares a usable ListNumbering: nothing to do, even aggressive.
	doc, alloc := newTestDoc(t, labelledListPDF(" /A << /O /List /ListNumbering /Decimal >>"))

	descs, err := fixListNumbering(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixListNumbering: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a list with ListNumbering needs no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixListNumbering_NoopWhenNoLabel(t *testing.T) {
	// LI has no Lbl child, so ListNumbering is not required.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [7 0 R] >>",
		"<< /Type /StructElem /S /LBody /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListNumbering(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixListNumbering: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("an unlabelled list needs no ListNumbering: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixListNumbering_AppendsToExistingAttrArray(t *testing.T) {
	// An existing /A array (with an unrelated owner) gains the List attribute
	// rather than being clobbered.
	doc, alloc := newTestDoc(t, labelledListPDF(" /A [<< /O /Layout /Placement /Block >>]"))

	descs, err := fixListNumbering(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixListNumbering: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	arr, ok := pendingDict(t, alloc, 5)["A"].(ast.PdfArray)
	if !ok || len(arr) != 2 {
		t.Fatalf("L /A: got %v, want 2-element array", pendingDict(t, alloc, 5)["A"])
	}
	last, ok := arr[1].(ast.PdfDict)
	if !ok || last["ListNumbering"] != defaultListNumbering {
		t.Errorf("appended attr: got %v, want ListNumbering=%v", arr[1], defaultListNumbering)
	}
}
