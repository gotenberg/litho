package fontsubset

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/internal/typography/type1"
)

// testGlyphs are the glyphs of the test programs after .notdef: space at
// glyph 1, then A to L at glyphs 2 to 13, Aacute (14) drawing A and acute,
// and acute (15).
func testGlyphs() []fontbuild.Glyph {
	glyphs := []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 500, Empty: true}}
	for c := 'A'; c <= 'L'; c++ {
		glyphs = append(glyphs, fontbuild.Glyph{Name: string(c), Unicode: c, Advance: 600})
	}
	glyphs = append(glyphs,
		fontbuild.Glyph{Name: "Aacute", Unicode: 0xC1, Advance: 600, Components: []uint16{2, 15}},
		fontbuild.Glyph{Name: "acute", Unicode: 0xB4, Advance: 300},
	)
	return glyphs
}

func ttfProgram() []byte {
	return fontbuild.TrueType{PostScriptName: "TestSans", Family: "Test Sans", Subfamily: "Regular", UnitsPerEm: 1000, Glyphs: testGlyphs()}.Build()
}

func cffProgram() []byte {
	var glyphs []fontbuild.Glyph
	for c := 'A'; c <= 'L'; c++ {
		glyphs = append(glyphs, fontbuild.Glyph{Name: string(c), Advance: 600})
	}
	return fontbuild.CFF{FontName: "TestSerif", Glyphs: glyphs, Subrs: true}.Build()
}

// testDoc builds an object graph in an allocator.
type testDoc struct {
	t     *testing.T
	alloc *writer.ObjAllocator
}

func newTestDoc(t *testing.T) *testDoc {
	t.Helper()
	return &testDoc{t: t, alloc: writer.NewObjAllocator(0)}
}

func (d *testDoc) add(obj ast.PdfObject) ast.PdfIndirectRef {
	return d.alloc.Alloc(obj)
}

// trueType adds a simple TrueType font over a fresh program and returns the
// font and the program.
func (d *testDoc) trueType() (font, prog ast.PdfIndirectRef) {
	src := ttfProgram()
	prog = d.add(ast.NewStream(ast.PdfDict{"Length1": ast.PdfInt(len(src))}, src))
	fd := d.add(ast.PdfDict{"Type": ast.PdfName("FontDescriptor"), "FontName": ast.PdfName("TestSans"), "FontFile2": prog})
	font = d.add(ast.PdfDict{
		"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("TrueType"), "BaseFont": ast.PdfName("TestSans"),
		"FontDescriptor": fd, "Encoding": ast.PdfName("WinAnsiEncoding"),
	})
	return font, prog
}

// type0 adds a Type 0 font with Identity-H over a CIDFontType2 drawing from a
// fresh TrueType program, and returns the font and the program.
func (d *testDoc) type0() (font, prog ast.PdfIndirectRef) {
	src := ttfProgram()
	prog = d.add(ast.NewStream(ast.PdfDict{"Length1": ast.PdfInt(len(src))}, src))
	fd := d.add(ast.PdfDict{"Type": ast.PdfName("FontDescriptor"), "FontName": ast.PdfName("TestSans"), "FontFile2": prog})
	cid := d.add(ast.PdfDict{
		"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("CIDFontType2"), "BaseFont": ast.PdfName("TestSans"),
		"FontDescriptor": fd, "CIDToGIDMap": ast.PdfName("Identity"),
	})
	font = d.add(ast.PdfDict{
		"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type0"), "BaseFont": ast.PdfName("TestSans"),
		"Encoding": ast.PdfName("Identity-H"), "DescendantFonts": ast.PdfArray{cid},
	})
	return font, prog
}

// type1C adds a simple Type 1 font over a fresh CFF program.
func (d *testDoc) type1C() (font, prog ast.PdfIndirectRef) {
	prog = d.add(ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("Type1C")}, cffProgram()))
	fd := d.add(ast.PdfDict{"Type": ast.PdfName("FontDescriptor"), "FontName": ast.PdfName("TestSerif"), "FontFile3": prog})
	font = d.add(ast.PdfDict{
		"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1"), "BaseFont": ast.PdfName("TestSerif"),
		"FontDescriptor": fd,
	})
	return font, prog
}

