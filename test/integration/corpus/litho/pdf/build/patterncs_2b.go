package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("patterncs_2b", patterncs2b) }

// patterncs2b builds patterncs_2b.pdf: pattern and appearance-stream colour at PDF/A-2b.
//
// ISO 19005-2, 6.2.4.3 test 4 forbids DeviceGray the OutputIntent does not cover.
// A tiling pattern and an annotation appearance stream both carry their own content
// stream, and neither is invoked by a `Do`, so the question of what colour space
// each begins in is decided by neither the page nor the object itself.
//
// Measured against veraPDF, both begin in the default graphics state: a bare `scn`
// inside either paints in the initial DeviceGray, and a colour space the page
// selected before painting the pattern does not carry into it. That is the opposite
// of a Form XObject, which does inherit the state at its `Do`, and it is why
// [formcs_2b] cannot cover these two scopes.
//
// Five objects sit in one file, two offending and three conforming, which is the
// masking probe: an implementation that reads a bare `scn` as proof some space was
// selected misses P0 and A0, and one that scans every declared pattern condemns P2,
// which nothing paints. litho before this fixture did both at once.
//
// | Object | Construct                                        | Rule        |
// | ------ | ------------------------------------------------ | ----------- |
// | P0     | tiling pattern, bare `scn`, painted              | `6.2.4.3/4` |
// | P1     | tiling pattern selecting its own ICCBased space   | silent      |
// | P2     | tiling pattern, DeviceCMYK, never painted         | silent      |
// | A0     | appearance stream, bare `scn`                    | `6.2.4.3/4` |
// | A1     | appearance stream selecting its own space         | silent      |
//
// P2 is the control that matters most, and it is the one litho got wrong: the
// pattern walk had no usage gate, so a pattern present in `/Resources/Pattern` was
// scanned whether or not any `scn` named it. veraPDF is usage-driven here and stays
// silent, so a declared-but-unpainted pattern was a false positive.
//
// P1 and A1 are the over-suppression controls: each selects a permitted ICCBased
// space before painting, so a firing against either means the scope's own `cs` was
// ignored.
//
// The two defects have to stay on different tests or they cancel. An earlier draft
// gave P2 a DeviceGray fill and added a Form XObject painting DeviceCMYK: litho
// before the fix then still reported both tests, missing P0 and A0 but scanning P2,
// so the file agreed with veraPDF for the wrong reasons and proved nothing. P2 now
// paints DeviceCMYK and no conforming object paints any, which leaves test 4
// sourced only by P0 and A0 and test 3 reachable only by scanning P2.
//
// The file declares no OutputIntent: test 4 is `gOutputCS != null`, so an
// OutputIntent of any destination colour space would silence the clause outright.
// The page paints its own text inside the ICCBased scope, because text drawn with
// no colour space selected is itself a DeviceGray use and would report the clause
// from the page, masking every object above.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func patterncs2b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccHeaderSize = 128
	const iccVersion = 0x02100000

	// minimalICC is a structurally minimal ICC profile veraPDF
	// will parse without throwing.
	minimalICC := func(colourSpace string, components int, tag string) *pdfgen.Stream {
		be := binary.BigEndian
		tagTable := be.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize, iccHeaderSize+len(tagTable))
		be.PutUint32(header[0:4], uint32(iccHeaderSize+len(tagTable))) //nolint:gosec // fixture lengths fit.
		copy(header[4:8], tag)
		be.PutUint32(header[8:12], iccVersion)
		copy(header[12:16], "mntr")
		copy(header[16:20], colourSpace)
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		be.PutUint32(header[68:72], 0x0000F6D6)
		be.PutUint32(header[72:76], 0x00010000)
		be.PutUint32(header[76:80], 0x0000D32D)
		stream := pdf.Stream(append(header, tagTable...))
		stream.Set("N", components)
		pdf.MakeIndirect(stream)
		return stream
	}

	iccbased := pdf.MakeIndirect(a(n("ICCBased"), minimalICC("RGB ", 3, "ICCB")))
	ownCS := d("ColorSpace", d("CS0", iccbased))

	// tilingPattern is one coloured tiling pattern carrying its own /Resources.
	tilingPattern := func(content string) *pdfgen.Stream {
		pat := pdf.Stream([]byte(content))
		pat.Set("Type", n("Pattern"))
		pat.Set("PatternType", 1)
		pat.Set("PaintType", 1)
		pat.Set("TilingType", 1)
		pat.Set("BBox", a(0, 0, 20, 20))
		pat.Set("XStep", 20)
		pat.Set("YStep", 20)
		pat.Set("Resources", ownCS)
		pdf.MakeIndirect(pat)
		return pat
	}

	// appearance is one annotation appearance stream, which is a Form XObject.
	appearance := func(content string) *pdfgen.Stream {
		ap := pdf.Stream([]byte(content))
		ap.Set("Type", n("XObject"))
		ap.Set("Subtype", n("Form"))
		ap.Set("BBox", a(0, 0, 20, 20))
		ap.Set("Resources", ownCS)
		pdf.MakeIndirect(ap)
		return ap
	}

	patterns := d(
		// A bare scn: nothing is inherited, so this paints in the initial DeviceGray.
		"P0", tilingPattern("0.5 scn 0 0 20 20 re f\n"),
		// Selects a permitted space of its own before painting.
		"P1", tilingPattern("/CS0 cs 0.2 0.4 0.6 scn 0 0 20 20 re f\n"),
		// Declared only. No scn names it, so it paints nothing and uses no colour.
		// Its fill is DeviceCMYK rather than DeviceGray so that scanning it by
		// mistake reports test 3, which nothing else in this file produces.
		"P2", tilingPattern("0 0 0 1 k 0 0 20 20 re f\n"),
	)

	annots := a(
		pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Square"),
			"Rect", a(400, 700, 420, 720),
			"F", 4,
			"AP", d("N", appearance("0.5 scn 0 0 20 20 re f\n")),
		)),
		pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Square"),
			"Rect", a(440, 700, 460, 720),
			"F", 4,
			"AP", d(
				"N", appearance("/CS0 cs 0.2 0.4 0.6 scn 0 0 20 20 re f\n"),
			),
		)),
	)

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	content := "q /CS0 cs 0.2 0.4 0.6 scn\n" +
		// The page text sits inside the ICCBased scope so the page adds no DeviceGray.
		"  BT /F1 12 Tf 50 740 Td (pattern and appearance colour) Tj ET\n" +
		"Q\n" +
		// P0 and P1 are painted. P2 is never named, so it stays unused.
		"q /Pattern cs /P0 scn 60 620 100 60 re f Q\n" +
		"q /Pattern cs /P1 scn 200 620 100 60 re f Q\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("F1", font),
			"ColorSpace", d("CS0", iccbased),
			"Pattern", patterns,
		),
		"Contents", pdf.Stream([]byte(content)),
		"Annots", annots,
	))
	pdf.AddPage(page)

	// Deliberately no /Metadata.
	return save(pdf, "1.7")
}
