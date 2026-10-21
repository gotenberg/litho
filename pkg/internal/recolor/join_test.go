package recolor

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// TestImageXObjectConvertedWithTheContent converts an image the page's
// resources name in the same walk as the page's colour operators.
func TestImageXObjectConvertedWithTheContent(t *testing.T) {
	f := onePage("/Resources << /XObject << /Im1 5 0 R >> >>", "1 0 0 rg q 1 0 0 1 0 0 cm /Im1 Do Q",
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8", "\xff\x00\x00"))
	r := recolor(t, f, cmyk, naive(Options{}))
	img, ok := r.object(5).(ast.PdfStream)
	if !ok {
		t.Fatalf("object 5 is %T", r.object(5))
	}
	if cs := img.Dict["ColorSpace"]; cs != ast.PdfName("DeviceCMYK") {
		t.Errorf("image /ColorSpace = %v, want DeviceCMYK", cs)
	}
	if got := decode(t, r, img); string(got) != "\x00\xff\xff\x00" {
		t.Errorf("image samples = %q, want red in CMYK", got)
	}
	if r.report.Images.Converted != 1 || r.report.Operators != 1 {
		t.Errorf("report: %d images, %d operators, want 1 and 1", r.report.Images.Converted, r.report.Operators)
	}
}

// TestImageReadsTheSourceDefaultSpace converts a DeviceGray image under a
// /DefaultGray the conversion to DeviceGray removes: the image is read
// through the default space, as the source draws it (ISO 32000-1, 8.6.5.6),
// so its samples change where the operators' colour does.
func TestImageReadsTheSourceDefaultSpace(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /DefaultGray [/CalGray << /WhitePoint [0.9642 1 0.8249] >>] >> /XObject << /Im1 5 0 R >> >>",
		"0.5 g /Im1 Do",
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x80"))
	r := recolor(t, f, grayTarget, naive(Options{}))
	spaces := r.dict(r.dict(r.pages[0].Dict["Resources"])["ColorSpace"])
	if _, ok := spaces["DefaultGray"]; ok {
		t.Errorf("DefaultGray kept: %v", spaces)
	}
	img := r.object(5).(ast.PdfStream)
	got := decode(t, r, img)
	ops := r.ops(0)
	if len(got) != 1 || len(ops) == 0 || len(ops[0].Operands) != 1 {
		t.Fatalf("samples %q, operators %v", got, ops)
	}
	op, _ := ops[0].Operands[0].(ast.PdfReal)
	// A one-component image converts by a colour table, its samples kept as
	// indices.
	value := got[0]
	if cs, ok := r.resolve(img.Dict["ColorSpace"]).(ast.PdfArray); ok && len(cs) == 4 {
		table, _ := r.resolve(cs[3]).(ast.PdfString)
		if int(value) >= len(table) {
			t.Fatalf("colour space %v has no entry %d", cs, value)
		}
		value = table[value]
	}
	// A linear CalGray 0.5 is sRGB 0.735: the image and the operator agree.
	if !near(float64(value)/255, float64(op), 2.0/255) {
		t.Errorf("image grey %d, operator %v: want both the CalGray reading", value, op)
	}
}

// TestImageKeepsTheSpotTheContentKeeps draws one Separation space with an
// operator and an image: both keep naming the one rewritten space.
func TestImageKeepsTheSpotTheContentKeeps(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /S1 5 0 R >> /XObject << /Im1 6 0 R >> >>", "/S1 cs 0.5 sc /Im1 Do",
		"[/Separation /Gold /DeviceRGB << /FunctionType 2 /Domain [0 1] /C0 [1 1 1] /C1 [1 0.8 0] /N 1 >>]",
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace 5 0 R /BitsPerComponent 8", "\x80"))
	r := recolor(t, f, cmyk, naive(Options{}))
	space, ok := r.object(5).(ast.PdfArray)
	if !ok || len(space) != 4 || space[2] != ast.PdfName("DeviceCMYK") {
		t.Fatalf("Separation = %v, want its alternate DeviceCMYK", r.object(5))
	}
	img := r.object(6).(ast.PdfStream)
	if img.Dict["ColorSpace"] != (ast.PdfIndirectRef{ObjectNum: 5}) {
		t.Errorf("image /ColorSpace = %v, want 5 0 R", img.Dict["ColorSpace"])
	}
	if !slices.Equal(r.report.KeptColorants, []string{"Gold"}) || r.report.SpotsKept != 1 {
		t.Errorf("report: kept %v, %d spaces, want [Gold] once", r.report.KeptColorants, r.report.SpotsKept)
	}
}

// TestICCEngineConvertsLabThroughThePCS converts a Lab colour with the ICC
// engine, through a link from the PCS, and compares it with the ICC engine's
// own link from Lab.
func TestICCEngineConvertsLabThroughThePCS(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /L [/Lab << /WhitePoint [0.9642 1 0.8249] >>] >> >>", "/L cs 60 20 -30 sc")
	r := recolor(t, f, cmyk, Options{Intent: icc.RelativeColorimetric})
	ops := r.ops(0)
	if len(ops) != 2 || ops[1].Operator != "k" || len(ops[1].Operands) != 4 {
		t.Fatalf("operators %v, want the initial colour and the colour in k", ops)
	}
	link, err := icc.NewLink(icc.LabProfile(), mustProfile(t, icc.CMYKPrinterProfile()), icc.TransformOptions{Intent: icc.RelativeColorimetric})
	if err != nil {
		t.Fatal(err)
	}
	want := make([]float64, 4)
	link.Convert([]float64{0.6, (20 + 128) / 255.0, (-30 + 128) / 255.0}, want)
	for i, o := range ops[1].Operands {
		got, _ := o.(ast.PdfReal)
		if v, isInt := o.(ast.PdfInt); isInt {
			got = ast.PdfReal(v)
		}
		if !near(float64(got), want[i], 2.0/255) {
			t.Errorf("component %d = %v, want %v", i, got, want[i])
		}
	}
}

// TestICCEngineFallsBackFromAnUnreadableDeviceProfile names a DeviceCMYK
// profile the engine cannot read: the device space reads through the
// built-in profile, as the renderer reads it.
func TestICCEngineFallsBackFromAnUnreadableDeviceProfile(t *testing.T) {
	f := onePage("", "0 1 1 0 k")
	r := recolor(t, f, rgbTarget, Options{Intent: icc.RelativeColorimetric, DeviceCMYK: []byte("not a profile")})
	ops := r.ops(0)
	if len(ops) != 1 || ops[0].Operator != "rg" {
		t.Fatalf("operators %v, want one rg", ops)
	}
	if len(r.report.Skipped) != 0 {
		t.Errorf("skipped %v", r.report.Skipped)
	}
}

// TestSpotGridScalesWithTheImageBudget bounds a tint transform's grid by the
// image budget: a lowered budget samples a 4-colorant DeviceN more coarsely.
func TestSpotGridScalesWithTheImageBudget(t *testing.T) {
	k, err := samplesPerInput(4, limits.Default().ScaleImageCap(maxSampledEntries))
	if err != nil || k != 16 {
		t.Errorf("default budget: %d samples per colorant, %v; want 16", k, err)
	}
	low := limits.Limits{MaxImagePixels: limits.DefaultMaxImagePixels / 100}
	k, err = samplesPerInput(4, low.ScaleImageCap(maxSampledEntries))
	if err != nil || k != 5 {
		t.Errorf("a hundredth of the budget: %d samples per colorant, %v; want 5", k, err)
	}
	if _, err := samplesPerInput(32, low.ScaleImageCap(maxSampledEntries)); err == nil {
		t.Error("32 colorants fit a hundredth of the budget")
	}
}

func mustProfile(t testing.TB, data []byte) *icc.Profile {
	t.Helper()
	p, err := icc.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestStreamBesideASharedOneConverts converts the stream of a page's
// /Contents that another page's array does not share, where the shared one
// was converted with the first page.
func TestStreamBesideASharedOneConverts(t *testing.T) {
	f := fixture{pages: []fixturePage{
		{content: "1 0 0 rg", contents: "[4 0 R 7 0 R]"},
		{content: "0 1 0 rg", contents: "[6 0 R 7 0 R]"},
	}, objs: []string{stream("", "0 0 1 rg")}}
	r := recolor(t, f, cmyk, naive(Options{}))
	for num, want := range map[int]string{4: " 0 1 1 0 k", 6: " 1 0 1 0 k", 7: " 1 1 0 0 k"} {
		if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: num})); got != want {
			t.Errorf("object %d = %q, want %q", num, got, want)
		}
	}
	if len(r.report.Skipped) != 0 {
		t.Errorf("skipped %v", r.report.Skipped)
	}
}

