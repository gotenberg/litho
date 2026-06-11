package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixGroupCS_SynthesizedGroupUsesICCBasedCS pins the blending colour
// space of a synthesized transparency group to a device-independent
// ICCBased sRGB stream. The fixer used to write /CS /DeviceRGB, which under
// ISO 19005-4 §6.2.4.3 is itself a device colour usage requiring an RGB
// OutputIntent, so the repair traded one violation for another.
func TestFixGroupCS_SynthesizedGroupUsesICCBasedCS(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixGroupCS(context.Background(), alloc, doc, Options{}, nil, "PDF/A-4")
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
