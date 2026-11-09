package build

import (
	"encoding/binary"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("blendcs_4", blendcs4) }

// blendcs4 builds blendcs_4.pdf: which transparency blending spaces cover device colour.
//
// ISO 19005-4, 6.2.4.3 lets DeviceRGB and DeviceCMYK stand when the current
// transparency blending space is a device-independent space of the same model,
// where ISO 19005-2 asks for an output intent alone. veraPDF tests the blending
// space's type: an ICC profile whose header names `RGB `, or a CalRGB space, for
// DeviceRGB (t2), an ICC profile naming `CMYK` for DeviceCMYK (t3). Its DeviceGray
// test (t4) reads the output intent and nothing else, so no blending space covers
// DeviceGray. It reads a group's `/CS` with no resources, so a `/CS` naming a
// `/ColorSpace` entry is no blending space at all. No file of any corpus paints a
// device colour inside an isolated group over a gray, CalGray or Lab space, or
// under a group whose `/CS` is a name.
//
// The first page's own group names `/CS0`, an ICCBased space over an RGB profile
// the page's resources define. The second page paints six forms, each with an
// isolated group:
//
// | Stream      | Group `/CS`                          | Paints    | 6.2.4.3 |
// | ----------- | ------------------------------------ | --------- | ------- |
// | page 1      | `/CS0`, a name                       | `rg`, `f` | `/2` x2 |
// | `FmName`    | `/CS1`, a name of its own entry      | `rg`, `f` | `/2` x2 |
// | `FmGray`    | `[/ICCBased]` over a `GRAY` profile  | `g`, `f`  | `/4` x2 |
// | `FmCalGray` | `[/CalGray]`                         | `g`, `f`  | `/4` x2 |
// | `FmLab`     | `[/Lab]`                             | `g`, `f`  | `/4` x2 |
// | `FmRGB`     | `[/ICCBased]` over an `RGB ` profile | `rg`, `f` | silent  |
// | `FmCalRGB`  | `[/CalRGB]`                          | `rg`, `f` | silent  |
//
// The last two are the controls: each covers the DeviceRGB its form paints, so a
// checker that reads no blending space at all reports them. They are painted
// last, after every form whose group covers nothing.
//
// The groups sit on two pages because ISO 19005-4, 6.2.9 t2 asks a page that uses
// transparency for a blending space, which a page's own group does not make it
// use: the second page's forms use transparency and it has no group, which both
// tools fail once, and the first page paints nothing transparent, so its named
// group owes the rule nothing.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func blendcs4() ([]byte, error) {
	pdf := pdfgen.New()

	const iccHeaderSize = 128
	const iccVersion4 = 0x04200000
	const fill = "0 0 10 10 re f\n"
	white := d("WhitePoint", a(0.9505, 1, 1.089))

	// iccProfile is a display profile whose header names colourSpace, and no
	// tags.
	iccProfile := func(colourSpace string, components int) *pdfgen.Stream {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize)
		copy(header[0:4], iccU32(nil, iccHeaderSize+len(tagTable)))
		binary.BigEndian.PutUint32(header[8:12], iccVersion4)
		copy(header[12:16], "mntr")
		copy(header[16:20], colourSpace)
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		binary.BigEndian.PutUint32(header[68:72], 0x0000F6D6)
		binary.BigEndian.PutUint32(header[72:76], 0x00010000)
		binary.BigEndian.PutUint32(header[76:80], 0x0000D32D)
		stream := pdf.Stream(append(header, tagTable...))
		stream.Set("N", components)
		pdf.MakeIndirect(stream)
		return stream
	}

	rgbProfile := iccProfile("RGB ", 3)
	grayProfile := iccProfile("GRAY", 1)

	// group is a transparency group dictionary blending in colourSpace.
	group := func(colourSpace obj, isolated bool) *pdfgen.Dict {
		g := d("Type", n("Group"), "S", n("Transparency"), "CS", colourSpace)
		if isolated {
			g.Set("I", true)
		}
		return g
	}

	// form is a form XObject with an isolated transparency group.
	form := func(colourSpace obj, paint string, resources *pdfgen.Dict) *pdfgen.Stream {
		stream := pdf.Stream([]byte(paint + fill))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 10, 10))
		if resources != nil {
			stream.Set("Resources", resources)
		} else {
			stream.Set("Resources", d())
		}
		stream.Set("Group", group(colourSpace, true))
		pdf.MakeIndirect(stream)
		return stream
	}

	const rg = "1 0 0 rg "
	const g = "0.5 g "
	type namedForm struct {
		name string
		form *pdfgen.Stream
	}
	forms := []namedForm{
		{"FmName", form(n("CS1"), rg, d("ColorSpace", d("CS1", a(n("ICCBased"), rgbProfile))))},
		{"FmGray", form(a(n("ICCBased"), grayProfile), g, nil)},
		{"FmCalGray", form(a(n("CalGray"), white), g, nil)},
		{"FmLab", form(a(n("Lab"), white), g, nil)},
		{"FmRGB", form(a(n("ICCBased"), rgbProfile), rg, nil)},
		{"FmCalRGB", form(a(n("CalRGB"), white), rg, nil)},
	}

	grouped := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d("ColorSpace", d("CS0", a(n("ICCBased"), rgbProfile))),
		"Group", group(n("CS0"), false),
		"Contents", pdf.Stream([]byte("q "+rg+fill+"Q\n")),
	))
	xobjects := d()
	var paint []byte
	for _, f := range forms {
		xobjects.Set(f.name, f.form)
		paint = fmt.Appendf(paint, "q /%s Do Q\n", f.name)
	}
	painting := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d("XObject", xobjects),
		"Contents", pdf.Stream(paint),
	))
	pdf.AddPage(grouped)
	pdf.AddPage(painting)

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "2.0")
}
