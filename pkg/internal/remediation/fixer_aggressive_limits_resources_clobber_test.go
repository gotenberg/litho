package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// nineColorantDeviceN is an inline DeviceN array one colourant past the
// PDF/A-1 cap, so [trimDeviceNColorants] must rewrite it under PDF/A-1b.
const nineColorantDeviceN = "[/DeviceN [/Cyan /Magenta /Yellow /Black /Spot1 /Spot2 /Spot3 /Spot4 /Spot5]" +
	" /DeviceCMYK 6 0 R]"

// deviceNPageColourants returns the colourant count of the DeviceN space name
// that page 3 uses, following the LIVE chain the output will serialise: the
// page, its /Resources, its /ColorSpace, the array and its name list, each
// read through the allocator. -1 means the chain does not resolve.
func deviceNPageColourants(t *testing.T, ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name ast.PdfName) int {
	t.Helper()
	page := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 3})
	res := liveDictValue(ctx, alloc, doc, page["Resources"])
	cs := liveDictValue(ctx, alloc, doc, res["ColorSpace"])
	arr := liveArray(ctx, alloc, doc, cs[name])
	if len(arr) < 2 {
		return -1
	}
	return len(liveArray(ctx, alloc, doc, arr[1]))
}

// runDeviceNTrim runs fixAggressiveLimits with the PDF/A-1 DeviceN violation
// and returns its fix report.
func runDeviceNTrim(t *testing.T, ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) []string {
	t.Helper()
	fixes, err := fixAggressiveLimits(ctx, alloc, doc, Options{}, pdfA1DeviceNViolation, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixAggressiveLimits: %v", err)
	}
	return fixes
}

// TestTrimDeviceNColorants_FollowsThePendingResources pins that the trim
// reaches the /ColorSpace the output uses after an earlier fixer rewrote the
// page's indirect /Resources.
//
// output-intent precedes aggressive-limits and, injecting a /DefaultCMYK,
// rewrites the /Resources at its own object number with the /ColorSpace
// dictionary INLINED (patchResourceColorSpace), orphaning the indirect one.
// trimDeviceNColorants read /Resources from the document, so it trimmed the
// orphaned dictionary, counted a repair, and the output kept all nine
// colourants.
//
// The peer write stands in for that injection: the same /ColorSpace, inlined,
// plus a /DefaultCMYK the fixture does not declare.
func TestTrimDeviceNColorants_FollowsThePendingResources(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 4 0 R >>",
		"<< /ColorSpace 5 0 R >>",
		"<< /CSN " + nineColorantDeviceN + " >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0.1 0 0 0] /N 1 >>",
	}))

	const resObj, csObj = 4, 5
	onDiskCS, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: csObj})
	if err != nil || onDiskCS == nil {
		t.Fatalf("fixture /ColorSpace at object %d did not resolve: %v", csObj, err)
	}
	if got := deviceNPageColourants(t, ctx, alloc, doc, "CSN"); got != pdfA1DeviceNComponents+1 {
		t.Fatalf("precondition: the page must use a DeviceN with %d colourants, got %d",
			pdfA1DeviceNComponents+1, got)
	}

	inlined := make(ast.PdfDict, len(onDiskCS)+1)
	maps.Copy(inlined, onDiskCS)
	inlined["DefaultCMYK"] = ast.PdfName("DeviceCMYK")
	if !alloc.AllocAt(resObj, ast.PdfDict{"ColorSpace": inlined}) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", resObj)
	}

	fixes := runDeviceNTrim(t, ctx, alloc, doc)

	if got := deviceNPageColourants(t, ctx, alloc, doc, "CSN"); got != pdfA1DeviceNComponents {
		t.Errorf("the /ColorSpace the output uses still declares %d colourants, want %d: the "+
			"trim went to the document's /Resources, not the pending one; fixes = %v",
			got, pdfA1DeviceNComponents, fixes)
	}
	res := liveDictValue(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: resObj})
	if _, kept := liveDictValue(ctx, alloc, doc, res["ColorSpace"])["DefaultCMYK"]; !kept {
		t.Errorf("/DefaultCMYK is gone: the trim discarded the earlier fixer's write; /Resources = %v", res)
	}
	want := []string{fmt.Sprintf("reduced 1 DeviceN colour space(s) to %d components", pdfA1DeviceNComponents)}
	if !slices.Equal(fixes, want) {
		t.Errorf("fixes = %v, want %v", fixes, want)
	}
}

