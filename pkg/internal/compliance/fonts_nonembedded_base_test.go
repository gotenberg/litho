package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestImplicitBaseNeedsAProgram pins that a font embedding no program supplies
// no implicit base encoding, so a code its /Differences array skips has no
// glyph name and therefore no Unicode value.
//
// ISO 32000-1, Table 114 makes the implicit base of a /Differences array the
// font program's built-in encoding. litho substituted StandardEncoding whenever
// it could not read one, which was right for an embedded program it cannot
// parse (a TrueType /FontFile2) but invented names for a font carrying no
// program at all. A standard 14 /BaseFont keeps the substitute only while the
// dictionary leaves its metrics implicit: ISO 32000-1, 9.6.2.2 makes
// /FirstChar, /LastChar, /Widths and /FontDescriptor all present or all absent,
// so a dictionary stating its own /FontDescriptor has left the shorthand
// behind. veraPDF draws the same line, loading the standard metrics only when
// the font dictionary carries no non-empty /FontDescriptor.
//
// govdocs thread_000_000578.pdf shows Times-Bold and thread_000_000885.pdf
// Palatino-Roman that way, both with a /FontDescriptor, no program and a
// /Differences array naming none of the codes they draw; veraPDF reports ISO
// 14289-1 §7.21.7 t1 on every glyph.
func TestImplicitBaseNeedsAProgram(t *testing.T) {
	t.Parallel()

	// pdf shows code 86 through a Type 1 font named baseFont whose /Encoding
	// names only code 219, carrying a /FontDescriptor when descriptor is set and
	// a /FontFile when embed is. No /ToUnicode either way, so the rule turns on
	// whether code 86 resolves to a name.
	pdf := func(baseFont string, descriptor, embed bool) []byte {
		const stream = "BT /F1 12 Tf 10 50 Td (V) Tj ET"
		const prog = "%!PS-AdobeFont-1.0: Test 001.001\n/FontName /Test def\n" +
			"currentdict end\ncurrentfile eexec\n"
		font := "<< /Type /Font /Subtype /Type1 /BaseFont /" + baseFont +
			" /Encoding << /Type /Encoding /Differences [219 /Zcaron] >>"
		if descriptor {
			font += " /FirstChar 0 /LastChar 255 /FontDescriptor 6 0 R"
		}
		font += " >>"
		fontFile := ""
		if embed {
			fontFile = "/FontFile 7 0 R "
		}
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 4 0 R >> >> /Contents 8 0 R >>",
			font,
			"<< /Type /StructTreeRoot >>",
			"<< /Type /FontDescriptor /FontName /"+baseFont+" /Flags 34 /ItalicAngle 0 "+
				"/Ascent 683 /Descent -217 /CapHeight 676 /StemV 139 /MissingWidth 500 "+
				"/FontBBox [-168 -218 1000 935] "+fontFile+">>",
			fmt.Sprintf("<< /Length %d /Length1 %d /Length2 0 /Length3 0 >>\nstream\n%s\nendstream",
				len(prog), len(prog), prog),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		)
	}

	fires := func(t *testing.T, data []byte) bool {
		t.Helper()
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == "7.21.7" && v.Rule.TestNumber == 1 {
				return true
			}
		}
		return false
	}

	cases := []struct {
		name       string
		baseFont   string
		descriptor bool
		embed      bool
		want       bool
	}{
		{"a standard 14 name with a descriptor and no program", "Times-Bold", true, false, true},
		{"the same name leaving its metrics implicit", "Times-Bold", false, false, false},
		{"a standard 14 name whose descriptor embeds a program", "Times-Bold", true, true, false},
		{"a non-standard name with a descriptor and no program", "Palatino-Roman", true, false, true},
		// Only a standard 14 name earns the substitute, so dropping the
		// descriptor does not rescue a name no consumer holds metrics for.
		{"a non-standard name with no descriptor at all", "Palatino-Roman", false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(t, pdf(tc.baseFont, tc.descriptor, tc.embed)); got != tc.want {
				t.Errorf("fired = %v, want %v", got, tc.want)
			}
		})
	}
}
