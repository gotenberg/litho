package text

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// ahemPath is the public-domain Ahem font of the layout tests.
const ahemPath = "../layout/testdata/Ahem.ttf"

// docFace loads program as a document face of family with d's other
// descriptors.
func docFace(t *testing.T, program []byte, family string, d FaceDescriptor) *Face {
	t.Helper()
	d.Family = family
	fc, err := LoadFace(program, d)
	if err != nil {
		t.Fatalf("LoadFace: %v", err)
	}
	return fc
}

// runFaces returns, for each glyph of the laid-out lines, the key of the
// face that draws it, by the glyph's text.
func runFaces(lines []Line) map[string]string {
	out := map[string]string{}
	for _, l := range lines {
		for _, r := range l.Runs {
			for _, g := range r.Glyphs {
				if g.Text != "" && g.Text != " " {
					out[g.Text] = r.Face.Key
				}
			}
		}
	}
	return out
}

func TestWithFaces_DocumentFamilyShadowsIndex(t *testing.T) {
	t.Parallel()
	base := newTestFonts(t, fontFile{name: "sans_regular.ttf", data: testSans("Regular", 400, false, 100)})
	doc := docFace(t, testSans("Regular", 400, false, 150), "Test Sans", FaceDescriptor{})
	f := base.WithFaces(NewFaceSet([]*Face{doc}))
	if got := f.Match(style()); got != doc {
		t.Fatalf("Match = %v, want the document face", got.Key())
	}
	lines, _ := layout(t, f, "AB", style(), 1000, Collapse)
	if w := lines[0].Width; math.Abs(w-1.5*(aAdvance+bAdvance)/100) > 1e-9 {
		t.Errorf("width %v: not the document face's advances", w)
	}
	for ch, key := range runFaces(lines) {
		if !strings.HasPrefix(key, "font-face:") {
			t.Errorf("%s drawn with %s", ch, key)
		}
	}
	// The shared index is untouched.
	if base.Len() != 1 || strings.HasPrefix(base.Match(style()).Key(), "font-face:") {
		t.Error("the document face reached the shared index")
	}
	lines, _ = layout(t, base, "AB", style(), 1000, Collapse)
	if w := lines[0].Width; math.Abs(w-float64(aAdvance+bAdvance)/100) > 1e-9 {
		t.Errorf("base width %v", w)
	}
}

func TestWithFaces_UnicodeRangeFallsThrough(t *testing.T) {
	t.Parallel()
	base := newTestFonts(t, sansFamily()...)
	// Doc covers A and C by its range, but its font has no C.
	only := fontbuild.TrueType{
		PostScriptName: "DocA", Family: "Doc A", Subfamily: "Regular", NotdefAdvance: 500,
		Glyphs: []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 300, Empty: true}, {Name: "A", Unicode: 'A', Advance: 900}},
	}.Build()
	doc := docFace(t, only, "Doc", FaceDescriptor{UnicodeRange: [][2]rune{{'A', 'A'}, {'C', 'C'}}})
	f := base.WithFaces(NewFaceSet([]*Face{doc}))
	s := style()
	s.Families = []string{"Doc", "Test Sans"}
	lines, _ := layout(t, f, "ABC", s, 1000, Collapse)
	faces := runFaces(lines)
	if !strings.HasPrefix(faces["A"], "font-face:") {
		t.Errorf("A drawn with %s, want the document face", faces["A"])
	}
	for _, ch := range []string{"B", "C"} {
		if !strings.Contains(faces[ch], "sans_regular.ttf") {
			t.Errorf("%s drawn with %s, want Test Sans", ch, faces[ch])
		}
	}
	// The range leaves U+0020 out: the first available font is Test Sans.
	if got := f.Match(s); !strings.Contains(got.Key(), "sans_regular.ttf") {
		t.Errorf("Match = %s, want Test Sans", got.Key())
	}
}

func TestWithFaces_CompositeLastRuleFirst(t *testing.T) {
	t.Parallel()
	base := newTestFonts(t, sansFamily()...)
	first := docFace(t, testSans("Regular", 400, false, 150), "Doc", FaceDescriptor{})
	last := docFace(t, testSans("Regular", 400, false, 200), "Doc", FaceDescriptor{UnicodeRange: [][2]rune{{'B', 'B'}}})
	f := base.WithFaces(NewFaceSet([]*Face{first, last}))
	s := style()
	s.Families = []string{"Doc"}
	lines, _ := layout(t, f, "AB", s, 1000, Collapse)
	faces := runFaces(lines)
	if faces["A"] != first.Key() || faces["B"] != last.Key() {
		t.Errorf("A from %s, B from %s", faces["A"], faces["B"])
	}
}

