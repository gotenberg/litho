package remediation

import (
	"compress/zlib"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// TestFixGroupCS_SynthesizedGroupUsesICCBasedCS pins the blending colour
// space of a synthesized transparency group to a device-independent
// ICCBased sRGB stream. The fixer used to write /CS /DeviceRGB, which under
// ISO 19005-4 §6.2.4.3 is itself a device colour usage requiring an RGB
// OutputIntent, so the repair traded one violation for another. The group is
// synthesized for the page ISO 19005-4, 6.2.9 t2 flags.
func TestFixGroupCS_SynthesizedGroupUsesICCBasedCS(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	flagged := []compliance.Violation{{Rule: compliance.Rule_19005_4_6_2_9_t02, Context: "page 1"}}
	descs, err := fixGroupCS(context.Background(), alloc, doc, Options{}, flagged, "PDF/A-4")
	if err != nil {
		t.Fatalf("fixGroupCS: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	page := pendingDict(t, alloc, 3)
	group, ok := page["Group"].(ast.PdfDict)
	if !ok {
		t.Fatalf("page /Group: got %T", page["Group"])
	}
	cs, ok := group["CS"].(ast.PdfArray)
	if !ok || len(cs) != 2 || cs[0] != ast.PdfName("ICCBased") {
		t.Fatalf("group /CS: got %v, want [/ICCBased ref]", group["CS"])
	}
	ref, ok := cs[1].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("group /CS profile: got %T, want indirect ref", cs[1])
	}
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("ICC profile object %d: got %T, want stream", ref.ObjectNum, alloc.Pending()[ref.ObjectNum])
	}
	if stream.Dict["N"] != ast.PdfInt(3) {
		t.Errorf("ICC profile /N: got %v, want 3", stream.Dict["N"])
	}
}

// TestFixGroupCS_DeviceGroupCSReplacedWithSRGB pins that an existing device
// transparency-group blending space is REPLACED with device-independent
// ICCBased sRGB, not dropped. Dropping /CS would leave a transparency page
// without a blending space, itself an ISO 19005-2 §6.2.10 test 2 violation
// when the document has no matching OutputIntent.
func TestFixGroupCS_DeviceGroupCSReplacedWithSRGB(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Group << /S /Transparency /CS /DeviceRGB >> >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	flagged := []compliance.Violation{{
		Rule:    compliance.Rule_19005_2_6_2_4_3_t02,
		Context: "page 1: transparency group blending colour space is DeviceRGB without RGB OutputIntent",
	}}
	descs, err := fixGroupCS(context.Background(), alloc, doc, Options{}, flagged, "PDF/A-2")
	if err != nil {
		t.Fatalf("fixGroupCS: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	page := pendingDict(t, alloc, 3)
	group, ok := page["Group"].(ast.PdfDict)
	if !ok {
		t.Fatalf("page /Group: got %T", page["Group"])
	}
	cs, ok := group["CS"].(ast.PdfArray)
	if !ok || len(cs) != 2 || cs[0] != ast.PdfName("ICCBased") {
		t.Fatalf("group /CS: got %v, want [/ICCBased ref] (device CS must be replaced, not dropped)", group["CS"])
	}
}

// deviceGroupsPDF builds a two-page document with a CMYK OutputIntent whose
// pages both carry a transparency group blending in a device space: the
// first, object 3, in DeviceRGB, which no RGB OutputIntent covers, and the
// second, object 4, in DeviceCMYK, which the OutputIntent covers.
func deviceGroupsPDF(t *testing.T) []byte {
	t.Helper()

	const content = "0 0 0 1 k 0 0 10 10 re f"
	cmyk := deflateAt(t, cmykICCProfile, zlib.BestCompression)
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [5 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R" +
			" /Group << /Type /Group /S /Transparency /CS /DeviceRGB >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R" +
			" /Group << /Type /Group /S /Transparency /CS /DeviceCMYK >> >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (Coated FOGRA39)" +
			" /DestOutputProfile 6 0 R >>",
		iccObj(cmyk, 4),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
}

// TestFixGroupCS_LeavesACoveredDeviceGroupAlone pins that group-cs replaces
// the device blending space of the group a violation flags, and leaves the one
// no violation flags.
//
// ISO 19005-2, 6.2.4.3 lets a transparency group blend in DeviceCMYK when a
// CMYK OutputIntent covers it, and the second page's group does. group-cs
// replaced every device blending space, flagged or not, with ICCBased sRGB,
// which changed how the second page's already conforming group composites.
func TestFixGroupCS_LeavesACoveredDeviceGroupAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, deviceGroupsPDF(t))
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	violations := toViolations(errs)
	var flagged []string
	for _, v := range violations {
		if v.Rule.Clause == "6.2.4.3" && strings.Contains(v.Context, "transparency group") {
			flagged = append(flagged, v.Context)
		}
	}
	if len(flagged) != 1 || !strings.HasPrefix(flagged[0], "page 1:") {
		t.Fatalf("precondition: 6.2.4.3 flags the groups %q, want page 1's alone", flagged)
	}

	f, matched := matchedBy(t, "group-cs", violations)
	if _, err := f.apply(ctx, alloc, doc, Options{}, matched, string(models.PDFA2b)); err != nil {
		t.Fatalf("group-cs: %v", err)
	}
	if cs, ok := blendingSpaceOf(t, alloc, doc, 1).(ast.PdfArray); !ok || len(cs) != 2 || cs[0] != ast.PdfName("ICCBased") {
		t.Errorf("the flagged group blends in %v, want [/ICCBased ref]", blendingSpaceOf(t, alloc, doc, 1))
	}
	if cs := blendingSpaceOf(t, alloc, doc, 2); cs != ast.PdfName("DeviceCMYK") {
		t.Errorf("the covered group blends in %v, want the /DeviceCMYK it carried", cs)
	}
}
