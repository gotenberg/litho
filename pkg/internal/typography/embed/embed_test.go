package embed

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/internal/typography/varfont"
)

// testGlyphs are the glyphs of the test programs: A, B and an "fi"
// ligature, which no cmap entry reaches, at GIDs 1 to 3.
func testGlyphs() []fontbuild.Glyph {
	return []fontbuild.Glyph{
		{Name: "A", Unicode: 'A', Advance: 600},
		{Name: "B", Unicode: 'B', Advance: 700},
		{Name: "f_i", Advance: 800},
	}
}

func testTrueType() []byte {
	return fontbuild.TrueType{
		PostScriptName: "TestSans", Family: "Test Sans", Subfamily: "Regular",
		UnitsPerEm: 2000, Glyphs: testGlyphs(), NotdefAdvance: 500,
	}.Build()
}

// testOpenTypeCFF builds an OpenType program with CFF outlines over the
// same glyphs: the TrueType program's tables, its glyf and loca replaced by
// a name-keyed CFF table.
func testOpenTypeCFF(t *testing.T) []byte {
	t.Helper()
	ttf := fontbuild.TrueType{
		PostScriptName: "TestSerif", Family: "Test Serif", Subfamily: "Regular",
		Glyphs: testGlyphs(), NotdefAdvance: 500,
	}.Build()
	records, err := truetype.ParseTableDirectory(ttf)
	if err != nil {
		t.Fatalf("ParseTableDirectory: %v", err)
	}
	tables := map[string][]byte{"CFF ": fontbuild.CFF{FontName: "TestSerif", Glyphs: testGlyphs(), NotdefAdvance: 500}.Build()}
	for _, rec := range records {
		if rec.Tag == "glyf" || rec.Tag == "loca" {
			continue
		}
		data, err := truetype.TableData(ttf, records, rec.Tag)
		if err != nil {
			t.Fatalf("TableData(%q): %v", rec.Tag, err)
		}
		tables[rec.Tag] = data
	}
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	var out, body bytes.Buffer
	out.WriteString("OTTO")
	// binary.Write into a bytes.Buffer of fixed-size values cannot fail.
	_ = binary.Write(&out, binary.BigEndian, uint16(len(tags))) //nolint:gosec // a handful of tables
	_ = binary.Write(&out, binary.BigEndian, [3]uint16{0, 0, 0})
	offset := 12 + 16*len(tags)
	for _, tag := range tags {
		data := tables[tag]
		out.WriteString(tag)
		_ = binary.Write(&out, binary.BigEndian, uint32(0))
		_ = binary.Write(&out, binary.BigEndian, uint32(offset+body.Len())) //nolint:gosec // a test program of a few hundred bytes
		_ = binary.Write(&out, binary.BigEndian, uint32(len(data)))         //nolint:gosec // a test program of a few hundred bytes
		body.Write(data)
		for body.Len()%4 != 0 {
			body.WriteByte(0)
		}
	}
	out.Write(body.Bytes())
	return out.Bytes()
}

func mustLoad(t *testing.T, program []byte) *Font {
	t.Helper()
	f, err := Load(t.Context(), program)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return f
}

// written writes s at a fresh object and returns the allocator and the
// Type0 font.
func written(t *testing.T, s *Subset, logger *slog.Logger) (*writer.ObjAllocator, ast.PdfDict) {
	t.Helper()
	alloc := writer.NewObjAllocator(0)
	ref := alloc.Alloc(ast.PdfNull{})
	s.Write(t.Context(), alloc, logger, ref)
	font, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("no Type0 font at %d: %v", ref.ObjectNum, alloc.Pending()[ref.ObjectNum])
	}
	return alloc, font
}

func pendingDict(t *testing.T, alloc *writer.ObjAllocator, obj ast.PdfObject) ast.PdfDict {
	t.Helper()
	ref, _ := obj.(ast.PdfIndirectRef)
	dict, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("%v is not a pending dictionary", obj)
	}
	return dict
}

func pendingBytes(t *testing.T, alloc *writer.ObjAllocator, obj ast.PdfObject) []byte {
	t.Helper()
	ref, _ := obj.(ast.PdfIndirectRef)
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("%v is not a pending stream", obj)
	}
	data, err := stream.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	if stream.Dict["Filter"] == ast.PdfName("FlateDecode") {
		r, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("inflating: %v", err)
		}
		if data, err = io.ReadAll(r); err != nil {
			t.Fatalf("inflating: %v", err)
		}
	}
	return data
}

