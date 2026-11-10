package build

import (
	"encoding/binary"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"unicode/utf16"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen/zdeflate"
)

// Type 0 fonts over embedded encoding CMaps, for the cmap*_2b fixtures.
//
// Each font is a CIDFontType2 over the three-glyph TrueType program of
// truetype.go. Its /CIDToGIDMap stream sends CIDs 1 to 6 to glyph A and stops
// after CID 6, so a CID past it selects no glyph, which veraPDF reads as absent
// (CIDFontType2Program.containsCID) and as .notdef. CID 0 is absent too:
// containsCID refuses it, and GFGlyph exempts only code 0. The /DW equals the
// program's advance, so no width rule answers.
//
// The encoding CMaps declare the two-byte code space <0000> <FFFF> unless a
// builder passes another, and name
// themselves with a /CMapName that is no predefined CMap. A font or CMap over a
// predefined character collection passes its CIDSystemInfo, and a font whose
// CIDs lie elsewhere passes its own CID-to-glyph map.

// ecIdentitySystemInfo is the Adobe-Identity-0 /CIDSystemInfo.
func ecIdentitySystemInfo() *pdfgen.Dict {
	return d("Registry", text("Adobe"), "Ordering", text("Identity"), "Supplement", 0)
}

// ecBFChar is _encodingcmap.BFCHAR.
const ecBFChar = "1 beginbfchar\n<0041> <0041>\nendbfchar\n"

// ecSystemInfo is the Adobe CIDSystemInfo
// dictionary of a character collection.
func ecSystemInfo(ordering string, supplement int) *pdfgen.Dict {
	return d("Registry", text("Adobe"), "Ordering", text(ordering), "Supplement", supplement)
}

// ecFont is one (key, font, shows) entry of Fixture.fonts.
type ecFont struct {
	Key   string
	Font  *pdfgen.Dict
	Shows string
}

// ecFixture is _encodingcmap.Fixture: one single-page fixture showing a
// string through each of its fonts.
type ecFixture struct {
	pdf       *pdfgen.Doc
	fontFile  *pdfgen.Stream
	cidGIDMap *pdfgen.Stream
	fonts     []ecFont
}

func newECFixture() *ecFixture {
	f := &ecFixture{pdf: pdfgen.New()}
	program := ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A", 0x42: "B"}}}, nil)
	f.fontFile = f.pdf.MakeIndirect(f.pdf.Stream(program, "Length1", len(program))).(*pdfgen.Stream)
	raw := []byte{0, 0}
	for range 6 {
		raw = append(raw, 0, 1)
	}
	gidMap := f.pdf.Stream(zdeflate.Compress(raw, 9))
	gidMap.Set("Filter", n("FlateDecode"))
	f.cidGIDMap = f.pdf.MakeIndirect(gidMap).(*pdfgen.Stream)
	return f
}

// gidMap is a /CIDToGIDMap stream sending each CID of cids to glyph A, and
// every other CID to no glyph.
func (f *ecFixture) gidMap(cids []int) *pdfgen.Stream {
	maxCID := 0
	for _, c := range cids {
		maxCID = max(maxCID, c)
	}
	table := make([]byte, 2*(maxCID+1))
	for _, c := range cids {
		table[2*c+1] = 1
	}
	stream := f.pdf.Stream(zdeflate.Compress(table, 9))
	stream.Set("Filter", n("FlateDecode"))
	return f.pdf.MakeIndirect(stream).(*pdfgen.Stream)
}

// ecCMapOpts are ecFixture.cmap's options; a zero field takes the default:
// no /UseCMap, ecIdentitySystemInfo and the code space <0000> <FFFF>.
type ecCMapOpts struct {
	UseCMap   pdfgen.Object
	Info      *pdfgen.Dict
	Codespace string
}

// cmap is an embedded encoding CMap stream carrying body after its code
// space, which codespace gives as one begincodespacerange entry.
func (f *ecFixture) cmap(name, body string, o ecCMapOpts) *pdfgen.Stream {
	info := o.Info
	if info == nil {
		info = ecIdentitySystemInfo()
	}
	codespace := o.Codespace
	if codespace == "" {
		codespace = "<0000> <FFFF>"
	}
	program := "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo 3 dict dup begin /Registry (Adobe) def " +
		"/Ordering (" + string(info.Get("Ordering").(pdfgen.String)) + ") def /Supplement " +
		fmt.Sprint(int(info.Get("Supplement").(pdfgen.Int))) + " def end def\n" +
		"/CMapName /" + name + " def\n/CMapType 1 def\n/WMode 0 def\n" +
		"1 begincodespacerange\n" + codespace + "\nendcodespacerange\n" + body +
		"endcmap\nCMapName currentdict /CMap defineresource pop\nend\n" + "end\n"
	stream := f.pdf.Stream([]byte(program))
	stream.Set("Type", n("CMap"))
	stream.Set("CMapName", n("/"+name))
	stream.Set("CIDSystemInfo", info)
	stream.Set("WMode", 0)
	if o.UseCMap != nil {
		stream.Set("UseCMap", o.UseCMap)
	}
	return f.pdf.MakeIndirect(stream).(*pdfgen.Stream)
}

