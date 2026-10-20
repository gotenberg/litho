package glyph

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// testDoc is a document with no file behind it, which reads the direct
// objects a test builds.
func testDoc() *document.Document {
	return &document.Document{Logger: document.NopLogger()}
}

// load loads the direct font dictionary fontDict.
func load(t *testing.T, fontDict ast.PdfDict, opts Options) *Font {
	t.Helper()
	f, err := Load(context.Background(), testDoc(), fontDict, opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return f
}

// glyphOf resolves the single code the bytes data hold.
func glyphOf(t *testing.T, f *Font, data ...byte) Glyph {
	t.Helper()
	codes := f.Codes(data)
	if len(codes) != 1 {
		t.Fatalf("Codes(% X) split %d codes, want 1", data, len(codes))
	}
	g, err := f.Glyph(context.Background(), codes[0])
	if err != nil {
		t.Fatalf("Glyph: %v", err)
	}
	return g
}

// width returns the width of an outline's bounds in glyph space, the
// distinguishing mark of the fontbuild glyphs, whose outlines are rectangles
// as wide as their advance less the side bearings.
func width(t *testing.T, g Glyph) float64 {
	t.Helper()
	r, ok := g.Outline.Bounds()
	if !ok {
		t.Fatalf("glyph has no outline (source %v, notdef %v)", g.Source, g.Notdef)
	}
	return r.Max.X - r.Min.X
}

// programAdvance returns the advance, in text space, of the glyph of the
// embedded program the code data selects: the fontbuild Type 1 and CFF
// glyphs share one outline, and their advances tell them apart.
func programAdvance(t *testing.T, f *Font, data ...byte) float64 {
	t.Helper()
	ref, ok := f.selectEmbedded(f.Codes(data)[0])
	if !ok {
		t.Fatalf("code % X selects no glyph", data)
	}
	_, _, adv, err := f.prog.draw(context.Background(), ref)
	if err != nil {
		t.Fatalf("draw: %v", err)
	}
	return adv
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// descriptor returns a font descriptor embedding program under key.
func descriptor(flags int, key ast.PdfName, program []byte, extra ast.PdfDict) ast.PdfDict {
	fd := ast.PdfDict{
		"Type":     ast.PdfName("FontDescriptor"),
		"FontName": ast.PdfName("TEST"),
		"Flags":    ast.PdfInt(flags),
		"FontBBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(-200), ast.PdfInt(1000), ast.PdfInt(800)},
	}
	if program != nil {
		fd[key] = ast.NewStream(extra, program)
	}
	return fd
}

// simpleFont returns a simple font dictionary of subtype over fd, whose
// /Widths give every code 0 to 255 the width w.
func simpleFont(subtype string, fd ast.PdfDict, encoding ast.PdfObject, w int) ast.PdfDict {
	widths := make(ast.PdfArray, 256)
	for i := range widths {
		widths[i] = ast.PdfInt(w)
	}
	d := ast.PdfDict{
		"Type":           ast.PdfName("Font"),
		"Subtype":        ast.PdfName(subtype),
		"BaseFont":       ast.PdfName("ABCDEF+Test"),
		"FirstChar":      ast.PdfInt(0),
		"LastChar":       ast.PdfInt(255),
		"Widths":         widths,
		"FontDescriptor": fd,
	}
	if encoding != nil {
		d["Encoding"] = encoding
	}
	return d
}

// glyphs are three fontbuild glyphs told apart by their advances, which set
// the width of their rectangle outlines.
var glyphs = []fontbuild.Glyph{
	{Name: "A", Unicode: 'A', Mac: 'A', Advance: 600},
	{Name: "B", Unicode: 'B', Mac: 'B', Advance: 700},
	{Name: "Aring", Unicode: 'Å', Mac: 0x81, Advance: 800},
}

func TestType1EmbeddedSelectsByEncodingName(t *testing.T) {
	t.Parallel()
	program, lengths := fontbuild.Type1{FontName: "TEST", Glyphs: glyphs, Encoding: map[byte]string{0x41: "A", 0x42: "B"}}.Build()
	fd := descriptor(32, "FontFile", program, ast.PdfDict{
		"Length1": ast.PdfInt(lengths[0]), "Length2": ast.PdfInt(lengths[1]), "Length3": ast.PdfInt(lengths[2]),
	})
	diffs := ast.PdfDict{"Differences": ast.PdfArray{ast.PdfInt(0x43), ast.PdfName("Aring")}}

	f := load(t, simpleFont("Type1", fd, diffs, 500), Options{})
	if f.Source() != SourceEmbedded {
		t.Fatalf("Source() = %v, want embedded", f.Source())
	}
	if a, b, c := programAdvance(t, f, 0x41), programAdvance(t, f, 0x42), programAdvance(t, f, 0x43); !near(a, 0.6) || !near(b, 0.7) || !near(c, 0.8) {
		t.Errorf("advances %v %v %v: the built-in encoding and /Differences did not select A, B and Aring", a, b, c)
	}
	a := glyphOf(t, f, 0x41)
	if a.Outline.Empty() {
		t.Error("glyph A has no outline")
	}
	if a.Advance != 0.5 {
		t.Errorf("Advance = %v, want the declared 500/1000", a.Advance)
	}
	if got := glyphOf(t, f, 0x44); !got.Notdef || got.Source != SourceEmbedded {
		t.Errorf("an unencoded code gave notdef %v source %v, want the embedded .notdef", got.Notdef, got.Source)
	}
}

func TestType1CSelectsByCharsetName(t *testing.T) {
	t.Parallel()
	program := fontbuild.CFF{FontName: "TEST", Glyphs: glyphs, Subrs: true}.Build()
	fd := descriptor(32, "FontFile3", program, ast.PdfDict{"Subtype": ast.PdfName("Type1C")})
	f := load(t, simpleFont("Type1", fd, ast.PdfName("WinAnsiEncoding"), 500), Options{})
	if a, ring := programAdvance(t, f, 0x41), programAdvance(t, f, 0xC5); !near(a, 0.6) || !near(ring, 0.8) {
		t.Errorf("advances %v %v: WinAnsi 0x41 and 0xC5 did not select A and Aring", a, ring)
	}
	if g := glyphOf(t, f, 0x41); g.Outline.Empty() {
		t.Error("glyph A has no outline")
	}
}

func TestTrueTypeNonsymbolicRoutes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		glyphs []fontbuild.Glyph
	}{
		{"(3,1) by the glyph name's character", []fontbuild.Glyph{
			{Name: "g1", Unicode: 'A', Advance: 600}, {Name: "g2", Unicode: 'Å', Advance: 800},
		}},
		{"(1,0) by the glyph name's Mac OS Roman code", []fontbuild.Glyph{
			{Name: "g1", Mac: 'A', Advance: 600}, {Name: "g2", Mac: 0x81, Advance: 800},
		}},
		{"post table name", []fontbuild.Glyph{
			{Name: "A", Advance: 600}, {Name: "Aring", Advance: 800},
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			program := fontbuild.TrueType{PostScriptName: "TEST", Family: "Test", Subfamily: "Regular", Glyphs: c.glyphs}.Build()
			f := load(t, simpleFont("TrueType", descriptor(32, "FontFile2", program, ast.PdfDict{}), ast.PdfName("WinAnsiEncoding"), 500), Options{})
			a, ring := glyphOf(t, f, 'A'), glyphOf(t, f, 0xC5)
			if a.Notdef || ring.Notdef || width(t, ring) <= width(t, a) {
				t.Errorf("A notdef %v, Aring notdef %v: WinAnsi 0x41 and 0xC5 did not select A and Aring", a.Notdef, ring.Notdef)
			}
		})
	}
}

