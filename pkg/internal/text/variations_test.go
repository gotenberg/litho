package text

import (
	"context"
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/internal/typography/varfont"
	"github.com/gotenberg/litho/pkg/models"
)

// varFontPath is a variable TrueType font of five axes, wght, wdth, slnt,
// opsz and TEST, made for the instancer's tests.
const varFontPath = "../typography/varfont/testdata/varfont.ttf"

// loadVarFace loads varFontPath as the document face of family "Var"
// with the descriptors d, beside an index holding Test Sans.
func loadVarFace(t *testing.T, d FaceDescriptor) (*Fonts, *Face) {
	t.Helper()
	program, err := os.ReadFile(varFontPath)
	if err != nil {
		t.Fatal(err)
	}
	d.Family = "Var"
	fc, err := LoadFace(context.Background(), program, d)
	if err != nil {
		t.Fatal(err)
	}
	fonts := newTestFonts(t, fontFile{"sans.ttf", testSans("Regular", 400, false, 100)})
	return fonts.WithFaces(NewFaceSet([]*Face{fc})), fc
}

// shapeVar shapes "AB" in the family "Var" in style.
func shapeVar(t *testing.T, fonts *Fonts, style Style) (*Paragraph, Report) {
	t.Helper()
	style.Families = []string{"Var"}
	if style.Size == 0 {
		style.Size = 30
	}
	p, rep, err := fonts.Shape(context.Background(), []ParagraphRun{{Text: "AB", Style: style}}, ParagraphOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.items) != 1 {
		t.Fatalf("%d shaped items, want 1", len(p.items))
	}
	return p, rep
}

// TestVariableFaceDrawsItsInstance pins that a variable document face is
// shaped and embedded as the instance its style asks for: the embedded
// program is static, named for the instance, and every glyph advances by
// the instance's own advance, the width the PDF states for it.
func TestVariableFaceDrawsItsInstance(t *testing.T) {
	t.Parallel()
	fonts, base := loadVarFace(t, FaceDescriptor{AutoStyle: true})
	for _, tc := range []struct {
		name  string
		style Style
		want  string
	}{
		{"weight", Style{Weight: 900, Fallback: FallbackChromium, Size: 9}, "LithoVar_900wght_100wdth_0slnt_12opsz_0TEST"},
		{"stretch", Style{Stretch: 80, Fallback: FallbackChromium, Size: 9}, "LithoVar_400wght_80wdth_0slnt_12opsz_0TEST"},
		{"oblique", Style{Italic: true, Oblique: 5, Size: 12}, "LithoVar_400wght_100wdth_-5slnt_12opsz_0TEST"},
		{"optical size in pixels", Style{Fallback: FallbackChromium, Size: 30}, "LithoVar_400wght_100wdth_0slnt_40opsz_0TEST"},
		{"optical size in points", Style{Size: 30}, "LithoVar_400wght_100wdth_0slnt_30opsz_0TEST"},
		{"variations", Style{Size: 12, Settings: &FontSettings{Variations: []Variation{{"TEST", 70}, {"wght", 650}, {"none", 3}}}}, "LithoVar_650wght_100wdth_0slnt_12opsz_70TEST"},
	} {
		p, rep := shapeVar(t, fonts, tc.style)
		if len(rep.DefaultInstances) > 0 {
			t.Errorf("%s: default instances %v", tc.name, rep.DefaultInstances)
		}
		it := p.items[0]
		program := it.face.Program
		if !strings.HasPrefix(it.face.Key, base.Key()[:len(base.Key())-2]+"@") {
			t.Errorf("%s: key %q is not an instance of %q", tc.name, it.face.Key, base.Key())
		}
		records, err := truetype.ParseTableDirectory(program)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := truetype.TableData(program, records, "fvar"); err == nil {
			t.Errorf("%s: the embedded program keeps fvar", tc.name)
		}
		if got := postScriptNameOf(t, program); got != tc.want {
			t.Errorf("%s: PostScript name %q, want %q", tc.name, got, tc.want)
		}
		hm, err := truetype.ReadHMetrics(program)
		if err != nil {
			t.Fatal(err)
		}
		size := it.style.Size
		for _, g := range it.glyphs {
			want := hm.Advance(int(g.gid())) * size / 1000
			if math.Abs(g.advance-want) > 1e-3 {
				t.Errorf("%s: glyph %d advances %v, the instance %v", tc.name, g.gid(), g.advance, want)
			}
		}
	}
}

