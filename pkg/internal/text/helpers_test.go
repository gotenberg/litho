package text

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// Repository fonts the tests reuse.
const (
	// dejavuAB is a DejaVu Sans subset covering U+0020, A and B, upem 2048.
	dejavuAB = "../render/page/testdata/dejavu_sans_ab.ttf"
	// outlinesA is the LithoOutlines test font covering U+0020 and A.
	outlinesA = "../typography/truetype/testdata/outlines.ttf"
)

// fontFile is one file a test font directory holds.
type fontFile struct {
	name string
	data []byte
}

// repoFont reads a font file of the repository.
func repoFont(t *testing.T, name, path string) fontFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return fontFile{name: name, data: data}
}

// fontDir writes files into a fresh directory and returns it.
func fontDir(t *testing.T, files ...fontFile) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name), f.data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// newTestFonts indexes files.
func newTestFonts(t *testing.T, files ...fontFile) *Fonts {
	t.Helper()
	f, err := NewFonts(context.Background(), nil, []string{fontDir(t, files...)})
	if err != nil {
		t.Fatalf("NewFonts: %v", err)
	}
	return f
}

// Advances of the built "Test Sans" faces, upem 1000.
const (
	spaceAdvance = 250
	aAdvance     = 500
	bAdvance     = 600
	cAdvance     = 700
)

// testSans builds a "Test Sans" face covering U+0020, A, B and C, with the
// OS/2 weight and italic bit given. Every advance is scaled by scale
// percent, so faces of the family tell apart by width.
func testSans(subfamily string, weight uint16, italic bool, scale int) []byte {
	adv := func(v int) uint16 { return uint16(v * scale / 100) } //nolint:gosec // test advances stay under 2000
	prog := fontbuild.TrueType{
		PostScriptName: "TestSans-" + strings.ReplaceAll(subfamily, " ", ""),
		Family:         "Test Sans",
		Subfamily:      subfamily,
		NotdefAdvance:  500,
		Glyphs: []fontbuild.Glyph{
			{Name: "space", Unicode: ' ', Advance: adv(spaceAdvance), Empty: true},
			{Name: "A", Unicode: 'A', Advance: adv(aAdvance)},
			{Name: "B", Unicode: 'B', Advance: adv(bAdvance)},
			{Name: "C", Unicode: 'C', Advance: adv(cAdvance)},
		},
	}.Build()
	return withAspect(prog, weight, italic)
}

// testHebrew builds a "Test Hebrew" face covering U+0020, alef, bet and
// gimel.
func testHebrew() []byte {
	return fontbuild.TrueType{
		PostScriptName: "TestHebrew",
		Family:         "Test Hebrew",
		Subfamily:      "Regular",
		NotdefAdvance:  500,
		Glyphs: []fontbuild.Glyph{
			{Name: "space", Unicode: ' ', Advance: spaceAdvance, Empty: true},
			{Name: "alef", Unicode: 'א', Advance: 400},
			{Name: "bet", Unicode: 'ב', Advance: 450},
			{Name: "gimel", Unicode: 'ג', Advance: 350},
		},
	}.Build()
}

// testDigits builds a "Test Digits" face covering U+0020 and D only, for
// fallback.
func testDigits() []byte {
	return fontbuild.TrueType{
		PostScriptName: "TestDigits",
		Family:         "Test Digits",
		Subfamily:      "Regular",
		NotdefAdvance:  500,
		Glyphs: []fontbuild.Glyph{
			{Name: "space", Unicode: ' ', Advance: spaceAdvance, Empty: true},
			{Name: "D", Unicode: 'D', Advance: 800},
		},
	}.Build()
}

// withAspect sets the OS/2 usWeightClass and fsSelection of prog.
func withAspect(prog []byte, weight uint16, italic bool) []byte {
	numTables := int(binary.BigEndian.Uint16(prog[4:]))
	for i := range numTables {
		rec := prog[12+16*i:]
		if string(rec[:4]) != "OS/2" {
			continue
		}
		off := binary.BigEndian.Uint32(rec[8:])
		binary.BigEndian.PutUint16(prog[off+4:], weight)
		sel := uint16(0x0040)
		if italic {
			sel = 0x0001
		}
		if weight >= 700 {
			sel = sel&^0x0040 | 0x0020
		}
		binary.BigEndian.PutUint16(prog[off+62:], sel)
	}
	return prog
}

// sansFamily is the regular, bold and italic Test Sans faces, the bold 20%
// and the italic 10% wider than the regular.
func sansFamily() []fontFile {
	return []fontFile{
		{name: "sans_bold.ttf", data: testSans("Bold", 700, false, 120)},
		{name: "sans_italic.ttf", data: testSans("Italic", 400, true, 110)},
		{name: "sans_regular.ttf", data: testSans("Regular", 400, false, 100)},
	}
}

// style returns a regular Test Sans style at 10 points.
func style() Style {
	return Style{Families: []string{"Test Sans"}, Size: 10, Color: compose.Black}
}

// lineText returns the text a line's glyphs stand for, in visual order.
func lineText(l Line) string {
	var b strings.Builder
	for _, r := range l.Runs {
		for _, g := range r.Glyphs {
			b.WriteString(g.Text)
		}
	}
	return b.String()
}

// layout lays text out in one run of s, failing the test on error.
func layout(t *testing.T, f *Fonts, text string, s Style, width float64, ws Whitespace) ([]Line, Report) {
	t.Helper()
	lines, rep, err := f.Layout(context.Background(), []Run{{Text: text, Style: s}}, width, Options{Whitespace: ws})
	if err != nil {
		t.Fatalf("Layout(%q): %v", text, err)
	}
	return lines, rep
}