// TestTrimDeviceNColorants_InlineColourSpaceInInlineResources pins the fully
// inline shape: the DeviceN array, its /ColorSpace dictionary and the
// /Resources are all written inline in the page.
//
// trimDeviceNColorants wrote an inline trim only into an INDIRECT /ColorSpace
// dictionary and gave up on any other, so such a page kept every colourant.
// The trimmed /ColorSpace now goes back where it lives: into the /Resources,
// and the /Resources into the page, which the allocator addresses.
//
// # Corpus evidence
//
//   - litho limits_2b.pdf and verapdf 6-1-12-t09-fail-a.pdf: both have this
//     shape, and both kept their test 9 violation in aggressive mode.
func TestTrimDeviceNColorants_InlineColourSpaceInInlineResources(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /ColorSpace << /CS0 " + nineColorantDeviceN + " >> /ProcSet [/PDF] >> >>",
		"<< /Type /Nothing >>",
		"<< /Type /Nothing >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0.1 0 0 0] /N 1 >>",
	}))

	if got := deviceNPageColourants(t, ctx, alloc, doc, "CS0"); got != pdfA1DeviceNComponents+1 {
		t.Fatalf("precondition: the page must use a DeviceN with %d colourants, got %d",
			pdfA1DeviceNComponents+1, got)
	}

	fixes := runDeviceNTrim(t, ctx, alloc, doc)

	if got := deviceNPageColourants(t, ctx, alloc, doc, "CS0"); got != pdfA1DeviceNComponents {
		t.Errorf("the inline colour space still declares %d colourants, want %d; fixes = %v",
			got, pdfA1DeviceNComponents, fixes)
	}
	page := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if _, kept := liveDictValue(ctx, alloc, doc, page["Resources"])["ProcSet"]; !kept {
		t.Errorf("the rewritten /Resources lost its /ProcSet; page = %v", page)
	}
	want := []string{fmt.Sprintf("reduced 1 DeviceN colour space(s) to %d components", pdfA1DeviceNComponents)}
	if !slices.Equal(fixes, want) {
		t.Errorf("fixes = %v, want %v", fixes, want)
	}
}

// TestTrimDeviceNColorants_InlineColourSpaceKeepsAPendingResourcesWrite pins
// the middle shape: an inline /ColorSpace inside an INDIRECT /Resources, which
// an earlier fixer has already rewritten at its own object number.
//
// The trimmed /ColorSpace is written into a copy of that /Resources, so the
// copy must start from the pending version, or the trim discards the earlier
// edit. The marker is a key the fixture does not declare.
func TestTrimDeviceNColorants_InlineColourSpaceKeepsAPendingResourcesWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 4 0 R >>",
		"<< /ColorSpace << /CSN " + nineColorantDeviceN + " >> >>",
		"<< /Type /Nothing >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0.1 0 0 0] /N 1 >>",
	}))

	const resObj = 4
	onDisk, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: resObj})
	if err != nil || onDisk == nil {
		t.Fatalf("fixture /Resources at object %d did not resolve: %v", resObj, err)
	}
	if _, has := onDisk["Marker"]; has {
		t.Fatalf("precondition: the fixture must NOT declare /Marker, else it proves nothing")
	}
	peer := make(ast.PdfDict, len(onDisk)+1)
	maps.Copy(peer, onDisk)
	peer["Marker"] = ast.PdfName("peer")
	if !alloc.AllocAt(resObj, peer) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", resObj)
	}

	fixes := runDeviceNTrim(t, ctx, alloc, doc)

	if got := deviceNPageColourants(t, ctx, alloc, doc, "CSN"); got != pdfA1DeviceNComponents {
		t.Errorf("the inline colour space still declares %d colourants, want %d; fixes = %v",
			got, pdfA1DeviceNComponents, fixes)
	}
	if _, kept := liveDictValue(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: resObj})["Marker"]; !kept {
		t.Errorf("/Marker is gone: the trim rebuilt /Resources from the on-disk copy and " +
			"discarded the earlier fixer's write")
	}
}