func TestLoadRefusesACollection(t *testing.T) {
	t.Parallel()
	_, err := Load(t.Context(), fontbuild.Collection(testTrueType(), testTrueType()))
	if !errors.Is(err, ErrCollection) {
		t.Fatalf("Load(t.Context(), collection) = %v, want ErrCollection", err)
	}
}

func TestLoadReadsTheProgram(t *testing.T) {
	t.Parallel()
	f := mustLoad(t, testTrueType())
	if got := f.PostScriptName(); got != "TestSans" {
		t.Errorf("PostScriptName = %q, want TestSans", got)
	}
	if got := f.UnitsPerEm(); got != 2000 {
		t.Errorf("UnitsPerEm = %d, want 2000", got)
	}
	if gid, ok := f.GID('B'); !ok || gid != 2 {
		t.Errorf("GID('B') = %d, %v, want 2, true", gid, ok)
	}
	if _, ok := f.GID('Z'); ok {
		t.Error("GID('Z') found a glyph the program lacks")
	}
	if r, ok := f.Rune(2); !ok || r != 'B' {
		t.Errorf("Rune(2) = %q, %v, want 'B', true", r, ok)
	}
	if _, ok := f.Rune(0); ok {
		t.Error("Rune(0) found a character for .notdef")
	}
	if w, ok := f.Width(3); !ok || w != 800 {
		t.Errorf("Width(3) = %d, %v, want 800, true", w, ok)
	}
	if got := f.Uncovered("AZBZY"); string(got) != "ZY" {
		t.Errorf("Uncovered = %q, want each missing character once, in order", string(got))
	}
}

// TestSubsetTrueTypeCodesPerGlyphAndText pins the TrueType code rule: one
// code per distinct (glyph, text) pair, in first-use order, two codes
// sharing a glyph through the CIDToGIDMap, and a ligature's code naming
// both characters in UTF-16BE.
func TestSubsetTrueTypeCodesPerGlyphAndText(t *testing.T) {
	t.Parallel()
	s := mustLoad(t, testTrueType()).NewSubset()
	codes := []uint16{
		s.Code(2, "B"),
		s.Code(3, "fi"),
		s.Code(2, "B"),
		s.Code(2, "β"),
		s.Code(1, ""),
	}
	if want := []uint16{1, 2, 1, 3, 4}; !reflect.DeepEqual(codes, want) {
		t.Fatalf("codes = %v, want %v", codes, want)
	}
	alloc, font := written(t, s, nil)
	if font["Encoding"] != ast.PdfName("Identity-H") {
		t.Errorf("Encoding = %v, want Identity-H", font["Encoding"])
	}
	base, _ := font["BaseFont"].(ast.PdfName)
	if !strings.HasSuffix(string(base), "+TestSans") || len(base) != len("ABCDEF+TestSans") {
		t.Errorf("BaseFont = %q, want a six-letter subset tag", base)
	}
	kids, _ := font["DescendantFonts"].(ast.PdfArray)
	desc := pendingDict(t, alloc, kids[0])
	if desc["Subtype"] != ast.PdfName("CIDFontType2") {
		t.Errorf("descendant = %v, want CIDFontType2", desc["Subtype"])
	}
	// The subset keeps .notdef, A, B and the ligature, renumbered 0 to 3.
	mapping := pendingBytes(t, alloc, desc["CIDToGIDMap"])
	want := []byte{0, 0, 0, 2, 0, 3, 0, 2, 0, 1}
	if !bytes.Equal(mapping, want) {
		t.Errorf("CIDToGIDMap = %x, want %x", mapping, want)
	}
	// Widths scale from 2000 units per em: B is 700, so 350.
	w, _ := desc["W"].(ast.PdfArray)
	if len(w) != 8 || w[0] != ast.PdfInt(1) || !reflect.DeepEqual(w[1], ast.PdfArray{ast.PdfInt(350)}) {
		t.Errorf("W = %v, want codes 1 to 4 with B at 350", w)
	}
	cmap := string(pendingBytes(t, alloc, font["ToUnicode"]))
	for _, entry := range []string{"<0001> <0042>\n", "<0002> <00660069>\n", "<0003> <03B2>\n"} {
		if !strings.Contains(cmap, entry) {
			t.Errorf("ToUnicode lacks %q:\n%s", entry, cmap)
		}
	}
	if strings.Contains(cmap, "<0004>") {
		t.Errorf("ToUnicode names code 4, drawn for no text:\n%s", cmap)
	}
	if !strings.Contains(cmap, "3 beginbfchar\n") {
		t.Errorf("ToUnicode does not hold three entries:\n%s", cmap)
	}
}

