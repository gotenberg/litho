package text

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

func TestMatch_PreferenceOrder(t *testing.T) {
	t.Parallel()
	// a_outlines.ttf sorts first, so it is the first indexed face.
	f := newTestFonts(t, append(sansFamily(),
		repoFont(t, "a_outlines.ttf", outlinesA),
		repoFont(t, "b_dejavu.ttf", dejavuAB))...)
	for _, tc := range []struct {
		families []string
		want     string
	}{
		{[]string{"LithoOutlines", "DejaVu Sans"}, "LithoOutlines"},
		{[]string{"DejaVu Sans", "LithoOutlines"}, "DejaVu Sans"},
		{[]string{"No Such Family", "dejavusans"}, "DejaVu Sans"},
		{[]string{"sans-serif"}, "DejaVu Sans"},
		{[]string{"System-UI"}, "DejaVu Sans"},
		{nil, "DejaVu Sans"},
		{[]string{"No Such Family"}, "DejaVu Sans"},
	} {
		if got := f.Match(Style{Families: tc.families, Size: 10}).Family(); got != tc.want {
			t.Errorf("Match(%q) = %s, want %s", tc.families, got, tc.want)
		}
	}
}

func TestMatch_WeightAndStyle(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	for _, tc := range []struct {
		weight int
		italic bool
		want   string
	}{
		{0, false, "sans_regular"},
		{400, false, "sans_regular"},
		{700, false, "sans_bold"},
		{900, false, "sans_bold"},
		{300, false, "sans_regular"},
		{0, true, "sans_italic"},
		{-5, false, "sans_regular"},
	} {
		got := f.Match(Style{Families: []string{"Test Sans"}, Weight: tc.weight, Italic: tc.italic, Size: 10})
		if !strings.Contains(got.Key(), tc.want) {
			t.Errorf("Match(weight %d, italic %v) = %s, want %s", tc.weight, tc.italic, got.Key(), tc.want)
		}
	}
}

func TestMatch_EmptyIndex(t *testing.T) {
	t.Parallel()
	dir := fontDir(t,
		fontFile{name: "broken.ttf", data: []byte("not a font")},
		fontFile{name: "readme.txt", data: testSans("Regular", 400, false, 100)},
		fontFile{name: "type1.pfb", data: []byte{0x80, 1}},
	)
	f, err := NewFonts(context.Background(), nil, []string{dir, filepath.Join(dir, "missing")})
	if err != nil {
		t.Fatal(err)
	}
	if f.Len() != 0 || f.Match(Style{Size: 10}) != nil {
		t.Errorf("index of %d faces, Match %v", f.Len(), f.Match(Style{Size: 10}))
	}
}

func TestNewFonts_CanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewFonts(ctx, nil, []string{fontDir(t, sansFamily()...)}); !errors.Is(err, context.Canceled) {
		t.Errorf("NewFonts: %v", err)
	}
}

func TestMatch_FallsBackToTheFirstFace(t *testing.T) {
	t.Parallel()
	// No go-text substitution reaches these families, so an unknown family
	// takes the first indexed face.
	f := newTestFonts(t, fontFile{name: "a_digits.ttf", data: testDigits()}, sansFamily()[2])
	if got := f.Match(Style{Families: []string{"No Such Family"}, Size: 10}).Family(); got != "Test Digits" {
		t.Errorf("Match = %s, want Test Digits", got)
	}
}

func TestNewFonts_DirectoryOrderAndDuplicates(t *testing.T) {
	t.Parallel()
	a := fontDir(t, fontFile{name: "z.ttf", data: testDigits()})
	b := fontDir(t, fontFile{name: "a.ttf", data: testHebrew()})
	f, err := NewFonts(context.Background(), nil, []string{a, b, a})
	if err != nil {
		t.Fatal(err)
	}
	if f.Len() != 2 || f.Match(Style{Families: []string{"nothing"}, Size: 10}).Family() != "Test Digits" {
		t.Errorf("index of %d faces; the first directory's face must come first", f.Len())
	}
}

