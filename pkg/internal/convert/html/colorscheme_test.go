package html

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestColorScheme_DarkRootPaintsTheSheet locks CSS Color Adjustment 1,
// 2.2: a root of a dark used colour scheme, from color-scheme or from
// <meta name=color-scheme> (HTML Standard, 4.2.5.1), paints the whole
// sheet in the dark Canvas colour under everything else, backgrounds
// omitted or not; a light root paints nothing.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints a page of
//     :root { color-scheme: dark } or <meta name=color-scheme
//     content=dark> rgb(18, 18, 18) to its edges, margins included, with
//     printBackground on or off, and a body of rgba(255, 0, 0, 0.5) over
//     it rgb(136, 8, 8).
func TestColorScheme_DarkRootPaintsTheSheet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, src string
		o         Options
		dark      bool
	}{
		{"property", `<html style="color-scheme: dark"><p>x</p>`, Options{}, true},
		{"meta", `<meta name=color-scheme content="dark"><p>x</p>`, Options{}, true},
		{"meta overridden", `<meta name=color-scheme content="dark"><style>:root { color-scheme: light }</style><p>x</p>`, Options{}, false},
		{"backgrounds omitted", `<html style="color-scheme: dark"><p>x</p>`, Options{OmitBackgrounds: true}, true},
		{"light dark", `<html style="color-scheme: light dark"><p>x</p>`, Options{}, false},
		{"none", `<p>x</p>`, Options{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, err := convert(context.Background(), strings.NewReader(tt.src), testFonts(t), tt.o, layout.Layout)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}
			pg := doc.Pages[0]
			p, ok := pg.Content[0].(*typeset.Path)
			filled := ok && p.Fill != nil && p.Fill.Color == (typeset.Color{R: 18.0 / 255, G: 18.0 / 255, B: 18.0 / 255, A: 1})
			if filled != tt.dark {
				t.Errorf("sheet filled dark %v, want %v", filled, tt.dark)
			}
		})
	}
}
