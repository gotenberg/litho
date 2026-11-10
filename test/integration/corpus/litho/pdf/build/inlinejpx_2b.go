package build

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen/zdeflate"
)

func init() { register("inlinejpx_2b", inlinejpx2b) }

// inlinejpx2b builds inlinejpx_2b.pdf: inline images that apply JPXDecode or JBIG2Decode,
// which no part of ISO 19005 after the first permits, for the check that states
// the inline image filter rule and the repair that moves such an image out of
// its content stream.
//
// ISO 19005-2/3, 6.1.10 and ISO 19005-4, 6.1.9 forbid an inline image filter
// that is LZW or outside the filters ISO 32000-2, 8.9.7, Table 92 abbreviates
// (AHx, A85, LZW, Fl, RL, CCF and DCT, under either name). JPXDecode and
// JBIG2Decode have no abbreviation, and ISO 32000-1, 7.4.9 applies the JPEG 2000
// filter to image XObjects alone. ISO 19005-1, 6.1.10 names LZWDecode
// alone, so part 1 judges none of these images by that rule. veraPDF judges each
// inline image as an image of its own:
//
//	page   P1 writes /F /JPXDecode with /CS /RGB and /BPC 8. P2 writes
//	       /F /JPXDecode with no /CS and no /BPC, the colour space read from
//	       the JPEG 2000 data (ISO 32000-1, 8.9.5, Table 89). P3 writes
//	       /F /JBIG2Decode, a 1-bit DeviceGray image. P4 writes
//	       /F [/AHx /JBIG2Decode] as an image mask. P5 writes /F /CCF and P6
//	       /F /Fl, which every part permits, and are the controls.
//	Fm     a form XObject the page draws, whose image writes /Filter /JPXDecode,
//	       the full key, with /ColorSpace /DeviceRGB.
//
// The JPEG 2000 data is the three-component codestream of jp2.go in a JP2
// container with one sRGB colour specification. The JBIG2 data is an embedded
// stream (ISO 32000-1, 7.4.7) of a page information segment, an immediate
// lossless generic region coded with MMR (ITU-T T.88, 6.2.6) and an end of page
// segment, the same 8x8 diagonal band the CCF control codes with Group 4. No
// image refers to JBIG2Globals.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func inlinejpx2b() ([]byte, error) {
	pdf := pdfgen.New()

	// The 8x8 band, ITU-T T.6 (Group 4) coded with the band black, ending with
	// EOFB.
	g4, _ := hex.DecodeString("26bdbb76eddbb64d5380080080")
	const size = 8

	// segment is one JBIG2 segment, ITU-T T.88, 7.2: a header and its data.
	segment := func(number uint32, segType byte, data []byte) []byte {
		const page = 1
		out := binary.BigEndian.AppendUint32(nil, number)
		out = append(out, segType, 0, page)
		out = binary.BigEndian.AppendUint32(out, uint32(len(data))) //nolint:gosec // small segments.
		return append(out, data...)
	}

	// jbig2 is an embedded JBIG2 stream: page information (type 48), an
	// immediate lossless generic region (type 39) and end of page (type 49).
	jbig2 := func(mmr []byte) []byte {
		var pageInfo []byte
		for _, v := range []uint32{size, size, 0, 0} {
			pageInfo = binary.BigEndian.AppendUint32(pageInfo, v)
		}
		pageInfo = append(pageInfo, 0, 0, 0)
		var region []byte
		for _, v := range []uint32{size, size, 0, 0} {
			region = binary.BigEndian.AppendUint32(region, v)
		}
		region = append(region, 0, 1)
		region = append(region, mmr...)
		return bytes.Join([][]byte{segment(0, 48, pageInfo), segment(1, 39, region), segment(2, 49, nil)}, nil)
	}

	jpx := jp2Container(jp2Three, 3, [][]byte{jp2Colr(1, 1, jp2EnumSRGB)}, nil)
	jb2 := jbig2(g4)
	grey := bytes.Repeat([]byte{0x00, 0x40, 0x80, 0xFF, 0xFF, 0x80, 0x40, 0x00}, 8)

	var inlineErr error
	inline := func(x int, entries string, data []byte) []byte {
		// A reader finds the end of inline image data by scanning for EI, so no
		// planted payload may hold one.
		if bytes.Contains(data, []byte("EI")) {
			inlineErr = fmt.Errorf("inline image %s holds EI", entries)
		}
		out := fmt.Appendf(nil, "q 40 0 0 40 %d 600 cm BI /W 8 /H 8 %s ID ", x, entries)
		out = append(out, data...)
		return append(out, "\nEI Q\n"...)
	}

	fm := pdf.Stream(inline(0, "/ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /JPXDecode", jpx))
	fm.Set("Type", n("XObject"))
	fm.Set("Subtype", n("Form"))
	fm.Set("BBox", a(0, 0, 100, 700))
	fm.Set("Resources", d())
	pdf.MakeIndirect(fm)

	content := pdf.Stream(bytes.Join([][]byte{
		inline(10, "/CS /RGB /BPC 8 /F /JPXDecode", jpx),
		inline(60, "/F /JPXDecode", jpx),
		inline(110, "/CS /G /BPC 1 /F /JBIG2Decode", jb2),
		inline(160, "/IM true /F [/AHx /JBIG2Decode]", []byte(strings.ToUpper(hex.EncodeToString(jb2))+">")),
		inline(210, "/CS /G /BPC 1 /F /CCF /DP << /K -1 /Columns 8 >>", g4),
		inline(260, "/CS /G /BPC 8 /F /Fl", zdeflate.Compress(grey, 9)),
		[]byte("q 1 0 0 1 300 0 cm /Fm Do Q\n"),
	}, nil))
	pdf.MakeIndirect(content)
	if inlineErr != nil {
		return nil, inlineErr
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("XObject", d("Fm", fm)),
		"Contents", content,
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.7")
}
