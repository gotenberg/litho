package text

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/internal/typography/varfont"
)

// installedVar indexes the variable test font beside a static sans face.
// The test font, LithoVar, names two instances: Light (wght 300) and Bold
// (wght 733.33, PostScript name LithoVar-BoldInstance).
func installedVar(t *testing.T) *Fonts {
	t.Helper()
	return newTestFonts(t, repoFont(t, "var.ttf", varFontPath), fontFile{"sans.ttf", testSans("Regular", 400, false, 100)})
}

// shapeInstalled shapes "AB" in LithoVar in style and returns the face
// that draws it.
func shapeInstalled(t *testing.T, fonts *Fonts, style Style) (shapedItem, Report) {
	t.Helper()
	style.Families = []string{"LithoVar"}
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
	return p.items[0], rep
}

// TestIndexListsNamedInstances pins that the index offers each named
// instance of a variable file as a face of its family, with the aspect of
// its coordinates and its full name, without instancing anything.
func TestIndexListsNamedInstances(t *testing.T) {
	t.Parallel()
	fonts := installedVar(t)
	var got []string
	for _, fc := range fonts.byFamily[familyKey("LithoVar")] {
		got = append(got, fc.fullName+"/"+strconv.Itoa(fc.weight))
		if fc.axes == nil {
			t.Errorf("%q: no design space", fc.fullName)
		}
		if fc.base != nil || fc.typesetFace != nil {
			t.Errorf("%q: indexing instanced or read the program", fc.fullName)
		}
	}
	if want := "/400 LithoVar Light/300 LithoVar Bold/733"; strings.Join(got, " ") != want {
		t.Errorf("LithoVar faces %q, want %q", strings.Join(got, " "), want)
	}
	if _, err := fonts.LocalFace("LithoVar Light", FaceDescriptor{Family: "X"}); err != nil {
		t.Errorf("local(LithoVar Light): %v", err)
	}
	if _, err := fonts.LocalFace("LithoVar-BoldInstance", FaceDescriptor{Family: "X"}); !errors.Is(err, ErrNoLocalFace) {
		t.Errorf("local(LithoVar-BoldInstance) = %v, want ErrNoLocalFace", err)
	}
}

// TestInstalledVariableFaceDrawsNamedInstance pins what an installed
// variable face draws: under Chromium's rule the named instance nearest
// the weight, at its own coordinates; under LibreOffice's, the named
// instance with opsz at the size in points. Glyphs advance by the
// instance's widths.
func TestInstalledVariableFaceDrawsNamedInstance(t *testing.T) {
	t.Parallel()
	fonts := installedVar(t).ForConversion()
	vf := parseVarFont(t)
	for _, tc := range []struct {
		name  string
		style Style
		user  []float64
	}{
		{"chromium light", Style{Weight: 300, Fallback: FallbackChromium}, []float64{300, 100, 0, 12, 0}},
		{"chromium nearest light", Style{Weight: 350, Fallback: FallbackChromium}, []float64{300, 100, 0, 12, 0}},
		{"chromium bold", Style{Weight: 700, Fallback: FallbackChromium}, []float64{733.3333282470703, 100, 0, 12, 0}},
		{"chromium ignores variations", Style{Weight: 700, Fallback: FallbackChromium, Settings: &FontSettings{Variations: []Variation{{"wght", 900}}}}, []float64{733.3333282470703, 100, 0, 12, 0}},
		{"libreoffice regular", Style{Size: 18, Fallback: FallbackLibreOffice}, []float64{400, 100, 0, 18, 0}},
		{"libreoffice bold", Style{Weight: 700, Size: 9}, []float64{733.3333282470703, 100, 0, 9, 0}},
	} {
		it, rep := shapeInstalled(t, fonts, tc.style)
		if len(rep.DefaultInstances) > 0 {
			t.Errorf("%s: default instances %v", tc.name, rep.DefaultInstances)
		}
		want, err := vf.Instance(context.Background(), tc.user)
		if err != nil {
			t.Fatal(err)
		}
		if string(it.face.Program) != string(want) {
			t.Errorf("%s: drew %q, not the instance at %v", tc.name, it.face.Key, tc.user)
		}
		hm, err := truetype.ReadHMetrics(want)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range it.glyphs {
			w := hm.Advance(int(g.gid())) * it.style.Size / 1000
			if math.Abs(g.advance-w) > 1e-3 {
				t.Errorf("%s: glyph %d advances %v, the instance %v", tc.name, g.gid(), g.advance, w)
			}
		}
	}
}