// finish adds a page tree of one page per content string, each drawing with
// res, and a catalog holding extra, and returns the trailer.
func (d *testDoc) finish(res ast.PdfObject, extra ast.PdfDict, contents ...string) ast.PdfDict {
	pagesRef := d.add(ast.PdfNull{})
	var kids ast.PdfArray
	for _, c := range contents {
		stream := d.add(ast.NewStream(ast.PdfDict{}, []byte(c)))
		kids = append(kids, d.add(ast.PdfDict{
			"Type": ast.PdfName("Page"), "Parent": pagesRef, "Resources": res, "Contents": stream,
			"MediaBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
		}))
	}
	d.alloc.AllocAt(pagesRef.ObjectNum, ast.PdfDict{"Type": ast.PdfName("Pages"), "Kids": kids, "Count": ast.PdfInt(len(kids))})
	catalog := ast.PdfDict{"Type": ast.PdfName("Catalog"), "Pages": pagesRef}
	for k, v := range extra {
		catalog[k] = v
	}
	return ast.PdfDict{"Root": d.add(catalog)}
}

// run runs the pass and returns its statistics.
func (d *testDoc) run(trailer ast.PdfDict) Stats {
	d.t.Helper()
	stats, err := Run(context.Background(), d.alloc, trailer, Config{})
	if err != nil {
		d.t.Fatalf("Run: %v", err)
	}
	return stats
}

// program returns the decoded program at ref.
func (d *testDoc) program(ref ast.PdfIndirectRef) []byte {
	d.t.Helper()
	s, ok := d.alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		d.t.Fatalf("object %d is no stream", ref.ObjectNum)
	}
	data, err := s.Bytes(context.Background())
	if err != nil {
		d.t.Fatalf("reading the program: %v", err)
	}
	if s.Dict["Filter"] == ast.PdfName("FlateDecode") {
		zr, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			d.t.Fatalf("inflating the program: %v", err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(zr); err != nil {
			d.t.Fatalf("inflating the program: %v", err)
		}
		data = buf.Bytes()
		// A Type 1 program's /Length1 is its clear-text portion alone.
		if l1, ok := s.Dict["Length1"].(ast.PdfInt); ok && s.Dict["Length2"] == nil && int(l1) != len(data) {
			d.t.Errorf("/Length1 %d, program of %d bytes", l1, len(data))
		}
	}
	return data
}

// wantTrueType asserts that the program at ref is the TrueType test program
// subset in place to keep.
func (d *testDoc) wantTrueType(ref ast.PdfIndirectRef, keep ...uint16) {
	d.t.Helper()
	want, err := truetype.SubsetInPlace(context.Background(), ttfProgram(), keep)
	if err != nil {
		d.t.Fatalf("SubsetInPlace: %v", err)
	}
	if !bytes.Equal(d.program(ref), want) {
		d.t.Errorf("program %d is not the subset keeping %v", ref.ObjectNum, keep)
	}
}

// wantWhole asserts that the program at ref is the whole source.
func (d *testDoc) wantWhole(ref ast.PdfIndirectRef, src []byte) {
	d.t.Helper()
	if !bytes.Equal(d.program(ref), src) {
		d.t.Errorf("program %d changed", ref.ObjectNum)
	}
}

func TestRun_SimpleTrueType(t *testing.T) {
	t.Parallel()
	d := newTestDoc(t)
	font, prog := d.trueType()
	res := ast.PdfDict{"Font": ast.PdfDict{"F1": font}}
	trailer := d.finish(res, nil, "BT /F1 12 Tf (AB) Tj ET", "BT /F1 12 Tf [(C) -20 (\\301)] TJ ET")
	if stats := d.run(trailer); stats.Programs != 1 || stats.SavedBytes <= 0 {
		t.Errorf("stats %+v, want one program subset", stats)
	}
	// A, B, C, Aacute (0xC1 in WinAnsi) and the A and acute it draws, with
	// the codes as glyph IDs: 0x41 to 0x43 and 0xC1 are past the 16 glyphs.
	d.wantTrueType(prog, 2, 3, 4, 14)
}

