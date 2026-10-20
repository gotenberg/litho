package graphics

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func op(name string, operands ...ast.PdfObject) content.Operation {
	return content.Operation{Operator: name, Operands: operands}
}

func num(v float64) ast.PdfObject { return ast.PdfReal(v) }

func run(t *testing.T, m *Machine, ops ...content.Operation) *State {
	t.Helper()
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return m.Current()
}

func TestGraphicsState_Defaults(t *testing.T) {
	s := testMachine().Current()
	// ISO 32000-1, 8.4.1, Tables 52 and 53.
	if s.LineWidth != 1 || s.LineCap != LineCapButt || s.LineJoin != LineJoinMiter || s.MiterLimit != 10 {
		t.Errorf("line parameters = %v %v %v %v, want 1 0 0 10", s.LineWidth, s.LineCap, s.LineJoin, s.MiterLimit)
	}
	if len(s.Dash.Array) != 0 || s.Dash.Phase != 0 {
		t.Errorf("dash = %+v, want solid", s.Dash)
	}
	if s.RenderingIntent != "RelativeColorimetric" || s.Flatness != 1 {
		t.Errorf("intent, flatness = %q %v", s.RenderingIntent, s.Flatness)
	}
	if s.StrokeAlpha != 1 || s.FillAlpha != 1 || s.BlendMode != "Normal" || s.SoftMask != nil || s.AlphaIsShape || !s.TextKnockout {
		t.Errorf("transparency defaults wrong: %+v", s)
	}
	for _, c := range []Color{s.Fill, s.Stroke} {
		if c.Space.Family != FamilyDeviceGray || !slices.Equal(c.Components, []float64{0}) {
			t.Errorf("initial colour = %+v, want DeviceGray 0", c)
		}
	}
}

func TestGraphicsState_ParameterOperators(t *testing.T) {
	s := run(t, testMachine(),
		op("w", num(2.5)),
		op("J", ast.PdfInt(1)),
		op("j", ast.PdfInt(2)),
		op("M", num(4)),
		op("d", ast.PdfArray{ast.PdfInt(3), ast.PdfInt(1)}, ast.PdfInt(2)),
		op("ri", ast.PdfName("Perceptual")),
		op("i", num(5)),
	)
	if s.LineWidth != 2.5 || s.LineCap != 1 || s.LineJoin != 2 || s.MiterLimit != 4 {
		t.Errorf("line parameters = %v %v %v %v", s.LineWidth, s.LineCap, s.LineJoin, s.MiterLimit)
	}
	if !slices.Equal(s.Dash.Array, []float64{3, 1}) || s.Dash.Phase != 2 {
		t.Errorf("dash = %+v, want [3 1] 2", s.Dash)
	}
	if s.RenderingIntent != "Perceptual" || s.Flatness != 5 {
		t.Errorf("intent, flatness = %q %v", s.RenderingIntent, s.Flatness)
	}
}

func TestGraphicsState_MalformedOperandsLeaveState(t *testing.T) {
	s := run(t, testMachine(),
		op("J", ast.PdfInt(7)), // Table 54 defines 0 to 2 only
		op("j", ast.PdfInt(-1)),
		op("w", ast.PdfName("x")),
		op("d", ast.PdfArray{ast.PdfName("x")}, ast.PdfInt(0)),
		op("M"),
	)
	if s.LineCap != 0 || s.LineJoin != 0 || s.LineWidth != 1 || s.Dash.Array != nil || s.MiterLimit != 10 {
		t.Errorf("malformed operands changed state: %+v", s)
	}
	// 8.6.5.8: an unrecognised intent selects RelativeColorimetric.
	s = run(t, testMachine(), op("ri", ast.PdfName("Perceptual")), op("ri", ast.PdfName("Bogus")))
	if s.RenderingIntent != "RelativeColorimetric" {
		t.Errorf("intent = %q, want RelativeColorimetric", s.RenderingIntent)
	}
}