func TestTrueTypeSymbolicReadsTheSymbolCmap(t *testing.T) {
	t.Parallel()
	program := fontbuild.TrueType{PostScriptName: "TEST", Family: "Test", Subfamily: "Regular", Glyphs: []fontbuild.Glyph{
		{Name: "g1", Symbol: 0xF041, Advance: 600},
		{Name: "g2", Symbol: 0xF042, Advance: 800},
	}}.Build()
	f := load(t, simpleFont("TrueType", descriptor(4, "FontFile2", program, ast.PdfDict{}), nil, 500), Options{})
	a, b := glyphOf(t, f, 0x41), glyphOf(t, f, 0x42)
	if a.Notdef || b.Notdef || width(t, b) <= width(t, a) {
		t.Errorf("codes 0x41 and 0x42 did not select the glyphs (3,0) maps at 0xF041 and 0xF042")
	}
	if got := glyphOf(t, f, 0x43); !got.Notdef {
		t.Error("a code the (3,0) cmap does not map selected a glyph")
	}
}

// cidTrueType returns a Type 0 font over a CIDFontType2 descendant embedding
// program, with cidToGID as its /CIDToGIDMap when non-nil.
func cidTrueType(program []byte, cidToGID ast.PdfObject) ast.PdfDict {
	cid := ast.PdfDict{
		"Type":           ast.PdfName("Font"),
		"Subtype":        ast.PdfName("CIDFontType2"),
		"BaseFont":       ast.PdfName("ABCDEF+Test"),
		"CIDSystemInfo":  ast.PdfDict{"Registry": ast.PdfString("Adobe"), "Ordering": ast.PdfString("Identity"), "Supplement": ast.PdfInt(0)},
		"DW":             ast.PdfInt(1000),
		"W":              ast.PdfArray{ast.PdfInt(1), ast.PdfArray{ast.PdfInt(250)}},
		"FontDescriptor": descriptor(4, "FontFile2", program, ast.PdfDict{}),
	}
	if cidToGID != nil {
		cid["CIDToGIDMap"] = cidToGID
	}
	return ast.PdfDict{
		"Type":            ast.PdfName("Font"),
		"Subtype":         ast.PdfName("Type0"),
		"BaseFont":        ast.PdfName("ABCDEF+Test"),
		"Encoding":        ast.PdfName("Identity-H"),
		"DescendantFonts": ast.PdfArray{cid},
	}
}

