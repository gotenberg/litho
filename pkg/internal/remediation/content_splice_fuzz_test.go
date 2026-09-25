package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// FuzzContentTokenSplices asserts the content-stream token walks, over the
// token boundaries of ISO 32000-1 and over the reading of a repair, never panic
// on crafted bytes and return replacements in stream order that do not overlap
// and lie inside the stream, inline image dictionaries included, so
// [applyTokenSplices] can write them.
func FuzzContentTokenSplices(f *testing.F) {
	f.Add([]byte("BT <414> Tj ET q BI /W 1 /H 1 /BPC 8 /CS [/I /G 0 <0F0>] ID \x00 EI Q 602 0 inf inf -inf -inf d1"))
	f.Add([]byte("BI /W 1 /H 1 /CS [/I /G 0 <0F")) // Truncated inside the dictionary.
	f.Add([]byte("[- 2] 0 d BT [(a) 1.5.5 (b)] TJ ET /Span << /MCID -. >> BDC EMC BI /W 1 /D [. 1] ID \x00 EI"))
	f.Add([]byte("q 3.1 0 0 3.1 150 -34310.223 cm [99999.5 2] 0 d 1 0 0 1 70000.5 -100000.25 cm Q"))
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := context.Background()
		respell := func(tok lexer.Token, _ tokenPlace) (string, bool) {
			return tok.Value, tok.Type == lexer.TokenHexString || tok.Type == lexer.TokenKeyword
		}
		for _, walk := range []func(context.Context, []byte, func(lexer.Token, tokenPlace) (string, bool)) ([]tokenSplice, bool){contentTokenSplices, repairTokenSplices} {
			splices, _ := walk(ctx, data, respell)
			prev := 0
			for _, s := range splices {
				if s.start < prev || s.end < s.start || s.end > len(data) {
					t.Fatalf("splice [%d, %d) after %d in a %d-byte stream", s.start, s.end, prev, len(data))
				}
				prev = s.end
			}
			applyTokenSplices(data, splices)
		}
		respellHexStrings(ctx, data)
		rewriteNonFiniteNumbers(ctx, data)
		rewriteNestedMalformedNumbers(ctx, data, 1)
		clampContentNumbers(ctx, data, 1, false)
		clampContentNumbers(ctx, data, 2, true)
	})
}
