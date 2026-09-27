package remediation

import (
	"context"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixJavaScript_SharedNextArrayFanOut pins that a /Next array written as
// an object of its own, whose direct actions each name it again, is spliced
// once and rewritten at its own object number: spliced anew wherever it is
// reached, it fans out 2^depth up to maxActionChainDepth.
func TestFixJavaScript_SharedNextArrayFanOut(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /OpenAction << /S /Named /N /NextPage /Next 4 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"[<< /S /JavaScript /JS (a) /Next 4 0 R >> << /S /GoTo /D [3 0 R /Fit] /Next 4 0 R >>]",
	))
	fixes, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if ctx.Err() != nil {
		t.Fatal("the splice did not end before the deadline")
	}
	if err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}
	if want := "cut 1 forbidden action(s) out of the action chain at /OpenAction of catalog"; len(fixes) != 1 || fixes[0] != want {
		t.Errorf("fixes = %v, want [%s]", fixes, want)
	}
	if _, rewritten := alloc.Pending()[1]; rewritten {
		t.Errorf("catalog rewritten: %v, want its /OpenAction kept, the array rewritten in place", alloc.Pending()[1])
	}
	arr, _ := alloc.Pending()[4].(ast.PdfArray)
	if len(arr) != 1 {
		t.Fatalf("array 4 = %v, want the GoTo action alone, rewritten at its own object number", alloc.Pending()[4])
	}
	goTo, _ := arr[0].(ast.PdfDict)
	if goTo["S"] != ast.PdfName("GoTo") || goTo["Next"] != (ast.PdfIndirectRef{ObjectNum: 4}) {
		t.Errorf("array 4 entry = %v, want the GoTo action, its /Next kept by reference", arr[0])
	}
}

// TestFixJavaScript_SharedFieldKidsFanOut pins that a /Kids array written as
// an object of its own, whose direct fields each name it again, is walked
// once: walked anew wherever it is reached, it fans out 2^depth up to
// maxFieldTreeDepth.
func TestFixJavaScript_SharedFieldKidsFanOut(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"[<< /T (a) /Kids 4 0 R /AA << /K << /S /JavaScript /JS (x) >> >> >> << /T (b) /Kids 4 0 R >>]",
		"<< /T (p) /FT /Tx /Kids 4 0 R >>",
	))
	if _, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("the field walk did not end before the deadline")
	}
	arr, _ := alloc.Pending()[4].(ast.PdfArray)
	if len(arr) != 2 {
		t.Fatalf("array 4 = %v, want it rewritten at its own object number", alloc.Pending()[4])
	}
	a, _ := arr[0].(ast.PdfDict)
	if _, hasAA := a["AA"]; hasAA || a["Kids"] != (ast.PdfIndirectRef{ObjectNum: 4}) {
		t.Errorf("field a = %v, want its /AA gone and its /Kids kept by reference", a)
	}
}
