package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// inheritedColourPDF builds a one-page document whose page states no
// /Resources and inherits the root node's, object 2, which holds every colour
// space inline (ISO 32000-1, 7.7.3.4). The page paints with each of them:
//
//   - /CSN, a DeviceN over the spot colorants SpotA and SpotB with no
//     /Attributes, fails ISO 19005-2/4, 6.2.4.4 t1.
//   - /CS1 and /CS2, two Separations naming SpotC over different tint
//     transforms, objects 9 and 10, fail 6.2.4.4 t2.
//   - /CS3, an ICCBased space over the OutputIntent's CMYK profile, and the
//     same space in the form /Fm0, object 11, fail ISO 19005-4, 6.2.4.2 t3.
//
// mediaBox is the root node's /MediaBox, which the page inherits too.
func inheritedColourPDF(t *testing.T, mediaBox string) []byte {
	t.Helper()

	const content = "/CSN cs 0.5 0.5 scn 0 0 10 10 re f /CS1 cs 1 scn 20 0 10 10 re f " +
		"/CS2 cs 1 scn 40 0 10 10 re f /CS3 cs 0 0 0 1 scn 60 0 10 10 re f /Fm0 Do"
	const form = "/CS0 cs 0 0 0 1 scn 0 0 5 5 re f"
	const tintN = "{ pop pop 0 0 0 0 }"
	cmyk := deflateAt(t, cmykICCProfile, zlib.BestCompression)
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /MediaBox " + mediaBox +
			" /Resources << /ColorSpace << /CSN [/DeviceN [/SpotA /SpotB] /DeviceCMYK 8 0 R]" +
			" /CS1 [/Separation /SpotC /DeviceCMYK 9 0 R] /CS2 [/Separation /SpotC /DeviceCMYK 10 0 R]" +
			" /CS3 [/ICCBased 6 0 R] >> /XObject << /Fm0 11 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /Contents 7 0 R >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (Coated FOGRA39)" +
			" /DestOutputProfile 5 0 R >>",
		iccObj(cmyk, 4),
		iccObj(cmyk, 4),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /FunctionType 4 /Domain [0 1 0 1] /Range [0 1 0 1 0 1 0 1] /Length %d >>\nstream\n%s\nendstream",
			len(tintN), tintN),
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [1 0 0 0] /N 1 >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 1 0 0] /N 1 >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 5 5]"+
			" /Resources << /ColorSpace << /CS0 [/ICCBased 6 0 R] >> >> /Length %d >>\nstream\n%s\nendstream",
			len(form), form),
	})
}

// inheritedRootNode is the root page tree node of [inheritedColourPDF].
const inheritedRootNode = 2

// inheritedPage is the page of [inheritedColourPDF].
const inheritedPage = 3

// liveColourSpaces returns the /ColorSpace dictionary the /Resources of the
// page tree node objNum holds once the queued repairs apply.
func liveColourSpaces(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, objNum int) ast.PdfDict {
	t.Helper()

	ctx := context.Background()
	node := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
	res := liveDictValue(ctx, alloc, doc, node["Resources"])
	if res == nil {
		t.Fatalf("object %d holds no /Resources dictionary: %v", objNum, node)
	}
	cs := liveDictValue(ctx, alloc, doc, res["ColorSpace"])
	if cs == nil {
		t.Fatalf("the /Resources of object %d hold no /ColorSpace dictionary: %v", objNum, res)
	}
	return cs
}

// assertNoResourcesOnPage fails when a repair gave the page a /Resources of its
// own, which would shadow the dictionary it inherits.
func assertNoResourcesOnPage(t *testing.T, alloc *writer.ObjAllocator) {
	t.Helper()

	if page, ok := alloc.Pending()[inheritedPage].(ast.PdfDict); ok {
		if res, has := page["Resources"]; has {
			t.Errorf("the page gained /Resources %v: the repair must land on the node the page inherits from", res)
		}
	}
}

