package build

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("defaultcs_4", defaultcs4) }

// defaultcs4 builds defaultcs_4.pdf: Default colour spaces judged in place of the family.
//
// ISO 32000-1, 8.6.5.6 has the DefaultGray, DefaultRGB and DefaultCMYK entries of
// a /ColorSpace resource dictionary replace the device family they name, so a
// validator judges the Default space wherever a link reaches the family: its ICC
// profile, its own device family, the alternate of a Separation. veraPDF reads
// it as the colour space it is, and reads a Default it cannot parse, a number or
// a reference to no object, as a resource name no dictionary defines, which ISO
// 19005-4, 6.2.2 test 3 fails once per content stream. Every corpus file that
// sets a Default sets an ICCBased, CalRGB or CalGray space whose profile passes,
// and none sets one veraPDF cannot read, so no count reached either reading.
//
// Every page has resources of its own, so each row counts on its own. The ICC
// profiles are abstract (`abst`), which ISO 19005-4, 6.2.4.2 t1 forbids, except
// the control's monitor profile:
//
// | Page | Default colour space                                     | Draws                          | Answers at PDF/A-4       |
// | ---- | -------------------------------------------------------- | ------------------------------ | ------------------------ |
// | 1    | DefaultRGB, a direct ICCBased array                      | `rg`, `f`                      | ICC x2                   |
// | 2    | DefaultRGB, an indirect ICCBased array                   | `rg`, `f`, `rg`, `f`           | ICC x1                   |
// | 3    | DefaultRGB `/DeviceCMYK`                                 | `rg`, `f`                      | DeviceCMYK x2            |
// | 4    | DefaultCMYK, an indirect ICCBased CMYK array             | `k`, `f`, `gs` `/OPM 1`, `f`   | ICC x2, `6.2.4.2/2`      |
// | 5    | DefaultCMYK, a DeviceN over DeviceCMYK                   | `k`, `f`                       | DeviceCMYK x2            |
// | 6    | DefaultCMYK `[/CalCMYK ...]`, read as DeviceCMYK         | `k`, `f`                       | DeviceCMYK x2            |
// | 7    | DefaultRGB `/Indexed` over DeviceGray, DefaultGray ICC   | `rg`, `f`                      | ICC x2                   |
// | 8    | DefaultRGB a Separation over DeviceGray, DefaultGray ICC | `rg`, `f`                      | DeviceGray x2            |
// | 9    | DefaultRGB `/CS0`, an entry holding DeviceGray           | `rg`, `f`                      | ICC x2                   |
// | 10   | DefaultGray ICC, over the initial DeviceGray             | `f`, `f`                       | ICC x2                   |
// | 11   | DefaultRGB ICC, an RGB image painted twice               | `Do`, `Do`                     | ICC x1                   |
// | 12   | DefaultRGB ICC, a page group `/CS /DeviceRGB`            | nothing                        | ICC x1                   |
// | 13   | none; a form's own DefaultRGB ICC                        | the form's `rg`, `f`           | ICC x2                   |
// | 14   | DefaultRGB ICC; a form with no `/Resources`              | the form's `rg`, `f`           | DeviceRGB x2             |
// | 15   | DefaultGray ICC; a form with empty resources             | the form's `f`                 | ICC x1                   |
// | 16   | DefaultRGB over a monitor GRAY profile                   | `rg`, `f`                      | nothing                  |
// | 17   | DefaultRGB `null`, which sets none                       | `cs`, `scn`, `f`               | DeviceRGB x3             |
// | 18   | DefaultRGB `5`                                           | `rg`, `f`                      | t3, DeviceRGB x2         |
// | 19   | DefaultRGB `99 0 R`, no such object                      | `/DeviceRGB cs`, `f`           | t3, DeviceGray x2        |
// | 20   | DefaultGray `5`                                          | nothing                        | t3                       |
// | 21   | an appearance stream's DefaultGray `5`                   | nothing                        | t3                       |
// | 22   | a Type 3 font's DefaultRGB `5`, glyph after `d0`         | the glyph's `rg`, `f`          | t3, DeviceRGB x2, Gray   |
// | 23   | a Type 3 font's DefaultRGB `5`, glyph past `d1`          | the glyph's `rg`, `f`          | Gray                     |
// | 24   | a form's DefaultGray `5`, inheriting the page's fill     | the form's `f`                 | DeviceGray               |
// | 25   | DefaultRGB `5`; a form with empty resources              | the form's `rg`, `f`           | DeviceRGB x2             |
// | 26   | DefaultRGB `5`, an RGB image                             | `Do`                           | t3                       |
// | 27   | none; a form's DefaultRGB `5`                            | the form's `rg`, then `n`      | t3, DeviceRGB            |
//
// ICC is ISO 19005-4, 6.2.4.2 t1, t3 is 6.2.2 t3, and the device families are
// 6.2.4.3 t2 to t4. The DeviceGray on pages 22 and 23 is the text show itself,
// painted in the page's initial DeviceGray.
//
// The readings the rows rest on are veraPDF's, measured one construct per file
// before they were packed. A direct Default is judged at every link and an
// indirect one once for the document. A family the Default itself gives is not
// replaced again (page 8's alternate), while the base of an `/Indexed` Default
// is (page 7), and a name the Default gives reads as the entry it names (page 9).
// A page's Default replaces no family a form, a tiling cell or an appearance
// stream selects (page 14), but a form inherits the page's initial DeviceGray
// and with it the page's DefaultGray (page 15). veraPDF checks no component
// count, so page 16's gray profile under DefaultRGB passes. A Default it cannot
// read leaves `rg` its family and makes `cs` a name no colour space answers,
// which it judges as DeviceGray: page 18 fails DeviceRGB twice and page 19
// DeviceGray twice at every PDF/A level, and t3 exists at PDF/A-4 alone. A glyph
// past `d1` ignores its colour operators (ISO 32000-1, 9.6.5), and a form
// inherits the colour its page selected.
//
// qpdf drops a null dictionary value and cannot write a reference to a missing
// object, so pages 17 and 19 carry placeholder names of the same length, patched
// as bytes after the save, which moves no offset.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func defaultcs4() ([]byte, error) {
	pdf := pdfgen.New()

	const (
		iccHeaderSize       = 128
		iccVersion4         = 0x04200000
		fill                = "0 0 10 10 re f\n"
		nullPlaceholder     = "/LithoNullDefaultRGB"
		danglingPlaceholder = "/LithoDanglingDefault"
	)

	// profile is a header-only ICC profile of the given class over the given
	// space.
	profile := func(deviceClass, space string, nComps int) *pdfgen.Stream {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize)
		binary.BigEndian.PutUint32(header[0:4], uint32(iccHeaderSize+len(tagTable))) //nolint:gosec // fixture profiles are small.
		binary.BigEndian.PutUint32(header[8:12], iccVersion4)
		copy(header[12:16], deviceClass)
		copy(header[16:20], space)
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		stream := pdf.Stream(append(header, tagTable...))
		stream.Set("N", nComps)
		pdf.MakeIndirect(stream)
		return stream
	}

	// icc is a direct ICCBased array over a profile of its own.
	icc := func(space string, nComps int, deviceClass string) *pdfgen.Array {
		return a(n("ICCBased"), profile(deviceClass, space, nComps))
	}

	// tint is a type 2 tint transform onto a space of the given component
	// count.
	tint := func(outputs int) *pdfgen.Dict {
		c0, c1 := a(), a()
		for range outputs {
			c0.Append(0)
			c1.Append(1)
		}
		return d("FunctionType", 2, "Domain", a(0, 1), "C0", c0, "C1", c1, "N", 1)
	}

	rgbImage := func() *pdfgen.Stream {
		image := pdf.Stream([]byte("\x00\x00\x00"))
		image.Set("Type", n("XObject"))
		image.Set("Subtype", n("Image"))
		image.Set("Width", 1)
		image.Set("Height", 1)
		image.Set("ColorSpace", n("DeviceRGB"))
		image.Set("BitsPerComponent", 8)
		pdf.MakeIndirect(image)
		return image
	}

	// form is a Form XObject; resources nil leaves /Resources out altogether.
	form := func(body string, resources *pdfgen.Dict) *pdfgen.Stream {
		stream := pdf.Stream([]byte(body))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 50, 50))
		if resources != nil {
			stream.Set("Resources", resources)
		}
		pdf.MakeIndirect(stream)
		return stream
	}

	// type3 is a Type 3 font whose resources hold a DefaultRGB veraPDF cannot
	// read.
	type3 := func(glyph string) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"FontBBox", a(0, 0, 10, 10),
			"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
			"CharProcs", d("a", pdf.MakeIndirect(pdf.Stream([]byte(glyph)))),
			"Encoding", d("Type", n("Encoding"), "Differences", a(97, n("/a"))),
			"FirstChar", 97,
			"LastChar", 97,
			"Widths", a(1000),
			"Resources", d("ColorSpace", d("DefaultRGB", 5)),
		))
	}

	// page is a page whose /ColorSpace resource holds spaces, painting
	// content. spaces nil leaves /ColorSpace out; resources are the extra resource
	// entries, extra further page entries.
	page := func(spaces []any, content string, resources []any, extra ...any) {
		res := d(resources...)
		if spaces != nil {
			res.Set("ColorSpace", d(spaces...))
		}
		dd := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", res,
			"Contents", pdf.Stream([]byte(content)),
		)
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		pdf.AddPage(pdf.Indirect(dd))
	}

	rg := "1 0 0 rg " + fill
	k := "0 0 0 1 k " + fill

	page([]any{"/DefaultRGB", icc("RGB ", 3, "abst")}, rg, nil)
	page([]any{"/DefaultRGB", pdf.MakeIndirect(icc("RGB ", 3, "abst"))}, rg+"0 1 0 rg "+fill, nil)
	page([]any{"/DefaultRGB", n("DeviceCMYK")}, rg, nil)
	page(
		[]any{"/DefaultCMYK", pdf.MakeIndirect(icc("CMYK", 4, "abst"))},
		k+"/GS1 gs "+fill,
		[]any{"/ExtGState", d("GS1", d("OPM", 1, "op", true))},
	)
	page([]any{"/DefaultCMYK", a(n("DeviceN"), a(n("Cyan"), n("Magenta"), n("Yellow"), n("Black")),
		n("DeviceCMYK"), tint(1))}, k, nil)
	page([]any{"/DefaultCMYK", a(n("/CalCMYK"), d("WhitePoint", a(0.9505, 1, 1.089)))}, k, nil)
	page([]any{
		"/DefaultRGB", a(n("Indexed"), n("DeviceGray"), 1, str("\x00\xff")),
		"/DefaultGray", icc("GRAY", 1, "abst"),
	}, rg, nil)
	page([]any{
		"/DefaultRGB", a(n("Separation"), n("/Spot"), n("DeviceGray"), tint(1)),
		"/DefaultGray", icc("GRAY", 1, "abst"),
	}, rg, nil)
	page([]any{"/DefaultRGB", n("/CS0"), "/CS0", n("DeviceGray"), "/DefaultGray", icc("GRAY", 1, "abst")}, rg, nil)
	page([]any{"/DefaultGray", icc("GRAY", 1, "abst")}, fill+fill, nil)
	page([]any{"/DefaultRGB", icc("RGB ", 3, "abst")}, "/Im0 Do /Im0 Do\n", []any{"/XObject", d("Im0", rgbImage())})
	page([]any{"/DefaultRGB", icc("RGB ", 3, "abst")}, "", nil, "Group", d("S", n("Transparency"), "CS", n("DeviceRGB")))
	page(nil, "/Fm0 Do\n", []any{"/XObject", d(
		"Fm0", form(rg, d("ColorSpace", d("DefaultRGB", icc("RGB ", 3, "abst")))))})
	page([]any{"/DefaultRGB", icc("RGB ", 3, "abst")}, "/Fm0 Do\n", []any{"/XObject", d("Fm0", form(rg, nil))})
	page([]any{"/DefaultGray", icc("GRAY", 1, "abst")}, "/Fm0 Do\n", []any{"/XObject", d("Fm0", form(fill, d()))})
	page([]any{"/DefaultRGB", icc("GRAY", 1, "mntr")}, rg, nil)
	page([]any{"/DefaultRGB", n(nullPlaceholder)}, "/DeviceRGB cs 1 0 0 scn "+fill, nil)

	page([]any{"/DefaultRGB", 5}, rg, nil)
	page([]any{"/DefaultRGB", n(danglingPlaceholder)}, "/DeviceRGB cs "+fill, nil)
	page([]any{"/DefaultGray", 5}, "0 0 10 10 re n\n", nil)
	appearance := form("0 0 10 10 re n\n", d("ColorSpace", d("DefaultGray", 5)))
	page(nil, "", nil, "Annots", a(pdf.Indirect(d(
		"Type", n("Annot"), "Subtype", n("Square"), "Rect", a(0, 0, 50, 50), "F", 4, "AP", d("N", appearance)))))
	page(nil, "BT /T3 12 Tf 10 10 Td (a) Tj ET\n",
		[]any{"/Font", d("T3", type3("1000 0 d0 "+rg))})
	page(nil, "BT /T3 12 Tf 10 10 Td (a) Tj ET\n",
		[]any{"/Font", d("T3", type3("1000 0 0 0 10 10 d1 "+rg))})
	page(nil, "/Fm0 Do\n", []any{"/XObject", d(
		"Fm0", form(fill, d("ColorSpace", d("DefaultGray", 5))))})
	page([]any{"/DefaultRGB", 5}, "/Fm0 Do\n", []any{"/XObject", d("Fm0", form(rg, d()))})
	page([]any{"/DefaultRGB", 5}, "/Im0 Do\n", []any{"/XObject", d("Im0", rgbImage())})
	page(nil, "/Fm0 Do\n", []any{"/XObject", d(
		"Fm0", form("1 0 0 rg 0 0 10 10 re n\n", d("ColorSpace", d("DefaultRGB", 5))))})

	// Deliberately no /Metadata and no output intent.
	raw, err := save(pdf, "2.0")
	if err != nil {
		return nil, err
	}

	for _, p := range [][2]string{{nullPlaceholder, "null"}, {danglingPlaceholder, "99 0 R"}} {
		placeholder, value := []byte(p[0]), p[1]
		if bytes.Count(raw, placeholder) != 1 {
			return nil, fmt.Errorf("defaultcs_4: expected exactly one %q", placeholder)
		}
		raw = bytes.Replace(raw, placeholder, []byte(fmt.Sprintf("%-*s", len(placeholder), value)), 1)
	}
	return raw, nil
}
