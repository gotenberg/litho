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

// TestInjectDefaultColorSpace_SharedFormResourcesIndependentOfOrder pins
// which form the walk rewrites when two forms share one /Resources.
//
// /X1 and /X2 are identical forms whose /Resources is one indirect
// dictionary. The walk patches that dictionary once, at its own object
// number, and rewrites the form it reached it through, leaving the other as it
// was (see [recurseColorSpacePatch]). It met the forms in Go's map order, so
// which one was rewritten changed from run to run (CONTRIBUTING "Deterministic
// output"). A full rewrite writes the same bytes either way, the two forms
// being identical, so only the staged objects show the order.
func TestInjectDefaultColorSpace_SharedFormResourcesIndependentOfOrder(t *testing.T) {
	t.Parallel()

	form := "0 0 10 10 re f"
	formObj := fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources 6 0 R /Length %d >>\nstream\n%s\nendstream",
		len(form), form)
	data := walkOrderPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /X1 4 0 R /X2 5 0 R >> >> >>",
		formObj,
		formObj,
		"<< /ProcSet [/PDF] >>",
	})

	ctx := context.Background()
	outcomes := make(map[string]int)
	// Go's map order walked /X2 first in about 1 run in 8 (1,259 of 10,240),
	// so 256 runs all agreeing on the unsorted walk happen far less than once
	// in a trillion.
	const runs = 256
	for range runs {
		doc, alloc := newTestDoc(t, data)
		catalog := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
		csArr := func() ast.PdfArray { return ast.PdfArray{ast.PdfName("DeviceRGB")} }
		if _, err := injectDefaultColorSpace(ctx, alloc, doc, catalog, "DefaultCMYK", csArr, nil); err != nil {
			t.Fatalf("injectDefaultColorSpace: %v", err)
		}
		pending := alloc.Pending()
		_, x1 := pending[4]
		_, x2 := pending[5]
		_, shared := pending[6]
		outcomes[fmt.Sprintf("shared=%v X1=%v X2=%v", shared, x1, x2)]++
	}
	// /X1 is walked first, so it is the form rewritten.
	want := "shared=true X1=true X2=false"
	if len(outcomes) != 1 || outcomes[want] != runs {
		t.Fatalf("%d runs gave %v, want %q every run", runs, outcomes, want)
	}
}
