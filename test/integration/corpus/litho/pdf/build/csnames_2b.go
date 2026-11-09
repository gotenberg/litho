package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("csnames_2b", csnames2b) }

// csnames2b builds csnames_2b.pdf: colour-space names inside colour space arrays.
//
// ISO 32000-1, 8.6.3 lets a colour space array name its base, its alternate or a
// colorant's space either by a family name or, as veraPDF reads it, by the key of
// another entry of the same /ColorSpace resource dictionary. veraPDF resolves the
// name against the one dictionary the space was read from, with no fallback to
// the page's, and judges the entry it names in its place. No file of any corpus
// names a /ColorSpace entry inside a colour space array, in a /Colorants
// dictionary, as a shading's colour space or as an image's /Indexed base, so no
// count reached the reading; a validator that took such a name for a space it
// cannot read judged DeviceGray instead.
//
// Every page has resources of its own, and every Separation names a colorant of
// its own, so no two pages share a verdict:
//
// | Page | Construct                                                         | Answers at PDF/A-2b       |
// | ---- | ----------------------------------------------------------------- | ------------------------- |
// | 1    | `[/Indexed /CS0 ...]`, `/CS0` being `/DeviceRGB`                  | DeviceRGB x2              |
// | 2    | the same over `/CS0 [/DeviceRGB]`, a one-element array            | DeviceRGB x2              |
// | 3    | `/CS2 /CS0`, an entry naming another                              | DeviceRGB x2              |
// | 4    | a Separation whose alternate is `/CS0`, `/DeviceCMYK`             | DeviceCMYK x3             |
// | 5    | a DeviceN whose alternate is `/CS0`, `/DeviceCMYK`                | DeviceCMYK x3             |
// | 6    | a DeviceN over DeviceGray whose `/Colorants` names a Separation entry over DeviceRGB | DeviceRGB x3, DeviceGray x3 |
// | 7    | DefaultRGB `[/Indexed /CS0 ...]`, `/CS0` being `/DeviceCMYK`      | DeviceCMYK x2             |
// | 8    | an inline image `[/Indexed /CS0 ...]`                             | DeviceRGB x1              |
// | 9    | `[/Indexed /CS0 ...]` over a direct abstract ICCBased entry       | ICC x2                    |
// | 10   | the same over an indirect ICCBased entry                          | ICC x1                    |
// | 11   | `/CSp /CSq`, `/CSq [/Pattern]`, painting a coloured tiling cell   | DeviceRGB x2, integer x1  |
// | 12   | `/CSp /CSq`, `/CSq [/Pattern /DeviceRGB]`, an uncoloured cell     | DeviceRGB x1              |
// | 13   | `[/Indexed 9 0 R ...]`, an indirect object holding `/DeviceRGB`   | DeviceRGB x2              |
// | 14   | a form's `/Indexed` over `/CS0`, which only the page defines      | DeviceGray x2             |
// | 15   | the page's `/Indexed` over `/CS0`, a form redefining `/CS0`       | DeviceRGB x2              |
// | 16   | the page's `/Indexed` image a form paints, the form redefining `/CS0` | DeviceRGB x1          |
// | 17   | a form's shading naming `/CS0`, which only the page defines       | nothing                   |
// | 18   | the page's DeviceRGB shading a form paints, under the page's DefaultRGB | ICC x1              |
// | 19   | the page's `/Indexed` DeviceRGB image a form paints, the same     | ICC x1                    |
// | 20   | two Separations of one colorant over `/CS0` and `/DeviceCMYK`     | DeviceCMYK x6             |
//
// ICC is ISO 19005-2, 6.2.4.2 t1 over an abstract profile, the device families
// 6.2.4.3 t2 to t4, and the integer the 2147483648 given to `w` in page 11's cell,
// 6.1.13 t1, which veraPDF reads only because the name reaches the Pattern space
// and the pattern is painted. Page 17 is a control: the form's shading resolves
// `/CS0` in the form's resources, where it names nothing, and veraPDF judges no
// colour space. Page 20 is another: ISO 19005-2, 6.2.4.4 t2 compares the two
// Separations' alternates as the spaces they name, the same DeviceCMYK, so the
// test stays silent. Pages 15, 16, 18 and 19 also fail 6.2.2 t2, once each: the
// form selects a colour space, an image or a shading only the page defines. Page
// 14's form does not: `/CS0` inside its own `/Indexed` is no resource name the
// form looks up, and it reads as no colour space, which veraPDF judges as
// DeviceGray.
//
// Two readings stay out, because veraPDF cannot finish them: a name that reaches
// itself again, which runs it out of time or stack, and a fan-out of names wide
// enough to need litho's bound on the names one link follows. The page group
// naming an entry is already in `blendcs_4.pdf`, and an entry holding an
// indirect family name in govdocs thread_300_300760.pdf.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func csnames2b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccHeaderSize = 128
	const iccVersion4 = 0x04200000
	const fill = "0 0 10 10 re f\n"
	index := str("\x00\x00\x00\xff\xff\xff")

	// abstractRGB is a header-only abstract profile over RGB, which 6.2.4.2 t1
	// forbids.
	abstractRGB := func() *pdfgen.Stream {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize, iccHeaderSize+len(tagTable))
		binary.BigEndian.PutUint32(header[0:4], uint32(iccHeaderSize+len(tagTable))) //nolint:gosec // a 132-byte profile.
		binary.BigEndian.PutUint32(header[8:12], iccVersion4)
		copy(header[12:16], "abst")
		copy(header[16:20], "RGB ")
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		stream := pdf.Stream(append(header, tagTable...))
		stream.Set("N", 3)
		pdf.MakeIndirect(stream)
		return stream
	}

	// tint is a type 2 tint transform onto a space of the given component
	// count.
	tint := func(outputs int) *pdfgen.Dict {
		c0 := make([]any, outputs)
		c1 := make([]any, outputs)
		for i := range c0 {
			c0[i], c1[i] = 0, 0
		}
		c1[0] = 1
		return d("FunctionType", 2, "Domain", a(0, 1), "C0", a(c0...),
			"C1", a(c1...), "N", 1)
	}

	indexed := func(base any) *pdfgen.Array {
		return a(n("Indexed"), base, 1, index)
	}

	// image is a one-pixel image XObject, or an image mask.
	image := func(colourSpace any, mask bool) *pdfgen.Stream {
		img := pdf.Stream([]byte("\x00"))
		img.Set("Type", n("XObject"))
		img.Set("Subtype", n("Image"))
		img.Set("Width", 1)
		img.Set("Height", 1)
		if mask {
			img.Set("ImageMask", true)
			img.Set("BitsPerComponent", 1)
		} else {
			img.Set("ColorSpace", colourSpace)
			img.Set("BitsPerComponent", 8)
		}
		pdf.MakeIndirect(img)
		return img
	}

	form := func(body string, resources *pdfgen.Dict) *pdfgen.Stream {
		stream := pdf.Stream([]byte(body))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 10, 10))
		stream.Set("Resources", resources)
		pdf.MakeIndirect(stream)
		return stream
	}

	tiling := func(paint int, body string, resources *pdfgen.Dict) *pdfgen.Stream {
		stream := pdf.Stream([]byte(body))
		stream.Set("Type", n("Pattern"))
		stream.Set("PatternType", 1)
		stream.Set("PaintType", paint)
		stream.Set("TilingType", 1)
		stream.Set("BBox", a(0, 0, 10, 10))
		stream.Set("XStep", 10)
		stream.Set("YStep", 10)
		stream.Set("Resources", resources)
		pdf.MakeIndirect(stream)
		return stream
	}

	shading := func(colourSpace any) *pdfgen.Dict {
		return d("ShadingType", 2, "ColorSpace", colourSpace, "Coords", a(0, 0, 10, 0), "Function", tint(3))
	}

	page := func(content string, resources ...any) {
		dd := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", d(resources...),
			"Contents", pdf.Stream([]byte(content)),
		)
		pdf.AddPage(pdf.Indirect(dd))
	}

	spaces := d

	const indexedFill = "/CS1 cs 0 sc " + fill

	page(indexedFill, "ColorSpace", spaces("CS0", n("DeviceRGB"), "CS1", indexed(n("/CS0"))))
	page(indexedFill, "ColorSpace", spaces("CS0", a(n("DeviceRGB")), "CS1", indexed(n("/CS0"))))
	page("/CS2 cs 1 0 0 sc "+fill, "ColorSpace", spaces("CS0", n("DeviceRGB"), "CS2", n("/CS0")))
	page("/CS1 cs 1 scn "+fill, "ColorSpace", spaces(
		"CS0", n("DeviceCMYK"), "CS1", a(n("Separation"), n("/SpotSep"), n("/CS0"), tint(4))))
	page("/CS1 cs 1 scn "+fill, "ColorSpace", spaces(
		"CS0", n("DeviceCMYK"), "CS1", a(n("DeviceN"), a(n("Cyan")), n("/CS0"), tint(4))))
	page("/CS1 cs 1 scn "+fill, "ColorSpace", spaces(
		"CS3", a(n("Separation"), n("/SpotCol"), n("DeviceRGB"), tint(3)),
		"CS1", a(n("DeviceN"), a(n("/SpotCol")), n("DeviceGray"), tint(4),
			d("Colorants", d("SpotCol", n("/CS3")))),
	))
	page("1 0 0 rg "+fill, "ColorSpace", spaces("CS0", n("DeviceCMYK"), "DefaultRGB", indexed(n("/CS0"))))
	page("BI /W 1 /H 1 /BPC 8 /CS [/Indexed /CS0 1 <000000FFFFFF>] ID \x00 EI\n", "ColorSpace", spaces("CS0", n("DeviceRGB")))
	page(indexedFill, "ColorSpace", spaces("CS0", a(n("ICCBased"), abstractRGB()), "CS1", indexed(n("/CS0"))))
	page(indexedFill, "ColorSpace", spaces(
		"CS0", pdf.MakeIndirect(a(n("ICCBased"), abstractRGB())), "CS1", indexed(n("/CS0"))))
	page("/CSp cs /P1 scn "+fill,
		"ColorSpace", spaces("CSq", a(n("Pattern")), "CSp", n("/CSq")),
		"Pattern", d("P1", tiling(1, "2147483648 w 1 0 0 rg 0 0 5 5 re f\n", d())))
	page("/CSp cs 1 0 0 /P2 scn "+fill,
		"ColorSpace", spaces("CSq", a(n("Pattern"), n("DeviceRGB")), "CSp", n("/CSq")),
		"Pattern", d("P2", tiling(2, "/Im0 Do\n", d("XObject", d("Im0", image(nil, true))))))
	page(indexedFill, "ColorSpace", spaces("CS1", indexed(pdf.MakeIndirect(n("DeviceRGB")))))

	page("/Fm0 Do\n", "ColorSpace", spaces("CS0", n("DeviceRGB")), "XObject", d(
		"Fm0", form(indexedFill, d("ColorSpace", spaces("CS1", indexed(n("/CS0")))))))
	page("/Fm0 Do\n", "ColorSpace", spaces("CS0", n("DeviceRGB"), "CS1", indexed(n("/CS0"))), "XObject", d(
		"Fm0", form(indexedFill, d("ColorSpace", spaces("CS0", n("DeviceCMYK"))))))
	page("/Fm0 Do\n", "ColorSpace", spaces("CS0", n("DeviceRGB")), "XObject", d(
		"Im0", image(indexed(n("/CS0")), false),
		"Fm0", form("/Im0 Do\n", d("ColorSpace", spaces("CS0", n("DeviceCMYK"))))))
	page("/Fm0 Do\n", "ColorSpace", spaces("CS0", n("DeviceRGB")), "XObject", d(
		"Fm0", form("/Sh0 sh\n", d("Shading", d("Sh0", shading(n("/CS0")))))))
	page("/Fm0 Do\n", "ColorSpace", spaces("DefaultRGB", a(n("ICCBased"), abstractRGB())),
		"Shading", d("Sh0", shading(n("DeviceRGB"))),
		"XObject", d("Fm0", form("/Sh0 sh\n", d("ExtGState", d()))))
	page("/Fm0 Do\n", "ColorSpace", spaces("DefaultRGB", a(n("ICCBased"), abstractRGB())),
		"XObject", d("Im0", image(indexed(n("DeviceRGB")), false),
			"Fm0", form("/Im0 Do\n", d("ExtGState", d()))))
	sharedTint := pdf.Indirect(tint(4))
	page("/CS1 cs 1 scn "+fill+"/CS2 cs 1 scn "+fill, "ColorSpace", spaces(
		"CS0", n("DeviceCMYK"),
		"CS1", a(n("Separation"), n("/SpotTwin"), n("/CS0"), sharedTint),
		"CS2", a(n("Separation"), n("/SpotTwin"), n("DeviceCMYK"), sharedTint),
	))

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "1.7")
}
