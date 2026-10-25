package recolor

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// FuzzDefaultAppearance converts the colour of an arbitrary default
// appearance string (ISO 32000-1, 12.7.3.3), as a field's /DA holds it. It
// asserts the rewrite never panics and that a /DA it reports changed parses.
func FuzzDefaultAppearance(f *testing.F) {
	da := "/Helv 12 Tf 1 0 0 rg 0.5 G"
	f.Add([]byte(da))
	f.Add([]byte(da[:len(da)/2+3]))
	f.Add([]byte("/Helv 0 Tf(x)Tj 0 0 1 RG1 g"))
	f.Fuzz(func(t *testing.T, data []byte) {
		s, _ := fuzzSession(t)
		ctx := context.Background()
		out, changed, err := s.daColour(ctx, data)
		if err != nil || !changed {
			return
		}
		if _, err := content.ParseStreamData(ctx, out, content.WholeStream()); err != nil {
			t.Fatalf("the rewritten /DA %q does not parse: %v", out, err)
		}
	})
}