func TestCIDFontType2IdentityAndStreamMaps(t *testing.T) {
	t.Parallel()
	program := fontbuild.TrueType{PostScriptName: "TEST", Family: "Test", Subfamily: "Regular", Glyphs: glyphs}.Build()

	identity := load(t, cidTrueType(program, ast.PdfName("Identity")), Options{})
	if got := identity.Codes([]byte{0, 1, 0, 2, 0}); len(got) != 3 || got[0].Value != 1 || got[2].Value != 0 {
		t.Fatalf("Identity-H split %v, want two-byte codes and a trailing one-byte code", got)
	}
	g1, g3 := glyphOf(t, identity, 0, 1), glyphOf(t, identity, 0, 3)
	if width(t, g3) <= width(t, g1) {
		t.Error("Identity /CIDToGIDMap did not take CID 1 and 3 to glyphs 1 and 3")
	}
	if g1.Advance != 0.25 {
		t.Errorf("CID 1 Advance = %v, want the /W width 250/1000", g1.Advance)
	}

	// CID 1 -> GID 3, CID 2 -> GID 1, CID 3 -> GID 0.
	stream := ast.NewStream(ast.PdfDict{}, []byte{0, 0, 0, 3, 0, 1, 0, 0})
	mapped := load(t, cidTrueType(program, stream), Options{})
	if width(t, glyphOf(t, mapped, 0, 1)) != width(t, g3) {
		t.Error("the /CIDToGIDMap stream did not take CID 1 to glyph 3")
	}
	if got := glyphOf(t, mapped, 0, 3); !got.Notdef {
		t.Error("a CID the stream maps to glyph 0 was not .notdef")
	}
	if got := glyphOf(t, mapped, 0, 9); !got.Notdef {
		t.Error("a CID past the stream was not .notdef")
	}
}

