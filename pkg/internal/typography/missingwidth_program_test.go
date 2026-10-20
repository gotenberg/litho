package typography

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// programPitchGlyphs are the glyphs "net:  host" shows, each 600 units wide
// as Courier's are.
func programPitchGlyphs() []fontbuild.Glyph {
	var glyphs []fontbuild.Glyph
	for _, g := range []struct {
		name string
		r    rune
	}{{"n", 'n'}, {"e", 'e'}, {"t", 't'}, {"colon", ':'}, {"space", ' '}, {"h", 'h'}, {"o", 'o'}, {"s", 's'}} {
		glyphs = append(glyphs, fontbuild.Glyph{Name: g.name, Unicode: g.r, Advance: 600, Empty: g.r == ' '})
	}
	return glyphs
}

// programPitchPDF is [missingWidthPitchPDF] with the font's program
// embedded under fontFile, whose stream dictionary holds streamDict, and the
// font dictionary's /Widths and /Encoding given by widths and encoding.
func programPitchPDF(pitch float64, subtype, fontFile, streamDict string, program []byte, widths, encoding string) []byte {
	step := strconv.FormatFloat(pitch, 'f', -1, 64)
	var body strings.Builder
	body.WriteString("BT /F1 19.79 Tf 10 20 TD")
	for i, c := range "net:  host" {
		if i > 0 {
			body.WriteString(" " + step + " 0 TD")
		}
		body.WriteString(" (" + string(c) + ") Tj")
	}
	body.WriteString(" ET")
	s := body.String()
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /" + subtype + " /BaseFont /TestMono " + widths + " " + encoding + " /FontDescriptor 7 0 R >>\nendobj\n" +
		"7 0 obj\n<< /Type /FontDescriptor /FontName /TestMono /Flags 32 /FontBBox [0 0 1000 1000] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /" + fontFile + " 8 0 R >>\nendobj\n" +
		"8 0 obj\n<< /Length " + strconv.Itoa(len(program)) + " " + streamDict + " >>\nstream\n" + string(program) + "\nendstream\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 50] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(s)) + " >>\nstream\n" + s + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")
}

// TestExtractText_CodesOutsideWidthsMeasuredByProgram pins option 2 of the
// /MissingWidth decision (user, 2026-10-20): a code outside /Widths still
// advances nothing (ISO 32000-1, 9.6.2.1, Tables 111 and 122), but the gap
// after its glyph is measured from the advance the embedded program gives
// that glyph, so the glyphs a producer set one per TD at the program's pitch
// join into words. Codes /Widths covers with a stated 0 keep reading as the
// dictionary has them.
//
// # Corpus evidence
//
//   - pdfjs bug1157493.pdf: an embedded Type 1 Courier sets "net:" and
//     "host" this way, at a 10.8688-unit pitch against the 11.874 units
//     the program gives each glyph at 19.79 points.
func TestExtractText_CodesOutsideWidthsMeasuredByProgram(t *testing.T) {
	t.Parallel()
	const pitch = 10.8688
	type1Program, lengths := fontbuild.Type1{FontName: "TestMono", Glyphs: programPitchGlyphs(), NotdefAdvance: 600}.Build()
	type1Dict := "/Length1 " + strconv.Itoa(lengths[0]) + " /Length2 " + strconv.Itoa(lengths[1]) + " /Length3 " + strconv.Itoa(lengths[2])
	cffProgram := fontbuild.CFF{FontName: "TestMono", Glyphs: programPitchGlyphs(), NotdefAdvance: 600}.Build()
	ttProgram := fontbuild.TrueType{PostScriptName: "TestMono", Family: "TestMono", Subfamily: "Regular", Glyphs: programPitchGlyphs(), NotdefAdvance: 600}.Build()
	const differences = "/Encoding << /Type /Encoding /Differences [32 /space 58 /colon 101 /e 104 /h 110 /n 111 /o 115 /s 116 /t] >>"
	outside := "/FirstChar 0 /LastChar 0 /Widths [600]"
	var zeros []string
	for range 256 {
		zeros = append(zeros, "0")
	}
	statedZero := "/FirstChar 0 /LastChar 255 /Widths [" + strings.Join(zeros, " ") + "]"

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"Type 1", programPitchPDF(pitch, "Type1", "FontFile", type1Dict, type1Program, outside, differences), "net: host"},
		{"CFF", programPitchPDF(pitch, "Type1", "FontFile3", "/Subtype /Type1C", cffProgram, outside, differences), "net: host"},
		{"TrueType", programPitchPDF(pitch, "TrueType", "FontFile2", "", ttProgram, outside, "/Encoding /WinAnsiEncoding"), "net: host"},
		{"stated zero", programPitchPDF(pitch, "Type1", "FontFile", type1Dict, type1Program, statedZero, differences), "n e t : h o s t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, err := document.NewDocument(ctx, bytes.NewReader(tt.data), int64(len(tt.data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			catalog, err := document.ResolveCatalog(ctx, doc)
			if err != nil {
				t.Fatalf("ResolveCatalog: %v", err)
			}
			pages, err := document.ResolvePages(ctx, doc, catalog)
			if err != nil || len(pages) == 0 {
				t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
			}
			ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
			if err != nil {
				t.Fatalf("ParsePageContent: %v", err)
			}

			elems, err := ExtractElements(ctx, doc, graphics.NewMachine([4]float64{0, 0, 200, 50}), pages[0], ops)
			if err != nil {
				t.Fatalf("ExtractElements: %v", err)
			}
			for i, el := range elems {
				if el.Width != 0 {
					t.Errorf("element %d (%q): width %v, want 0 (the dictionary gives the code none)", i, el.Text, el.Width)
				}
			}

			got, err := ExtractText(ctx, doc, graphics.NewMachine([4]float64{0, 0, 200, 50}), pages[0], ops, ExtractionConfig{})
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if strings.Join(strings.Fields(got), " ") != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
