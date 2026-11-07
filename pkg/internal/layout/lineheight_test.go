package layout

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// tallFallback indexes Ahem and a face covering U+2603 alone whose
// ascent is 0.95 em and descent 0.05 em, unlike Ahem's 0.8 and 0.2.
func tallFallback(t *testing.T) *text.Fonts {
	t.Helper()
	prog := fontbuild.TrueType{
		PostScriptName: "Tall", Family: "Tall", Subfamily: "Regular", NotdefAdvance: 1000, NoOS2: true,
		Glyphs: []fontbuild.Glyph{{Name: "snowman", Unicode: '☃', Advance: 1000}},
	}.Build()
	n := int(binary.BigEndian.Uint16(prog[4:]))
	for i := range n {
		rec := prog[12+16*i:]
		if string(rec[:4]) == "hhea" {
			off := binary.BigEndian.Uint32(rec[8:])
			binary.BigEndian.PutUint16(prog[off+4:], 950)
			binary.BigEndian.PutUint16(prog[off+6:], uint16(0x10000-50))
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tall.ttf"), prog, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := text.NewFonts(context.Background(), nil, []string{"testdata", dir})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLineHeight_FallbackFace(t *testing.T) {
	f := tallFallback(t)
	tests := []struct {
		name string
		lh   css.Dimension
		want float64
	}{
		// line-height: 1 lays the line out on the first available
		// font's metrics alone, whichever face draws the glyphs.
		{"number", css.Dimension{Kind: css.Number, Value: 1}, px(10)},
		// normal takes the fallback face's own ascent: 9.5 above the
		// baseline and Ahem's 2 below.
		{"normal", css.Dimension{Kind: css.Normal}, px(11.5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := sty(func(s *css.Style) { s.LineHeight = tt.lh })
			pb := page(200, 200)
			e := newEngine(context.Background(), f, Options{Page: pb}, pb.Width, pb.Height)
			rb := e.generate(doc(el(st, txt(st, "X☃X"))))
			e.layoutRoot(rb)
			if e.err != nil {
				t.Fatal(e.err)
			}
			if got := kid(t, rb, 0, 0).h; !near(got, tt.want) {
				t.Errorf("line box %v, want %v", got, tt.want)
			}
		})
	}
}