// TestSubsetCFFKeepsTheFirstText pins the CFF rule: a glyph's code is fixed
// by the charset, so its ToUnicode entry keeps the first text it was drawn
// for, and a differing later text is reported at Debug.
func TestSubsetCFFKeepsTheFirstText(t *testing.T) {
	t.Parallel()
	s := mustLoad(t, testOpenTypeCFF(t)).NewSubset()
	codes := []uint16{s.Code(3, ""), s.Code(3, "fi"), s.Code(2, "B"), s.Code(3, "ﬁ"), s.Code(3, "fi")}
	if want := []uint16{3, 3, 2, 3, 3}; !reflect.DeepEqual(codes, want) {
		t.Fatalf("codes = %v, want the glyph IDs a name-keyed program takes as CIDs: %v", codes, want)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	alloc, font := written(t, s, logger)
	kids, _ := font["DescendantFonts"].(ast.PdfArray)
	desc := pendingDict(t, alloc, kids[0])
	if desc["Subtype"] != ast.PdfName("CIDFontType0") {
		t.Errorf("descendant = %v, want CIDFontType0", desc["Subtype"])
	}
	fd := pendingDict(t, alloc, desc["FontDescriptor"])
	if _, ok := fd["FontFile3"]; !ok {
		t.Error("the descriptor embeds no FontFile3")
	}
	cmap := string(pendingBytes(t, alloc, font["ToUnicode"]))
	if !strings.Contains(cmap, "<0003> <00660069>\n") || !strings.Contains(cmap, "<0002> <0042>\n") {
		t.Errorf("ToUnicode does not keep the first text of each glyph:\n%s", cmap)
	}
	if got := strings.Count(logs.String(), "differing text"); got != 1 {
		t.Errorf("logged %d differing texts, want 1:\n%s", got, logs.String())
	}
}

// TestSubsetWritesDeterministically pins that the same glyphs recorded in
// the same order write the same objects.
func TestSubsetWritesDeterministically(t *testing.T) {
	t.Parallel()
	for _, program := range [][]byte{testTrueType(), testOpenTypeCFF(t)} {
		dump := func() string {
			s := mustLoad(t, program).NewSubset()
			for _, gt := range []glyphText{{2, "B"}, {3, "fi"}, {1, "A"}, {2, "B"}} {
				s.Code(gt.gid, gt.text)
			}
			alloc, _ := written(t, s, nil)
			nums := make([]int, 0, len(alloc.Pending()))
			for n := range alloc.Pending() {
				nums = append(nums, n)
			}
			sort.Ints(nums)
			var b strings.Builder
			for _, n := range nums {
				obj := alloc.Pending()[n]
				if stream, ok := obj.(ast.PdfStream); ok {
					data, _ := stream.Bytes(context.Background())
					b.WriteString(stream.Dict.String())
					b.Write(data)
					continue
				}
				b.WriteString(obj.String())
			}
			return b.String()
		}
		if first, second := dump(), dump(); first != second {
			t.Error("two writes of the same subset differ")
		}
	}
}

func TestSubsetCapsTheCodes(t *testing.T) {
	t.Parallel()
	s := mustLoad(t, testTrueType()).NewSubset()
	s.entries = make([]glyphText, 65535)
	if got := s.Code(1, "A"); got != 0 {
		t.Errorf("Code past 65535 pairs = %d, want 0", got)
	}
}

func TestToUnicodeWritesSurrogatePairs(t *testing.T) {
	t.Parallel()
	data, err := toUnicodeCMap(map[uint16]string{1: "😀", 2: "", 0: "A"}).Bytes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "1 beginbfchar\n<0001> <D83DDE00>\n") {
		t.Errorf("ToUnicode = %s, want U+1F600 as a surrogate pair and nothing for codes 0 and 2", data)
	}
}

func TestClampText(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", maxToUnicodeUnits-1) + "😀"
	if got := clampText(long); got != strings.Repeat("a", maxToUnicodeUnits-1) {
		t.Errorf("clampText split a surrogate pair or kept too much: %d bytes", len(got))
	}
	if got := clampText("fi"); got != "fi" {
		t.Errorf("clampText(fi) = %q", got)
	}
}