func TestGraphicsState_SaveRestore(t *testing.T) {
	m := testMachine()
	run(t, m,
		op("d", ast.PdfArray{ast.PdfInt(4)}, ast.PdfInt(0)),
		op("q"),
		op("w", num(9)),
		op("d", ast.PdfArray{ast.PdfInt(1), ast.PdfInt(2)}, ast.PdfInt(1)),
		op("rg", num(1), num(0), num(0)),
	)
	inner := m.Current()
	if inner.LineWidth != 9 || inner.Fill.Space.Family != FamilyDeviceRGB {
		t.Fatalf("inner state not set: %+v", inner)
	}
	s := run(t, m, op("Q"))
	if s.LineWidth != 1 || !slices.Equal(s.Dash.Array, []float64{4}) || s.Fill.Space.Family != FamilyDeviceGray {
		t.Errorf("Q did not restore: width %v dash %+v fill %+v", s.LineWidth, s.Dash, s.Fill)
	}
}

func TestGraphicsState_ExtGState(t *testing.T) {
	smask := ast.PdfDict{"S": ast.PdfName("Luminosity")}
	fontRef := ast.PdfIndirectRef{ObjectNum: 12}
	m := testMachine()
	m.Resources = ast.PdfDict{"ExtGState": ast.PdfDict{
		"GS1": ast.PdfDict{
			"Type":  ast.PdfName("ExtGState"),
			"LW":    num(3),
			"LC":    ast.PdfInt(2),
			"LJ":    ast.PdfInt(1),
			"ML":    num(2),
			"D":     ast.PdfArray{ast.PdfArray{ast.PdfInt(5), ast.PdfInt(5)}, ast.PdfInt(1)},
			"RI":    ast.PdfName("Saturation"),
			"OP":    ast.PdfBool(true),
			"OPM":   ast.PdfInt(1),
			"Font":  ast.PdfArray{fontRef, ast.PdfInt(14)},
			"FL":    num(2),
			"SM":    num(0.02),
			"SA":    ast.PdfBool(true),
			"BM":    ast.PdfArray{ast.PdfName("Bogus"), ast.PdfName("Multiply"), ast.PdfName("Screen")},
			"SMask": smask,
			"CA":    num(0.5),
			"ca":    num(0.25),
			"AIS":   ast.PdfBool(true),
			"TK":    ast.PdfBool(false),
		},
		"GS2": ast.PdfDict{"SMask": ast.PdfName("None"), "BM": ast.PdfName("Compatible"), "op": ast.PdfBool(false)},
	}}
	s := run(t, m,
		op("Tf", ast.PdfName("F1"), ast.PdfInt(10)),
		op("cm", num(2), num(0), num(0), num(2), num(10), num(20)),
		op("gs", ast.PdfName("GS1")),
	)
	if s.LineWidth != 3 || s.LineCap != 2 || s.LineJoin != 1 || s.MiterLimit != 2 {
		t.Errorf("line parameters = %v %v %v %v", s.LineWidth, s.LineCap, s.LineJoin, s.MiterLimit)
	}
	if !slices.Equal(s.Dash.Array, []float64{5, 5}) || s.Dash.Phase != 1 {
		t.Errorf("dash = %+v", s.Dash)
	}
	if s.RenderingIntent != "Saturation" || s.Flatness != 2 || s.Smoothness != 0.02 || !s.StrokeAdjustment {
		t.Errorf("intent/flatness/smoothness/SA = %q %v %v %v", s.RenderingIntent, s.Flatness, s.Smoothness, s.StrokeAdjustment)
	}
	// Table 58: OP sets op too when op is absent.
	if !s.StrokeOverprint || !s.FillOverprint || s.OverprintMode != 1 {
		t.Errorf("overprint = %v %v %v", s.StrokeOverprint, s.FillOverprint, s.OverprintMode)
	}
	// 11.3.5: the first recognised name of a BM array applies.
	if s.BlendMode != "Multiply" {
		t.Errorf("BlendMode = %q, want Multiply", s.BlendMode)
	}
	if s.SoftMask == nil || s.SoftMask.Dict["S"] != ast.PdfName("Luminosity") || s.SoftMask.CTM != [6]float64{2, 0, 0, 2, 10, 20} {
		t.Errorf("SoftMask = %+v, want the dictionary with the CTM at gs", s.SoftMask)
	}
	if s.StrokeAlpha != 0.5 || s.FillAlpha != 0.25 || !s.AlphaIsShape || s.TextKnockout {
		t.Errorf("alpha = %v %v AIS %v TK %v", s.StrokeAlpha, s.FillAlpha, s.AlphaIsShape, s.TextKnockout)
	}
	// /Font goes to GStateFont; Tf and Tfs, which extraction reads, stay.
	if s.Text.GStateFont != fontRef || s.Text.GStateFontSize != 14 || s.Text.Tf != "F1" || s.Text.Tfs != 10 {
		t.Errorf("font = %v %v, Tf %q %v", s.Text.GStateFont, s.Text.GStateFontSize, s.Text.Tf, s.Text.Tfs)
	}

	s = run(t, m, op("gs", ast.PdfName("GS2")))
	if s.SoftMask != nil || s.BlendMode != "Normal" || s.FillOverprint || !s.StrokeOverprint {
		t.Errorf("GS2: SoftMask %v BM %q op %v OP %v", s.SoftMask, s.BlendMode, s.FillOverprint, s.StrokeOverprint)
	}
	s = run(t, m, op("Tf", ast.PdfName("F2"), ast.PdfInt(8)))
	if s.Text.GStateFont != nil {
		t.Errorf("Tf left GStateFont = %v", s.Text.GStateFont)
	}
}

