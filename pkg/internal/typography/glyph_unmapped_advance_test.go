package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/models"
)

// TestDecodeNextCode_SimpleFontReadsOneByteUnderWideToUnicode pins that a
// simple font's codes stay one byte each (ISO 32000-1, 9.6.1) under a
// /ToUnicode CMap whose codespace is two bytes wide, as pdfjs issue18208
// writes: read two at a time, "Pa" lost every second glyph.
func TestDecodeNextCode_SimpleFontReadsOneByteUnderWideToUnicode(t *testing.T) {
	t.Parallel()

	tu, err := cmap.Parse(context.Background(), []byte("begincmap\n"+
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"+
		"1 beginbfchar\n<0000> <0000>\nendbfchar\nendcmap\n"))
	if err != nil {
		t.Fatalf("cmap.Parse: %v", err)
	}
	font := &Font{Subtype: "TrueType", ToUnicode: tu, Encoding: map[byte]rune{'P': 'P', 'a': 'a'}}
	data := []byte("Pa")
	var got string
	for len(data) > 0 {
		char, n, skip, _ := decodeNextCode(font, data)
		if n != 1 || skip {
			t.Fatalf("code %q: consumed %d bytes, skip %v; want 1 byte, no skip", data[0], n, skip)
		}
		got += char
		data = data[n:]
	}
	if got != "Pa" {
		t.Errorf("decoded %q, want %q", got, "Pa")
	}
}

// TestProcessText_UnmappedCodeAdvances pins that an Identity-H code the
// /ToUnicode CMap maps to nothing still advances the text position by its
// /DW width (ISO 32000-1, 9.4.4 and 9.7.4.3), as PDF.js advances every code
// it shows, so the glyph after it is placed one advance on. Over a CIDFont
// the code is placed as an element with no character, so a redaction can
// keep it; over a Type 0 font naming no CIDFont no width is stated, and it
// is recorded unemitted.
func TestProcessText_UnmappedCodeAdvances(t *testing.T) {
	t.Parallel()

	tu, err := cmap.Parse(context.Background(), []byte("begincmap\n"+
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"+
		"1 beginbfchar\n<0041> <0041>\nendbfchar\nendcmap\n"))
	if err != nil {
		t.Fatalf("cmap.Parse: %v", err)
	}
	for _, tc := range []struct {
		name    string
		metrics bool
		emitted bool
	}{
		{"over a CIDFont", true, true},
		{"without a CIDFont", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			font := &Font{
				Subtype: "Type0", ToUnicode: tu, identityEncoding: true,
				MissingWidth: 1000, CIDWidths: map[int]float64{}, cidFontMetrics: tc.metrics,
			}
			machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
			machine.Current().Text.Tfs = 10
			machine.RecordGlyphAdvances = true
			var elements []models.TextElement
			if err := processText(context.Background(), ast.PdfString("\x00\x00\x00\x41"), font, machine, &elements, nil); err != nil {
				t.Fatalf("processText: %v", err)
			}
			var a *models.TextElement
			for i := range elements {
				if elements[i].Text == "A" {
					a = &elements[i]
				}
			}
			if a == nil {
				t.Fatalf("no element reads A: %+v", elements)
			}
			if a.X != 10 {
				t.Errorf("A placed at x %v, want 10, one 1000-unit advance at 10 points", a.X)
			}
			glyphs := machine.GlyphAdvances()[machine.OpIndex]
			if len(glyphs) != 2 {
				t.Fatalf("recorded %d codes, want 2", len(glyphs))
			}
			if g := glyphs[0]; g.Emitted != tc.emitted || g.After[4] != 10 {
				t.Errorf("code 0000 recorded Emitted %v and advanced to x %v, want %v and 10", g.Emitted, g.After[4], tc.emitted)
			}
		})
	}
}