// TestInstalledVariableFaceDefaultInstance pins that a style matching the
// default instance under Chromium's rule draws the file itself, and that
// a heavier request with no heavier instance is emboldened.
func TestInstalledVariableFaceDefaultInstance(t *testing.T) {
	t.Parallel()
	fonts := installedVar(t).ForConversion()
	it, _ := shapeInstalled(t, fonts, Style{Weight: 450, Fallback: FallbackChromium})
	if strings.Contains(it.face.Key, "@") {
		t.Errorf("weight 450 drew %q, want the file's own face", it.face.Key)
	}
	if fonts.inst.count.n != 0 {
		t.Errorf("%d instances made", fonts.inst.count.n)
	}
}

// TestInstalledInstancesBelongToTheConversion pins the ownership of the
// instances of installed faces: each conversion makes its own, charged to
// its budget and counted against its bound, and the shared index keeps
// none.
func TestInstalledInstancesBelongToTheConversion(t *testing.T) {
	t.Parallel()
	index := installedVar(t)
	a, b := index.ForConversion(), index.ForConversion()
	if a.ForConversion() != a {
		t.Error("ForConversion on a conversion's index made another")
	}
	style := Style{Weight: 700, Fallback: FallbackChromium}
	ia, _ := shapeInstalled(t, a, style)
	ib, _ := shapeInstalled(t, b, style)
	if ia.face == ib.face {
		t.Error("two conversions share an instance")
	}
	if string(ia.face.Program) != string(ib.face.Program) || ia.face.Key != ib.face.Key {
		t.Error("two conversions wrote the instance differently")
	}
	for _, fc := range index.faces {
		if fc.base != nil {
			t.Errorf("the index holds the instance %q", fc.Key())
		}
	}

	c := index.ForConversion()
	c.inst.count.n = maxDocumentInstances
	it, rep := shapeInstalled(t, c, style)
	if strings.Contains(it.face.Key, "@") {
		t.Errorf("drew %q past the bound, want the file's own face", it.face.Key)
	}
	if len(rep.DefaultInstances) != 1 || !rep.DefaultInstances[0].Limit || rep.DefaultInstances[0].Family != "LithoVar" {
		t.Errorf("report %+v, want the bound for LithoVar", rep.DefaultInstances)
	}

	ctx, mem, done := limits.WithBudget(context.Background(), limits.Limits{MaxConvertMemoryBytes: 1 << 20})
	defer done()
	_, _, err := index.ForConversion().Shape(ctx, []ParagraphRun{{Text: "AB", Style: Style{Families: []string{"LithoVar"}, Weight: 700, Size: 30, Fallback: FallbackChromium}}}, ParagraphOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if mem.Peak() == 0 {
		t.Error("the instance was not charged to the budget")
	}
}

// TestInstalledVariableFaceOutsideConversion pins that the shared index
// draws a named instance at the default instance, emboldened as a static
// face would be, and reports why.
func TestInstalledVariableFaceOutsideConversion(t *testing.T) {
	t.Parallel()
	it, rep := shapeInstalled(t, installedVar(t), Style{Weight: 700, Fallback: FallbackChromium})
	if strings.Contains(it.face.Key, "@") {
		t.Errorf("drew %q, want the file's own face", it.face.Key)
	}
	if it.stroke == 0 {
		t.Error("the default instance is not emboldened")
	}
	if len(rep.DefaultInstances) != 1 || rep.DefaultInstances[0].Family != "LithoVar" {
		t.Errorf("report %+v, want LithoVar", rep.DefaultInstances)
	}
}

// TestInstanceWeightRank pins the weight a variable file's instances are
// chosen by, as measured in Chromium and LibreOffice (see
// [instanceWeightRank]).
func TestInstanceWeightRank(t *testing.T) {
	t.Parallel()
	weights := []int{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
	pick := func(want int, rule Fallback) int {
		best := 0
		var bestRank [2]int
		for _, w := range weights {
			a, b := instanceWeightRank(want, w, rule)
			if r := [2]int{a, b}; best == 0 || r[0] < bestRank[0] || (r[0] == bestRank[0] && r[1] < bestRank[1]) {
				best, bestRank = w, r
			}
		}
		return best
	}
	for want, got := range map[int]int{250: 200, 330: 300, 350: 300, 370: 400, 450: 400, 480: 500, 520: 500, 550: 500, 580: 600, 620: 600, 650: 600, 680: 700, 750: 700, 950: 900, 1000: 1000} {
		if p := pick(want, FallbackChromium); p != got {
			t.Errorf("chromium %d: picked %d, want %d", want, p, got)
		}
	}
	for want, got := range map[int]int{100: 100, 300: 300, 400: 400, 600: 600, 700: 700, 800: 800, 900: 1000} {
		if p := pick(want, FallbackLibreOffice); p != got {
			t.Errorf("libreoffice %d: picked %d, want %d", want, p, got)
		}
	}
}

// parseVarFont reads the variable test font's design space.
func parseVarFont(t *testing.T) *varfont.Font {
	t.Helper()
	program := repoFont(t, "var.ttf", varFontPath).data
	vf, err := varfont.Parse(program)
	if err != nil {
		t.Fatal(err)
	}
	return vf
}

// varCFF2Path is a variable font with CFF2 outlines, LithoVarCFF: wght
// 200 to 900 (default 200) and wdth 75 to 100, named instances Regular
// (wght 450) and Bold (wght 742.1).
const varCFF2Path = "../typography/varfont/testdata/varfont_cff2.otf"

// TestCFF2FacesEmbedAsCFF pins that a face with CFF2 outlines, a document
// face or an installed one, is embedded as a program with CFF outlines,
// at its default coordinates too: a PDF font file holds CFF, never CFF2
// (ISO 32000-1, 9.9, Table 126).
func TestCFF2FacesEmbedAsCFF(t *testing.T) {
	t.Parallel()
	program := repoFont(t, "cff2.otf", varCFF2Path).data
	doc, err := LoadFace(context.Background(), program, FaceDescriptor{Family: "Var"})
	if err != nil {
		t.Fatal(err)
	}
	fonts := newTestFonts(t, fontFile{"cff2.otf", program}, fontFile{"sans.ttf", testSans("Regular", 400, false, 100)})
	conv := fonts.ForConversion().WithFaces(NewFaceSet([]*Face{doc}))
	for _, tc := range []struct {
		name   string
		family string
		style  Style
		ps     string
	}{
		{"document face at its default", "Var", Style{Weight: 200, Size: 12}, "LithoVarCFF_200wght_100wdth"},
		{"installed face at its default", "LithoVarCFF", Style{Weight: 200, Fallback: FallbackChromium}, "LithoVarCFF_200wght_100wdth"},
		{"installed named instance", "LithoVarCFF", Style{Weight: 700, Fallback: FallbackChromium}, "LithoVarCFF-BoldInstance"},
	} {
		tc.style.Families = []string{tc.family}
		if tc.style.Size == 0 {
			tc.style.Size = 30
		}
		p, rep, err := conv.Shape(context.Background(), []ParagraphRun{{Text: "OH", Style: tc.style}}, ParagraphOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.DefaultInstances) > 0 || len(p.items) != 1 {
			t.Fatalf("%s: %d items, default instances %v", tc.name, len(p.items), rep.DefaultInstances)
		}
		face := p.items[0].face.Program
		records, err := truetype.ParseTableDirectory(face)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := truetype.TableData(face, records, "CFF2"); err == nil {
			t.Errorf("%s: the embedded program keeps CFF2", tc.name)
		}
		if _, err := truetype.CFFTable(face); err != nil {
			t.Errorf("%s: the embedded program has no CFF table: %v", tc.name, err)
		}
		if got := postScriptNameOf(t, face); got != tc.ps {
			t.Errorf("%s: PostScript name %q, want %q", tc.name, got, tc.ps)
		}
	}
}
