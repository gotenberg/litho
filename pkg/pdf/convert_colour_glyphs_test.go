package pdf

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// colourGlyphsWant pins the SHA-256 of the conversion
// [TestConvert_SameBytesWithColourGlyphs] writes, as
// [TestConvert_SameBytesOnEveryMachine] pins its cases.
const colourGlyphsWant = "877b8289cdd33ffefbfd494cbcc7289db516eee83b5cf5fa637a7fc175515835"

// colourGlyphFonts writes into a new directory the sans face of
// testdata/fonts and two colour faces built for the test: "Pin Emoji", a
// colour bitmap face drawing U+231A, U+263A and U+26BD as 8 by 8 pixel
// pictures, and "Pin Layers", whose A draws two layers in palette
// entries 0 and 1 of two palettes.
func colourGlyphFonts(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sans, err := os.ReadFile(filepath.Join("testdata", "fonts", "example_sans.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	picture := func(c color.NRGBA) []byte {
		img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := range 8 {
			for x := range 8 {
				if (x+y)%3 != 0 {
					img.SetNRGBA(x, y, c)
				}
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	glyph := func(c color.NRGBA) fontbuild.BitmapGlyph {
		return fontbuild.BitmapGlyph{PNG: picture(c), Width: 8, Height: 8, Top: 7, Advance: 8}
	}
	cblc, cbdt := fontbuild.ColorBitmaps(8, map[uint16]fontbuild.BitmapGlyph{
		1: glyph(color.NRGBA{R: 230, G: 120, A: 255}),
		2: glyph(color.NRGBA{G: 160, B: 220, A: 200}),
		3: glyph(color.NRGBA{R: 40, G: 40, B: 40, A: 255}),
	})
	emoji := fontbuild.TrueType{
		PostScriptName: "PinEmoji", Family: "Pin Emoji", Subfamily: "Regular", NotdefAdvance: 1000,
		Glyphs: []fontbuild.Glyph{
			{Name: "watch", Unicode: 0x231A, Advance: 1000, Empty: true},
			{Name: "smile", Unicode: 0x263A, Advance: 1000, Empty: true},
			{Name: "ball", Unicode: 0x26BD, Advance: 1000, Empty: true},
			{Name: "space", Unicode: ' ', Advance: 300, Empty: true},
		},
		Extra: map[string][]byte{"CBLC": cblc, "CBDT": cbdt},
	}.Build()
	colr, cpal := fontbuild.ColorLayers(map[uint16][]fontbuild.Layer{3: {{GID: 1, Palette: 0}, {GID: 2, Palette: 1}}},
		[][][4]uint8{{{200, 30, 30, 255}, {30, 30, 200, 128}}, {{30, 160, 30, 255}, {0, 0, 0, 255}}})
	layers := fontbuild.TrueType{
		PostScriptName: "PinLayers", Family: "Pin Layers", Subfamily: "Regular", NotdefAdvance: 600,
		Glyphs: []fontbuild.Glyph{
			{Name: "bar", Unicode: 'B', Advance: 300},
			{Name: "box", Unicode: 'C', Advance: 600},
			{Name: "A", Unicode: 'A', Advance: 600},
			{Name: "space", Unicode: ' ', Advance: 250, Empty: true},
		},
		Extra: map[string][]byte{"COLR": colr, "CPAL": cpal},
	}.Build()
	for name, data := range map[string][]byte{"sans.ttf": sans, "emoji.ttf": emoji, "layers.ttf": layers} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// colourGlyphsSource is the HTML the colour glyph pin converts: emoji of
// default emoji presentation, a text default asked as emoji, glyphs drawn
// several times at several sizes, and layered glyphs in two palettes.
const colourGlyphsSource = `<!doctype html><html lang="en"><head><meta charset="utf-8"><style>
@font-palette-values --second { font-family: "Pin Layers"; base-palette: 1; override-colors: 0 rebeccapurple; }
p { font-family: "Example Sans", sans-serif; font-size: 14px; }
.big { font-size: 40px; }
.layers { font-family: "Pin Layers"; font-size: 30px; }
</style></head><body>
<p>Watch ⌚ smile ☺&#xFE0F; ball ⚽ and again ⌚⌚ ☺</p>
<p class="big">⌚⚽☺&#xFE0F;</p>
<p class="layers">A <span style="font-palette: --second">A</span> <span style="color: teal">AA</span></p>
</body></html>`

// TestConvert_SameBytesWithColourGlyphs converts a page of colour glyphs,
// bitmaps and layers in two palettes, with fonts built for the test, and
// compares the SHA-256 of the PDF with [colourGlyphsWant]: every machine
// writes these bytes.
func TestConvert_SameBytesWithColourGlyphs(t *testing.T) {
	t.Parallel()
	fonts := colourGlyphFonts(t)
	var out bytes.Buffer
	if _, err := FromHTML(strings.NewReader(colourGlyphsSource), WithConvertFontDirs(fonts)).WriteTo(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("/Subtype /Type3")) {
		t.Error("no Type 3 font: the colour glyphs are not drawn as such")
	}
	sum := sha256.Sum256(out.Bytes())
	if got := hex.EncodeToString(sum[:]); got != colourGlyphsWant {
		t.Errorf("SHA-256 %s, pinned %s", got, colourGlyphsWant)
	}
}

// TestConvert_ColorGlyphsOff checks [WithColorGlyphs]: with false the page
// of [colourGlyphsSource] holds no Type 3 font and no image, the layered
// face's letters are drawn from their outlines, the emoji only the bitmap
// face covers are reported missing and stay text, and with true the
// conversion reports none missing and writes the bytes it writes without
// the option.
func TestConvert_ColorGlyphsOff(t *testing.T) {
	t.Parallel()
	fonts := colourGlyphFonts(t)
	convert := func(opts ...HTMLOption) ([]byte, *ConvertReport) {
		var out bytes.Buffer
		rep, err := FromHTML(strings.NewReader(colourGlyphsSource), append(opts, WithConvertFontDirs(fonts))...).WriteTo(t.Context(), &out)
		if err != nil {
			t.Fatal(err)
		}
		return out.Bytes(), rep
	}
	plain, rep := convert(WithColorGlyphs(false))
	for _, mark := range []string{"/Subtype /Type3", "/Subtype /Image"} {
		if bytes.Contains(plain, []byte(mark)) {
			t.Errorf("the file holds %s: a colour glyph was written", mark)
		}
	}
	// A missing emoji is reported once a conversion, without its name.
	blank := func(rep *ConvertReport) bool {
		for _, w := range rep.Warnings {
			if w.Code == WarningMissingGlyph && strings.Contains(w.Message, "emoji") {
				return true
			}
		}
		return false
	}
	if !blank(rep) {
		t.Errorf("warnings %+v do not report the emoji only the bitmap face covers", rep.Warnings)
	}
	r, err := Open(t.Context(), bytes.NewReader(plain), int64(len(plain)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	text, err := r.ExtractText(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\u231a", "\u26bd", "A"} {
		if !strings.Contains(text, want) {
			t.Errorf("text %q lacks %q", text, want)
		}
	}

	colour, rep := convert(WithColorGlyphs(true))
	if blank(rep) {
		t.Errorf("with colour glyphs, warnings %+v report an emoji missing", rep.Warnings)
	}
	unset, _ := convert()
	if !bytes.Equal(colour, unset) {
		t.Error("WithColorGlyphs(true) does not write the bytes of the default")
	}
}
