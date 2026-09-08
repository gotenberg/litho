package cff

import (
	"context"
	"os"
	"testing"
)

// TestCIDWidths_DuplicateCIDDeterministic guards CIDWidths against a subset CFF
// charset that maps several GIDs to the same CID (a duplicate glyph). The g5.cff
// fixture (a real govdocs CIDFontType0 subset) maps GID 21 and GID 22 both to
// CID 39. Inverting the GID→width map by ranging it let map iteration order pick
// the winner, so CID 39's width flipped between the two glyphs' advances (444 vs
// 778) run to run, intermittently masking a §6.2.11.5 width mismatch. The
// canonical glyph for a CID is the lowest GID (444 here, matching veraPDF), and
// the result must be stable.
func TestCIDWidths_DuplicateCIDDeterministic(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/g5.cff")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ctx := context.Background()
	for i := range 50 {
		w, err := CIDWidths(ctx, data)
		if err != nil {
			t.Fatalf("CIDWidths: %v", err)
		}
		if w[39] != 444 {
			t.Fatalf("run %d: CID 39 width = %g, want 444 (lowest-GID canonical glyph)", i, w[39])
		}
	}
}