func TestRun_Type0IdentityH(t *testing.T) {
	t.Parallel()
	d := newTestDoc(t)
	font, prog := d.type0()
	res := ast.PdfDict{"Font": ast.PdfDict{"F1": font}}
	trailer := d.finish(res, nil, "BT /F1 12 Tf <00050007> Tj ET")
	if stats := d.run(trailer); stats.Programs != 1 {
		t.Errorf("stats %+v", stats)
	}
	d.wantTrueType(prog, 5, 7)
}

func TestRun_Type1C(t *testing.T) {
	t.Parallel()
	d := newTestDoc(t)
	font, prog := d.type1C()
	res := ast.PdfDict{"Font": ast.PdfDict{"F1": font}}
	trailer := d.finish(res, nil, "BT /F1 12 Tf (BD) Tj ET")
	if stats := d.run(trailer); stats.Programs != 1 {
		t.Errorf("stats %+v", stats)
	}
	got := d.program(prog)
	names, err := cff.GlyphNames(context.Background(), got)
	if err != nil || names[2] != "B" || names[4] != "D" {
		t.Fatalf("glyph names %v, %v", names, err)
	}
	// The Standard encoding gives B and D, and the codes 0x42 and 0x44 name
	// no glyph of the 13, so B (2) and D (4) alone keep their outlines.
	want, err := cff.SubsetInPlace(context.Background(), cffProgram(), []uint16{2, 4})
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("program is not the subset keeping B and D: %v", err)
	}
}

// type1Program returns a Type 1 program of .notdef, A to L, Aacute (a seac
// of A and acute) and acute, with its three lengths.
func type1Program() ([]byte, [3]int) {
	var glyphs []fontbuild.Glyph
	for c := 'A'; c <= 'L'; c++ {
		glyphs = append(glyphs, fontbuild.Glyph{Name: string(c), Advance: 600})
	}
	glyphs = append(glyphs,
		fontbuild.Glyph{Name: "Aacute", Advance: 600, Seac: &[2]byte{0x41, 0xC2}},
		fontbuild.Glyph{Name: "acute", Advance: 333},
	)
	return fontbuild.Type1{FontName: "TestType1", Glyphs: glyphs, Subrs: true}.Build()
}

func TestRun_Type1(t *testing.T) {
	t.Parallel()
	d := newTestDoc(t)
	src, lens := type1Program()
	prog := d.add(ast.NewStream(ast.PdfDict{
		"Length1": ast.PdfInt(lens[0]), "Length2": ast.PdfInt(lens[1]), "Length3": ast.PdfInt(lens[2]),
	}, src))
	fd := d.add(ast.PdfDict{"Type": ast.PdfName("FontDescriptor"), "FontName": ast.PdfName("TestType1"), "FontFile": prog})
	font := d.add(ast.PdfDict{
		"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1"), "BaseFont": ast.PdfName("TestType1"),
		"FontDescriptor": fd,
		"Encoding":       ast.PdfDict{"Differences": ast.PdfArray{ast.PdfInt(200), ast.PdfName("Aacute")}},
	})
	res := ast.PdfDict{"Font": ast.PdfDict{"F1": font}}
	trailer := d.finish(res, nil, "BT /F1 12 Tf (B\\310) Tj ET")
	if stats := d.run(trailer); stats.Programs != 1 || stats.SavedBytes <= 0 {
		t.Fatalf("stats %+v, want one program subset", stats)
	}
	// B through the Standard encoding, Aacute through /Differences, and the
	// A and acute its seac draws.
	want, wantLens, err := type1.SubsetInPlace(context.Background(), src, []string{".notdef", "B", "Aacute"})
	if err != nil {
		t.Fatalf("SubsetInPlace: %v", err)
	}
	if !bytes.Equal(d.program(prog), want) {
		t.Error("program is not the subset keeping B and Aacute")
	}
	s, _ := d.alloc.Pending()[prog.ObjectNum].(ast.PdfStream)
	for key, n := range map[ast.PdfName]int{"Length1": wantLens.Clear, "Length2": wantLens.Encrypted, "Length3": wantLens.Trailer} {
		if s.Dict[key] != ast.PdfInt(n) {
			t.Errorf("/%s %v, want %d", key, s.Dict[key], n)
		}
	}
}