// postScriptNameOf reads name ID 6 through the face reader.
func postScriptNameOf(t *testing.T, program []byte) string {
	t.Helper()
	fc, err := LoadFace(context.Background(), program, FaceDescriptor{Family: "X"})
	if err != nil {
		t.Fatal(err)
	}
	return fc.postScript
}

// TestVariableFaceDefaultInstance pins that a style at the default
// instance draws the variable face itself: no instance is made.
func TestVariableFaceDefaultInstance(t *testing.T) {
	t.Parallel()
	fonts, base := loadVarFace(t, FaceDescriptor{})
	for _, style := range []Style{
		{Size: 12},
		{Size: 9, Fallback: FallbackChromium},
		{Size: 40, Settings: &FontSettings{NoOpticalSizing: true}},
	} {
		p, _ := shapeVar(t, fonts, style)
		if got := p.items[0].face.Key; got != base.Key() {
			t.Errorf("%+v: drawn with %q, want the face itself", style, got)
		}
	}
	if n := fonts.inst.count.n; n != 0 {
		t.Errorf("%d instances made", n)
	}
	fonts, base = loadVarFace(t, FaceDescriptor{})
	shapeVar(t, fonts, Style{Weight: 700, Size: 12})
	shapeVar(t, fonts, Style{Weight: 700, Size: 12})
	if n := fonts.inst.count.n; n != 1 || len(base.instances) != 1 {
		t.Errorf("two runs at one instance made %d instances", n)
	}
}

// TestVariableFaceCoordinates pins the axis values a style and a rule set:
// the font properties clamped to the rule's descriptors, the optical size,
// the rule's variations, then the style's variations, as Chromium orders
// them; italic as a slant of 14 degrees, never the ital axis.
func TestVariableFaceCoordinates(t *testing.T) {
	t.Parallel()
	// The axes are wght, wdth, slnt, opsz and TEST.
	for _, tc := range []struct {
		name  string
		d     FaceDescriptor
		style Style
		want  []float64
	}{
		{"auto weight", FaceDescriptor{}, Style{Weight: 900, Size: 12}, []float64{900, 100, 0, 12, 0}},
		{"weight range", FaceDescriptor{WeightMin: 300, WeightMax: 500}, Style{Weight: 900, Size: 12}, []float64{500, 100, 0, 12, 0}},
		{"rule variations", FaceDescriptor{Variations: []Variation{{"wght", 800}}}, Style{Weight: 300, Size: 12}, []float64{800, 100, 0, 12, 0}},
		{"style variations", FaceDescriptor{Variations: []Variation{{"wght", 800}}}, Style{Size: 12, Settings: &FontSettings{Variations: []Variation{{"wght", 200}}}}, []float64{200, 100, 0, 12, 0}},
		{"stretch range", FaceDescriptor{StretchMin: 90, StretchMax: 110}, Style{Stretch: 75, Size: 12}, []float64{400, 90, 0, 12, 0}},
		{"italic", FaceDescriptor{}, Style{Italic: true, Size: 12}, []float64{400, 100, -14, 12, 0}},
		{"oblique range", FaceDescriptor{Style: FaceOblique, ObliqueMin: 0, ObliqueMax: 3}, Style{Italic: true, Oblique: 5, Size: 12}, []float64{400, 100, -3, 12, 0}},
		{"italic rule", FaceDescriptor{Style: FaceItalic}, Style{Size: 12}, []float64{400, 100, -14, 12, 0}},
		{"normal rule", FaceDescriptor{}, Style{Italic: true, Size: 12}, []float64{400, 100, 0, 12, 0}},
		{"rule optical size", FaceDescriptor{Variations: []Variation{{"opsz", 100}}}, Style{Size: 24}, []float64{400, 100, 0, 100, 0}},
		{"no optical sizing", FaceDescriptor{}, Style{Size: 24, Settings: &FontSettings{NoOpticalSizing: true}}, []float64{400, 100, 0, 12, 0}},
		{"pixels", FaceDescriptor{}, Style{Size: 24, Fallback: FallbackChromium}, []float64{400, 100, 0, 32, 0}},
	} {
		d := tc.d
		d.AutoStyle = tc.name != "normal rule" && tc.d.Style == FaceNormal
		_, fc := loadVarFace(t, d)
		if got := fc.userCoordinates(&tc.style); !equalFloats(got, tc.want) {
			t.Errorf("%s: coordinates %v, want %v", tc.name, got, tc.want)
		}
	}
}

