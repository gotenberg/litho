package slides

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// warpDeck returns a deck of n slides, each a box of warped text.
func warpDeck(n int, warp *shape.TextWarp) *Document {
	var ss []*Slide
	for range n {
		b := box(40, 40, 600, 400, para(strings.Repeat("WordArt ", 40), arial(24)))
		b.Text.Warp = warp
		ss = append(ss, &Slide{Shapes: []*Shape{b}})
	}
	return deck(ss...)
}

// peakOf converts doc under a budget of limit bytes and returns the
// budget's peak and the conversion's error, the cause of the context's
// end when it ended.
func peakOf(t *testing.T, doc *Document, limit int64) (int64, error) {
	t.Helper()
	ctx, b, done := limits.WithBudget(context.Background(), limits.Limits{MaxConvertMemoryBytes: limit})
	defer done()
	_, err := Convert(ctx, doc, pinnedFonts(t), office.Options{})
	if err != nil && ctx.Err() != nil {
		err = context.Cause(ctx)
	}
	return b.Peak(), err
}

// TestWarpText_ChargesTheWarpedOutlines checks that the outlines a warp
// draws are charged to the budget while their page is built, and that a
// deck whose warped text passes the budget is refused as other content
// is.
func TestWarpText_ChargesTheWarpedOutlines(t *testing.T) {
	plain, err := peakOf(t, warpDeck(3, nil), 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	warped, err := peakOf(t, warpDeck(3, &shape.TextWarp{Preset: "textWave1"}), 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	// 320 glyphs of a few dozen operations each, cut into pieces: well
	// past 100 KB of path operations.
	if warped < plain+100_000 {
		t.Fatalf("budget peak %d warped, %d plain: the warped outlines are not charged", warped, plain)
	}
	if _, err := peakOf(t, warpDeck(3, &shape.TextWarp{Preset: "textWave1"}), plain+(warped-plain)/2); !errors.Is(err, limits.ErrMemoryBudget) {
		t.Errorf("a budget between the plain and the warped peaks: error %v, want the budget's refusal", err)
	}
}
