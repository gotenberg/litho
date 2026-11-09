package build

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("oirgb_2b", oirgb2b) }

// oirgb2b builds oirgb_2b.pdf: device colour images under an RGB output intent.
//
// No corpus file reads DeviceRGB through an output intent that changes its
// colours: every RGB destination profile the corpora carry is sRGB, which gives
// each colour back as it came. Here the document's output intent holds an RGB
// monitor profile of the sRGB colorants over a 1.8 gamma, so a DeviceRGB colour
// it characterises is lighter than the same numbers read as sRGB. The litho
// corpus runs the Writer mutations, so the redaction of page 1 decodes every
// image here.
//
// | Image  | Colour                     | Reading                         |
// | ------ | -------------------------- | ------------------------------- |
// | `RGB`  | `/DeviceRGB`               | through the output intent       |
// | `Idx`  | `[/Indexed /DeviceRGB ..]` | its base through the intent     |
// | `Gray` | `/DeviceGray`              | as sRGB grey: the intent is RGB |
//
// ISO 32000-1, 14.11.5 has an output intent's destination profile characterise
// the device the document was prepared for, and ISO 19005-2, 6.2.4.3 admits
// DeviceRGB into a PDF/A file on the strength of an RGB destination profile and
// DeviceGray on the strength of any output intent.
//
// The document has no /Metadata, so the metadata clauses answer from elsewhere.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func oirgb2b() ([]byte, error) {
	pdf := pdfgen.New()

	be := binary.BigEndian

	// s15 is an s15Fixed16Number (ICC.1:2001-04, 5.3.5).
	s15 := func(v float64) []byte {
		return be.AppendUint32(nil, uint32(int32(math.RoundToEven(v*65536)))) //nolint:gosec // s15Fixed16 is a signed 32-bit field.
	}

	xyz := func(x, y, z float64) []byte {
		return slices.Concat([]byte("XYZ \x00\x00\x00\x00"), s15(x), s15(y), s15(z))
	}

	textType := func(s string) []byte {
		return slices.Concat([]byte("text\x00\x00\x00\x00"), []byte(s), []byte{0})
	}

	// desc is a version 2 textDescriptionType (ICC.1:2001-04, 6.5.17).
	desc := func(s string) []byte {
		asciiPart := append([]byte(s), 0)
		out := []byte("desc\x00\x00\x00\x00")
		out = be.AppendUint32(out, uint32(len(asciiPart))) //nolint:gosec // fixture lengths fit.
		out = append(out, asciiPart...)
		out = be.AppendUint32(out, 0)
		out = be.AppendUint32(out, 0)
		out = be.AppendUint16(out, 0)
		out = append(out, 0)
		return append(out, make([]byte, 67)...)
	}

	// gamma is a curveType holding one gamma, as u8Fixed8Number (6.5.3).
	gamma := func(g float64) []byte {
		out := be.AppendUint32([]byte("curv\x00\x00\x00\x00"), 1)
		return be.AppendUint16(out, uint16(math.RoundToEven(g*256)))
	}

	// profile is a version 2.1 RGB monitor profile: sRGB's D50 colorants, 1.8
	// gamma.
	profile := func() []byte {
		tags := []iccTag{
			{"desc", desc("litho RGB gamma 1.8")},
			{"cprt", textType("No copyright, use freely")},
			{"wtpt", xyz(0.9642, 1.0, 0.8249)},
			{"rXYZ", xyz(0.4361, 0.2225, 0.0139)},
			{"gXYZ", xyz(0.3851, 0.7169, 0.0971)},
			{"bXYZ", xyz(0.1431, 0.0606, 0.7141)},
			{"rTRC", gamma(1.8)},
			{"gTRC", gamma(1.8)},
			{"bTRC", gamma(1.8)},
		}
		offset := 128 + 4 + 12*len(tags)
		table, body := be.AppendUint32(nil, uint32(len(tags))), []byte{} //nolint:gosec // fixture lengths fit.
		for _, t := range tags {
			data := t.Data
			for len(data)%4 != 0 {
				data = append(data, 0)
			}
			table = append(table, t.Sig...)
			table = be.AppendUint32(table, uint32(offset+len(body))) //nolint:gosec // fixture lengths fit.
			table = be.AppendUint32(table, uint32(len(data)))        //nolint:gosec // fixture lengths fit.
			body = append(body, data...)
		}
		header := make([]byte, 128)
		be.PutUint32(header[8:12], 0x02100000)
		copy(header[12:16], "mntr")
		copy(header[16:20], "RGB ")
		copy(header[20:24], "XYZ ")
		for i, v := range []uint16{2026, 1, 1, 0, 0, 0} {
			be.PutUint16(header[24+2*i:], v)
		}
		copy(header[36:40], "acsp")
		copy(header[68:80], slices.Concat(s15(0.9642), s15(1.0), s15(0.8249)))
		data := slices.Concat(header, table, body)
		return append(be.AppendUint32(nil, uint32(len(data))), data[4:]...) //nolint:gosec // fixture lengths fit.
	}

	image := func(samples []byte, width int, colorSpace any) *pdfgen.Stream {
		stream := pdf.Stream(samples)
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Image"))
		stream.Set("Width", width)
		stream.Set("Height", 1)
		stream.Set("ColorSpace", colorSpace)
		stream.Set("BitsPerComponent", 8)
		pdf.MakeIndirect(stream)
		return stream
	}

	var ramp []byte
	for v := 0; v < 256; v += 32 {
		ramp = append(ramp, byte(v))
	}
	rgb := make([]byte, 0, 3*len(ramp))
	for _, v := range ramp {
		rgb = append(rgb, v, 255-v, 128)
	}
	type namedImage struct {
		name string
		ref  *pdfgen.Stream
	}
	images := []namedImage{
		{"RGB", image(rgb, len(ramp), n("DeviceRGB"))},
		{"Idx", image([]byte{0, 1, 2, 3}, 4,
			a(n("Indexed"), n("DeviceRGB"), 3, str("\xc8\x50\x28\x28\xc8\x50\x80\x80\x80\x00\x00\x00")))},
		{"Gray", image(ramp, len(ramp), n("DeviceGray"))},
	}

	xobjects := d()
	lines := make([]string, 0, len(images))
	y := 700
	for _, im := range images {
		xobjects.Set(im.name, im.ref)
		lines = append(lines, fmt.Sprintf("q 80 0 0 40 50 %d cm /%s Do Q", y, im.name))
		y -= 60
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("XObject", xobjects),
		"Contents", pdf.Stream([]byte(strings.Join(lines, "\n")+"\n"))))
	pdf.AddPage(page)

	dest := pdf.Stream(profile())
	dest.Set("N", 3)
	pdf.Root.Set("OutputIntents", a(pdf.Indirect(d(
		"Type", n("OutputIntent"),
		"S", n("GTS_PDFA1"),
		"OutputConditionIdentifier", text("litho RGB gamma 1.8"),
		"DestOutputProfile", pdf.MakeIndirect(dest),
	))))

	// Deliberately no /Metadata.
	return save(pdf, "1.7")
}