func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestVariableFaceSynthesis pins that a rule's auto weight covers the
// face's wght axis, so no weight on it is synthesised, and that an auto
// style on a face with a slnt axis slants by the axis alone, while an
// explicit range synthesises past it as Chromium does.
func TestVariableFaceSynthesis(t *testing.T) {
	t.Parallel()
	_, auto := loadVarFace(t, FaceDescriptor{AutoStyle: true})
	if auto.doc.WeightMin != 100 || auto.doc.WeightMax != 900 {
		t.Errorf("an auto weight covers %d to %d, want the axis's 100 to 900", auto.doc.WeightMin, auto.doc.WeightMax)
	}
	bold := Style{Weight: 900, Italic: true, Size: 20}
	if stroke, skew := synthesis(requestOf(bold), auto, bold.Size); stroke != 0 || skew != 0 {
		t.Errorf("auto rule: synthetic stroke %v, skew %v, want none", stroke, skew)
	}
	_, fixed := loadVarFace(t, FaceDescriptor{WeightMin: 400, WeightMax: 400})
	if stroke, skew := synthesis(requestOf(bold), fixed, bold.Size); stroke == 0 || skew == 0 {
		t.Errorf("rule of weight 400 and normal style: synthetic stroke %v, skew %v, want both", stroke, skew)
	}
}

// TestVariableFaceInstanceLimit pins the bound on instances: past it, text
// draws in the default instance and the report says why.
func TestVariableFaceInstanceLimit(t *testing.T) {
	t.Parallel()
	fonts, base := loadVarFace(t, FaceDescriptor{})
	fonts.inst.count.n = maxDocumentInstances
	p, rep := shapeVar(t, fonts, Style{Weight: 700, Size: 12})
	if got := p.items[0].face.Key; got != base.Key() {
		t.Errorf("drawn with %q past the bound, want the face itself", got)
	}
	if len(rep.DefaultInstances) != 1 || !rep.DefaultInstances[0].Limit || rep.DefaultInstances[0].Family != "Var" {
		t.Errorf("report %+v, want the bound for Var", rep.DefaultInstances)
	}
}

// TestVariableFaceUnsupported pins that a variable face the instancer
// refuses, such as one without glyf outlines, draws at its default
// instance with the reason.
func TestVariableFaceUnsupported(t *testing.T) {
	t.Parallel()
	program, err := os.ReadFile(varFontPath)
	if err != nil {
		t.Fatal(err)
	}
	records, err := truetype.ParseTableDirectory(program)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range records {
		if r.Tag == "glyf" {
			copy(program[12+16*i:], "glyX")
		}
	}
	vf, err := varfont.Parse(program)
	if err != nil {
		t.Fatal(err)
	}
	fc := &Face{src: &fontSource{path: "x"}, doc: &FaceDescriptor{WeightMin: 100, WeightMax: 900}, vf: vf}
	got, err := fc.instanceOf(context.Background(), &Style{Weight: 700, Size: 12}, nil, &instanceCount{})
	if got != fc || !errors.Is(err, varfont.ErrUnsupported) {
		t.Errorf("instanceOf = %p, %v; want the face itself and ErrUnsupported", got, err)
	}
}

// TestVariableFaceChargesTheBudget pins that an instance a paragraph
// creates is charged to the conversion's memory budget: a budget smaller
// than the instance refuses it.
func TestVariableFaceChargesTheBudget(t *testing.T) {
	t.Parallel()
	fonts, base := loadVarFace(t, FaceDescriptor{})
	ctx, b, done := limits.WithBudget(context.Background(), models.Limits{MaxConvertMemoryBytes: 1 << 20})
	defer done()
	if err := b.Charge(1<<20-1024, "test"); err != nil {
		t.Fatal(err)
	}
	style := Style{Families: []string{"Var"}, Weight: 700, Size: 12}
	_, _, err := fonts.Shape(ctx, []ParagraphRun{{Text: "AB", Style: style}}, ParagraphOptions{})
	if !errors.Is(err, limits.ErrMemoryBudget) {
		t.Errorf("Shape past the budget = %v, want ErrMemoryBudget", err)
	}
	if len(base.instances) != 0 {
		t.Error("the refused instance was kept")
	}
}