// TestMissingObjectsReadAsNull converts a page whose /Resources and one
// /Contents element name objects the file lacks, which read as null (ISO
// 32000-1, 7.3.10).
func TestMissingObjectsReadAsNull(t *testing.T) {
	f := fixture{pages: []fixturePage{{dict: "/Resources 99 0 R", content: "1 0 0 rg", contents: "[98 0 R 4 0 R]"}}}
	r := recolor(t, f, cmyk, naive(Options{}))
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 4})); got != " 0 1 1 0 k" {
		t.Errorf("content = %q", got)
	}
	if len(r.report.Skipped) != 0 {
		t.Errorf("skipped %v", r.report.Skipped)
	}
}

// TestICCSpaceOfTheTargetIsWrittenInTheTarget writes the colour of an
// ICCBased space that reads as the target, here through its device fallback
// since its profile does not parse, with the target's operator.
func TestICCSpaceOfTheTargetIsWrittenInTheTarget(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /C [/ICCBased 5 0 R] >> >>", "/C cs 0 0 0 1 sc",
		stream("/N 4", "not a profile"))
	r := recolor(t, f, cmyk, naive(Options{}))
	// cs sets the initial colour of an ICCBased space, 0 in each component
	// (ISO 32000-1, 8.6.8, Table 74).
	if got, want := r.text(0), "0 0 0 0 k\n0 0 0 1 k"; got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
}
