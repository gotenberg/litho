package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// type1Program builds a minimal Type 1 font program: the clear-text preamble
// carrying the built-in encoding, then the eexec-encrypted /CharStrings
// dictionary defining one charstring per name. The encrypted section is written
// in the ASCII-hex form so the fixture stays printable inside a PDF stream.
func type1Program(preamble string, glyphs ...string) string {
	var plain strings.Builder
	// The first four plaintext bytes are eexec salt the decoder discards.
	plain.WriteString("salt/Private 10 dict dup begin\n")
	fmt.Fprintf(&plain, "/CharStrings %d dict dup begin\n", len(glyphs))
	for _, name := range glyphs {
		fmt.Fprintf(&plain, "/%s 4 RD ABCD ND\n", name)
	}
	plain.WriteString("end\nend")

	// Per ISO 32000-1, 9.9.1 the private section is eexec-encrypted with the
	// key and multipliers Adobe's Type 1 specification fixes.
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

// TestCheckSimpleGlyphPresence_Type1Program pins that a classic Type 1
// (/FontFile) font is judged for glyph presence at all, and by name.
//
// Its glyph programs live in the eexec /CharStrings dictionary keyed by glyph
// name (ISO 32000-1, 9.9.1), not in a GID-keyed table, so the GID path reads
// nothing and every such font went unchecked. A code whose name the dictionary
// does not hold paints no glyph, which ISO 19005-2 §6.2.11.4.1 t2 forbids:
// govdocs thread_300_300516.pdf shows code 202 named "blank" and pdfjs
// issue11477_reduced.pdf shows a space its 61-charstring subset omits.
func TestCheckSimpleGlyphPresence_Type1Program(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// fires reports whether the presence rule fails for a font carrying program
	// with the given PDF /Encoding entry, when code is shown.
	fires := func(t *testing.T, program, encoding string, code byte) bool {
		t.Helper()
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /Font /Subtype /Type1 /BaseFont /Test /FirstChar 0 /LastChar 255"+
				" /FontDescriptor 5 0 R /Encoding "+encoding+" >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 32 /FontFile 6 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), program),
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
		if err != nil {
			t.Fatalf("resolve font dict: %v", err)
		}
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{code: true}}
		return hasRule(
			checkSimpleGlyphPresence(ctx, doc, fontDict, "T1_0", 1, "PDF/A-2b", usage),
			Rule_19005_2_6_2_11_4_1_t02,
		)
	}

	vector := "/Encoding 256 array\ndup 65 /G65 put\ndup 202 /ring put\nreadonly def"
	empty := "/Encoding 256 array\nreadonly def"

	cases := []struct {
		name     string
		program  string
		encoding string
		code     byte
		want     bool
	}{
		{
			// /Differences states the file's own name for the code and overrides
			// the program's built-in "ring" (ISO 32000-1, 9.6.6.1).
			"a /Differences name the program lacks fires",
			type1Program(vector, ".notdef", "ring", "A"),
			"<< /Type /Encoding /Differences [202 /blank] >>", 202, true,
		},
		{
			"a /Differences name the program defines does not",
			type1Program(vector, ".notdef", "ring", "A"),
			"<< /Type /Encoding /Differences [202 /ring] >>", 202, false,
		},
		{
			// Outside /Differences a subset's synthetic built-in name answers the
			// code the base encoding names "A".
			"the program's built-in name answers a base-encoding code",
			type1Program(vector, ".notdef", "G65"),
			"/WinAnsiEncoding", 65, false,
		},
		{
			"a base-encoding code neither encoding defines fires",
			type1Program(empty, ".notdef"),
			"/WinAnsiEncoding", 65, true,
		},
		{
			// The program names a predefined encoding rather than spelling a
			// vector; resolving the reference as a glyph name reported every code
			// missing under the name "StandardEncoding".
			"a program referencing a predefined encoding resolves through it",
			type1Program("/Encoding StandardEncoding def", ".notdef", "space"),
			"null", 32, false,
		},
		{
			"a program with no readable charstrings stays unchecked",
			"%!PS-AdobeFont-1.0: Test 1\n/Encoding 256 array\nreadonly def\n",
			"/WinAnsiEncoding", 65, false,
		},
		{
			// WinAnsiEncoding leaves code 10 undefined, so it names nothing to
			// look up in /CharStrings and selects no glyph.
			"a code the named encoding leaves undefined fires",
			type1Program(vector, ".notdef", "ring", "A"),
			"/WinAnsiEncoding", 10, true,
		},
		{
			// StandardEncoding leaves code 224 undefined the same way.
			"a code the program's predefined encoding leaves undefined fires",
			type1Program("/Encoding StandardEncoding def", ".notdef", "space"),
			"null", 224, true,
		},
		{
			// The program's own vector answers the code the base leaves unset.
			"a code only the program's vector names does not",
			type1Program(vector, ".notdef", "ring", "A"),
			"null", 202, false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(t, tc.program, tc.encoding, tc.code); got != tc.want {
				t.Errorf("presence violation = %v, want %v", got, tc.want)
			}
		})
	}
}
