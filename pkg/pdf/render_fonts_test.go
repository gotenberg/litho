package pdf

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// unembeddedTextPDF is a 100 by 100 page showing A at 50 points in a TrueType
// font named Rect that it does not embed, declared 600 units wide: the glyph
// spans user x 10..40 and y 10..35, device rows 65..90.
func unembeddedTextPDF() []byte {
	const content = "0 g BT /F 50 Tf 10 10 Td (A) Tj ET"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R /Resources << /Font << /F 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /TrueType /BaseFont /Rect /FirstChar 65 /LastChar 65 /Widths [600] /FontDescriptor 6 0 R >>",
		"<< /Type /FontDescriptor /FontName /Rect /Flags 32 /FontBBox [0 0 1000 500] /ItalicAngle 0 /Ascent 500 /Descent 0 /CapHeight 500 /StemV 80 >>",
	})
}

// rectProgram is a program named Rect whose A is a filled 600 by 500
// rectangle.
func rectProgram() []byte {
	return fontbuild.TrueType{PostScriptName: "Rect", Family: "Rect", Subfamily: "Regular",
		Glyphs: []fontbuild.Glyph{{Name: "A", Unicode: 'A', Advance: 600}}}.Build()
}

var (
	ink   = color.NRGBA{0, 0, 0, 255}
	paper = color.NRGBA{255, 255, 255, 255}
)

func TestRenderPageFonts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openLimited(t, unembeddedTextPDF(), Limits{})
	program := rectProgram()

	// The middle of the glyph, and its left edge.
	const midX, midY, edgeX = 25, 78, 10
	cases := []struct {
		name string
		opts []RenderOption
		mid  color.NRGBA
	}{
		// No program: a hollow placeholder box.
		{"none", nil, paper},
		{"files", []RenderOption{WithRenderFontFiles(fstest.MapFS{"fonts/rect.ttf": {Data: program}})}, ink},
		{"provider", []RenderOption{WithRenderFontProvider(FontProviderFunc(func(_ context.Context, req FontRequest) ([]byte, error) {
			if req.BaseFont != "Rect" || req.Subtype != "TrueType" {
				return nil, nil
			}
			return program, nil
		}))}, ink},
		// A provider that fails draws placeholders and fails nothing.
		{"failing", []RenderOption{WithRenderFontProvider(FontProviderFunc(func(context.Context, FontRequest) ([]byte, error) {
			return nil, errors.New("boom")
		}))}, paper},
		// A provider that supplies nothing gives way to the next.
		{"chain", []RenderOption{
			WithRenderFontProvider(FontProviderFunc(func(context.Context, FontRequest) ([]byte, error) { return nil, nil })),
			WithRenderFontFiles(fstest.MapFS{"rect.ttf": {Data: program}}),
		}, ink},
	}
	for _, c := range cases {
		img, err := r.RenderPage(ctx, 1, append([]RenderOption{WithRenderDPI(72)}, c.opts...)...)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := nrgbaAt(t, img, midX, midY); got != c.mid {
			t.Errorf("%s: glyph middle %v, want %v", c.name, got, c.mid)
		}
		if got := nrgbaAt(t, img, edgeX, midY); got != ink {
			t.Errorf("%s: glyph edge %v, want ink", c.name, got)
		}
	}
}

func TestWithRenderSystemFonts(t *testing.T) {
	t.Parallel()
	var o renderOpts
	WithRenderSystemFonts()(&o)
	if o.systemFonts == nil {
		t.Fatal("the option registered no provider")
	}
	if p := renderFontProvider(nil, []FontProvider{o.systemFonts}); p == nil {
		t.Error("no glyph provider over the system fonts")
	}
	if p := renderFontProvider(nil, nil); p != nil {
		t.Error("a glyph provider over no font provider")
	}
}
