package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// walkOrderPDF writes objs as objects 1 to n of a PDF 1.7 file with an exact
// cross-reference table. Object 1 is the catalog.
func walkOrderPDF(objs []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestDedupICCInXObjects_SharedColourSpaceIndependentOfOrder pins that the
// form walked first does not decide a colour space two forms share.
//
// /A declares a transparency group blending in the profile P, and /B declares
// none. Both list /CS0, one [/ICCBased P] object. ISO 19005-4, 6.2.4.2 test 3
// judges /CS0 against the blending space of the form using it, so it repeats
// /A's and not /B's. The walk marks /CS0 visited at the first form it meets,
// and it met them in Go's map order, so /CS0 became DeviceRGB in some runs and
// stayed ICCBased in others (CONTRIBUTING "Deterministic output").
func TestDedupICCInXObjects_SharedColourSpaceIndependentOfOrder(t *testing.T) {
	t.Parallel()

	profile := bytes.Repeat([]byte{0x5A}, 64)
	form := "0 0 10 10 re f"
	data := walkOrderPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /A 4 0 R /B 5 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency /CS 6 0 R >> "+
			"/Resources << /ColorSpace << /CS0 6 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(form), form),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] "+
			"/Resources << /ColorSpace << /CS0 6 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(form), form),
		"[/ICCBased 7 0 R]",
		fmt.Sprintf("<< /N 3 /Length %d >>\nstream\n%s\nendstream", len(profile), profile),
	})

	ctx := context.Background()
	outcomes := make(map[string]int)
	const runs = 64
	for range runs {
		doc, alloc := newTestDoc(t, data)
		page := annotPageEntry{ref: ast.PdfIndirectRef{ObjectNum: 3}, dict: resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 3})}
		// No output intent profile: /CS0 can only repeat a form's blending
		// space.
		dedupICCInXObjects(ctx, alloc, doc, page, profileSet{}, make(map[int]bool))
		pending, replaced := alloc.Pending()[6]
		outcomes[fmt.Sprintf("replaced=%v %v", replaced, pending)]++
	}
	if len(outcomes) != 1 {
		t.Fatalf("%d runs gave %d outcomes, want one: %v", runs, len(outcomes), outcomes)
	}
	// /A, the form blending in P, is walked first, so /CS0 repeats its
	// blending space and becomes the device space with P's component count.
	want := fmt.Sprintf("replaced=true %v", ast.PdfName("DeviceRGB"))
	for got := range outcomes {
		if got != want {
			t.Errorf("outcome %q, want %q", got, want)
		}
	}
}