func TestNewFonts_Collection(t *testing.T) {
	t.Parallel()
	regular := fontbuild.TrueType{
		PostScriptName: "Coll", Family: "Coll", Subfamily: "Regular", NotdefAdvance: 500,
		Glyphs: []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 250, Empty: true}, {Name: "A", Unicode: 'A', Advance: 600}},
	}.Build()
	bold := withAspect(fontbuild.TrueType{
		PostScriptName: "Coll-Bold", Family: "Coll", Subfamily: "Bold", NotdefAdvance: 500,
		Glyphs: []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 250, Empty: true}, {Name: "A", Unicode: 'A', Advance: 700}},
	}.Build(), 700, false)
	f := newTestFonts(t, fontFile{name: "coll.ttc", data: fontbuild.Collection(regular, bold)})
	if f.Len() != 2 {
		t.Fatalf("index of %d faces, want 2", f.Len())
	}
	face := f.Match(Style{Families: []string{"Coll"}, Weight: 700, Size: 10})
	if !strings.HasSuffix(face.Key(), "coll.ttc#1") {
		t.Fatalf("bold face %s", face.Key())
	}
	cf, err := face.Compose()
	if err != nil {
		t.Fatal(err)
	}
	if truetype.IsCollection(cf.Program) || !truetype.IsSfnt(cf.Program) || cf.Key != face.Key() {
		t.Errorf("Compose returns a collection or a wrong key %s", cf.Key)
	}
	if again, _ := face.Compose(); again != cf {
		t.Error("Compose returns a new value on the second call")
	}
	widths, err := truetype.GlyphWidths(context.Background(), cf.Program)
	if err != nil || widths[2] != 700 {
		t.Errorf("extracted program widths %v, %v", widths, err)
	}
	lines, _ := layout(t, f, "AA", Style{Families: []string{"Coll"}, Weight: 700, Size: 10}, 100, Collapse)
	if !near(lines[0].Width, 14) || lines[0].Runs[0].Face != cf {
		t.Errorf("width %v, face %v", lines[0].Width, lines[0].Runs[0].Face.Key)
	}
}

func TestFace_ComposeSingleProgram(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(dejavuAB)
	if err != nil {
		t.Fatal(err)
	}
	f := newTestFonts(t, fontFile{name: "dejavu.ttf", data: data})
	cf, err := f.Match(Style{Size: 10}).Compose()
	if err != nil || !bytes.Equal(cf.Program, data) || !strings.HasSuffix(cf.Key, "dejavu.ttf#0") {
		t.Errorf("Compose = %s, %v", cf.Key, err)
	}
}

func TestFace_MetricsMatchHhea(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(dejavuAB)
	if err != nil {
		t.Fatal(err)
	}
	head, hhea := sfntTable(t, data, "head"), sfntTable(t, data, "hhea")
	upem := float64(binary.BigEndian.Uint16(head[18:]))
	ascender := float64(int16(binary.BigEndian.Uint16(hhea[4:])))
	descender := float64(int16(binary.BigEndian.Uint16(hhea[6:])))
	lineGap := float64(int16(binary.BigEndian.Uint16(hhea[8:])))
	f := newTestFonts(t, fontFile{name: "dejavu.ttf", data: data})
	m := f.Match(Style{Size: 10}).Metrics(20)
	if !near(m.Ascent, ascender*20/upem) || !near(m.Descent, -descender*20/upem) || !near(m.LineGap, lineGap*20/upem) {
		t.Errorf("metrics %+v, hhea %v %v %v over %v", m, ascender, descender, lineGap, upem)
	}
	if m.Ascent <= 0 || m.Descent <= 0 {
		t.Errorf("metrics %+v must be positive", m)
	}
	// OS/2 version 2 and later record sxHeight after the code page ranges;
	// without it the x-height is half the size.
	want := 10.0
	if os2 := sfntTable(t, data, "OS/2"); len(os2) >= 88 && binary.BigEndian.Uint16(os2) >= 2 {
		want = float64(int16(binary.BigEndian.Uint16(os2[86:]))) * 20 / upem
	}
	if !near(m.XHeight, want) {
		t.Errorf("x-height %v, want %v", m.XHeight, want)
	}
}

// sfntTable returns the table tag of the sfnt program data.
func sfntTable(t *testing.T, data []byte, tag string) []byte {
	t.Helper()
	n := int(binary.BigEndian.Uint16(data[4:]))
	for i := range n {
		rec := data[12+16*i:]
		if string(rec[:4]) == tag {
			off, length := binary.BigEndian.Uint32(rec[8:]), binary.BigEndian.Uint32(rec[12:])
			return data[off : off+length]
		}
	}
	t.Fatalf("no %s table", tag)
	return nil
}