func TestRun_FormsPatternsAndAnnotations(t *testing.T) {
	t.Parallel()
	d := newTestDoc(t)
	font, prog := d.trueType()
	fonts := ast.PdfDict{"F1": font}
	form := d.add(ast.NewStream(ast.PdfDict{
		"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Form"),
		"Resources": ast.PdfDict{"Font": fonts},
	}, []byte("BT /F1 1 Tf (D) Tj ET")))
	pattern := d.add(ast.NewStream(ast.PdfDict{"PatternType": ast.PdfInt(1)}, []byte("BT /F1 1 Tf (E) Tj ET")))
	appearance := d.add(ast.NewStream(ast.PdfDict{
		"Subtype": ast.PdfName("Form"), "Resources": ast.PdfDict{"Font": fonts},
	}, []byte("BT /F1 1 Tf (F) Tj ET")))
	res := ast.PdfDict{
		"Font":    fonts,
		"XObject": ast.PdfDict{"X1": form},
		"Pattern": ast.PdfDict{"P1": pattern},
	}
	trailer := d.finish(res, nil, "BT /F1 12 Tf (A) Tj ET")
	pages := d.alloc.Pending()[trailer["Root"].(ast.PdfIndirectRef).ObjectNum].(ast.PdfDict)["Pages"].(ast.PdfIndirectRef)
	page := d.alloc.Pending()[pages.ObjectNum].(ast.PdfDict)["Kids"].(ast.PdfArray)[0].(ast.PdfIndirectRef)
	pageDict := d.alloc.Pending()[page.ObjectNum].(ast.PdfDict)
	pageDict["Annots"] = ast.PdfArray{ast.PdfDict{"Subtype": ast.PdfName("Square"), "AP": ast.PdfDict{"N": appearance}}}
	if stats := d.run(trailer); stats.Programs != 1 {
		t.Errorf("stats %+v", stats)
	}
	d.wantTrueType(prog, 2, 5, 6, 7)
}