// TestFixDeviceNColorants_RepairsAnInheritedColourSpace pins that the DeviceN
// /Colorants repair reaches a colour space the page inherits from its page tree
// ancestor, and writes it back into that ancestor.
//
// The detector reads the /Resources a page inherits (ISO 32000-1, 7.7.3.4), so
// it fails ISO 19005-2/4, 6.2.4.4 t1 on /CSN, and veraPDF does too. The fixer
// read the page's own /Resources alone and repaired nothing.
func TestFixDeviceNColorants_RepairsAnInheritedColourSpace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inheritedColourPDF(t, "[0 0 612 792]"))
	fixes, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, string(models.PDFA4))
	if err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}
	if !containsFix(fixes, "populated /Colorants for 1 DeviceN colour space(s)") {
		t.Errorf("fixes = %q, want the inherited DeviceN populated", fixes)
	}
	if _, ok := alloc.Pending()[inheritedRootNode]; !ok {
		t.Fatalf("the root node holding the inline /Resources was not rewritten; pending = %v", alloc.Pending())
	}

	csn, ok := liveColourSpaces(t, alloc, doc, inheritedRootNode)["CSN"].(ast.PdfArray)
	if !ok || len(csn) < 5 {
		t.Fatalf("/CSN is %v, want a DeviceN array carrying /Attributes", csn)
	}
	attrs, _ := csn[4].(ast.PdfDict)
	colorants, _ := attrs["Colorants"].(ast.PdfDict)
	for _, name := range []ast.PdfName{"SpotA", "SpotB"} {
		if _, has := colorants[name]; !has {
			t.Errorf("/Colorants %v has no entry for %s", colorants, name)
		}
	}
	assertNoResourcesOnPage(t, alloc)
}

// TestFixSepCanon_RepairsAnInheritedSeparation pins that the Separation
// canonicalisation reaches the Separations a page inherits, and writes them
// back into the ancestor that holds them. It fixed nothing there, and the
// output kept failing ISO 19005-2/4, 6.2.4.4 t2.
func TestFixSepCanon_RepairsAnInheritedSeparation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inheritedColourPDF(t, "[0 0 612 792]"))
	fixes, err := fixSepCanon(ctx, alloc, doc, Options{}, nil, string(models.PDFA4))
	if err != nil {
		t.Fatalf("fixSepCanon: %v", err)
	}
	if !containsFix(fixes, "canonicalised 1 Separation array(s)") {
		t.Errorf("fixes = %q, want one inherited Separation canonicalised", fixes)
	}

	cs := liveColourSpaces(t, alloc, doc, inheritedRootNode)
	cs2, ok := cs["CS2"].(ast.PdfArray)
	if !ok || len(cs2) != 4 {
		t.Fatalf("/CS2 is %v, want a Separation array", cs["CS2"])
	}
	if got := fmt.Sprint(cs2[3]); got != "9 0 R" {
		t.Errorf("/CS2 paints through tint transform %s, want 9 0 R, the one /CS1 names", got)
	}
	assertNoResourcesOnPage(t, alloc)
}

// TestFixICCDedup_RepairsAnInheritedICCBasedSpace pins that the redundant-ICC
// substitution reaches an ICCBased space the page inherits, and a form the
// inherited /XObject names. Both fail ISO 19005-4, 6.2.4.2 t3, and the fixer
// read neither.
func TestFixICCDedup_RepairsAnInheritedICCBasedSpace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inheritedColourPDF(t, "[0 0 612 792]"))
	fixes, err := fixICCDedup(ctx, alloc, doc, Options{}, nil, string(models.PDFA4))
	if err != nil {
		t.Fatalf("fixICCDedup: %v", err)
	}
	if !containsFix(fixes, "replaced 2 redundant ICCBased colour space(s)") {
		t.Errorf("fixes = %q, want the inherited space and the form's replaced", fixes)
	}
	if got := liveColourSpaces(t, alloc, doc, inheritedRootNode)["CS3"]; got != ast.PdfName("DeviceCMYK") {
		t.Errorf("the inherited /CS3 is %v, want /DeviceCMYK", got)
	}
	form, ok := alloc.Pending()[11].(ast.PdfStream)
	if !ok {
		t.Fatalf("the form was not rewritten as a stream: %T", alloc.Pending()[11])
	}
	if !formColourSpaceHas(t, form.Dict, "CS0") || formColourSpace(t, form.Dict)["CS0"] != ast.PdfName("DeviceCMYK") {
		t.Errorf("the form's /CS0 is %v, want /DeviceCMYK", formColourSpace(t, form.Dict)["CS0"])
	}
	assertNoResourcesOnPage(t, alloc)
}