func TestGraphicsState_ExtGStateUnresolvedIsInert(t *testing.T) {
	before := *testMachine().Current()
	m := testMachine()
	s := run(t, m, op("gs", ast.PdfName("Missing")))
	if s.LineWidth != before.LineWidth || s.BlendMode != before.BlendMode {
		t.Errorf("unresolved gs changed state")
	}
	m.Resources = ast.PdfDict{"ExtGState": ast.PdfDict{"R": ast.PdfIndirectRef{ObjectNum: 3}}}
	s = run(t, m, op("gs", ast.PdfName("R")))
	if s.LineWidth != before.LineWidth {
		t.Errorf("gs through a reference with no document changed state")
	}
}

func TestColor_DeviceOperators(t *testing.T) {
	s := run(t, testMachine(),
		op("rg", num(1), num(0.5), num(0)),
		op("K", num(0), num(0), num(0), num(1)),
	)
	if s.Fill.Space.Family != FamilyDeviceRGB || s.Fill.Space.Components != 3 || !slices.Equal(s.Fill.Components, []float64{1, 0.5, 0}) {
		t.Errorf("fill = %+v", s.Fill)
	}
	if s.Stroke.Space.Family != FamilyDeviceCMYK || !slices.Equal(s.Stroke.Components, []float64{0, 0, 0, 1}) {
		t.Errorf("stroke = %+v", s.Stroke)
	}
	// The legacy fields keep their reading.
	if s.FillColorSpace != "DeviceRGB" || !slices.Equal(s.FillColor, []float64{1, 0.5, 0}) {
		t.Errorf("legacy fill = %q %v", s.FillColorSpace, s.FillColor)
	}
}

