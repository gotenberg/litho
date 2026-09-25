package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// FuzzContentTokenSplices asserts the content-stream token walk never panics
// on crafted bytes and returns replacements in stream order that do not
// overlap and lie inside the stream, inline image dictionaries included, so
// [applyTokenSplices] can write them.
func FuzzContentTokenSplices(f *testing.F) {
	f.Add([]byte("BT <414> Tj ET q BI /W 1 /H 1 /BPC 8 /CS [/I /G 0 <0F0>] ID \x00 EI Q 602 0 inf inf -inf -inf d1"))
	f.Add([]byte("BI /W 1 /H 1 /CS [/I /G 0 <0F")) // Truncated inside the dictionary.
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := context.Background()
		splices, _ := contentTokenSplices(ctx, data, func(tok lexer.Token, _ tokenPlace) (string, bool) {
			return tok.Value, tok.Type == lexer.TokenHexString || tok.Type == lexer.TokenKeyword
		})
		prev := 0
		for _, s := range splices {
			if s.start < prev || s.end < s.start || s.end > len(data) {
				t.Fatalf("splice [%d, %d) after %d in a %d-byte stream", s.start, s.end, prev, len(data))
			}
			prev = s.end
		}
		applyTokenSplices(data, splices)
		respellHexStrings(ctx, data)
		rewriteNonFiniteNumbers(ctx, data)
	})
}
