package recolor

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// TestAnnotationColours checks that the device colours an annotation's
// dictionary states are converted: /C and /IC of an annotation written as an
// object of its own and of one written in the /Annots array, and /BC and /BG
// of a /MK written as an object of its own. A colour already in the target
// stays.
func TestAnnotationColours(t *testing.T) {
	t.Parallel()
	f := onePage("/Annots [5 0 R << /Type /Annot /Subtype /Circle /Rect [0 0 9 9] /C [0 0 0 1] /IC [0 1 0] >>]", "",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 9 9] /C [1 0 0] /IC [0 0 1] /MK 6 0 R >>",
		"<< /BC [0] /BG [1 1 0] >>")
	r := recolor(t, f, cmyk, naive(Options{}))

	square := r.dict(ast.PdfIndirectRef{ObjectNum: 5})
	for key, want := range map[ast.PdfName]string{"C": "[0 1 1 0]", "IC": "[1 1 0 0]"} {
		if got := fmt.Sprint(square[key]); got != want {
			t.Errorf("square /%s: %s, want %s", key, got, want)
		}
	}
	mk := r.dict(ast.PdfIndirectRef{ObjectNum: 6})
	for key, want := range map[ast.PdfName]string{"BC": "[0 0 0 1]", "BG": "[0 0 1 0]"} {
		if got := fmt.Sprint(mk[key]); got != want {
			t.Errorf("/MK /%s: %s, want %s", key, got, want)
		}
	}
	annots, ok := r.resolve(r.pages[0].Dict["Annots"]).(ast.PdfArray)
	if !ok || len(annots) != 2 {
		t.Fatalf("/Annots: %v", r.pages[0].Dict["Annots"])
	}
	circle := r.dict(annots[1])
	if got := fmt.Sprint(circle["C"]); got != "[0 0 0 1]" {
		t.Errorf("circle /C: %s, want it kept", got)
	}
	if got := fmt.Sprint(circle["IC"]); got != "[1 0 1 0]" {
		t.Errorf("circle /IC: %s, want [1 0 1 0]", got)
	}
}

// TestAnnotationColoursKeptWithoutColour checks that an empty colour array,
// which paints nothing (ISO 32000-1, Table 164), and one of another length
// stay as they are.
func TestAnnotationColoursKeptWithoutColour(t *testing.T) {
	t.Parallel()
	f := onePage("/Annots [5 0 R]", "",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 9 9] /C [] /IC [1 0] >>")
	r := recolor(t, f, cmyk, naive(Options{}))
	square := r.dict(ast.PdfIndirectRef{ObjectNum: 5})
	if got := fmt.Sprint(square["C"], square["IC"]); got != "[] [1 0]" {
		t.Errorf("/C and /IC: %s", got)
	}
}

// TestPageGroupNamingAResource checks that a page group whose /CS names a
// /ColorSpace resource, which names no colour space outside a content stream
// (ISO 32000-1, 8.6.3), blends in a grey or CMYK target, as one naming no
// space does, and that an RGB target leaves it.
func TestPageGroupNamingAResource(t *testing.T) {
	t.Parallel()
	f := onePage("/Group << /S /Transparency /CS /CS0 >> /Resources << /ColorSpace << /CS0 /DeviceRGB >> >>", "/CS0 cs 1 0 0 sc 0 0 9 9 re f")
	r := recolor(t, f, cmyk, naive(Options{}))
	if cs := r.dict(r.pages[0].Dict["Group"])["CS"]; cs != ast.PdfName("DeviceCMYK") {
		t.Errorf("CMYK target: page group /CS %v, want DeviceCMYK", cs)
	}
	r = recolor(t, f, rgbTarget, naive(Options{}))
	if cs := r.dict(r.pages[0].Dict["Group"])["CS"]; cs != ast.PdfName("CS0") {
		t.Errorf("RGB target: page group /CS %v, want /CS0 kept", cs)
	}
}

// TestGroupPages checks that [Options.GroupPages] gives a page with no group
// a page group blending in a grey or CMYK target, and none under an RGB
// target, without the option, or on a page whose own output intent
// characterises a CMYK device.
func TestGroupPages(t *testing.T) {
	t.Parallel()
	content := "1 0 0 rg 0 0 9 9 re f"
	group := func(r result) ast.PdfObject { return r.pages[0].Dict["Group"] }

	r := recolor(t, onePage("", content), cmyk, naive(Options{GroupPages: true}))
	g := r.dict(group(r))
	if g["S"] != ast.PdfName("Transparency") || g["CS"] != ast.PdfName("DeviceCMYK") {
		t.Errorf("CMYK target: page group %v", g)
	}
	r = recolor(t, onePage("", content), grayTarget, naive(Options{GroupPages: true}))
	if cs := r.dict(group(r))["CS"]; cs != ast.PdfName("DeviceGray") {
		t.Errorf("grey target: page group /CS %v", cs)
	}
	for name, run := range map[string]func() result{
		"RGB target": func() result {
			return recolor(t, onePage("", content), rgbTarget, naive(Options{GroupPages: true}))
		},
		"no option": func() result { return recolor(t, onePage("", content), cmyk, naive(Options{})) },
		"page intent": func() result {
			profile := string(icc.CMYKPrinterProfile())
			return recolor(t, onePage("/OutputIntents [<< /Type /OutputIntent /S /GTS_PDFA1 /DestOutputProfile 5 0 R >>]", content,
				stream("/N 4", profile)), cmyk, naive(Options{GroupPages: true}))
		},
	} {
		if g := run().pages[0].Dict["Group"]; g != nil {
			t.Errorf("%s: page group %v, want none", name, g)
		}
	}
}