func TestColor_InitialColorPerSpace(t *testing.T) {
	icc := ast.NewStream(ast.PdfDict{"N": ast.PdfInt(3), "Range": ast.PdfArray{
		ast.PdfInt(0), ast.PdfInt(1), num(0.2), ast.PdfInt(1), num(-1), num(-0.5),
	}}, nil)
	m := testMachine()
	m.Resources = ast.PdfDict{"ColorSpace": ast.PdfDict{
		"ICC": ast.PdfArray{ast.PdfName("ICCBased"), icc},
		"Sep": ast.PdfArray{ast.PdfName("Separation"), ast.PdfName("Spot"), ast.PdfName("DeviceCMYK"), ast.PdfDict{}},
		"DN":  ast.PdfArray{ast.PdfName("DeviceN"), ast.PdfArray{ast.PdfName("A"), ast.PdfName("B"), ast.PdfName("C")}, ast.PdfName("DeviceCMYK"), ast.PdfDict{}},
		"L1":  ast.PdfArray{ast.PdfName("Lab"), ast.PdfDict{"WhitePoint": ast.PdfArray{num(0.95), ast.PdfInt(1), num(1.09)}, "Range": ast.PdfArray{ast.PdfInt(10), ast.PdfInt(20), ast.PdfInt(-5), ast.PdfInt(5)}}},
		"Idx": ast.PdfArray{ast.PdfName("Indexed"), ast.PdfName("DeviceRGB"), ast.PdfInt(1), ast.PdfString("\x00\x00\x00\xff\xff\xff")},
		// A resource named like a device family does not override it (8.6.3).
		"DeviceRGB": ast.PdfName("DeviceGray"),
	}}
	cases := []struct {
		name   string
		family string
		want   []float64
	}{
		// 8.6.8: every component 0.0, clipped into an ICCBased /Range.
		{"ICC", FamilyICCBased, []float64{0, 0.2, -0.5}},
		// 8.6.6.4 and 8.6.6.5: Separation and DeviceN start at 1.0.
		{"Sep", FamilySeparation, []float64{1}},
		{"DN", FamilyDeviceN, []float64{1, 1, 1}},
		// a* and b* clipped into the Lab /Range.
		{"L1", FamilyLab, []float64{0, 10, 0}},
		{"Idx", FamilyIndexed, []float64{0}},
		{"DeviceCMYK", FamilyDeviceCMYK, []float64{0, 0, 0, 1}},
		{"DeviceRGB", FamilyDeviceRGB, []float64{0, 0, 0}},
	}
	for _, tc := range cases {
		s := run(t, m, op("cs", ast.PdfName(tc.name)), op("CS", ast.PdfName(tc.name)))
		for _, c := range []Color{s.Fill, s.Stroke} {
			if c.Space.Family != tc.family || !slices.Equal(c.Components, tc.want) {
				t.Errorf("%s: colour = %+v, want %s %v", tc.name, c, tc.family, tc.want)
			}
		}
		wantResource := tc.name
		if tc.family == tc.name {
			wantResource = ""
		}
		if s.Fill.Space.Resource != wantResource {
			t.Errorf("%s: Resource = %q, want %q", tc.name, s.Fill.Space.Resource, wantResource)
		}
	}
	s := run(t, m, op("cs", ast.PdfName("DN")), op("scn", num(0.1), num(0.2), num(0.3)))
	if !slices.Equal(s.Fill.Components, []float64{0.1, 0.2, 0.3}) || s.Fill.Space.Family != FamilyDeviceN {
		t.Errorf("scn in DeviceN = %+v", s.Fill)
	}
	s = run(t, m, op("cs", ast.PdfName("Nowhere")))
	if s.Fill.Space.Family != "" || s.Fill.Space.Resource != "Nowhere" || s.Fill.Components != nil {
		t.Errorf("unresolved space = %+v, want an empty family naming the resource", s.Fill)
	}
}

func TestColor_PatternSpaces(t *testing.T) {
	m := testMachine()
	m.Resources = ast.PdfDict{"ColorSpace": ast.PdfDict{
		"PRGB": ast.PdfArray{ast.PdfName("Pattern"), ast.PdfName("DeviceRGB")},
		"Bad":  ast.PdfArray{ast.PdfName("Pattern"), ast.PdfArray{ast.PdfName("Pattern")}},
	}}
	s := run(t, m, op("cs", ast.PdfName("Pattern")))
	if s.Fill.Space.Family != FamilyPattern || s.Fill.Pattern != "" || s.Fill.Components != nil {
		t.Errorf("initial pattern colour = %+v, want no pattern", s.Fill)
	}
	s = run(t, m, op("scn", ast.PdfName("P1")), op("CS", ast.PdfName("PRGB")), op("SCN", num(1), num(0), num(0), ast.PdfName("P2")))
	if s.Fill.Pattern != "P1" {
		t.Errorf("fill pattern = %q, want P1", s.Fill.Pattern)
	}
	// 8.7.3.3: an uncoloured pattern carries its colour in the base space.
	if s.Stroke.Pattern != "P2" || s.Stroke.Space.Base == nil || s.Stroke.Space.Base.Family != FamilyDeviceRGB ||
		s.Stroke.Space.Components != 3 || !slices.Equal(s.Stroke.Components, []float64{1, 0, 0}) {
		t.Errorf("stroke = %+v", s.Stroke)
	}
	// A pattern whose base is itself a Pattern space is malformed.
	s = run(t, m, op("cs", ast.PdfName("Bad")))
	if s.Fill.Space.Base != nil {
		t.Errorf("nested Pattern base accepted: %+v", s.Fill.Space)
	}
}