func TestType3NamesItsGlyphDescription(t *testing.T) {
	t.Parallel()
	proc := ast.NewStream(ast.PdfDict{}, []byte("500 0 0 0 500 500 d1 0 0 500 500 re f"))
	resources := ast.PdfDict{"ProcSet": ast.PdfArray{ast.PdfName("PDF")}}
	font := ast.PdfDict{
		"Type":       ast.PdfName("Font"),
		"Subtype":    ast.PdfName("Type3"),
		"FontBBox":   ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(500), ast.PdfInt(500)},
		"FontMatrix": ast.PdfArray{ast.PdfReal(0.002), ast.PdfInt(0), ast.PdfInt(0), ast.PdfReal(0.002), ast.PdfInt(0), ast.PdfInt(0)},
		"CharProcs":  ast.PdfDict{"square": proc},
		"Encoding":   ast.PdfDict{"Differences": ast.PdfArray{ast.PdfInt(65), ast.PdfName("square")}},
		"FirstChar":  ast.PdfInt(65),
		"LastChar":   ast.PdfInt(65),
		"Widths":     ast.PdfArray{ast.PdfInt(500)},
		"Resources":  resources,
	}
	f := load(t, font, Options{})
	g := glyphOf(t, f, 'A')
	if g.Source != SourceType3 || g.CharProc == nil || g.Resources == nil {
		t.Fatalf("Type 3 glyph source %v, CharProc %v, Resources %v", g.Source, g.CharProc != nil, g.Resources != nil)
	}
	if g.Matrix != (geom.Matrix{0.002, 0, 0, 0.002, 0, 0}) {
		t.Errorf("Matrix = %v, want the /FontMatrix", g.Matrix)
	}
	if math.Abs(g.Advance-1) > 1e-9 {
		t.Errorf("Advance = %v, want 500 glyph units through /FontMatrix", g.Advance)
	}
	if got := glyphOf(t, f, 'B'); !got.Notdef || got.CharProc != nil {
		t.Error("a code with no glyph name named a description")
	}
}

// unembedded returns a simple Type 1 font the document does not embed.
func unembedded(baseFont string, flags int) ast.PdfDict {
	d := simpleFont("Type1", descriptor(flags, "", nil, nil), ast.PdfName("WinAnsiEncoding"), 500)
	d["BaseFont"] = ast.PdfName(baseFont)
	return d
}

func TestUnembeddedWithoutProviderDrawsPlaceholders(t *testing.T) {
	t.Parallel()
	f := load(t, unembedded("Melior", 34), Options{})
	if f.Source() != SourcePlaceholder {
		t.Fatalf("Source() = %v, want placeholder", f.Source())
	}
	g := glyphOf(t, f, 'A')
	got, ok := g.Outline.Transform(g.Matrix).Bounds()
	if g.Source != SourcePlaceholder || !ok {
		t.Fatalf("glyph source %v, outline %v", g.Source, ok)
	}
	if math.Abs(got.Max.X-0.5) > 1e-9 || math.Abs(got.Min.Y+0.2) > 1e-9 || math.Abs(got.Max.Y-0.8) > 1e-9 {
		t.Errorf("placeholder spans %v in text space, want the 500 width by the /FontBBox height", got)
	}
	if sp := glyphOf(t, f, ' '); !sp.Outline.Empty() {
		t.Error("a space drew a placeholder box")
	}
}

