package build

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("whitetext_2b", whitetext2b) }

// whitetext2b builds whitetext_2b.pdf: white text on a coloured bar, an image and the bare
// page, at PDF/A-2b.
//
// Text extraction judged text filled in device white invisible, as white on a
// white page, whatever lay under it, so the default reading dropped a white
// heading on a coloured bar. A rendering of the page now settles it: the glyphs
// that change pixels no other glyph covers show. Rendering mode 3 paints nothing
// (ISO 32000-1, 9.3.6, Table 106), so an OCR layer stays invisible without one.
// Every glyph is a Type 3 glyph whose description paints a block in the text's
// colour, with a /ToUnicode map, so the text extracts and draws the same in every
// renderer:
//
// | Run      | Drawn                                         | Default reading |
// | -------- | --------------------------------------------- | --------------- |
// | `HEAD`   | white, on a navy bar drawn before it          | read            |
// | `PHOTO`  | white, on a dark image drawn before it        | read            |
// | `BLANK`  | white, on the bare page                       | dropped         |
// | `OCR`    | white in mode 3, on the navy bar              | dropped         |
// | `BODY`   | black, on the bare page                       | read            |
// | `HIDDEN` | white, on the bare page, a navy bar after it  | dropped         |
//
// `HIDDEN` sits under a bar the page paints after it: what the page paints after
// a glyph can hide it, never show it, so it stays out with or without a
// rendering.
//
// The file declares an RGB output intent, so its DeviceRGB colours are in
// order, and it is conforming at PDF/A-2b.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func whitetext2b() ([]byte, error) {
	pdf := pdfgen.New()

	destination := pdf.Stream(iccRGBDisplayProfile())
	destination.Set("N", 3)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho RGB display"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	const letters = "ABCDEHIKLNOPRTY"
	glyphs := make([]type3Proc, 0, len(letters))
	widths := make([]any, 0, len(letters))
	toUnicode := map[int]int{}
	for i, letter := range letters {
		glyphs = append(glyphs, type3Proc{string(letter), 700, "50 0 600 700 re f"})
		widths = append(widths, 700)
		toUnicode[65+i] = int(letter)
	}
	font := type3Font(pdf, glyphs, widths)
	font.Set("ToUnicode", toUnicodeCMap(pdf, toUnicode))
	pdf.Indirect(font)

	// shown is the codes of word in the font, which numbers letters from 65 on.
	shown := func(word string) []byte {
		var out []byte
		for _, c := range word {
			out = append(out, byte(65+strings.IndexRune(letters, c))) //nolint:gosec // letters holds 15 codes.
		}
		return out
	}

	imageStream := pdf.Stream(bytes.Repeat([]byte{0x20, 0x30, 0x40}, 4))
	imageStream.Set("Type", n("XObject"))
	imageStream.Set("Subtype", n("Image"))
	imageStream.Set("Width", 2)
	imageStream.Set("Height", 2)
	imageStream.Set("ColorSpace", n("DeviceRGB"))
	imageStream.Set("BitsPerComponent", 8)
	image := pdf.MakeIndirect(imageStream)

	textOps := func(word string, y int, colour string, mode int) string {
		return fmt.Sprintf("BT %d Tr %s rg /F 20 Tf 50 %d Td <%s> Tj ET\n", mode, colour, y, hex.EncodeToString(shown(word)))
	}

	const (
		white = "1 1 1"
		black = "0 0 0"
	)
	content := "0 0 0.5 rg 40 330 320 40 re f\n" +
		textOps("HEAD", 340, white, 0) +
		"q 320 0 0 40 40 270 cm /Im Do Q\n" +
		textOps("PHOTO", 280, white, 0) +
		textOps("BLANK", 220, white, 0) +
		"0 0 0.5 rg 40 150 320 40 re f\n" +
		textOps("OCR", 160, white, 3) +
		textOps("BODY", 100, black, 0) +
		textOps("HIDDEN", 40, white, 0) +
		"0 0 0.5 rg 40 30 320 40 re f\n"

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 400, 400),
		"Resources", d("Font", d("F", font), "XObject", d("Im", image)),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