func TestType3Glyph(t *testing.T) {
	m := testMachine()
	run(t, m, op("d0", ast.PdfInt(500), ast.PdfInt(0)), op("rg", num(1), num(0), num(0)))
	if m.Glyph == nil || m.Glyph.Wx != 500 || !m.Glyph.Colored {
		t.Fatalf("d0 glyph = %+v", m.Glyph)
	}
	if m.Current().Fill.Space.Family != FamilyDeviceRGB {
		t.Errorf("d0 glyph ignored its colour operator")
	}

	m = testMachine()
	s := run(t, m,
		op("d1", ast.PdfInt(600), ast.PdfInt(0), ast.PdfInt(10), ast.PdfInt(-20), ast.PdfInt(590), ast.PdfInt(700)),
		op("rg", num(1), num(0), num(0)),
		op("cs", ast.PdfName("DeviceCMYK")),
		op("w", num(4)),
	)
	if m.Glyph == nil || m.Glyph.Colored || m.Glyph.BBox != [4]float64{10, -20, 590, 700} {
		t.Fatalf("d1 glyph = %+v", m.Glyph)
	}
	// Table 113: a d1 glyph's colour operators are ignored, others are not.
	if s.Fill.Space.Family != FamilyDeviceGray || s.FillColorSpace != "DeviceGray" || s.LineWidth != 4 {
		t.Errorf("d1 glyph: fill %+v legacy %q width %v", s.Fill, s.FillColorSpace, s.LineWidth)
	}
}

func TestGraphicsState_ShadingStaysUnknown(t *testing.T) {
	m := testMachine()
	m.RecordUnknownOperators = true
	m.RecordShadingBoxes = true
	run(t, m, op("gs", ast.PdfName("G")), op("w", num(1)), op("sh", ast.PdfName("Sh0")))
	got := m.UnknownOperators()
	if len(got) != 1 || got[0].Operator != "sh" {
		t.Errorf("UnknownOperators() = %v, want only sh", got)
	}
	if len(m.ShadingBoxes()) != 1 {
		t.Errorf("ShadingBoxes() = %v, want one", m.ShadingBoxes())
	}
}

func BenchmarkGraphicsStateOperators(b *testing.B) {
	m := testMachine()
	m.Resources = ast.PdfDict{
		"ExtGState":  ast.PdfDict{"G": ast.PdfDict{"CA": num(0.5), "ca": num(0.5), "BM": ast.PdfName("Multiply"), "LW": num(2)}},
		"ColorSpace": ast.PdfDict{"CS0": ast.PdfArray{ast.PdfName("Separation"), ast.PdfName("Spot"), ast.PdfName("DeviceCMYK"), ast.PdfDict{}}},
	}
	ops := []content.Operation{
		op("q"),
		op("w", num(2)),
		op("d", ast.PdfArray{ast.PdfInt(3)}, ast.PdfInt(0)),
		op("gs", ast.PdfName("G")),
		op("cs", ast.PdfName("CS0")),
		op("scn", num(0.5)),
		op("rg", num(1), num(0), num(0)),
		op("Q"),
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if err := m.Execute(ctx, ops); err != nil {
			b.Fatal(err)
		}
	}
}
