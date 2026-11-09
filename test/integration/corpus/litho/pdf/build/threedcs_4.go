package build

import (
	"encoding/binary"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("threedcs_4", threedcs4) }

// threedcs4 builds threedcs_4.pdf: the colour space of a 3D stream, as veraPDF builds it.
//
// A 3D annotation's `/3DD` holds a 3D stream, or a 3D reference dictionary whose
// own `/3DD` does (ISO 32000-2, 13.6.3). veraPDF reads the colour space of a
// stream whose `/Type` is `/3D` from its `/ColorSpace` alone, never from its
// `/Resources` (GFPD3DStream). A name, alone or in a one-element array, reads as
// `cs` would select it in the page's resources, so the page's Default colour
// space replaces a device family and a `/ColorSpace` entry is read in its place.
// Any other array is read with no resources at all: no Default replaces a family
// inside it and a name inside it reads as no colour space, DeviceGray to the
// `/Indexed` base that holds it. `threed_4e.pdf` plants 3D streams with no colour
// space, and no file of any corpus gives one a colour space.
//
// The page's DefaultRGB is an ICCBased space over an abstract profile, which
// ISO 19005-4, 6.2.4.2 rejects, so a link it reaches answers there, not at the
// DeviceRGB test. Nine 3D annotations sit on the page:
//
// | Annotation | 3D stream `/ColorSpace`                      | Answers       |
// | ---------- | -------------------------------------------- | ------------- |
// | `A1`       | `/DeviceRGB`, which DefaultRGB replaces      | `6.2.4.2/1`   |
// | `A2`       | `[/ICCBased]` over an abstract profile       | `6.2.4.2/1`   |
// | `A3`       | `[/Indexed /DeviceRGB ...]`, not replaced    | `6.2.4.3/2`   |
// | `A4`       | `/CS0`, a Separation over DeviceCMYK         | `6.2.4.3/3`   |
// | `A5`       | `[/Indexed /CS1 ...]`, `/CS1` read as nothing | `6.2.4.3/4`  |
// | `A6`       | `/DeviceGray`, through a 3D reference        | `6.2.4.3/4`   |
// | `A7`       | none; `/DeviceRGB` in its `/Resources`       | nothing       |
// | `A8`       | none; `/DeviceRGB` in a `/3DV` view's stream | nothing       |
// | `A9`       | `/DeviceRGB`, on a stream with no `/Type /3D` | nothing      |
//
// The last three are controls, and each carries DeviceRGB, so a checker reading
// any of them raises the DeviceRGB count above the one `A3` owes. At PDF/A-4 a 3D
// annotation is also a forbidden subtype (ISO 19005-4, 6.3.1), which answers on
// its own object; every annotation carries an appearance, so ISO 19005-4, 6.3.3
// stays silent.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func threedcs4() ([]byte, error) {
	pdf := pdfgen.New()

	const iccHeaderSize = 128
	const iccVersion4 = 0x04200000
	indexTable := str("\x00\x00\x00\xff\xff\xff")

	// abstractRGB is an abstract profile over RGB, which no PDF/A colour space
	// may name.
	abstractRGB := func() *pdfgen.Stream {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize)
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

	// artwork is a U3D 3D stream, ISO 32000-2, 13.6.3, typed /3D unless keys
	// override it.
	artwork := func(keys ...any) *pdfgen.Stream {
		stream := pdf.Stream([]byte("litho corpus 3D artwork"))
		stream.Set("Type", n("/3D"))
		stream.Set("Subtype", n("/U3D"))
		for i := 0; i < len(keys); i += 2 {
			if keys[i+1] == nil {
				stream.Del(keys[i].(string))
			} else {
				stream.Set(keys[i].(string), keys[i+1])
			}
		}
		pdf.MakeIndirect(stream)
		return stream
	}

	// annotation is a 3D annotation with an appearance, so only its stream
	// answers.
	annotation := func(index int, data any, extra ...any) *pdfgen.Dict {
		appearance := pdf.Stream([]byte(""))
		appearance.Set("Type", n("XObject"))
		appearance.Set("Subtype", n("Form"))
		appearance.Set("BBox", a(0, 0, 20, 20))
		annot := d(
			"Type", n("Annot"),
			"Subtype", n("/3D"),
			"Rect", a(10+index*20, 10, 30+index*20, 30),
			"F", 4,
			"Contents", text(fmt.Sprintf("3D artwork A%d", index+1)),
			"AP", d("N", pdf.MakeIndirect(appearance)),
			"3DD", data,
		)
		for i := 0; i < len(extra); i += 2 {
			annot.Set(extra[i].(string), extra[i+1])
		}
		return pdf.Indirect(annot)
	}

	tint := d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0, 0, 0, 0), "C1", a(1, 0, 0, 0), "N", 1)
	annotations := a(
		annotation(0, artwork("ColorSpace", n("DeviceRGB"))),
		annotation(1, artwork("ColorSpace", a(n("ICCBased"), abstractRGB()))),
		annotation(2, artwork("ColorSpace", a(n("Indexed"), n("DeviceRGB"), 1, indexTable))),
		annotation(3, artwork("ColorSpace", n("/CS0"))),
		annotation(4, artwork("ColorSpace", a(n("Indexed"), n("/CS1"), 1, indexTable))),
		annotation(5, pdf.Indirect(d("Type", n("/3DRef"), "3DD", artwork("ColorSpace", n("DeviceGray"))))),
		annotation(6, artwork("Resources", d("ColorSpace", d("C", n("DeviceRGB"))))),
		annotation(7, artwork(), "3DV", d("3DD", artwork("ColorSpace", n("DeviceRGB")))),
		annotation(8, artwork("Type", nil, "ColorSpace", n("DeviceRGB"))),
	)

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(
			"ColorSpace", d(
				"DefaultRGB", a(n("ICCBased"), abstractRGB()),
				"CS0", a(n("Separation"), n("/Spot"), n("DeviceCMYK"), tint),
				"CS1", n("DeviceRGB"),
			),
		),
		"Contents", pdf.Stream([]byte("")),
		"Annots", annotations,
	))
	pdf.AddPage(page)

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "2.0")
}