func TestSubstituteRequestsAndScaling(t *testing.T) {
	t.Parallel()
	program := fontbuild.TrueType{PostScriptName: "Sub", Family: "Sub", Subfamily: "Regular", Glyphs: glyphs}.Build()
	var asked []string
	provider := func(_ context.Context, req Request) ([]Candidate, error) {
		asked = append(asked, req.BaseFont)
		if req.BaseFont != "Times-Bold" {
			return nil, nil
		}
		return []Candidate{
			func(context.Context) ([]byte, error) { return []byte("not a font"), nil },
			func(context.Context) ([]byte, error) { return program, nil },
		}, nil
	}
	// Serif and ForceBold flags on a name no metric table knows.
	f := load(t, unembedded("ABCDEF+Melior", 2|32|1<<18), Options{Substitutes: provider})
	if f.Source() != SourceSubstitute {
		t.Fatalf("Source() = %v after asking %v, want substitute", f.Source(), asked)
	}
	if len(asked) != 2 || asked[0] != "ABCDEF+Melior" || asked[1] != "Times-Bold" {
		t.Errorf("asked for %v, want the font's own name then Times-Bold", asked)
	}
	g := glyphOf(t, f, 'A')
	if g.Source != SourceSubstitute {
		t.Fatalf("glyph source %v, want substitute", g.Source)
	}
	// The substitute's A advances 600 units of a 1000-unit em; the font
	// declares 500, so the glyph is drawn at 5/6 its width.
	if got := g.Matrix.ApplyVector(geom.Point{X: 600}).X; math.Abs(got-0.5) > 1e-9 {
		t.Errorf("substitute advance maps to %v in text space, want the declared 0.5", got)
	}
	if got := glyphOf(t, f, 'Z'); got.Source != SourcePlaceholder {
		t.Errorf("a glyph the substitute lacks drew from %v, want a placeholder", got.Source)
	}
}

func TestSubstituteRequestOrder(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		flags int
		want  []string
	}{
		{"Arial", 32, []string{"Arial", "Helvetica"}},
		{"Helvetica", 32, []string{"Helvetica"}},
		{"ArialNarrow-Bold", 32, []string{"ArialNarrow-Bold", "Helvetica-Narrow-Bold", "Helvetica-Bold"}},
		{"Melior-Italic", 2 | 32, []string{"Melior-Italic", "Times-Italic"}},
		{"Mono", 1 | 32, []string{"Mono", "Courier"}},
		{"WPIconicSymbolsA", 4, []string{"WPIconicSymbolsA"}},
	} {
		var got []string
		for _, r := range substituteRequests(Request{BaseFont: c.name, Flags: c.flags}) {
			got = append(got, r.BaseFont)
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: requests %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: requests %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestProviderErrorAborts(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	provider := func(context.Context, Request) ([]Candidate, error) { return nil, boom }
	_, err := Load(context.Background(), testDoc(), unembedded("Melior", 32), Options{Substitutes: provider})
	if !errors.Is(err, boom) {
		t.Errorf("Load error %v, want the provider's", err)
	}
}

func TestBrokenEmbeddedProgramFallsBack(t *testing.T) {
	t.Parallel()
	f := load(t, simpleFont("TrueType", descriptor(32, "FontFile2", []byte("\x00\x01\x00\x00garbage"), ast.PdfDict{}), nil, 500), Options{})
	if f.Source() != SourcePlaceholder {
		t.Errorf("a program that does not parse drew from %v, want placeholders", f.Source())
	}
}

func TestGlyphHonoursContext(t *testing.T) {
	t.Parallel()
	f := load(t, unembedded("Helvetica", 32), Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Glyph(ctx, Code{Bytes: []byte{'A'}, Value: 'A'}); !errors.Is(err, context.Canceled) {
		t.Errorf("Glyph on a cancelled context: %v", err)
	}
}

func TestParseCIDToGIDMap(t *testing.T) {
	t.Parallel()
	if got := parseCIDToGIDMap([]byte{0, 5, 1, 2, 9}); len(got) != 2 || got[0] != 5 || got[1] != 0x102 {
		t.Errorf("parseCIDToGIDMap = %v, want [5 258]", got)
	}
	if got := parseCIDToGIDMap(make([]byte, 2*maxCIDs+10)); len(got) != maxCIDs {
		t.Errorf("an oversized stream gave %d entries, want %d", len(got), maxCIDs)
	}
}
