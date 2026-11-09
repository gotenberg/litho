package build

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("recolorimg_2b", recolorimg2b) }

// recolorimg2b builds recolorimg_2b.pdf: images whose samples a colour conversion has to
// read exactly, at PDF/A-2b.
//
// Converting an image to another colour space reads its samples as the filters
// leave them, takes each through its /Decode array, and writes them back in the
// target. Each image here carries one construct that conversion must keep, and
// that no other corpus file reaches:
//
//	Im16   a 4x4 ICCBased RGB image at 16 bits a component, whose precision a
//	       conversion to 8 bits would drop (ISO 32000-1, 8.9.5.1, Table 89).
//	ImLab  a 4x4 CIE L*a*b* image with a non-default /Range, whose components
//	       are not on [0, 1] (8.6.5.4, Table 65).
//	ImKey  a 4x4 ICCBased RGB image with a colour-key /Mask over its raw
//	       samples (8.9.6.4), which no longer exist once the samples change.
//	ImMat  a 4x4 ICCBased RGB image whose soft mask carries a /Matte: its
//	       samples are preblended with that colour in the image's own space
//	       (11.6.5.3).
//	ImDec  a 4x4 ICCBased RGB image under /Decode [1 0 1 0 1 0], which the
//	       samples carry only together with it (8.9.5.2).
//
// Every image is in a device-independent space and the page group states its
// blending space, so the file needs no output intent and conforms at PDF/A-2b.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func recolorimg2b() ([]byte, error) {
	pdf := pdfgen.New()

	profile := pdf.Stream(zlibCompress(iccRGBDisplayProfile()))
	profile.Set("N", 3)
	profile.Set("Filter", n("FlateDecode"))
	iccRGB := a(n("ICCBased"), pdf.MakeIndirect(profile))

	const side = 4

	// image's Python default bpc is 8.
	image := func(samples []byte, colourSpace pdfgen.Object, bpc int, extra ...any) *pdfgen.Stream {
		stream := pdf.Stream(zlibCompress(samples))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Image"))
		stream.Set("Width", side)
		stream.Set("Height", side)
		stream.Set("ColorSpace", colourSpace)
		stream.Set("BitsPerComponent", bpc)
		stream.Set("Filter", n("FlateDecode"))
		for i := 0; i < len(extra); i += 2 {
			stream.Set(extra[i].(string), extra[i+1])
		}
		return pdf.MakeIndirect(stream).(*pdfgen.Stream)
	}

	// gradient's Python default scale is 1.
	gradient := func(components, scale int) []byte {
		var out []byte
		for i := 0; i < side*side; i++ {
			for c := 0; c < components; c++ {
				v := (i*37 + c*71) % 256
				out = append(out, byte(v))
				if scale == 2 {
					out = append(out, byte((i*53+c*29)%256))
				}
			}
		}
		return out
	}

	im16 := image(gradient(3, 2), iccRGB, 16)
	imLab := image(
		gradient(3, 1),
		a(
			n("Lab"),
			d(
				"WhitePoint", a(0.9505, 1, 1.089),
				"Range", a(-60, 40, -30, 90),
			),
		),
		8,
	)
	keySamples := gradient(3, 1)
	for _, pixel := range []int{0, 5, 10, 15} {
		copy(keySamples[3*pixel:3*pixel+3], []byte{3, 4, 5})
	}
	imKey := image(keySamples, iccRGB, 8, "Mask", a(0, 8, 0, 8, 0, 8))

	var alpha []byte
	for i := 0; i < side; i++ {
		alpha = append(alpha, 255, 192, 128, 64)
	}
	matteMask := pdf.Stream(zlibCompress(alpha))
	matteMask.Set("Type", n("XObject"))
	matteMask.Set("Subtype", n("Image"))
	matteMask.Set("Width", side)
	matteMask.Set("Height", side)
	matteMask.Set("ColorSpace", n("DeviceGray"))
	matteMask.Set("BitsPerComponent", 8)
	matteMask.Set("Filter", n("FlateDecode"))
	matteMask.Set("Matte", a(1, 1, 1))
	// Preblended with white: c' = m + a(c - m), per ISO 32000-1, 11.6.5.3.
	colour := gradient(3, 1)
	var preblended []byte
	for i := 0; i < side*side; i++ {
		al := float64(alpha[i]) / 255
		for c := 0; c < 3; c++ {
			preblended = append(preblended, byte(math.RoundToEven(255+al*float64(int(colour[3*i+c])-255))))
		}
	}
	imMat := image(preblended, iccRGB, 8, "SMask", pdf.MakeIndirect(matteMask))

	imDec := image(gradient(3, 1), iccRGB, 8, "Decode", a(1, 0, 1, 0, 1, 0))

	names := []string{"Im16", "ImLab", "ImKey", "ImMat", "ImDec"}
	images := []*pdfgen.Stream{im16, imLab, imKey, imMat, imDec}
	body := ""
	xobjects := d()
	for i, name := range names {
		body += fmt.Sprintf("q 100 0 0 100 %d 600 cm /%s Do Q\n", 20+115*i, name)
	}
	contents := pdf.MakeIndirect(pdf.Stream([]byte(body)))
	for i, name := range names {
		xobjects.Set("/"+name, images[i])
	}
	resources := d("XObject", xobjects)
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", resources,
		"Contents", contents,
		// The soft mask makes the page transparent, and with no output
		// intent ISO 19005-2, 6.2.10 t2 wants the page group's blending
		// space stated.
		"Group", d("S", n("Transparency"), "CS", iccRGB),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
