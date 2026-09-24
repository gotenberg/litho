package compliance

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// type1Charstring encodes a Type 1 charstring whose hsbw declares the given
// advance width, encrypted with the 4330 key and the four discarded lenIV bytes
// the Type 1 specification fixes. [type1Program] writes stub charstring bytes,
// which carry no width, so a width fixture has to state real ones.
func type1Charstring(width int) string {
	plain := []byte{139} // sbx = 0, encoded as v+139
	switch {
	case width >= 108 && width <= 1131:
		v := width - 108
		plain = append(plain, byte(247+(v>>8)), byte(v&0xFF))
	default:
		plain = append(plain, 255, byte(width>>24), byte(width>>16), byte(width>>8), byte(width))
	}
	plain = append(plain, 13) // hsbw

	full := append([]byte{0, 0, 0, 0}, plain...)
	r := uint32(4330)
	out := make([]byte, 0, len(full))
	for _, p := range full {
		c := p ^ byte(r>>8)
		out = append(out, c)
		r = (uint32(c)+r)*52845 + 22719
	}
	return string(out)
}

// type1WidthProgram builds a Type 1 program whose /CharStrings hold real
// charstrings, so [typography.ExtractType1AdvanceWidths] reads a width for each
// name. It mirrors [type1Program]'s eexec envelope, which writes stub bytes.
func type1WidthProgram(preamble string, widths map[string]int) string {
	var plain strings.Builder
	plain.WriteString("salt/Private 10 dict dup begin\n")
	fmt.Fprintf(&plain, "/CharStrings %d dict dup begin\n", len(widths))
	for _, name := range []string{"A", "space"} {
		w, ok := widths[name]
		if !ok {
			continue
		}
		cs := type1Charstring(w)
		fmt.Fprintf(&plain, "/%s %d RD %s ND\n", name, len(cs), cs)
	}
	plain.WriteString("end\nend")

	const key, c1, c2 = 55665, 52845, 22719
	r := uint32(key)
	var hex strings.Builder
	for _, p := range []byte(plain.String()) {
		c := p ^ byte(r>>8)
		fmt.Fprintf(&hex, "%02x", c)
		r = (uint32(c)+r)*c1 + c2
	}
	return "%!PS-AdobeFont-1.0: Test 1\n" + preamble + "\ncurrentfile eexec\n" + hex.String() + "\n"
}

// TestType1WidthsUncoveredCodeUsesMissingWidth pins that a shown code the
// /Widths array does not cover is judged against the descriptor's /MissingWidth
// (ISO 32000-1, Table 111, default 0), the same comparison
// [checkSimpleFontUncoveredWidths] already made on the GID-keyed path.
//
// The Type 1 path skipped those codes outright, so a whole class of fonts went
// unjudged.
//
// # Corpus evidence
//
//   - pdfjs bug1157493.pdf: shows a Courier glyph of 600 units at a code its
//     /Widths never reaches, and veraPDF reports "Glyph width 600 in the
//     embedded font program is not consistent with the Widths entry of the
//     font dictionary (value 0)".
func TestType1WidthsUncoveredCodeUsesMissingWidth(t *testing.T) {
	t.Parallel()

	const preamble = "/Encoding 256 array\n0 1 255 {1 index exch /.notdef put} for\n" +
		"dup 65 /A put\ndup 32 /space put\nreadonly def"

	build := func(t *testing.T, missingWidth string, shown byte) []Violation {
		t.Helper()

		program := type1WidthProgram(preamble, map[string]int{"A": 600, "space": 600})
		descriptor := "<< /Type /FontDescriptor /FontName /LithoT1 /Flags 32 /FontFile 4 0 R" +
			missingWidth + " >>"
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			// /Widths covers code 100 only, so 65 and 32 fall outside it.
			"<< /Type /Font /Subtype /Type1 /BaseFont /LithoT1 /FirstChar 100 /LastChar 100"+
				" /Widths [600] /FontDescriptor 3 0 R >>",
			descriptor,
			fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream",
				len(program), len(program), program),
		)
		fontDict, err := document.ResolveDict(t.Context(), doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{shown: true}}
		return checkType1FontWidths(t.Context(), doc, fontDict, "F1", 1, Rule_19005_1_6_3_6_t01, usage)
	}

	t.Run("an uncovered code against the default MissingWidth reports", func(t *testing.T) {
		t.Parallel()
		if got := build(t, "", 65); len(got) == 0 {
			t.Error("a 600-unit glyph against an implied width of 0 must report")
		}
	})

	t.Run("a MissingWidth that matches the program stays silent", func(t *testing.T) {
		t.Parallel()
		if got := build(t, " /MissingWidth 600", 65); len(got) != 0 {
			t.Errorf("a matching /MissingWidth must report nothing, got %v", got)
		}
	})

	t.Run("a code the program does not define stays silent", func(t *testing.T) {
		t.Parallel()
		// Code 13 is .notdef in the preamble's encoding, so no glyph advance
		// answers for it and the glyph-presence rule owns the case instead.
		if got := build(t, "", 13); len(got) != 0 {
			t.Errorf("an undefined glyph must not be width-judged, got %v", got)
		}
	})
}