// font adds the Type 0 font key over encoding, showing the hex string shows.
// info nil is ecIdentitySystemInfo and gidMap nil the fixture's map.
func (f *ecFixture) font(key, name string, encoding pdfgen.Object, shows string, info *pdfgen.Dict, gidMap *pdfgen.Stream) {
	pdf := f.pdf
	if info == nil {
		info = ecIdentitySystemInfo()
	}
	var cidToGID pdfgen.Object = f.cidGIDMap
	if gidMap != nil {
		cidToGID = gidMap
	}
	descendant := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("CIDFontType2"),
		"BaseFont", n("/"+name),
		"CIDSystemInfo", info,
		"DW", ttAdvanceWidth,
		"CIDToGIDMap", cidToGID,
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"), "FontName", n("/"+name), "Flags", 32, "ItalicAngle", 0, "Ascent", 800,
			"Descent", -200, "CapHeight", 700, "StemV", 80, "FontBBox", a(0, -200, 550, 700),
			"FontFile2", f.fontFile)),
	))
	font := pdf.Indirect(d("Type", n("Font"), "Subtype", n("Type0"), "BaseFont", n("/"+name),
		"Encoding", encoding, "DescendantFonts", a(descendant)))
	f.fonts = append(f.fonts, ecFont{key, font, shows})
}

func (f *ecFixture) save() ([]byte, error) {
	pdf := f.pdf
	var content []byte
	fonts := d()
	for i, e := range f.fonts {
		content = fmt.Appendf(content, "BT /%s 12 Tf 50 %d Td <%s> Tj ET\n", e.Key, 720-i*20, e.Shows)
		fonts.Set(e.Key, e.Font)
	}
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", fonts),
		"Contents", pdf.Stream(content),
	))
	pdf.AddPage(page)
	// Deliberately no /Metadata and no output intent.
	return save(pdf, "1.7")
}

// iccMinimalProfile is the structurally minimal ICC profile several builders
// define: a 128-byte version header and an empty tag table.
// tag fills the preferred CMM field, which only makes otherwise identical
// profiles differ byte for byte.
func iccMinimalProfile(deviceClass, colourSpace string, version uint32, tag string) []byte {
	const headerSize = 128
	tagTable := binary.BigEndian.AppendUint32(nil, 0)
	header := make([]byte, headerSize, headerSize+len(tagTable))
	binary.BigEndian.PutUint32(header[0:4], uint32(headerSize+len(tagTable))) //nolint:gosec // a 132-byte profile.
	copy(header[4:8], tag)
	binary.BigEndian.PutUint32(header[8:12], version)
	copy(header[12:16], deviceClass)
	copy(header[16:20], colourSpace)
	copy(header[20:24], "XYZ ")
	copy(header[36:40], "acsp")
	binary.BigEndian.PutUint32(header[68:72], 0x0000F6D6)
	binary.BigEndian.PutUint32(header[72:76], 0x00010000)
	binary.BigEndian.PutUint32(header[76:80], 0x0000D32D)
	return append(header, tagTable...)
}

// Helpers shared by iccv4gray_1b and iccv4space_1b.
//
// Every product below goes through an explicit float64 conversion: Go may
// fuse a*b+c into one rounding on some architectures, and a one-ulp drift can
// move an s15Fixed16 value and so the committed profiles' bytes.

// iccMat3 is a 3 by 3 matrix, rows first.
type iccMat3 [3][3]float64

// iccBradfordMatrix is the Bradford cone response matrix.
var iccBradfordMatrix = iccMat3{
	{0.8951, 0.2664, -0.1614},
	{-0.7502, 1.7135, 0.0367},
	{0.0389, -0.0685, 1.0296},
}

// iccMatVec is the product of m and the column vector v.
func iccMatVec(m iccMat3, v [3]float64) [3]float64 {
	var out [3]float64
	for i := range 3 {
		s := 0.0
		for j := range 3 {
			s = float64(s + float64(m[i][j]*v[j]))
		}
		out[i] = s
	}
	return out
}

// iccMatMul is the matrix product a b.
func iccMatMul(a, b iccMat3) iccMat3 {
	var out iccMat3
	for i := range 3 {
		for j := range 3 {
			s := 0.0
			for k := range 3 {
				s = float64(s + float64(a[i][k]*b[k][j]))
			}
			out[i][j] = s
		}
	}
	return out
}

