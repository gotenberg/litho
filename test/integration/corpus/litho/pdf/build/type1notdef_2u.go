package build

import (
	"encoding/hex"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("type1notdef_2u", type1notdef2u) }

// type1notdef2u builds type1notdef_2u.pdf: a code an embedded Type 1 font's encoding leaves
// unnamed takes the name its program gives it.
//
// ISO 32000-1, 9.10.2 derives a simple font's Unicode value from the glyph name
// the code selects, and Table 114 makes the program's built-in encoding the base
// of an /Encoding that names no table. A program that names a code .notdef gives
// it the value U+0000, which ISO 19005-2, 6.2.11.7.2 t2 forbids; one that names
// it nothing gives it no value, which t1 forbids. Each font embeds a readable
// program, carries no /ToUnicode, and shows 41 and one code its encoding leaves
// unnamed:
//
//	N1  /FontFile whose vector is filled with .notdef and names 41 A, 42 B; no
//	    /Encoding. Shows 41 43: the program names 43 .notdef.
//	N2  N1 with the vector left unfilled. Shows 41 43: the program names 43
//	    nothing.
//	N3  /FontFile of /Encoding StandardEncoding under /Differences [65 /A] with
//	    no /BaseEncoding. Shows 41 80: StandardEncoding leaves 80 undefined.
//	N4  /FontFile3 /Type1C on the Standard encoding, no /Encoding. Shows 41 80.
//	N5  N1's program under /Encoding /StandardEncoding, a table veraPDF holds
//	    none of. Shows 41 43.
//	N6  N4's program under /Encoding /StandardEncoding. Shows 41 80.
//
// NC, N1's program showing 41 42 only, is the control. N1, N2, N3 and N5 also
// fail 6.2.11.4.1 t2 for their second code, a glyph the program does not hold;
// N4 and N6 do not, since a CFF holds .notdef; N2 fails 6.2.11.5 t1 too.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func type1notdef2u() ([]byte, error) {
	pdf := pdfgen.New()
	const nonsymbolic = 32

	type prog struct {
		data  []byte
		extra [][2]any // ordered (key, value) pairs set on the font file stream
	}

	program := func(tag string, encoding []byte) prog {
		data, l1, l2, l3 := t1Build(t1Options{Name: tag, Encoding: encoding})
		return prog{data, [][2]any{{"Length1", l1}, {"Length2", l2}, {"Length3", l3}}}
	}

	type fontRef struct {
		tag   string
		ref   *pdfgen.Dict
		shows []byte
	}

	font := func(tag, key string, p prog, encoding obj, shows string) fontRef {
		stream := pdf.Stream(p.data)
		for _, kv := range p.extra {
			stream.Set(kv[0].(string), kv[1])
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
		descriptor.Set(key, pdf.MakeIndirect(stream))
		widths := a()
		for range 256 {
			widths.Append(600)
		}
		fd := d(
			"Type", n("Font"),
			"Subtype", n("Type1"),
			"BaseFont", n("/"+tag),
			"FirstChar", 0,
			"LastChar", 255,
			"Widths", widths,
			"FontDescriptor", pdf.Indirect(descriptor),
		)
		if encoding != nil {
			fd.Set("Encoding", encoding)
		}
		return fontRef{tag, pdf.Indirect(fd), []byte(shows)}
	}

	filled := program("N1", t1EncodingArray(map[int]string{65: "A", 66: "B"}, true))
	unfilled := program("N2", t1EncodingArray(map[int]string{65: "A", 66: "B"}, false))
	standard := program("N3", nil)
	cff := prog{cffBuild(), [][2]any{{"Subtype", n("Type1C")}}}
	differences := d("Type", n("Encoding"), "Differences", a(65, n("A")))

	fonts := []fontRef{
		font("N1", "FontFile", filled, nil, "AC"),
		font("N2", "FontFile", unfilled, nil, "AC"),
		font("N3", "FontFile", standard, differences, "A\x80"),
		font("N4", "FontFile3", cff, nil, "A\x80"),
		font("N5", "FontFile", filled, n("StandardEncoding"), "AC"),
		font("N6", "FontFile3", cff, n("StandardEncoding"), "A\x80"),
		font("NC", "FontFile", filled, nil, "AB"),
	}

	resources := d()
	var body []byte
	y := 750
	for _, f := range fonts {
		resources.Set(f.tag, f.ref)
		body = fmt.Appendf(body, "BT 10 %d Td /%s 12 Tf <%s> Tj ET\n", y, f.tag, hex.EncodeToString(f.shows))
		y -= 20
	}
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", resources),
		"Contents", pdf.MakeIndirect(pdf.Stream(body)),
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.7")
}