func TestRun_LeavesWhole(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		build func(d *testDoc, font ast.PdfIndirectRef) ast.PdfDict
	}{
		{"a form's default resources", func(d *testDoc, font ast.PdfIndirectRef) ast.PdfDict {
			return d.finish(ast.PdfDict{"Font": ast.PdfDict{"F1": font}},
				ast.PdfDict{"AcroForm": ast.PdfDict{"DR": ast.PdfDict{"Font": ast.PdfDict{"F1": font}}}},
				"BT /F1 12 Tf (A) Tj ET")
		}},
		{"a default appearance naming the font", func(d *testDoc, font ast.PdfIndirectRef) ast.PdfDict {
			return d.finish(ast.PdfDict{"Font": ast.PdfDict{"Helv": font}},
				ast.PdfDict{"AcroForm": ast.PdfDict{"DA": ast.PdfString("/Helv 0 Tf 0 g")}},
				"BT /Helv 12 Tf (A) Tj ET")
		}},
		{"a holder outside the resources", func(d *testDoc, font ast.PdfIndirectRef) ast.PdfDict {
			return d.finish(ast.PdfDict{"Font": ast.PdfDict{"F1": font}}, ast.PdfDict{"Custom": font}, "BT /F1 12 Tf (A) Tj ET")
		}},
		{"a stream that does not decode", func(d *testDoc, font ast.PdfIndirectRef) ast.PdfDict {
			bad := d.add(ast.NewStream(ast.PdfDict{
				"Subtype": ast.PdfName("Form"), "Filter": ast.PdfName("FlateDecode"),
			}, []byte("not zlib")))
			return d.finish(ast.PdfDict{"Font": ast.PdfDict{"F1": font}, "XObject": ast.PdfDict{"X1": bad}}, nil, "BT /F1 12 Tf (A) Tj ET")
		}},
		{"a resource dictionary held by an unread object", func(d *testDoc, font ast.PdfIndirectRef) ast.PdfDict {
			res := d.add(ast.PdfDict{"Font": ast.PdfDict{"F1": font}})
			return d.finish(res, ast.PdfDict{"Custom": ast.PdfDict{"Resources": res}}, "BT /F1 12 Tf (A) Tj ET")
		}},
		{"a signature", func(d *testDoc, font ast.PdfIndirectRef) ast.PdfDict {
			return d.finish(ast.PdfDict{"Font": ast.PdfDict{"F1": font}},
				ast.PdfDict{"Sig": ast.PdfDict{"ByteRange": ast.PdfArray{ast.PdfInt(0)}}},
				"BT /F1 12 Tf (A) Tj ET")
		}},
		{"an XFA form", func(d *testDoc, font ast.PdfIndirectRef) ast.PdfDict {
			return d.finish(ast.PdfDict{"Font": ast.PdfDict{"F1": font}},
				ast.PdfDict{"AcroForm": ast.PdfDict{"XFA": ast.PdfArray{}}},
				"BT /F1 12 Tf (A) Tj ET")
		}},
		{"a form showing text with an inherited font", func(d *testDoc, font ast.PdfIndirectRef) ast.PdfDict {
			form := d.add(ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("Form")}, []byte("BT (Z) Tj ET")))
			return d.finish(ast.PdfDict{"Font": ast.PdfDict{"F1": font}, "XObject": ast.PdfDict{"X1": form}}, nil,
				"BT /F1 12 Tf (A) Tj ET /X1 Do")
		}},
		{"a widget appearance under NeedAppearances", func(d *testDoc, font ast.PdfIndirectRef) ast.PdfDict {
			ap := d.add(ast.NewStream(ast.PdfDict{"Resources": ast.PdfDict{"Font": ast.PdfDict{"F9": font}}}, []byte("BT /F9 9 Tf (A) Tj ET")))
			trailer := d.finish(ast.PdfDict{"Font": ast.PdfDict{"F1": font}},
				ast.PdfDict{"AcroForm": ast.PdfDict{"NeedAppearances": ast.PdfBool(true)}}, "BT /F1 12 Tf (A) Tj ET")
			pages := d.alloc.Pending()[trailer["Root"].(ast.PdfIndirectRef).ObjectNum].(ast.PdfDict)["Pages"].(ast.PdfIndirectRef)
			page := d.alloc.Pending()[pages.ObjectNum].(ast.PdfDict)["Kids"].(ast.PdfArray)[0].(ast.PdfIndirectRef)
			d.alloc.Pending()[page.ObjectNum].(ast.PdfDict)["Annots"] = ast.PdfArray{ast.PdfDict{"Subtype": ast.PdfName("Widget"), "AP": ast.PdfDict{"N": ap}}}
			return trailer
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := newTestDoc(t)
			font, prog := d.trueType()
			trailer := tc.build(d, font)
			if stats := d.run(trailer); stats.Programs != 0 {
				t.Errorf("stats %+v, want no program subset", stats)
			}
			d.wantWhole(prog, ttfProgram())
		})
	}
}

func TestRun_SharedProgramKeepsTheUnion(t *testing.T) {
	t.Parallel()
	d := newTestDoc(t)
	font, prog := d.trueType()
	fd := d.alloc.Pending()[font.ObjectNum].(ast.PdfDict)["FontDescriptor"]
	other := d.add(ast.PdfDict{
		"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("TrueType"), "BaseFont": ast.PdfName("TestSans"),
		"FontDescriptor": fd, "Encoding": ast.PdfName("WinAnsiEncoding"),
	})
	res := ast.PdfDict{"Font": ast.PdfDict{"F1": font, "F2": other}}
	trailer := d.finish(res, nil, "BT /F1 12 Tf (A) Tj /F2 12 Tf (B) Tj ET", "BT /F2 12 Tf (C) Tj ET")
	if stats := d.run(trailer); stats.Programs != 1 {
		t.Errorf("stats %+v", stats)
	}
	d.wantTrueType(prog, 2, 3, 4)
}

