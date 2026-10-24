package recolor

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// kOnlyContent paints DeviceGray text, a grey stroke, DeviceRGB black and an
// RGB grey, a DeviceGray resource selected by cs, and a DeviceCMYK colour.
const kOnlyContent = "0 g BT /F1 9 Tf 1 1 Td (a) Tj ET 0.25 G 0 0 9 9 re S " +
	"0 0 0 rg 0 0 5 5 re f 0.5 0.5 0.5 rg 1 1 2 2 re f /CS0 cs 0 sc 2 2 3 3 re f 0.1 0.2 0.3 0.4 k 3 3 1 1 re f"

const kOnlyResources = "/Resources << /ColorSpace << /CS0 /DeviceGray >> >>"

// TestKOnlyBlackSolidColours checks that under Options.KOnlyBlack a CMYK
// target receives DeviceGray colours as the black colorant alone (ISO
// 32000-1, 10.3.3), and DeviceRGB black as 100% K, while an RGB grey still
// converts through its profile and a DeviceCMYK colour stays; and that
// without the option the same black converts to a four-colour black.
func TestKOnlyBlackSolidColours(t *testing.T) {
	t.Parallel()
	f := onePage(kOnlyResources, kOnlyContent)

	r := recolor(t, f, cmyk, Options{KOnlyBlack: true})
	got := strings.Split(r.text(0), "\n")
	want := map[int]string{0: "0 0 0 1 k", 6: "0 0 0 0.75 K", 9: "0 0 0 1 k", 15: "0 0 0 1 k", 16: "0 0 0 1 k"}
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Errorf("operation %d: %q, want %q\n%s", i, at(got, i), w, r.text(0))
		}
	}
	if line := at(got, 12); !strings.HasSuffix(line, " k") || strings.HasPrefix(line, "0 0 0 ") {
		t.Errorf("RGB grey: %q, want a four-colour grey", line)
	}
	if line := at(got, 19); line != "0.1 0.2 0.3 0.4 k" {
		t.Errorf("DeviceCMYK colour: %q", line)
	}
	// cs writes the space's initial colour, black, then sc its own.
	if r.report.KOnlyBlack != 5 {
		t.Errorf("KOnlyBlack %d, want 5", r.report.KOnlyBlack)
	}

	base := recolor(t, f, cmyk, Options{})
	if line := at(strings.Split(base.text(0), "\n"), 0); strings.HasPrefix(line, "0 0 0 ") {
		t.Errorf("without the option DeviceGray black: %q, want a four-colour black", line)
	}
	if base.report.KOnlyBlack != 0 {
		t.Errorf("without the option KOnlyBlack %d", base.report.KOnlyBlack)
	}
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// TestKOnlyBlackLeavesCalibratedAndOtherTargets checks that a DeviceGray a
// default space stands in for (ISO 32000-1, 8.6.5.6) converts through that
// space's profile, and that the option changes nothing under an RGB target.
func TestKOnlyBlackLeavesCalibratedAndOtherTargets(t *testing.T) {
	t.Parallel()
	f := onePage("/Resources << /ColorSpace << /DefaultGray [/CalGray << /WhitePoint [0.9505 1 1.089] >>] >> >>",
		"0 g 0 0 9 9 re f")
	r := recolor(t, f, cmyk, Options{KOnlyBlack: true})
	if line := r.text(0); strings.HasPrefix(line, "0 0 0 1 k") {
		t.Errorf("DefaultGray stood in: %q, want its profile's black", line)
	}
	if r.report.KOnlyBlack != 0 {
		t.Errorf("KOnlyBlack %d, want 0", r.report.KOnlyBlack)
	}

	plain := onePage(kOnlyResources, kOnlyContent)
	a := recolor(t, plain, rgbTarget, Options{KOnlyBlack: true})
	b := recolor(t, plain, rgbTarget, Options{})
	if a.text(0) != b.text(0) {
		t.Errorf("RGB target: option changed the content\n%s\nwant\n%s", a.text(0), b.text(0))
	}
}

// TestKOnlyBlackImagesKeepTheProfile checks that a DeviceGray image converts
// through the profile under the option: its black keeps cyan, magenta and
// yellow.
func TestKOnlyBlackImagesKeepTheProfile(t *testing.T) {
	t.Parallel()
	f := onePage("/Resources << /XObject << /Im0 5 0 R >> >>", "q 9 0 0 9 0 0 cm /Im0 Do Q",
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x00"))
	r := recolor(t, f, cmyk, Options{KOnlyBlack: true})
	res := r.dict(r.pages[0].Dict["Resources"])
	im := r.dict(r.dict(res["XObject"])["Im0"])
	// The conversion may write the image as an Indexed palette over the
	// target, whose entry 0 is the converted black.
	black := decode(t, r, r.dict(res["XObject"])["Im0"])
	if cs, ok := im["ColorSpace"].(ast.PdfArray); ok && len(cs) == 4 && cs[0] == ast.PdfName("Indexed") {
		lookup, _ := r.resolve(cs[3]).(ast.PdfString)
		black = []byte(lookup)
	}
	if len(black) < 4 || black[0] == 0 {
		t.Errorf("image black % x, want a four-colour black", black)
	}
}

// TestKOnlyBlackAnnotationColours checks that a /DA's DeviceGray colour and
// an annotation's one-component /C convert to the black colorant alone.
func TestKOnlyBlackAnnotationColours(t *testing.T) {
	t.Parallel()
	src := formPDF("<< /Fields [5 0 R] >>", "/Annots [5 0 R]",
		"<< /Type /Annot /Subtype /Widget /T (a) /FT /Tx /Rect [0 0 9 9] /C [0.5] /DA (/Helv 9 Tf 0 g) >>")
	r := recolorBytes(t, src, cmyk, Options{KOnlyBlack: true})
	annot := r.dict(ast.PdfIndirectRef{ObjectNum: 5})
	if got := daText(annot["DA"]); got != "/Helv 9 Tf 0 0 0 1 k" {
		t.Errorf("/DA: %q", got)
	}
	if got := annot["C"]; numbersText(got) != "0 0 0 0.5" {
		t.Errorf("/C: %v", got)
	}
	if r.report.KOnlyBlack != 2 {
		t.Errorf("KOnlyBlack %d, want 2", r.report.KOnlyBlack)
	}
}

// numbersText spells an array of numbers as content operands are spelled.
func numbersText(obj ast.PdfObject) string {
	arr, _ := obj.(ast.PdfArray)
	return strings.TrimSpace(spell(content.Operation{Operands: arr}))
}

// TestKOnlyBlackUnderOverprint checks that K-only black painted under
// overprint mode 1 gets /OPM 0, so it knocks out the inks under it as the
// DeviceGray source did (ISO 32000-1, 8.6.7), and the report says so.
func TestKOnlyBlackUnderOverprint(t *testing.T) {
	t.Parallel()
	f := onePage("/Resources << /ExtGState << /GS0 5 0 R >> >>",
		"/GS0 gs 0 g BT /F1 9 Tf (a) Tj ET",
		"<< /Type /ExtGState /OP true /OPM 1 >>")
	r := recolor(t, f, cmyk, Options{KOnlyBlack: true})
	if got := r.report.Overprint; got.Preserved != 1 || got.States != 1 || got.Changed != 0 {
		t.Errorf("overprint report: %+v", got)
	}
	if m := opm(t, r.object(5)); m != 0 {
		t.Errorf("/OPM %d, want 0", m)
	}
}
