package plaintext

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/models"
)

// discard is a sink that keeps no page.
type discard struct{}

func (discard) Expect(int) {}

func (discard) Page(context.Context, *typeset.Page) error { return nil }

// TestConvert_ChargesTheStructureTree pins what a line that draws text
// leaves charged once its page is handed over: its P element and its
// marked run, which live until the document is written.
func TestConvert_ChargesTheStructureTree(t *testing.T) {
	dir := t.TempDir()
	face := fontbuild.TrueType{
		PostScriptName: "Budget-Regular", Family: "Budget", Subfamily: "Regular", NotdefAdvance: 500,
		Glyphs: []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 500, Empty: true}, {Name: "a", Unicode: 'a', Advance: 500}},
	}.Build()
	if err := os.WriteFile(filepath.Join(dir, "budget.ttf"), face, 0o600); err != nil {
		t.Fatal(err)
	}
	fonts, err := text.NewFonts(context.Background(), nil, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	held := func(lines int) int64 {
		ctx, mem, done := limits.WithBudget(context.Background(), models.Limits{})
		defer done()
		if _, err := Convert(ctx, strings.NewReader(strings.Repeat("aa a\n", lines)), fonts, Options{FontFamilies: []string{"Budget"}, Sink: discard{}}); err != nil {
			t.Fatal(err)
		}
		return mem.Held()
	}
	if got, want := held(200)-held(100), int64(100*(paragraphCost+markedRunCost)); got != want {
		t.Errorf("100 more lines hold %d more bytes, want %d", got, want)
	}
}
