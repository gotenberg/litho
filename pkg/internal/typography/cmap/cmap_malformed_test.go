package cmap

import (
	"context"
	"strings"
	"testing"
)

// cmapWithName builds a ToUnicode CMap mapping code <0041> to U+0061, naming
// itself with the given /CMapName token.
func cmapWithName(name string) []byte {
	return []byte(`/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def
/CMapName ` + name + ` def
/CMapType 2 def
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
1 beginbfchar
<0041> <0061>
endbfchar
endcmap
CMapName currentdict /CMap defineresource pop
end
end`)
}

// TestParse_MalformedTokenFlagsTheCMapAndKeepsTheMapping pins what a token the
// CMap syntax cannot produce does to a parsed CMap.
//
// ISO 32000-1, 7.3.5 ends a name at a delimiter, so "/Adobe-Identity-UC>"
// leaves a bare ">" that is valid nowhere: the lexer returns ">>" whole and a
// hex string consumes its own ">", so no well-formed CMap yields that token.
// veraPDF discards such a CMap entirely and reports ISO 14289-1, 7.21.7 t1 on
// every code the page shows, including codes the bfchar list maps. Measured on
// pdfjs issue11651.pdf, whose CMap carries both "/CMapName /Adobe-Identity-UC>"
// and "/Ordering (UC>)", and on fixtures isolating each: corrupting /CMapName
// alone is enough for veraPDF to fail the file.
//
// The mapping itself stays readable, because text extraction keeps every entry
// that parsed while a validator judges the CMap as mapping nothing.
func TestParse_MalformedTokenFlagsTheCMapAndKeepsTheMapping(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		cmapName  string
		malformed bool
	}{
		{"a well-formed name", "/Adobe-Identity-UCS", false},
		{"a name a stray delimiter cut short", "/Adobe-Identity-UC>", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := Parse(context.Background(), cmapWithName(tc.cmapName))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := parsed.Malformed(); got != tc.malformed {
				t.Errorf("Malformed() = %v, want %v", got, tc.malformed)
			}
			if got := parsed.Mapping["\x00A"]; got != "a" {
				t.Errorf("Mapping kept %q for <0041>, want %q: extraction reads every entry that parsed", got, "a")
			}
		})
	}
}

// TestParse_StringHoldingADelimiterIsWellFormed holds the flag to tokens the
// syntax cannot produce. A ">" inside a literal string or a hex string belongs
// to that string, so neither makes the CMap malformed.
func TestParse_StringHoldingADelimiterIsWellFormed(t *testing.T) {
	t.Parallel()

	data := cmapWithName("/Adobe-Identity-UCS")
	data = []byte(strings.Replace(string(data), "/Ordering (UCS)", "/Ordering (UC>)", 1))

	parsed, err := Parse(context.Background(), data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Malformed() {
		t.Error("a \">\" inside a literal string made the CMap malformed")
	}
}