func TestWithFaces_DeclaredFamilyWithoutFaces(t *testing.T) {
	t.Parallel()
	base := newTestFonts(t, append(sansFamily(), fontFile{name: "digits.ttf", data: testDigits()})...)
	f := base.WithFaces(NewFaceSet(nil).Declare("Test Sans"))
	s := style()
	s.Families = []string{"Test Sans", "Test Digits"}
	if got := f.Match(s); !strings.Contains(got.Key(), "digits.ttf") {
		t.Errorf("Match = %s, want Test Digits: a declared family shadows the installed one", got.Key())
	}
}

func TestWithFaces_EmptySetReturnsIndex(t *testing.T) {
	t.Parallel()
	base := newTestFonts(t, sansFamily()...)
	if base.WithFaces(nil) != base || base.WithFaces(NewFaceSet(nil)) != base {
		t.Error("an empty set made an overlay")
	}
}

func TestSelectFaces_Descriptors(t *testing.T) {
	t.Parallel()
	mk := func(name string, d FaceDescriptor) *Face {
		nd := d.normalized()
		nd.Family = name
		return &Face{family: name, doc: &nd}
	}
	faces := []*Face{
		mk("w300", FaceDescriptor{WeightMin: 300, WeightMax: 300}),
		mk("w400", FaceDescriptor{}),
		mk("w600-800", FaceDescriptor{WeightMin: 600, WeightMax: 800}),
		mk("italic", FaceDescriptor{Style: FaceItalic}),
		mk("oblique20", FaceDescriptor{Style: FaceOblique, ObliqueMin: 20, ObliqueMax: 20}),
		mk("condensed", FaceDescriptor{StretchMin: 75, StretchMax: 75, Style: FaceItalic}),
	}
	tests := []struct {
		weight int
		slant  uint8
		faces  []*Face
		want   string
	}{
		{400, slantNormal, faces, "w400"},
		{500, slantNormal, faces, "w400"},
		{700, slantNormal, faces, "w600-800"},
		{900, slantNormal, faces, "w600-800"},
		{200, slantNormal, faces, "w300"},
		{450, slantNormal, faces[:3], "w400"},
		{350, slantNormal, faces[1:3], "w400"},
		{400, slantItalic, faces, "italic"},
		{400, slantItalic, faces[:3], "w400"},
		{400, slantItalic, append(append([]*Face{}, faces[:3]...), faces[4]), "oblique20"},
		{400, slantNormal, []*Face{faces[3], faces[4]}, "oblique20"},
		{400, slantItalic, []*Face{faces[5], faces[1]}, "w400"},
	}
	for _, tt := range tests {
		got := selectFaces(tt.faces, request{weight: tt.weight, slant: tt.slant})
		if len(got) == 0 || got[0].family != tt.want {
			names := make([]string, len(got))
			for i, fc := range got {
				names[i] = fc.family
			}
			t.Errorf("weight %d slant %d: %v, want %s", tt.weight, tt.slant, names, tt.want)
		}
	}
}

func TestFaceDescriptor_Synthesis(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		d      FaceDescriptor
		req    request
		stroke bool
		skew   bool
	}{
		{"regular", FaceDescriptor{}, request{weight: 400}, false, false},
		{"semibold on 400", FaceDescriptor{}, request{weight: 600}, true, false},
		{"500 on 400", FaceDescriptor{}, request{weight: 500}, false, false},
		{"bold on 500", FaceDescriptor{WeightMin: 500, WeightMax: 500}, request{weight: 700}, true, false},
		{"bold on 400-700", FaceDescriptor{WeightMin: 400, WeightMax: 700}, request{weight: 700}, false, false},
		{"italic on normal", FaceDescriptor{}, request{weight: 400, slant: slantItalic}, false, true},
		{"italic on italic", FaceDescriptor{Style: FaceItalic}, request{weight: 400, slant: slantItalic}, false, false},
		// Chromium slants an oblique 10deg face again for italic, not an
		// oblique 20deg one.
		{"italic on oblique 10", FaceDescriptor{Style: FaceOblique, ObliqueMin: 10, ObliqueMax: 10}, request{weight: 400, slant: slantItalic}, false, true},
		{"italic on oblique 20", FaceDescriptor{Style: FaceOblique, ObliqueMin: 20, ObliqueMax: 20}, request{weight: 400, slant: slantItalic}, false, false},
		{"italic on oblique 0", FaceDescriptor{Style: FaceOblique}, request{weight: 400, slant: slantItalic}, false, true},
	}
	for _, tt := range tests {
		d := tt.d.normalized()
		stroke, skew := d.synthesis(tt.req, 16)
		if (stroke > 0) != tt.stroke || (skew > 0) != tt.skew {
			t.Errorf("%s: stroke %v skew %v", tt.name, stroke, skew)
		}
	}
}

