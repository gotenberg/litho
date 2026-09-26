package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixDeviceNColorants_KeepsOutputIntentsResourcesWriteAroundAShading pins
// that completing a shading written directly in the page's /Resources keeps
// the /Default colour space output-intent (registry 6) injected there ahead of
// devicen (37).
//
// Both write the page: output-intent its /Resources with the injected space,
// the repair the /Shading dictionary inside them, through the page. A copy of
// the parsed /Resources would drop the space.
func TestFixDeviceNColorants_KeepsOutputIntentsResourcesWriteAroundAShading(t *testing.T) {
	t.Parallel()

	const shading = "<< /ShadingType 2 /ColorSpace " + shadingDeviceN + " /Coords [0 0 1 0] /Function 6 0 R >>"
	ctx := context.Background()
	doc, alloc := newTestDoc(t, shadingDeviceNPDF("<< /Shading << /Sh0 "+shading+" >> >>", false))
	if n := injectTestDefaultRGB(t, alloc, doc); n == 0 {
		t.Fatal("control: output-intent patched no page, so there is no earlier edit to lose")
	}
	if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}

	if cs := liveShadingColourSpace(t, alloc, doc, "Shading", "Sh0", "ColorSpace"); !hasColorants(cs) {
		t.Errorf("the shading's DeviceN has no /Colorants: %v", cs)
	}
	res := liveOwnerResources(t, alloc, doc, 3)
	if cs := liveDictValue(ctx, alloc, doc, res["ColorSpace"]); cs["DefaultRGB"] == nil {
		t.Errorf("/DefaultRGB is gone: the shading repair rebuilt /Resources from the "+
			"document and discarded output-intent's write; /Resources = %v", res)
	}
}

// TestFixDeviceNColorants_KeepsNameUTF8sPatternWrite pins that completing the
// DeviceN of a shading pattern builds on the pattern name-utf8 (registry 33)
// rewrote ahead of devicen (37).
//
// name-utf8 rewrites an indirect object carrying a name that is not valid
// UTF-8 (ISO 19005-2, 6.1.8), here the colorant /Sp#E9t of the DeviceN inside
// pattern 7. Completing the DeviceN from the document's copy of the pattern
// would bring the raw name back and key /Colorants by it.
func TestFixDeviceNColorants_KeepsNameUTF8sPatternWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, shadingDeviceNPDF("<< /Pattern << /P0 7 0 R >> >>", false,
		"<< /PatternType 2 /Shading << /ShadingType 2 /ColorSpace [/DeviceN [/Sp#E9t] /DeviceCMYK 6 0 R]"+
			" /Coords [0 0 1 0] /Function 6 0 R >> >>"))
	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	seeded := liveShadingColourSpace(t, alloc, doc, "Pattern", "P0", "Shading", "ColorSpace")
	names, _ := seeded[1].(ast.PdfArray)
	if len(names) != 1 || names[0] == ast.PdfName("Sp\xe9t") {
		t.Fatalf("control: name-utf8 did not respell the colorant, so there is no earlier edit to lose: %v", seeded)
	}
	sanitised, _ := names[0].(ast.PdfName)

	if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}

	cs := liveShadingColourSpace(t, alloc, doc, "Pattern", "P0", "Shading", "ColorSpace")
	if !hasColorants(cs) {
		t.Fatalf("the pattern's DeviceN has no /Colorants: %v", cs)
	}
	if got, _ := cs[1].(ast.PdfArray); len(got) != 1 || got[0] != sanitised {
		t.Errorf("the DeviceN names %v, want name-utf8's %q", cs[1], sanitised)
	}
	attrs, _ := cs[4].(ast.PdfDict)
	if colorants := liveDictValue(ctx, alloc, doc, attrs["Colorants"]); colorants[sanitised] == nil {
		t.Errorf("/Colorants = %v, want an entry for name-utf8's %q", colorants, sanitised)
	}
}
