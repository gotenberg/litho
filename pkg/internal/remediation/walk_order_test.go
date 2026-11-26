package remediation

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"strings"
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

// orderRuns is how often an order test repeats its walk. Go's map order
// walked the second of two names first in about 1 run in 8 (1,259 of 10,240
// for two forms), so 256 runs all agreeing on an unsorted walk happen far less
// than once in a trillion.
const orderRuns = 256

// walkOrderStream is a stream object with dict and body.
func walkOrderStream(dict, body string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(body), body)
}

// walkOrderType3 is a Type 3 font dictionary with extra entries.
func walkOrderType3(extra string) string {
	return "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.001 0 0 0.001 0 0] " +
		"/CharProcs << >> /Encoding << /Differences [] >> /FirstChar 0 /LastChar 0 /Widths [0] " + extra + " >>"
}

// TestInjectDefaultColorSpace_SharedResourcesIndependentOfOrder pins which
// object the walk rewrites when two share one /Resources.
//
// Objects 4 and 5 are identical, named /X1 and /X2 by the page, and their
// /Resources is one indirect dictionary, object 6. The walk patches that
// dictionary once, at its own object number, and rewrites the object it
// reached it through, leaving the other as it was (see
// [recurseColorSpacePatch] and [patchType3FontColorSpace]). It met the two in
// Go's map order, so which one was rewritten changed from run to run
// (CONTRIBUTING "Deterministic output"). A full rewrite writes the same bytes
// either way, the two being identical, so only the staged objects show the
// order.
func TestInjectDefaultColorSpace_SharedResourcesIndependentOfOrder(t *testing.T) {
	t.Parallel()

	form := walkOrderStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources 6 0 R", "0 0 10 10 re f")
	font := walkOrderType3("/Resources 6 0 R")
	for _, tc := range []struct {
		name     string
		resource string
		object   string
	}{
		{name: "forms", resource: "XObject", object: form},
		{name: "Type 3 fonts", resource: "Font", object: font},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := walkOrderPDF([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /%s << /X1 4 0 R /X2 5 0 R >> >> >>", tc.resource),
				tc.object,
				tc.object,
				"<< /ProcSet [/PDF] >>",
			})

			ctx := context.Background()
			outcomes := make(map[string]int)
			for range orderRuns {
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
			// /X1 is walked first, so it is the object rewritten.
			want := "shared=true X1=true X2=false"
			if len(outcomes) != 1 || outcomes[want] != orderRuns {
				t.Fatalf("%d runs gave %v, want %q every run", orderRuns, outcomes, want)
			}
		})
	}
}

// TestFixResourceTransparency_DepthBoundIndependentOfOrder pins which path
// repairs an object reachable by two when one of them reaches the depth
// bound.
//
// The walked dictionary names the target twice over: /A names it, and /Z
// names a head whose resources open a chain of nine forms, the last naming
// the target at depth [maxTransparencyResourceDepth]. The target's own
// resources carry a /CA 0.5 graphics state. visited marks the target at the
// first path that reaches it, and the path through /Z stops there, at the
// bound, without repairing it (see [fixXObjectTransparency]). The walks met
// /A and /Z in Go's map order, so the target was repaired in some runs and
// left in others (CONTRIBUTING "Deterministic output").
func TestFixResourceTransparency_DepthBoundIndependentOfOrder(t *testing.T) {
	t.Parallel()

	const gs = "/ExtGState << /GS << /CA 0.5 >> >>"
	const body = "/GS gs 0 0 10 10 re f"
	form := func(res string) string {
		return walkOrderStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << "+res+" >>", body)
	}
	pattern := func(res string) string {
		return walkOrderStream("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << "+res+" >>", body)
	}
	type3 := func(res string) string { return walkOrderType3("/Resources << " + res + " >>") }
	charProc := func(res string) string { return walkOrderStream("/Resources << "+res+" >>", body) }

	// Objects 1 to 3 are the catalog, the page tree and the page, 4 an
	// optional Type 3 font holding the glyph procedures, then the target,
	// the head and the chain.
	const firstChain = 7
	for _, tc := range []struct {
		name string
		// pageRes is the page's resources and charProcs the Type 3 font's
		// /CharProcs entry; the one naming /A and /Z takes the target and
		// head references.
		pageRes   string
		charProcs string
		object    func(res string) string
	}{
		{name: "forms", pageRes: "/XObject << /A %s /Z %s >>", object: form},
		{name: "patterns", pageRes: "/Pattern << /A %s /Z %s >>", object: pattern},
		{name: "Type 3 fonts", pageRes: "/Font << /A %s /Z %s >>", object: type3},
		{name: "glyph procedures", pageRes: "/Font << /F 4 0 R >>", charProcs: "/CharProcs << /A %s /Z %s >>", object: charProc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const target, head = "5 0 R", "6 0 R"
			pageRes := tc.pageRes
			font := "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"
			if tc.charProcs != "" {
				font = walkOrderType3(fmt.Sprintf(tc.charProcs, target, head))
			} else {
				pageRes = fmt.Sprintf(pageRes, target, head)
			}
			objs := []string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << " + pageRes + " >> >>",
				font,
				tc.object(gs),
				tc.object(fmt.Sprintf("/XObject << /C %d 0 R >>", firstChain)),
			}
			// The head's resources are at depth 1 and chain form k's at
			// depth k+1, so the ninth names the target at the bound.
			for k := 1; k <= 9; k++ {
				next := fmt.Sprintf("%d 0 R", firstChain+k)
				if k == 9 {
					next = target
				}
				objs = append(objs, form("/XObject << /C "+next+" >>"))
			}
			data := walkOrderPDF(objs)

			ctx := context.Background()
			outcomes := make(map[string]int)
			for range orderRuns {
				doc, alloc := newTestDoc(t, data)
				page := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 3})
				res, ok := page["Resources"].(ast.PdfDict)
				if !ok {
					t.Fatal("page has no inline /Resources")
				}
				fixResourceTransparency(ctx, doc, alloc, res, maps.Clone(page), make(map[int]bool), 0)
				repaired := false
				if obj, ok := alloc.Pending()[5]; ok {
					repaired = !strings.Contains(obj.String(), "/CA 0.5")
				}
				outcomes[fmt.Sprintf("repaired=%v", repaired)]++
			}
			// /A is walked first and reaches the target at depth 0, so the
			// target is repaired.
			want := "repaired=true"
			if len(outcomes) != 1 || outcomes[want] != orderRuns {
				t.Fatalf("%d runs gave %v, want %q every run", orderRuns, outcomes, want)
			}
		})
	}
}