func TestWithFaces_SyntheticBoldOnRun(t *testing.T) {
	t.Parallel()
	base := newTestFonts(t, sansFamily()...)
	doc := docFace(t, testSans("Regular", 400, false, 100), "Doc", FaceDescriptor{})
	f := base.WithFaces(NewFaceSet([]*Face{doc}))
	s := style()
	s.Families, s.Weight, s.Size = []string{"Doc"}, 600, 9
	lines, _ := layout(t, f, "A", s, 1000, Collapse)
	if got := lines[0].Runs[0].Stroke; math.Abs(got-9.0/24) > 1e-9 {
		t.Errorf("stroke %v, want %v", got, 9.0/24)
	}
}

func TestLocalFace_ByName(t *testing.T) {
	t.Parallel()
	base := newTestFonts(t, append(sansFamily(), repoFont(t, "ahem.ttf", ahemPath))...)
	for _, name := range []string{"TestSans-Bold", "testsans-bold", "Ahem"} {
		fc, err := base.LocalFace(name, FaceDescriptor{Family: "Local"})
		if err != nil {
			t.Fatalf("LocalFace(%q): %v", name, err)
		}
		if fc.doc == nil || fc.pinned == nil || strings.HasPrefix(fc.Key(), "font-face:") {
			t.Errorf("LocalFace(%q) = %s", name, fc.Key())
		}
	}
	if _, err := base.LocalFace("Test Sans", FaceDescriptor{}); !errors.Is(err, ErrNoLocalFace) {
		t.Errorf("a family name answered local(): %v", err)
	}
	if _, err := base.LocalFace(" ", FaceDescriptor{}); !errors.Is(err, ErrNoLocalFace) {
		t.Errorf("an empty name answered local(): %v", err)
	}
	if base.Len() != 4 {
		t.Errorf("index of %d faces", base.Len())
	}
}

func TestLoadFace_Programs(t *testing.T) {
	t.Parallel()
	sans := testSans("Regular", 400, false, 100)
	a, err := LoadFace(sans, FaceDescriptor{Family: "A"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadFace(sans, FaceDescriptor{Family: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Key() != b.Key() || a == b {
		t.Errorf("same bytes, keys %s and %s", a.Key(), b.Key())
	}
	coll, err := LoadFace(fontbuild.Collection(testDigits(), sans), FaceDescriptor{Family: "C"})
	if err != nil {
		t.Fatalf("collection: %v", err)
	}
	if coll.Family() != "Test Digits" {
		t.Errorf("collection yields %s, want its first face", coll.Family())
	}
	cf, err := coll.Compose()
	if err != nil || len(cf.Program) == 0 {
		t.Errorf("Compose: %v", err)
	}
	for _, bad := range [][]byte{nil, []byte("garbage"), sans[:len(sans)/3]} {
		if _, err := LoadFace(bad, FaceDescriptor{Family: "X"}); err == nil {
			t.Errorf("LoadFace(%d bytes) succeeded", len(bad))
		}
	}
}

func TestWithFaces_ConcurrentLayouts(t *testing.T) {
	t.Parallel()
	base := newTestFonts(t, sansFamily()...)
	sans := testSans("Regular", 400, false, 130)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			doc, err := LoadFace(sans, FaceDescriptor{Family: "Doc"})
			if err != nil {
				t.Error(err)
				return
			}
			f := base.WithFaces(NewFaceSet([]*Face{doc}))
			s := style()
			s.Families = []string{"Doc", "Test Sans"}
			if i%2 == 0 {
				f = base
			}
			if _, _, err := f.Layout(context.Background(), []Run{{Text: "ABC AB", Style: s}}, 100, Options{}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}
