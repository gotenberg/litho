package compliance

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// shownFontsSeeds are content streams that select fonts, show text in and out
// of rendering mode 3, save and restore the text state, and show text with no
// font selected.
var shownFontsSeeds = []string{
	"BT /F1 12 Tf (Hello) Tj ET",
	"BT /F1 12 Tf 3 Tr (hidden) Tj /F2 9 Tf 0 Tr [(a) 2 (b)] TJ ET",
	"q BT /F1 12 Tf 3 Tr ET Q BT (no font) Tj /F3 1 Tf (x) ' 1 2 (y) \" ET",
	"BT /F1 12 Tf 3 Tr Tj (z) Tj 0 Tr Tj ET",
	"/P <</MCID 0>> BDC BT /F4 10 Tf (m) Tj ET EMC",
	"BT /F1 12 Tf (unterminated",
}

// FuzzShownFontsMatchesUsage pins that [shownFonts] answers what the fonts
// and visible flags of [collectFontUsage] answer, whatever the content.
func FuzzShownFontsMatchesUsage(f *testing.F) {
	for _, seed := range shownFontsSeeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		ops, _ := content.ParseStreamData(context.Background(), data) // A stream that fails to parse still yields the operators read before the failure, which both functions read.
		got := shownFonts(ops)
		usage := collectFontUsage(ops)
		if (got == nil) != (usage == nil) {
			t.Fatalf("shownFonts nil %v, collectFontUsage nil %v", got == nil, usage == nil)
		}
		want := make(map[string]bool, len(usage))
		for name, u := range usage {
			want[name] = u.visible
		}
		if !maps.Equal(got, want) {
			t.Fatalf("shownFonts = %v, want %v", got, want)
		}
	})
}