// TestRemediatePDFA_RepairsInheritedColourSpacesInOneRound runs the whole
// engine once, every fixer in registry order, at PDF/A-4 with content-changing
// repairs allowed, and revalidates the output.
//
// page-resources pins the inherited /Resources on the page, but it runs after
// devicen, sep-canon and icc-dedup, so those three found nothing to repair in
// the round and the output of one round kept failing 6.2.4.4 t1 and t2 and
// 6.2.4.2 t3. It also pins what page-resources copies: a repair written into
// the ancestor must reach the page's own copy.
func TestRemediatePDFA_RepairsInheritedColourSpacesInOneRound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inheritedColourPDF(t, "[0 0 612 792]"))
	before := inheritedColourFindings(t, doc)
	for _, want := range []string{"6.2.4.4 t1", "6.2.4.4 t2", "6.2.4.2 t3"} {
		if before[want] == 0 {
			t.Fatalf("precondition: the source does not fail %s; findings = %v", want, before)
		}
	}

	if _, err := RemediatePDFA(ctx, doc, alloc, models.PDFA4, Options{}); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	var out bytes.Buffer
	if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	if after := inheritedColourFindings(t, reread); len(after) > 0 {
		t.Errorf("the output of one round still fails %v", after)
	}
}

// inheritedColourFindings counts the ISO 19005-4 findings the inherited colour
// spaces of [inheritedColourPDF] raise, keyed by clause and test.
func inheritedColourFindings(t *testing.T, doc *document.Document) map[string]int {
	t.Helper()

	errs, err := compliance.ValidatePDFA(context.Background(), doc, models.PDFA4)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	found := make(map[string]int)
	for _, v := range toViolations(errs) {
		key := fmt.Sprintf("%s t%d", v.Rule.Clause, v.Rule.TestNumber)
		switch key {
		case "6.2.4.4 t1", "6.2.4.4 t2", "6.2.4.2 t3":
			found[key]++
		}
	}
	return found
}

// TestFixDeviceNColorants_KeepsPageBoxesOnTheInheritedHolder pins that the
// DeviceN repair and page-boxes, which both rewrite the root node here, keep
// each other's edit. devicen writes the node with its repaired /Resources, and
// page-boxes, later in the registry, clamps the /MediaBox of the same node
// (ISO 19005-2, 6.1.13 t11). Both run on the violations the source raises at
// PDF/A-2b.
func TestFixDeviceNColorants_KeepsPageBoxesOnTheInheritedHolder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inheritedColourPDF(t, "[0 0 2 2]"))
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	violations := toViolations(errs)
	for _, name := range []string{"devicen", "page-boxes"} {
		f, matched := matchedBy(t, name, violations)
		if _, err := f.apply(ctx, alloc, doc, Options{}, matched, string(models.PDFA2b)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	node := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: inheritedRootNode})
	if got := fmt.Sprint(node["MediaBox"]); got == "[0 0 2 2]" {
		t.Errorf("the root node kept /MediaBox %s: page-boxes did not clamp it", got)
	}
	csn, ok := liveColourSpaces(t, alloc, doc, inheritedRootNode)["CSN"].(ast.PdfArray)
	if !ok || len(csn) < 5 {
		t.Errorf("/CSN is %v after page-boxes, want the DeviceN devicen gave /Attributes", csn)
	}
}

// TestFixDeviceNColorants_RepairsThePageCopyOutputIntentMade pins the order the
// other way round. output-intent, ahead of devicen, injects /DefaultRGB into a
// page that inherits an inline /Resources by writing the patched copy into the
// page itself, so the page no longer inherits. devicen must repair that copy,
// the one the page resolves its names against, and keep the injection.
func TestFixDeviceNColorants_RepairsThePageCopyOutputIntentMade(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inheritedColourPDF(t, "[0 0 612 792]"))
	if n := injectTestDefaultRGB(t, alloc, doc); n != 1 {
		t.Fatalf("precondition: output-intent patched %d page(s), want 1", n)
	}
	page := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: inheritedPage})
	if _, has := page["Resources"]; !has {
		t.Fatal("precondition: output-intent did not give the page its own /Resources")
	}

	if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, string(models.PDFA4)); err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}
	cs := liveColourSpaces(t, alloc, doc, inheritedPage)
	if _, has := cs["DefaultRGB"]; !has {
		t.Errorf("the page's /ColorSpace %v lost output-intent's /DefaultRGB", cs)
	}
	if csn, ok := cs["CSN"].(ast.PdfArray); !ok || len(csn) < 5 {
		t.Errorf("the page's /CSN is %v, want the DeviceN devicen gave /Attributes", cs["CSN"])
	}
}