func TestRun_FollowsQAndQ(t *testing.T) {
	t.Parallel()
	d := newTestDoc(t)
	f1, p1 := d.trueType()
	f2, p2 := d.type1C()
	res := ast.PdfDict{"Font": ast.PdfDict{"F1": f1, "F2": f2}}
	// The F2 selected inside q ... Q is lost at Q, so (B) shows with F1.
	trailer := d.finish(res, nil, "BT /F1 12 Tf q /F2 12 Tf (A) Tj Q (B) Tj ET")
	if stats := d.run(trailer); stats.Programs != 2 {
		t.Errorf("stats %+v", stats)
	}
	d.wantTrueType(p1, 3)
	want, err := cff.SubsetInPlace(context.Background(), cffProgram(), []uint16{1})
	if err != nil || !bytes.Equal(d.program(p2), want) {
		t.Errorf("CFF program is not the subset keeping A: %v", err)
	}
}

// TestRun_IdenticalProgramsShareOneSubset checks that byte-identical programs
// get one subset, the union of what their fonts show, so they stay identical
// and stream dedup still merges them.
func TestRun_IdenticalProgramsShareOneSubset(t *testing.T) {
	t.Parallel()
	d := newTestDoc(t)
	f1, p1 := d.trueType()
	f2, p2 := d.trueType()
	res := ast.PdfDict{"Font": ast.PdfDict{"F1": f1, "F2": f2}}
	trailer := d.finish(res, nil, "BT /F1 12 Tf (A) Tj /F2 12 Tf (B) Tj ET")
	if stats := d.run(trailer); stats.Programs != 2 {
		t.Errorf("stats %+v", stats)
	}
	d.wantTrueType(p1, 2, 3)
	d.wantTrueType(p2, 2, 3)
}

func TestRun_IsDeterministic(t *testing.T) {
	t.Parallel()
	build := func() (*testDoc, ast.PdfDict) {
		d := newTestDoc(t)
		f1, _ := d.trueType()
		f2, _ := d.type0()
		f3, _ := d.type1C()
		res := ast.PdfDict{"Font": ast.PdfDict{"F1": f1, "F2": f2, "F3": f3}}
		return d, d.finish(res, nil, "BT /F1 12 Tf (AB) Tj /F2 9 Tf <0003> Tj /F3 9 Tf (CD) Tj ET")
	}
	serialize := func() []byte {
		d, trailer := build()
		d.run(trailer)
		var buf bytes.Buffer
		if err := writer.WriteRewrite(context.Background(), &buf, writer.RewriteConfig{Allocator: d.alloc, Trailer: trailer}); err != nil {
			t.Fatalf("WriteRewrite: %v", err)
		}
		return buf.Bytes()
	}
	if !bytes.Equal(serialize(), serialize()) {
		t.Error("two runs wrote different bytes")
	}
}

// TestRun_PastTheCacheRedoesTheSubset lowers the stream budget so the subsets
// the pass keeps in memory, a quarter of it, cannot hold them all: the ones
// past it are redone when written, and the output is the same bytes.
func TestRun_PastTheCacheRedoesTheSubset(t *testing.T) {
	t.Parallel()
	const fonts = 8
	var largest int
	build := func() (*testDoc, ast.PdfDict, []ast.PdfIndirectRef) {
		d := newTestDoc(t)
		names := ast.PdfDict{}
		var progs []ast.PdfIndirectRef
		content := "BT"
		for i := range fonts {
			src := fontbuild.TrueType{PostScriptName: fmt.Sprintf("TestSans%d", i), Family: "Test Sans", Subfamily: "Regular", UnitsPerEm: 1000, Glyphs: testGlyphs()}.Build()
			largest = max(largest, len(src))
			prog := d.add(ast.NewStream(ast.PdfDict{"Length1": ast.PdfInt(len(src))}, src))
			fd := d.add(ast.PdfDict{"Type": ast.PdfName("FontDescriptor"), "FontFile2": prog})
			font := d.add(ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("TrueType"), "FontDescriptor": fd})
			name := fmt.Sprintf("F%d", i)
			names[ast.PdfName(name)] = font
			progs = append(progs, prog)
			content += fmt.Sprintf(" /%s 9 Tf (A) Tj", name)
		}
		return d, d.finish(ast.PdfDict{"Font": names}, nil, content+" ET"), progs
	}
	serialize := func(lim limits.Limits) ([]byte, int) {
		d, trailer, progs := build()
		if _, err := Run(context.Background(), d.alloc, trailer, Config{Limits: lim}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		encoded := 0
		for _, p := range progs {
			data, err := d.alloc.Pending()[p.ObjectNum].(ast.PdfStream).Bytes(context.Background())
			if err != nil {
				t.Fatalf("reading a subset: %v", err)
			}
			encoded += len(data)
		}
		var buf bytes.Buffer
		if err := writer.WriteRewrite(context.Background(), &buf, writer.RewriteConfig{Allocator: d.alloc, Trailer: trailer}); err != nil {
			t.Fatalf("WriteRewrite: %v", err)
		}
		return buf.Bytes(), encoded
	}
	want, encoded := serialize(limits.Limits{})
	tight := limits.Limits{MaxStreamBytes: int64(largest)}
	if cache := tight.MaxFontProgCacheBytes(); int64(encoded) <= cache {
		t.Fatalf("precondition: the %d subset bytes fit the %d byte cache", encoded, cache)
	}
	if got, _ := serialize(tight); !bytes.Equal(got, want) {
		t.Error("the subsets redone past the cache wrote different bytes")
	}
}

