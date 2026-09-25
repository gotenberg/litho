package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// imageData returns the bytes of the first inline image's data in stream,
// from the byte after its ID to the EI.
func imageData(t *testing.T, stream string) string {
	t.Helper()
	start := strings.Index(stream, " ID ")
	end := strings.LastIndex(stream, " EI")
	if start < 0 || end < start {
		t.Fatalf("no inline image data in %q", stream)
	}
	return stream[start+len(" ID ") : end]
}

// TestRespellHexStrings_InlineImageDictionary pins the repair of a hex string
// inside an inline image's dictionary: the string is respelled where it
// stands, and the data between ID and EI is carried across byte for byte
// (ISO 32000-1, 8.9.7), whatever its filter. A dictionary the walk cannot
// bound, and a BI with no ID, leave the dictionary as written, and nothing in
// the stream moves that the walk did not reach.
func TestRespellHexStrings_InlineImageDictionary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	longDecode := "/D [" + strings.Repeat("0 1 ", maxInlineImageDictTokens/2) + "]"
	for _, tc := range []struct {
		name, src, want string
		n               int
	}{
		{
			name: "an Indexed lookup with an odd digit count",
			src:  "q BI /W 2 /H 1 /BPC 8 /CS [/I /RGB 1 <FF00000000F>] ID \x01\x00 EI Q",
			want: "q BI /W 2 /H 1 /BPC 8 /CS [/I /RGB 1 <FF00000000F0>] ID \x01\x00 EI Q",
			n:    1,
		},
		{
			// " EI]" ends the data unless the parser reads the dictionary's
			// ASCII85 filter, whose alphabet holds "]", and the "<414>" after
			// it would then be respelled inside the data.
			name: "ASCII85 data holding EI before a delimiter",
			src:  "BI /W 1 /H 1 /BPC 8 /CS [/I /G 1 <0F!F>] /F /A85 ID 9 EI]<414>~> EI <424> Tj",
			want: "BI /W 1 /H 1 /BPC 8 /CS [/I /G 1 <0FF0>] /F /A85 ID 9 EI]<414>~> EI <4240> Tj",
			n:    2,
		},
		{
			name: "data that reads like a malformed string",
			src:  "BI /W 5 /H 1 /BPC 8 /CS /G ID <414> EI",
			want: "BI /W 5 /H 1 /BPC 8 /CS /G ID <414> EI",
		},
		{
			name: "a dictionary longer than the walk reads ahead",
			src:  "BI /W 1 /H 1 /BPC 8 /CS /G " + longDecode + " /X <414> ID \x00 EI <424> Tj",
			want: "BI /W 1 /H 1 /BPC 8 /CS /G " + longDecode + " /X <414> ID \x00 EI <4240> Tj",
			n:    1,
		},
		{
			name: "a BI with no ID",
			src:  "<414> Tj BI /W 1 /H 1 <424>",
			want: "<4140> Tj BI /W 1 /H 1 <424>",
			n:    1,
		},
	} {
		got, n := respellHexStrings(ctx, []byte(tc.src))
		if string(got) != tc.want || n != tc.n {
			t.Errorf("%s: respellHexStrings(%q) = %q, %d; want %q, %d", tc.name, tc.src, got, n, tc.want, tc.n)
			continue
		}
		if strings.Contains(tc.src, " ID ") && strings.Contains(tc.src, " EI") && imageData(t, string(got)) != imageData(t, tc.src) {
			t.Errorf("%s: the image data moved: %q, want %q", tc.name, imageData(t, string(got)), imageData(t, tc.src))
		}
	}
}

// TestFixHexString_RepairsAnInlineImageDictionary runs the fixer on a page
// drawing an inline image whose Indexed lookup is written with an odd digit
// count. The page is rewritten with no malformed string left, it reads as the
// same operations, the image's dictionary among them, and the image data is
// the source's.
//
// The walk used to skip the dictionary with the data, so compliance kept
// stating ISO 19005-2, 6.1.6 t1 for it after remediation.
func TestFixHexString_RepairsAnInlineImageDictionary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	image := "BI /W 2 /H 1 /BPC 8 /CS [/I /RGB 1 <FF000000FF0>] ID \x01\x00 EI"
	src := "q 10 0 0 10 0 0 cm " + image + " Q"
	if n := streamHexViolations(t, []byte(src)); n != 1 {
		t.Fatalf("control: the parse records %d malformed hex string(s) in the source, want 1", n)
	}
	got := rewrittenContent(t, hexStringPDF("<4142> Tj ET "+src+" BT"))
	if got == "" {
		t.Fatal("the content stream was not rewritten")
	}
	if n := streamHexViolations(t, []byte(got)); n != 0 {
		t.Errorf("the rewrite still holds %d malformed hex string(s): %q", n, got)
	}
	if !strings.Contains(got, "<FF000000FF00>] ID \x01\x00 EI") {
		t.Errorf("the lookup was not respelled in place, or the data moved: %q", got)
	}
	before, err := content.ParseStreamData(ctx, []byte(src))
	if err != nil {
		t.Fatalf("parsing the source: %v", err)
	}
	start := strings.Index(got, "q 10")
	after, err := content.ParseStreamData(ctx, []byte(got[start:strings.Index(got, " Q")+len(" Q")]))
	if err != nil {
		t.Fatalf("parsing the rewrite: %v", err)
	}
	if !sameOperations(after, before) {
		t.Errorf("the image reads as\n  %v\nwant\n  %v", after, before)
	}
}
