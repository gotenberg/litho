package build

import (
	"encoding/ascii85"
	"encoding/hex"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("undecodable_2b", undecodable2b) }

// undecodable2b builds undecodable_2b.pdf: content streams veraPDF reads no operator from.
//
// veraPDF 1.30 decodes a stream through five filters alone, FlateDecode,
// ASCIIHexDecode, ASCII85Decode, LZWDecode and RunLengthDecode, and reads no
// operator from a content stream whose chain ends with any other name: the
// abbreviations of inline images, the image codecs, BrotliDecode, an unknown
// name. A name it does not decode earlier in the chain passes its input on
// unchanged. Nor does it read a /Contents that is neither a stream nor an array.
// litho read the operators of streams in filters it decodes itself, judged every
// font of a page whose content it could not read, and read a Type 3 glyph's width
// from a procedure veraPDF cannot read either. The corpus files that reach the
// abbreviations, BrotliDecode, an image codec or a /Contents that is no stream
// carry no compliance golden, and no corpus file writes a filter veraPDF does not
// decode before the last of a chain, or parameters that do not fit their chain.
//
// Every page paints a probe wherever a reader takes its operators, each with a
// font of its own: it shows text in an unembedded font, fills in DeviceRGB with
// no OutputIntent, and runs an undefined operator. Page 1 is the control.
//
// | Page | Where the probe sits                                     | Read |
// | ---- | -------------------------------------------------------- | ---- |
// | 1    | the page, no filter                                      | yes  |
// | 2    | the page, `[/Foo /FlateDecode]` over Flate data          | yes  |
// | 3    | the page, `[/FlateDecode /Foo]` over Flate data          | no   |
// | 4    | the page, `/Fl` over Flate data                          | no   |
// | 5    | the page, `/DCTDecode` over the probe itself             | no   |
// | 6    | the page, `/BrotliDecode` over Brotli data               | no   |
// | 7    | none: `/Contents` is a dictionary                        | no   |
// | 8    | a form XObject, `/Fl` over Flate data                    | no   |
// | 9    | a tiling pattern cell, `/AHx` over hex digits            | no   |
// | 10   | an annotation appearance, `/A85` over base-85            | no   |
// | 12   | the page, `/FlateDecode` with two parameter dictionaries | no   |
// | 13   | the page, `/FlateDecode` with the parameters `[5]`       | no   |
//
// Page 2 gives `/Foo` a PNG predictor and `/FlateDecode` null, so the chain is
// read without `/Foo` only if the parameters leave with it. veraPDF fails to
// decode a stream whose parameters count other than its filters, or hold an
// entry that is neither a dictionary nor null.
//
// Page 11 shows the one glyph of a Type 3 font whose procedure is `/AHx` over
// `500 0 d0`, under a `/Widths` of 500. veraPDF cannot read the procedure, takes
// the missing width of 0, and fails the glyph width.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func undecodable2b() ([]byte, error) {
	probe := []byte("BT /F1 12 Tf 10 10 Td (A) Tj ET 1 0 0 rg 0 0 5 5 re f zzfoo\n")

	// brotliStored encodes data as a Brotli stream of one uncompressed meta-block.
	//
	// RFC 7932: a 16-bit window (9.1), a meta-block that is not last, whose
	// length takes four nibbles and whose data is uncompressed (9.2), and an
	// empty last meta-block.
	brotliStored := func(data []byte) []byte {
		var out []byte
		acc := 0
		nbits := 0

		put := func(value, width int) {
			acc |= value << nbits
			nbits += width
			for nbits >= 8 {
				out = append(out, byte(acc&0xFF))
				acc >>= 8
				nbits -= 8
			}
		}

		align := func() {
			if nbits > 0 {
				out = append(out, byte(acc&0xFF))
			}
			acc = 0
			nbits = 0
		}

		if len(data) == 0 || len(data) > 1<<16 {
			panic("undecodable_2b: brotliStored takes 1 to 65536 bytes")
		}
		put(0, 1)            // WBITS = 16
		put(0, 1)            // ISLAST = 0
		put(0, 2)            // MNIBBLES = 4
		put(len(data)-1, 16) // MLEN - 1
		put(1, 1)            // ISUNCOMPRESSED
		align()
		out = append(out, data...)
		put(1, 1) // ISLAST
		put(1, 1) // ISLASTEMPTY
		align()
		return out
	}

	pdf := pdfgen.New()

	probeFont := func() *pdfgen.Dict {
		return pdf.Indirect(d("Type", n("Font"), "Subtype", n("Type1"), "BaseFont", n("/Helvetica")))
	}

	filtered := func(data []byte, filters any) *pdfgen.Stream {
		stream := pdf.Stream(data)
		stream.Set("Filter", filters)
		return stream
	}

	addPage := func(contents any, resources *pdfgen.Dict, extra ...any) {
		page := pdf.Indirect(d(append([]any{
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", resources,
			"Contents", contents,
		}, extra...)...))
		pdf.AddPage(page)
	}

	probePage := func(contents any) {
		addPage(contents, d("Font", d("F1", probeFont())))
	}

	flate := zlibCompress(probe)

	probePage(pdf.Stream(probe))
	fooFlate := filtered(flate, a(n("/Foo"), n("FlateDecode")))
	fooFlate.Set("DecodeParms", a(d("Predictor", 12, "Columns", 4), nil))
	probePage(fooFlate)
	probePage(filtered(flate, a(n("FlateDecode"), n("/Foo"))))
	probePage(filtered(flate, n("/Fl")))
	probePage(filtered(probe, n("DCTDecode")))
	probePage(filtered(brotliStored(probe), n("/BrotliDecode")))
	probePage(pdf.Indirect(d("NotAStream", true)))

	form := filtered(flate, n("/Fl"))
	form.Set("Type", n("XObject"))
	form.Set("Subtype", n("Form"))
	form.Set("BBox", a(0, 0, 100, 100))
	form.Set("Resources", d("Font", d("F1", probeFont())))
	addPage(pdf.Stream([]byte("/Fm0 Do\n")), d("XObject", d("Fm0", form)))

	cell := filtered([]byte(hex.EncodeToString(probe)+">"), n("/AHx"))
	cell.Set("Type", n("Pattern"))
	cell.Set("PatternType", 1)
	cell.Set("PaintType", 1)
	cell.Set("TilingType", 1)
	cell.Set("BBox", a(0, 0, 10, 10))
	cell.Set("XStep", 10)
	cell.Set("YStep", 10)
	cell.Set("Resources", d("Font", d("F1", probeFont())))
	addPage(
		pdf.Stream([]byte("/Pattern cs /P0 scn 0 0 100 100 re f\n")),
		d("Pattern", d("P0", cell)),
	)

	a85 := make([]byte, ascii85.MaxEncodedLen(len(probe)))
	a85 = a85[:ascii85.Encode(a85, probe)]
	appearance := filtered(append(a85, "~>"...), n("/A85"))
	appearance.Set("Type", n("XObject"))
	appearance.Set("Subtype", n("Form"))
	appearance.Set("BBox", a(0, 0, 50, 50))
	appearance.Set("Resources", d("Font", d("F1", probeFont())))
	annot := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(10, 10, 60, 60),
		"F", 4,
		"AP", d("N", appearance),
	))
	addPage(pdf.Stream([]byte("0 0 m\n")), d(), "Annots", a(annot))

	glyph := filtered([]byte(hex.EncodeToString([]byte("500 0 d0 0 0 500 500 re f"))+">"), n("/AHx"))
	type3 := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 500, 500),
		"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
		"CharProcs", d("a", pdf.MakeIndirect(glyph)),
		"Encoding", d("Type", n("Encoding"), "Differences", a(97, n("/a"))),
		"FirstChar", 97,
		"LastChar", 97,
		"Widths", a(500),
		"Resources", d(),
	))
	addPage(
		pdf.Stream([]byte("BT /T3 12 Tf 10 10 Td (a) Tj ET\n")),
		d("Font", d("T3", type3)),
	)

	twoParms := filtered(flate, n("FlateDecode"))
	twoParms.Set("DecodeParms", a(d(), d()))
	probePage(twoParms)
	badParms := filtered(flate, n("FlateDecode"))
	badParms.Set("DecodeParms", a(5))
	probePage(badParms)

	// Deliberately no /Metadata.
	return save(pdf, "1.7")
}
