package content

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestInlineImagesReadAsPDFJS pins how [SplitGluedOperators] reads the data of
// inline images, as PDF.js does (see [pdfjsInlineImageEnd]). Every stream was
// measured with PDF.js 6.3.289 as the content of a one-page file, after a
// shown "B": the text getTextContent returns holds the "A" and "C" shown here
// exactly when the operations below show them, and holds only the "B" when
// the operations stop, where PDF.js stops reading the stream.
func TestInlineImagesReadAsPDFJS(t *testing.T) {
	t.Parallel()

	const (
		g = "BI /W 2 /H 1 /BPC 8 /CS /G ID "
		a = "BT (A) Tj ET\n"
		c = "BT (C) Tj ET\n"
	)
	shown := []string{"BT", "Tj", "ET"}
	with := func(ops ...string) []string { return append(ops, shown...) }
	for _, tc := range []struct {
		name, data string
		want       []string
	}{
		{"an EI against the data", g + "xyEI Q\n" + a, with("BI", "Q")},
		{"a tab after EI", g + "x EI\tQ\n" + a, []string{"BI"}},
		{"a form feed after EI", g + "x EI\fQ\n" + a, []string{"BI"}},
		{"a carriage return after EI", g + "x EI\rQ\n" + a, with("BI", "Q")},
		{"the last EI when no operator follows any", g + "x EI zz EI zz\n" + a + "EI\nzz\n" + c, with("BI", "zz")},
		{"an array after EI", g + "x EI [(A)] TJ\n" + c, with("BI", "TJ")},
		{"operands before an operator taking none", g + "x EI 1 2 Q " + a, with("BI", "Q")},
		{"binary after EI", g + "x EI \x01Q EI Q\n" + a, with("BI", "Q")},
		{"a NUL after EI", g + "x EI Q\x00" + a, with("BI", "Q")},
		{"two NULs after EI", g + "x EI Q\x00\x00zz EI Q\n" + a, with("BI", "Q")},
		{"EI after E", g + "xEEI Q\n" + a + "EI\n" + c, with("BI")},
		{"EI after EI", g + "xEIEI Q\n" + a + "EI\n" + c, with("BI")},
		{"a comment after EI", g + "x EI %c\nQ\n" + a, with("BI", "Q")},
		{"true before an operator", g + "x EI true Q\n" + a, with("BI", "Q")},
		{"an operator glued to a number", g + "x EI Q0 g\n" + a, with("BI", "Q", "g")},
		{"an operator past the window", g + "x EI /N" + strings.Repeat("a", 72) + " Q\n" + a + "EI zz\n" + c, with("BI", "zz")},
		{"a stray parenthesis after EI", "q " + g + "x EI ) Q\n" + a, []string{"q"}},
		{"a malformed number after EI", "q " + g + "x EI -x Q\n" + a, []string{"q"}},
		{"a sign then a line feed", g + "x EI -\n5 w\n" + a, with("BI", "w")},
		{"a stray parenthesis after a rejected EI", "q " + g + "x EI zz EI ) EI\n" + a, []string{"q"}},
		{"a key that is no name", "q BI /W 2 3 /H 1 ID xy EI Q\n" + a, []string{"q"}},
		{"an array where a key stands", "q BI /W 2 [1] /H 1 ID xy EI Q\n" + a, []string{"q"}},
		{"a dictionary the stream ends in", "q BI /W 2 /H 1", []string{"q"}},
		{"a DCTDecode image", "BI /W 2 /H 1 /F /DCT ID \xff\xd8 EI Q \xff\xd9 zz EI\n" + a, with("BI")},
		{"a DCTDecode image in an array", "BI /W 2 /H 1 /F [/DCT] ID \xff\xd8 EI Q \xff\xd9 zz EI\n" + a, with("BI")},
		{"a DCTDecode image with no EOI", "BI /W 2 /H 1 /F /DCT ID \xff\xd8 EI Q\n" + a, with("BI", "Q")},
		{"a DCTDecode segment over an EOI", "BI /W 2 /H 1 /F /DCT ID \xff\xe0\x00\x06\xff\xd9zz\xff\xd9 EI\n" + a, with("BI")},
		{"an ASCII85Decode image", "BI /W 2 /H 1 /F /A85 ID ab EI Q cd~>\n EI\n" + a, with("BI")},
		{"an ASCII85Decode image ending in a tilde", "BI /W 2 /H 1 /F /A85 ID ab~ EI\n" + a, with("BI")},
		{"an ASCIIHexDecode image", "BI /W 2 /H 1 /F /AHx ID 0 EI Q 0>zzEIx" + a, with("BI")},
		{"the /F entry over /Filter", "BI /W 2 /H 1 /F /AHx /Filter /A85 ID 00 EI Q >EI " + a, with("BI")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := operatorsOf(t, tc.data, SplitGluedOperators()); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("operators = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPDFJSInlineImageEnd pins where the reading of PDF.js resumes after the
// data of an inline image: past the white-space byte after its EI, past the
// byte after the EI that follows its marker, or at the end of the stream.
func TestPDFJSInlineImageEnd(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		data   string
		filter ast.PdfName
		want   int
		stops  bool
	}{
		{"ID xyEI Q", "", len("ID xyEI "), false},
		{"ID x EI", "", len("ID x EI"), false},
		{"ID x EI ", "", len("ID x EI "), false},
		{"ID x", "", len("ID x"), false},
		{"ID", "", len("ID"), false},
		{"ID x EI zz EI zz", "", len("ID x EI zz EI "), false},
		{"ID x EI )", "", len("ID x EI "), true},
		{"ID \xff\xd9EIxQ", "DCT", len("ID \xff\xd9EIx"), false},
		{"ID \xff\xd9", "DCT", len("ID \xff\xd9"), false},
		// A marker in the last byte, which PDF.js steps back to forever.
		{"ID \xff\xc0", "DCT", len("ID \xff\xc0"), false},
		{"ID \xff\xc0\x00", "DCT", len("ID \xff\xc0\x00"), false},
		{"ID ~>EI Q", "A85", len("ID ~>EI "), false},
		{"ID >EI Q", "AHx", len("ID >EI "), false},
		{"ID x EI Q", "AHx", len("ID x EI "), false},
	} {
		dict := ast.PdfDict{}
		if tc.filter != "" {
			dict["F"] = tc.filter
		}
		got, stops := pdfjsInlineImageEnd(context.Background(), []byte(tc.data), len("ID"), dict)
		if got != tc.want || stops != tc.stops {
			t.Errorf("pdfjsInlineImageEnd(%q, %q) = %d, %t, want %d, %t", tc.data, tc.filter, got, stops, tc.want, tc.stops)
		}
	}
}
