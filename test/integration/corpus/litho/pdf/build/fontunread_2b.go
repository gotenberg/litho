package build

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("fontunread_2b", fontunread2b) }

// fontunread2b builds fontunread_2b.pdf: font programs in the right container that a
// validator's parser cannot finish embed nothing.
//
// ISO 19005-2, 6.2.11.4.1 t1 asks that every font program be embedded (ISO
// 32000-1, 9.9). veraPDF answers it with the program's own parse: a font whose
// program does not parse is reported as not embedded, and no rule weighs its
// glyphs, widths or cmap. Each font shows codes 41 and 42 through
// /WinAnsiEncoding:
//
//	U1  Type 1, /FontFile holding the clear-text portion alone, no eexec.
//	U2  Type 1, /FontFile whose Private dictionary defines no /CharStrings.
//	U3  Type 1, /FontFile3 /Type1C whose Top DICT INDEX is empty.
//	U4  Type 1, /FontFile3 /Type1C whose Top DICT states no CharStrings.
//	T1  TrueType, /FontFile2 an sfnt with no table at all.
//	T2  TrueType, /FontFile2 with every table but 'hmtx'.
//	T3  TrueType, /FontFile2 with every table but 'hhea'.
//	O1  Type 1, /FontFile3 /OpenType an "OTTO" with no table.
//
// Controls, each embedded: OK the well-formed Type 1 program U1 and U2 are cut
// from, CN a CFF with an empty Name INDEX, and TK the well-formed TrueType T2
// and T3 are cut from.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func fontunread2b() ([]byte, error) {
	pdf := pdfgen.New()
	const nonsymbolic = 32

	// dropTables rewrites an sfnt without the tables named in drop.
	dropTables := func(font []byte, drop map[string]bool) []byte {
		num := int(binary.BigEndian.Uint16(font[4:6]))
		tables := map[string][]byte{}
		for i := 0; i < num; i++ {
			rec := font[12+16*i : 28+16*i]
			tag := string(rec[0:4])
			off := binary.BigEndian.Uint32(rec[8:12])
			length := binary.BigEndian.Uint32(rec[12:16])
			tables[tag] = font[off : off+length]
		}
		var tags []string
		for t := range tables {
			if !drop[t] {
				tags = append(tags, t)
			}
		}
		sort.Strings(tags)
		offset := 12 + 16*len(tags)
		var records, body []byte
		for _, tag := range tags {
			data := tables[tag]
			records = append(records, tag...)
			records = binary.BigEndian.AppendUint32(records, ttChecksum(data))
			records = binary.BigEndian.AppendUint32(records, uint32(offset))    //nolint:gosec // offsets of a small font.
			records = binary.BigEndian.AppendUint32(records, uint32(len(data))) //nolint:gosec // tables of a small font.
			padded := ttPad4(data)
			body = append(body, padded...)
			offset += len(padded)
		}
		out := append([]byte(nil), font[:4]...)
		for _, v := range []int{len(tags), 16, 0, 0} {
			out = binary.BigEndian.AppendUint16(out, uint16(v)) //nolint:gosec // table counts and sizes of a small font.
		}
		out = append(out, records...)
		return append(out, body...)
	}

	// cffProgram is _cff.build's program, with the Name INDEX given and the
	// CharStrings operator replaced by a no-op when charstringsOp is false.
	// Python's default names is (_cff.FONT_NAME,).
	cffProgram := func(names [][]byte, charstringsOp bool) ([]byte, error) {
		charstrings := [][]byte{{0x0e}, cffRectCharstring(400), cffRectCharstring(500)}
		private := cffCat(cffDictInt(cffAdvanceWidth), []byte{0x14}, cffDictInt(0), []byte{0x15})
		topLen := len(bytes.Repeat(cffDictInt(0), 4)) + 3
		header := []byte{0x01, 0x00, 0x04, 0x01}
		nameIndex := cffIndex(names)
		headLen := len(header) + len(nameIndex) + len(cffIndex([][]byte{make([]byte, topLen)})) + 4
		charsetOff := headLen
		charset := cffCat([]byte{0x00}, cffU16(nil, cffSIDA), cffU16(nil, cffSIDB))
		charstringsOff := charsetOff + len(charset)
		charstringsIndex := cffIndex(charstrings)
		privateOff := charstringsOff + len(charstringsIndex)
		// Operator 13 is UniqueID, which a reader skips; 17 is CharStrings.
		op := []byte{0x0d}
		if charstringsOp {
			op = []byte{0x11}
		}
		cs := cffCat(cffDictInt(charstringsOff), op)
		top := cffCat(cffDictInt(charsetOff), []byte{0x0f}, cs,
			cffDictInt(len(private)), cffDictInt(privateOff), []byte{0x12})
		if len(top) != topLen {
			return nil, fmt.Errorf("top DICT is %d bytes, want %d", len(top), topLen)
		}
		return cffCat(header, nameIndex, cffIndex([][]byte{top}), cffIndex(nil), cffIndex(nil),
			charset, charstringsIndex, private), nil
	}

	type namedFont struct {
		tag string
		ref *pdfgen.Dict
	}

	font := func(tag, subtype, key string, program []byte, streamExtra ...any) namedFont {
		stream := pdf.Stream(program)
		for i := 0; i < len(streamExtra); i += 2 {
			stream.Set(streamExtra[i].(string), streamExtra[i+1])
		}
		descriptor := d(
			"Type", n("FontDescriptor"),
			"FontName", n("/"+tag),
			"Flags", nonsymbolic,
			"FontBBox", a(0, -200, 550, 700),
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
		)
		descriptor.Set("/"+key, pdf.MakeIndirect(stream))
		return namedFont{tag, pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("/"+subtype),
			"BaseFont", n("/"+tag),
			"FirstChar", 65,
			"LastChar", 66,
			"Widths", a(600, 600),
			"Encoding", n("WinAnsiEncoding"),
			"FontDescriptor", pdf.Indirect(descriptor),
		))}
	}

	type1 := func(tag string, charstrings bool) ([]byte, []any) {
		program, l1, l2, l3 := t1Build(t1Options{
			Name:            tag,
			Encoding:        t1EncodingArray(map[int]string{65: "A", 66: "B"}, true),
			OmitCharStrings: !charstrings,
		})
		return program, []any{"Length1", l1, "Length2", l2, "Length3", l3}
	}

	okProgram, okLengths := type1("OK", true)
	clear := bytes.ReplaceAll(okProgram[:okLengths[1].(int)], []byte("currentfile eexec\n"), nil)
	truetype := ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A", 0x42: "B"}}}, nil)
	type1C := []any{"Subtype", n("Type1C")}
	zeros := make([]byte, 8)

	u2Program, u2Lengths := type1("U2", false)
	u4Program, err := cffProgram([][]byte{[]byte(cffFontName)}, false)
	if err != nil {
		return nil, err
	}
	cnProgram, err := cffProgram(nil, true)
	if err != nil {
		return nil, err
	}
	fonts := []namedFont{
		font("U1", "Type1", "FontFile", clear, "Length1", len(clear), "Length2", 0, "Length3", 0),
		font("U2", "Type1", "FontFile", u2Program, u2Lengths...),
		font("U3", "Type1", "FontFile3", cffCat(cffBuild()[:4], cffIndex([][]byte{[]byte(cffFontName)}),
			cffIndex(nil), cffIndex(nil), cffIndex(nil)), type1C...),
		font("U4", "Type1", "FontFile3", u4Program, type1C...),
		font("T1", "TrueType", "FontFile2", cffCat(truetype[:4], zeros)),
		font("T2", "TrueType", "FontFile2", dropTables(truetype, map[string]bool{"hmtx": true})),
		font("T3", "TrueType", "FontFile2", dropTables(truetype, map[string]bool{"hhea": true})),
		font("O1", "Type1", "FontFile3", cffCat([]byte("OTTO"), zeros), "Subtype", n("OpenType")),
		font("OK", "Type1", "FontFile", okProgram, okLengths...),
		font("CN", "Type1", "FontFile3", cnProgram, type1C...),
		font("TK", "TrueType", "FontFile2", truetype),
	}

	resources := d()
	body := ""
	y := 750
	for _, f := range fonts {
		resources.Set("/"+f.tag, f.ref)
		body += fmt.Sprintf("BT 10 %d Td /%s 12 Tf <4142> Tj ET\n", y, f.tag)
		y -= 20
	}
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", resources),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(body))),
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.7")
}
