package build

import (
	"bytes"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("forbidden_mix", forbiddenMix) }

// forbiddenMix builds complex_forbidden.pdf: a second deliberately complex fixture whose
// violation mix is disjoint from megafont2b.pdf.
//
// Targets the forbidden-construct families plus PostScript (Type 4) function tint
// transforms on Separation / DeviceN, which is an open litho false-negative
// cluster (veraPDF fires 6.2.4.4-t1 on such files, litho is silent).
//
// Intended independent violation classes:
//
//	A. LZWDecode filter                       (part 1 forbids it)
//	B. embedded file / /EmbeddedFiles          (part 1 forbids)
//	C. optional content /OCProperties          (part 1 forbids)
//	D. /Launch action                          (forbidden action type)
//	E. document-level JavaScript               (forbidden action type)
//	F. /NeedsRendering true                    (forbidden)
//	G. PostScript XObject (/Subtype /PS)       (forbidden)
//	H. Separation with a Type 4 PS tint transform
//	I. DeviceN with a Type 4 PS tint transform
//	J. font not embedded
//	K. annotation with /CA 0.5 (transparency) and no appearance stream
//	L. no XMP metadata, no OutputIntent
func forbiddenMix() ([]byte, error) {
	pdf := pdfgen.New()

	// lzwCompress is a minimal LZW encoder matching the PDF/TIFF
	// variable-code variant.
	lzwCompress := func(data []byte) []byte {
		dictSize := 256
		table := make(map[string]int, dictSize)
		for i := 0; i < dictSize; i++ {
			table[string([]byte{byte(i)})] = i
		}
		nextCode := 258
		codeLen := 9
		var outBits []byte

		emit := func(code, width int) {
			for i := width - 1; i >= 0; i-- {
				outBits = append(outBits, byte((code>>i)&1))
			}
		}

		emit(256, codeLen) // clear
		w := ""
		for _, ch := range data {
			c := string([]byte{ch})
			if _, ok := table[w+c]; ok {
				w += c
				continue
			}
			emit(table[w], codeLen)
			table[w+c] = nextCode
			nextCode++
			if nextCode == (1<<codeLen)+1 && codeLen < 12 {
				codeLen++
			}
			w = c
		}
		if w != "" {
			emit(table[w], codeLen)
		}
		emit(257, codeLen) // EOD
		for len(outBits)%8 != 0 {
			outBits = append(outBits, 0)
		}
		out := make([]byte, 0, len(outBits)/8)
		for i := 0; i < len(outBits); i += 8 {
			var v byte
			for _, b := range outBits[i : i+8] {
				v = v<<1 | b
			}
			out = append(out, v)
		}
		return out
	}

	// --- H/I: Type 4 (PostScript calculator) tint transforms.
	psSep := pdf.Stream([]byte("{ dup 0.5 mul exch dup 0.3 mul exch 0.2 mul }"))
	psSep.Set("FunctionType", 4)
	psSep.Set("Domain", a(0, 1))
	psSep.Set("Range", a(0, 1, 0, 1, 0, 1))

	sepCS := a(n("Separation"), n("/Spot1"), n("DeviceRGB"), psSep)

	psDevN := pdf.Stream([]byte("{ 2 copy add 0.5 mul 3 1 roll pop pop dup dup }"))
	psDevN.Set("FunctionType", 4)
	psDevN.Set("Domain", a(0, 1, 0, 1))
	psDevN.Set("Range", a(0, 1, 0, 1, 0, 1))

	devnCS := a(n("DeviceN"), a(n("/InkA"), n("/InkB")), n("DeviceRGB"), psDevN)

	// --- G: PostScript XObject.
	psx := pdf.Stream([]byte("%!PS-Adobe-3.0\n0 0 moveto 10 10 lineto stroke\n"))
	psx.Set("Type", n("XObject"))
	psx.Set("Subtype", n("PS"))

	// --- A: an LZW-compressed image.
	rawImg := bytes.Repeat([]byte{0, 64, 128, 255}, 4)
	lzwImg := pdf.Stream(lzwCompress(rawImg))
	lzwImg.Set("Type", n("XObject"))
	lzwImg.Set("Subtype", n("Image"))
	lzwImg.Set("Width", 4)
	lzwImg.Set("Height", 4)
	lzwImg.Set("ColorSpace", n("DeviceGray"))
	lzwImg.Set("BitsPerComponent", 8)
	lzwImg.Set("Filter", n("LZWDecode"))

	// --- J: a font with no embedded program.
	font := d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/Arial"),
		"Encoding", n("WinAnsiEncoding"),
		"FirstChar", 65,
		"LastChar", 67,
		"Widths", a(722, 722, 722),
		"FontDescriptor", d(
			"Type", n("FontDescriptor"),
			"FontName", n("/Arial"),
			"Flags", 32,
			"FontBBox", a(-166, -225, 1000, 931),
			"ItalicAngle", 0,
			"Ascent", 718,
			"Descent", -207,
			"CapHeight", 718,
			"StemV", 88,
		),
	)

	// --- C: an optional content group, referenced from a BDC in the content.
	ocg := pdf.Indirect(d("Type", n("OCG"), "Name", text("Layer 1")))

	content := `q
/CS0 cs 0.7 sc
50 700 100 40 re f
/CS1 cs 0.4 0.6 scn
50 650 100 40 re f
BT /F1 12 Tf 50 600 Td (ABC) Tj ET
q 60 0 0 60 50 500 cm /ImLZW Do Q
/OC /OC1 BDC
q 1 0 0 1 50 420 cm /PSX Do Q
EMC
Q
`

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("F1", pdf.Indirect(font)),
			"ColorSpace", d("CS0", sepCS, "CS1", devnCS),
			"XObject", d("ImLZW", lzwImg, "PSX", psx),
			"Properties", d("OC1", ocg),
		),
		"Contents", pdf.Stream([]byte(content)),
	)
	pdf.AddPage(pdf.Indirect(page))

	// --- K: transparent annotation with no appearance stream.
	annot := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 700, 500, 750),
		"F", 4,
		"CA", 0.5,
		"IC", a(1, 0, 0),
	)
	pdf.Page(0).Set("Annots", a(pdf.Indirect(annot)))

	// --- B: an embedded file, reachable from the catalog name tree.
	ef := pdf.Stream([]byte("attachment payload"))
	ef.Set("Type", n("EmbeddedFile"))
	spec := pdf.Indirect(d(
		"Type", n("Filespec"),
		"F", text("data.txt"),
		"UF", text("data.txt"),
		"EF", d("F", ef),
	))

	// --- D/E: forbidden actions.
	launch := pdf.Indirect(d("Type", n("Action"), "S", n("Launch"), "F", text("evil.exe")))
	js := pdf.Indirect(d("Type", n("Action"), "S", n("JavaScript"), "JS", text("app.alert('hi');")))

	pdf.Root.Set("Names", d(
		"EmbeddedFiles", d("Names", a(text("data.txt"), spec)),
		"JavaScript", d("Names", a(text("boot"), js)),
	))
	pdf.Root.Set("OpenAction", launch)
	pdf.Root.Set("OCProperties", d("OCGs", a(ocg), "D", d("ON", a(ocg))))
	pdf.Root.Set("NeedsRendering", true)

	// Deliberately no /Metadata and no /OutputIntents.
	return save(pdf, "1.7")
}