// iccMatInv is the inverse of m, by cofactors.
func iccMatInv(m iccMat3) iccMat3 {
	a, b, c := m[0][0], m[0][1], m[0][2]
	d, e, f := m[1][0], m[1][1], m[1][2]
	g, h, i := m[2][0], m[2][1], m[2][2]
	// sub is x*y - z*w, each product rounded on its own.
	sub := func(x, y, z, w float64) float64 { return float64(x*y) - float64(z*w) }
	det := float64(float64(a*sub(e, i, f, h))-float64(b*sub(d, i, f, g))) + float64(c*sub(d, h, e, g))
	return iccMat3{
		{sub(e, i, f, h) / det, sub(c, h, b, i) / det, sub(b, f, c, e) / det},
		{sub(f, g, d, i) / det, sub(a, i, c, g) / det, sub(c, d, a, f) / det},
		{sub(d, h, e, g) / det, sub(b, g, a, h) / det, sub(a, e, b, d) / det},
	}
}

// iccBradford is the Bradford adaptation from white src to white
// dst (ICC.1:2010, E.3).
func iccBradford(src, dst [3]float64) iccMat3 {
	s, d := iccMatVec(iccBradfordMatrix, src), iccMatVec(iccBradfordMatrix, dst)
	var scale iccMat3
	for i := range 3 {
		scale[i][i] = d[i] / s[i]
	}
	return iccMatMul(iccMatInv(iccBradfordMatrix), iccMatMul(scale, iccBradfordMatrix))
}

// iccMluc is a multiLocalizedUnicodeType of one enUS record.
func iccMluc(text string) []byte {
	var units []byte
	for _, u := range utf16.Encode([]rune(text)) {
		units = binary.BigEndian.AppendUint16(units, u)
	}
	out := append([]byte("mluc"), make([]byte, 4)...)
	out = iccU32(iccU32(out, 1), 12)
	out = append(out, "enUS"...)
	out = iccU32(iccU32(out, len(units)), 28)
	return append(out, units...)
}

// iccGreyProfile is the profile of iccv4gray_1b and iccv4space_1b: an
// ICC grey display profile of the given version and tags.
func iccGreyProfile(version uint32, tags []iccTag) []byte {
	table := iccU32(nil, len(tags))
	var body []byte
	offset := 128 + 4 + 12*len(tags)
	for _, t := range tags {
		table = append(table, t.Sig...)
		table = iccU32(table, offset+len(body))
		table = iccU32(table, len(t.Data))
		body = append(body, t.Data...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}
	header := make([]byte, 128)
	binary.BigEndian.PutUint32(header[0:4], uint32(128+len(table)+len(body))) //nolint:gosec // fixture profiles are small.
	binary.BigEndian.PutUint32(header[8:12], version)
	copy(header[12:16], "mntr")
	copy(header[16:20], "GRAY")
	copy(header[20:24], "XYZ ")
	copy(header[36:40], "acsp")
	copy(header[68:80], iccS15(iccD50...))
	return slices.Concat(header, table, body)
}

// lithoBareSansPage is the tail the iccbad* builders share: the conforming
// embedded TrueType of baretree_2b.pdf (LithoBareSans, drawing A and B) and
// a 200 by 200 page painting content in it, returned before it is appended.
func lithoBareSansPage(pdf *pdfgen.Doc, content string) *pdfgen.Dict {
	font := latinTrueTypeFont(pdf, "/LithoBareSans")
	return pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
	))
}

// pyG formats value as C's "%g" does: six significant digits, trailing zeros
// and a trailing point trimmed. The committed files print their reals so.
func pyG(value float64) string {
	return strconv.FormatFloat(value, 'g', 6, 64)
}

// sortedLetters returns the distinct ASCII letters of lines, sorted. The
// builders' text holds no other letters.
func sortedLetters(lines ...string) []string {
	seen := map[byte]bool{}
	var letters []string
	for _, line := range lines {
		for i := 0; i < len(line); i++ {
			c := line[i]
			if ('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') && !seen[c] {
				seen[c] = true
				letters = append(letters, string(c))
			}
		}
	}
	sort.Strings(letters)
	return letters
}

// latinTrueTypeFont is the conforming embedded TrueType several builders
// spell out in full: ttBuild's A and B glyphs under (3, 1) and (1, 0)
// cmap subtables, a non-symbolic descriptor and WinAnsiEncoding over codes
// 0x41 and 0x42. base is its /BaseFont and /FontName. Objects are created in
// the builders' order: the font file, then the descriptor, then the font.
func latinTrueTypeFont(pdf *pdfgen.Doc, base string) *pdfgen.Dict {
	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := pdf.Indirect(d(
		"Type", n("FontDescriptor"),
		"FontName", n(base),
		"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontBBox", a(0, -200, 550, 700),
		"FontFile2", pdf.MakeIndirect(fontFile),
	))
	return pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n(base),
		"FirstChar", 0x41,
		"LastChar", 0x42,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"FontDescriptor", descriptor,
		"Encoding", n("WinAnsiEncoding"),
	))
}