// TestRun_Type0EmbeddedCMapKeepsEverySplit pins the reading of pdfjs
// issue11768_reduced.pdf: a one-byte <e0> cidchar sits outside the codespace,
// which makes <e0> the lead byte of a three-byte code, and poppler draws each
// <e0> through the cidchar all the same. A subset keeping only the CIDs
// litho's own split reaches dropped the glyphs poppler draws.
func TestRun_Type0EmbeddedCMapKeepsEverySplit(t *testing.T) {
	t.Parallel()
	d := newTestDoc(t)
	font, prog := d.type0()
	cmapData := "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CMapName /T def\n" +
		"2 begincodespacerange <00> <7f> <e08080> <efbfbf> endcodespacerange\n" +
		"1 beginnotdefrange <00> <1f> 1 endnotdefrange\n" +
		"1 begincidchar <e0> 5 endcidchar\n" +
		"1 begincidrange <63> <6d> 3 endcidrange\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"
	encoding := d.add(ast.NewStream(ast.PdfDict{"Type": ast.PdfName("CMap")}, []byte(cmapData)))
	d.alloc.Pending()[font.ObjectNum].(ast.PdfDict)["Encoding"] = encoding
	res := ast.PdfDict{"Font": ast.PdfDict{"F1": font}}
	trailer := d.finish(res, nil, "BT /F1 20 Tf <e0e0e0636d> Tj ET")
	if stats := d.run(trailer); stats.Programs != 1 {
		t.Errorf("stats %+v", stats)
	}
	// <e0> through the cidchar (5), c and m through the cidrange (3 and
	// 13), and the notdef range's CID 1.
	d.wantTrueType(prog, 1, 3, 5, 13)
}

// TestRun_CIDToGIDMapStream checks that a CIDFontType2 keeps the glyphs its
// /CIDToGIDMap stream maps the CIDs shown to, and the CIDs as glyph IDs.
func TestRun_CIDToGIDMapStream(t *testing.T) {
	t.Parallel()
	d := newTestDoc(t)
	font, prog := d.type0()
	descendant := d.alloc.Pending()[font.ObjectNum].(ast.PdfDict)["DescendantFonts"].(ast.PdfArray)[0].(ast.PdfIndirectRef)
	// CID 1 to glyph 9, CID 2 to glyph 11.
	mapRef := d.add(ast.NewStream(ast.PdfDict{}, []byte{0, 0, 0, 9, 0, 11}))
	d.alloc.Pending()[descendant.ObjectNum].(ast.PdfDict)["CIDToGIDMap"] = mapRef
	res := ast.PdfDict{"Font": ast.PdfDict{"F1": font}}
	trailer := d.finish(res, nil, "BT /F1 20 Tf <00010002> Tj ET")
	if stats := d.run(trailer); stats.Programs != 1 {
		t.Errorf("stats %+v", stats)
	}
	d.wantTrueType(prog, 1, 2, 9, 11)
}