// TestWriteIdentityKeepsGlyphIDs pins the text stamp's shape: codes are
// glyph IDs through CIDToGIDMap Identity, the BaseFont carries no subset
// tag, and the program is written uncompressed.
func TestWriteIdentityKeepsGlyphIDs(t *testing.T) {
	t.Parallel()
	f := mustLoad(t, testTrueType())
	alloc := writer.NewObjAllocator(0)
	ref := alloc.Alloc(ast.PdfNull{})
	f.WriteIdentity(alloc, nil, ref, []uint16{2})
	font, _ := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
	if font["BaseFont"] != ast.PdfName("TestSans") || font["Encoding"] != ast.PdfName("Identity-H") {
		t.Errorf("font = %v, want TestSans over Identity-H", font)
	}
	kids, _ := font["DescendantFonts"].(ast.PdfArray)
	desc := pendingDict(t, alloc, kids[0])
	if desc["CIDToGIDMap"] != ast.PdfName("Identity") {
		t.Errorf("CIDToGIDMap = %v, want Identity", desc["CIDToGIDMap"])
	}
	fd := pendingDict(t, alloc, desc["FontDescriptor"])
	ffRef, _ := fd["FontFile2"].(ast.PdfIndirectRef)
	ff, _ := alloc.Pending()[ffRef.ObjectNum].(ast.PdfStream)
	if _, compressed := ff.Dict["Filter"]; compressed {
		t.Error("the program is compressed")
	}
	program := pendingBytes(t, alloc, fd["FontFile2"])
	if _, err := Load(t.Context(), program); err != nil {
		t.Errorf("the written program does not load: %v", err)
	}
	if n, err := truetype.NumGlyphs(program); err != nil || n != 4 {
		t.Errorf("the written program holds %d glyphs (%v), want every glyph ID kept", n, err)
	}
	cmap := string(pendingBytes(t, alloc, font["ToUnicode"]))
	if !strings.Contains(cmap, "<0001> <0041>\n<0002> <0042>\n") {
		t.Errorf("ToUnicode does not name every mapped glyph by its ID:\n%s", cmap)
	}
}

// TestLoad_StopsOnCancel checks that a cancelled context stops the walk
// over the program's cmap.
func TestLoad_StopsOnCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Load(ctx, testTrueType()); !errors.Is(err, context.Canceled) {
		t.Errorf("Load on a cancelled context = %v, want context.Canceled", err)
	}
}

// TestLoadWritesCFF2AsCFF pins that a program with CFF2 outlines, which no
// font file of ISO 32000-1, 9.9, Table 126 holds, is embedded as a
// CIDFontType0 over CFF outlines at its default instance, whether the
// program is variable or carries no 'fvar', and that the conversion is
// charged to the budget.
func TestLoadWritesCFF2AsCFF(t *testing.T) {
	t.Parallel()
	program, err := os.ReadFile("../varfont/testdata/varfont_cff2.otf")
	if err != nil {
		t.Fatal(err)
	}
	records, err := truetype.ParseTableDirectory(program)
	if err != nil {
		t.Fatal(err)
	}
	tables := map[string][]byte{}
	var tags []string
	for _, r := range records {
		if r.Tag == "fvar" || r.Tag == "avar" || r.Tag == "HVAR" || r.Tag == "MVAR" || r.Tag == "STAT" {
			continue
		}
		if tables[r.Tag], err = truetype.TableData(program, records, r.Tag); err != nil {
			t.Fatal(err)
		}
		tags = append(tags, r.Tag)
	}
	static := truetype.AssembleTables(0x4F54544F, tags, tables)
	for name, p := range map[string][]byte{"variable": program, "static": static} {
		ctx, budget, release := limits.WithBudget(t.Context(), limits.Default())
		f, err := Load(ctx, p)
		if err != nil {
			release()
			t.Fatalf("%s: %v", name, err)
		}
		if budget.Held() == 0 {
			t.Errorf("%s: the converted program is not charged", name)
		}
		release()
		if f.CFFTable() == nil || varfont.IsCFF2(f.Program()) {
			t.Fatalf("%s: the loaded program keeps CFF2 outlines", name)
		}
		gid, ok := f.GID('H')
		if !ok {
			t.Fatalf("%s: no glyph for H", name)
		}
		s := f.NewSubset()
		s.Code(gid, "H")
		alloc, font := written(t, s, slog.New(slog.DiscardHandler))
		kids, _ := font["DescendantFonts"].(ast.PdfArray)
		desc := pendingDict(t, alloc, kids[0])
		if desc["Subtype"] != ast.PdfName("CIDFontType0") {
			t.Errorf("%s: descendant = %v, want CIDFontType0", name, desc["Subtype"])
		}
		fd := pendingDict(t, alloc, desc["FontDescriptor"])
		if _, ok := fd["FontFile2"]; ok {
			t.Errorf("%s: the descriptor embeds a FontFile2", name)
		}
		if _, ok := fd["FontFile3"]; !ok {
			t.Errorf("%s: the descriptor embeds no FontFile3", name)
		}
	}
}
