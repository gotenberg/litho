package text

import (
	"context"
	"log/slog"
	"testing"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

func FuzzLoadFontData(f *testing.F) {
	valid := testSans("Regular", 400, false, 100)
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Add(fontbuild.Collection(valid, testDigits()))
	f.Fuzz(func(t *testing.T, data []byte) {
		logger := slog.New(slog.DiscardHandler)
		fonts := &Fonts{logger: logger, byFont: make(map[*font.Font]*Face), fm: fontscan.NewFontMap(fontscanLogger{logger})}
		fonts.loadData("fuzz.ttf", data)
		if fonts.Len() == 0 {
			return
		}
		for _, fc := range fonts.faces {
			if _, err := fc.Compose(); err != nil {
				continue
			}
		}
		if _, _, err := fonts.Layout(context.Background(), []Run{{Text: "AB Cé D", Style: Style{Size: 10}}}, 20, Options{}); err != nil {
			t.Fatalf("Layout on a fuzzed face: %v", err)
		}
	})
}
