package build

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("usebpc_4", usebpc4) }

// usebpc4 builds usebpc_4.pdf: ICC colours under each /UseBlackPtComp value, PDF/A-4.
//
// PDF 2.0 adds the ExtGState entry /UseBlackPtComp (ISO 32000-2, Table 57):
// /ON, /OFF, or /Default, its initial value, which leaves black point
// compensation to the reader. The two corpus files that carry it never reach an
// ICC profile under /OFF (pdf20 `PDF_2.0_image_with_BPC` is CalRGB under /ON,
// pdfjs `issue17794` states /Default), so this fixture draws ICC colours whose
// profiles have a black lighter than sRGB's under all three values.
//
// Three 100 x 200 pages, /Default (no gs), /ON and /OFF in that order, paint
// the same three things, each 80 points wide from x = 10:
//
// | Row       | Draws                                                             |
// | --------- | ----------------------------------------------------------------- |
// | y 140-190 | `Gy`, an 8 x 8 grey ramp, samples 0 to 224, under profile `grey`  |
// | y 80-130  | `Rg`, an 8 x 8 RGB ramp from black, under profile `rgb`           |
// | y 20-70   | a fill of `0 sc` in `[/ICCBased grey]`                            |
//
// Both profiles are version 2.1 display profiles whose tone curves start at a
// luminance of 1/8 (a two-entry sampled curve from 0x2000), so their black reads
// L* 42. Black point compensation (ISO 18619) maps it onto sRGB's black;
// without it the black draws as a mid grey. One state per page keeps the pages
// that compensate apart from the one that does not, so each is judged alone.
// /OFF comes last, on page 3: the corpus gate's redact-render scenarios redact
// pages 1 and 2 and judge a blanked image by its darkness over black, which a
// blank under /OFF, drawn L* 42, cannot reach (the library's pixel check reads
// it compensated).
//
// See ../FIXTURES.md for the authoritative expected firing set.
func usebpc4() ([]byte, error) {
	pdf := pdfgen.New()

	// liftedCurve is a curveType (ICC.1:2001-04, 6.5.3) of two entries:
	// linear from 1/8.
	liftedCurve := func() []byte {
		out := append([]byte("curv"), make([]byte, 4)...)
		out = binary.BigEndian.AppendUint32(out, 2)
		out = binary.BigEndian.AppendUint16(out, 0x2000)
		return binary.BigEndian.AppendUint16(out, 0xFFFF)
	}

	greyProfile := func() []byte {
		tags := make([]iccTag, 0, 1+len(iccCommonTags)+1)
		tags = append(tags, iccTag{"desc", iccTextDescription("litho lifted grey")})
		tags = append(tags, iccCommonTags...)
		tags = append(tags, iccTag{"kTRC", liftedCurve()})
		return iccProfile("mntr", "GRAY", "XYZ ", tags)
	}

	rgbProfile := func() []byte {
		tags := make([]iccTag, 0, 1+len(iccCommonTags)+len(iccPrimaries)+3)
		tags = append(tags, iccTag{"desc", iccTextDescription("litho lifted RGB")})
		tags = append(tags, iccCommonTags...)
		for _, p := range iccPrimaries {
			tags = append(tags, iccTag{p.Sig, iccXYZ(p.XYZ...)})
		}
		for _, sig := range []string{"rTRC", "gTRC", "bTRC"} {
			tags = append(tags, iccTag{sig, liftedCurve()})
		}
		return iccProfile("mntr", "RGB ", "XYZ ", tags)
	}

	profileStream := func(data []byte, components int) *pdfgen.Stream {
		stream := pdf.Stream(data)
		stream.Set("N", components)
		return pdf.MakeIndirect(stream).(*pdfgen.Stream)
	}

	grey := pdf.MakeIndirect(a(n("ICCBased"), profileStream(greyProfile(), 1)))
	rgb := pdf.MakeIndirect(a(n("ICCBased"), profileStream(rgbProfile(), 3)))

	// Eight rows, so a region over part of an image covers whole samples.
	const rows = 8

	// image is an image of rows identical rows of samples row.
	image := func(space pdfgen.Object, row []byte, components int) *pdfgen.Stream {
		stream := pdf.Stream(bytes.Repeat(row, rows))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Image"))
		stream.Set("Width", len(row)/components)
		stream.Set("Height", rows)
		stream.Set("BitsPerComponent", 8)
		stream.Set("ColorSpace", space)
		return pdf.MakeIndirect(stream).(*pdfgen.Stream)
	}

	var ramp []byte
	for i := 0; i < 8; i++ {
		ramp = append(ramp, byte(32*i))
	}
	greyImage := image(grey, ramp, 1)
	// A ramp on red alone, from the neutral black of its first sample.
	red := make([]byte, 0, 3*len(ramp))
	for _, v := range ramp {
		red = append(red, v, 0, 0)
	}
	rgbImage := image(rgb, red, 3)

	states := d(
		"Off", d("Type", n("ExtGState"), "UseBlackPtComp", n("OFF")),
		"On", d("Type", n("ExtGState"), "UseBlackPtComp", n("ON")),
	)
	for _, gs := range []string{"", "/On gs ", "/Off gs "} {
		content := fmt.Sprintf("q %s"+
			"q 80 0 0 50 10 140 cm /Gy Do Q "+
			"q 80 0 0 50 10 80 cm /Rg Do Q "+
			"/G cs 0 sc 10 20 80 50 re f Q\n", gs)
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 100, 200),
			"Resources", d(
				"ColorSpace", d("G", grey),
				"XObject", d("Gy", greyImage, "Rg", rgbImage),
				"ExtGState", states,
			),
			"Contents", pdf.Stream([]byte(content)),
		))
		pdf.AddPage(page)
	}

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 4, Rev: 2020}))
	return save(pdf, "2.0")
}
