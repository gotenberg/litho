package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// tilingPattern renders a tiling pattern stream object whose cell runs body
// against the given /Resources.
func tilingPattern(paintType int, resources, body string) string {
	return fmt.Sprintf("<< /Type /Pattern /PatternType 1 /PaintType %d /TilingType 1 /BBox [0 0 20 20]"+
		" /XStep 20 /YStep 20 /Resources %s /Length %d >>\nstream\n%s\nendstream",
		paintType, resources, len(body), body)
}

// formXObject renders a Form XObject stream object whose content runs body
// against the given /Resources.
func formXObject(resources, body string) string {
	return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Resources %s /Length %d >>\nstream\n%s\nendstream",
		resources, len(body), body)
}

// TestCheckPatternFonts_UnembeddedFontInCell pins that a font reached only
// through a tiling pattern's cell is checked. A coloured tiling pattern
// (ISO 32000-1, 8.7.3.3) paints its cell with a content stream of its own, and
// veraPDF descends into it from the scn or SCN that selects it: pdfjs
// issue15716.pdf fills a rectangle with a pattern whose cell shows a glyph from
// an unembedded ZapfDingbats, which veraPDF reports under ISO 19005-2,
// 6.2.11.4.1 t1. Every case was measured against veraPDF.
func TestCheckPatternFonts_UnembeddedFontInCell(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const fontRes = "<< /Font << /F1 7 0 R >> >>"
	const text = "BT /F1 12 Tf 2 2 Td (a) Tj ET"
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources"+
			" << /Pattern << /P1 4 0 R /P2 5 0 R /Q 6 0 R /P3 10 0 R >> /XObject << /Fm 8 0 R >> >> >>",
		tilingPattern(1, fontRes, text),
		tilingPattern(2, fontRes, text),
		tilingPattern(1, "<< /Pattern << /Q1 9 0 R >> >>", "/Pattern cs /Q1 scn 0 0 5 5 re f"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /ZapfDingbats >>",
		formXObject("<< /Pattern << /P1 4 0 R >> >>", "/Pattern cs /P1 scn 0 0 5 5 re f"),
		tilingPattern(1, fontRes, text),
		tilingPattern(1, fontRes, "0 0 5 5 re f"),
	)

	page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("resolve page: %v", err)
	}
	res, err := document.ResolveDict(ctx, doc, page["Resources"])
	if err != nil {
		t.Fatalf("resolve resources: %v", err)
	}

	tests := []struct {
		name string
		src  string
		form bool
		want bool
	}{
		{"a coloured pattern set by scn", "/Pattern cs /P1 scn 0 0 10 10 re f", false, true},
		{"a coloured pattern set by SCN", "/Pattern CS /P1 SCN 0 0 10 10 re S", false, true},
		{"a pattern declared but never set", "0 0 10 10 re f", false, false},
		{"an uncoloured pattern", "[/Pattern /DeviceRGB] cs 1 0 0 /P2 scn 0 0 10 10 re f", false, false},
		{"a cell that shows no text", "/Pattern cs /P3 scn 0 0 10 10 re f", false, false},
		{"a pattern set inside another pattern's cell", "/Pattern cs /Q scn 0 0 10 10 re f", false, true},
		{"a pattern set inside a Form XObject", "/Fm Do", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ops := parseOps(t, tt.src)
			var got []Violation
			if tt.form {
				got = checkFormXObjectFonts(ctx, doc, ops, res, nil, 1, "PDF/A-2b", map[int]bool{}, map[int]bool{}, 0)
			} else {
				got = checkPatternFonts(ctx, doc, ops, res, nil, 1, "PDF/A-2b", map[int]bool{}, map[int]bool{}, 0)
			}
			if fired := hasRule(got, Rule_19005_2_6_2_11_4_1_t01); fired != tt.want {
				t.Errorf("6.2.11.4.1 t1 fired = %v, want %v (%v)", fired, tt.want, got)
			}
		})
	}
}
