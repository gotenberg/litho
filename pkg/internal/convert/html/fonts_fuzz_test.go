package html

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// FuzzLoadFontFace checks that loading an @font-face source, WOFF and WOFF2
// decoding included, then shaping, laying out and composing text with the
// face never panics: the decoded bytes reach go-text's parser and shaper
// only through the text package's guard.
func FuzzLoadFontFace(f *testing.F) {
	for _, path := range []string{ahemTTF, ahemWOFF, ahemWOFF2} {
		data := readTestFile(f, path)
		f.Add(data)
		f.Add(data[:len(data)/2])
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := context.Background()
		base := testFonts(t)
		res := newResources(fstest.MapFS{"f": {Data: data}})
		rule := css.FontFace{Family: "F", Sources: []css.FontSource{{URL: rootPrefix + "f"}}, WeightMin: 400, WeightMax: 400}
		set, _ := loadFontFaces(ctx, []css.FontFace{rule}, res, base, nil)
		fonts := base.WithFaces(set)
		style := text.Style{Families: []string{"F"}, Size: 12, Weight: 700, Italic: true}
		lines, _, err := fonts.Layout(ctx, []text.Run{{Text: "AB Épé一 X", Style: style}}, 100, text.Options{})
		if err != nil {
			return
		}
		for _, line := range lines {
			for _, r := range line.Runs {
				if r.Face == nil || len(r.Face.Program) == 0 {
					t.Fatal("a run without a program")
				}
			}
		}
	})
}
