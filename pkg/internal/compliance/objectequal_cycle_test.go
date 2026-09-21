package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestPdfObjectEqual_SelfReferencingArrayTerminates pins that comparing an
// array that names itself does not recurse forever.
//
// pdfObjectEqual resolves both operands and then walks arrays element-wise, so
// object 4 = [4 0 R] compared with itself re-resolves the same array from a
// fresh frame every time: ResolveObject's cycle guard is per-resolution, the
// same property that made the DPart walk fatal. Reached from ValidatePDFA,
// ValidatePDFUA and ValidateWTPDF via tintTransformEqual/functionStreamEqual
// -> functionDictEqual, where a stack overflow is fatal and uncatchable.
func TestPdfObjectEqual_SelfReferencingArrayTerminates(t *testing.T) {
	t.Parallel()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"[4 0 R]",
	)

	ref := ast.PdfIndirectRef{ObjectNum: 4}
	// An object is equal to itself. Returning false on a repeated pair would
	// turn a hang into a WRONG VERDICT, which is worse: this function decides
	// whether two tint transforms agree.
	if !pdfObjectEqual(context.Background(), doc, ref, ref) {
		t.Error("an object compared with itself reported unequal")
	}
}

// TestPdfObjectEqual_DistinctCyclesCompareEqual is the case that actually
// exercises PAIR keying. Two different objects, each naming itself, are
// structurally indistinguishable: the walk meets the pair (5,6) a second time
// and must answer from that, not recurse.
func TestPdfObjectEqual_DistinctCyclesCompareEqual(t *testing.T) {
	t.Parallel()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"[5 0 R]",
		"[4 0 R]",
	)

	a := ast.PdfIndirectRef{ObjectNum: 4}
	b := ast.PdfIndirectRef{ObjectNum: 5}
	if !pdfObjectEqual(context.Background(), doc, a, b) {
		t.Error("two structurally identical cycles reported unequal")
	}
}

// TestPdfObjectEqual_AcyclicStillCompares is the control. A guard that answered
// "equal" for every pair it had seen, or bailed out early, would satisfy the
// tests above while making every real comparison meaningless.
func TestPdfObjectEqual_AcyclicStillCompares(t *testing.T) {
	t.Parallel()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /N 1 >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /N 1 >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /N 1 >>",
	)

	ctx := context.Background()
	same := pdfObjectEqual(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4}, ast.PdfIndirectRef{ObjectNum: 5})
	if !same {
		t.Error("two identical function dictionaries reported unequal")
	}
	diff := pdfObjectEqual(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4}, ast.PdfIndirectRef{ObjectNum: 6})
	if diff {
		t.Error("two function dictionaries differing in /C0 reported equal")
	}
}

// TestPdfObjectEqual_SharedSubtreeComparedTwice pins that the mark is keyed on
// the PAIR, not on one operand.
//
// Be precise about what this proves. The pairs compared here are (6,6) then
// (6,7), which are distinct keys, so this test returns false with a pair-keyed
// guard, with a persistent one, and with NO guard at all: it does NOT
// discriminate against an absent cycle guard. TestPdfObjectEqual_DistinctCycles-
// CompareEqual is the test that does that.
//
// What this one catches is a guard keyed on a SINGLE object number, the obvious
// wrong implementation: having seen object 6 under key /A, it would skip the
// comparison under /B and report these dictionaries EQUAL, which is a false
// compliance verdict rather than a hang.
func TestPdfObjectEqual_SharedSubtreeComparedTwice(t *testing.T) {
	t.Parallel()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /A 6 0 R /B 6 0 R >>",
		"<< /A 6 0 R /B 7 0 R >>",
		"<< /V 1 >>",
		"<< /V 2 >>",
	)

	if pdfObjectEqual(context.Background(), doc,
		ast.PdfIndirectRef{ObjectNum: 4}, ast.PdfIndirectRef{ObjectNum: 5}) {
		t.Error("dictionaries differing in /B reported equal: the second comparison was skipped")
	}
}
