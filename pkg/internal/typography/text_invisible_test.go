package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func TestTextIsInvisible_ReadsFillColour(t *testing.T) {
	op := func(name string, operands ...ast.PdfObject) content.Operation {
		return content.Operation{Operator: name, Operands: operands}
	}
	one, zero := ast.PdfInt(1), ast.PdfInt(0)
	resources := ast.PdfDict{
		"ColorSpace": ast.PdfDict{"CS0": ast.PdfArray{ast.PdfName("DeviceRGB")}},
		"Pattern":    ast.PdfDict{"P1": ast.PdfDict{"PatternType": ast.PdfInt(2)}},
		"ExtGState": ast.PdfDict{
			"NoStroke": ast.PdfDict{"CA": zero},
			"NoFill":   ast.PdfDict{"ca": zero},
		},
	}
	tests := []struct {
		name string
		ops  []content.Operation
		want bool
	}{
		{"initial black", nil, false},
		{"white gray", []content.Operation{op("g", one)}, true},
		{"white rgb", []content.Operation{op("rg", one, one, one)}, true},
		{"white cmyk", []content.Operation{op("k", zero, zero, zero, zero)}, true},
		{"white through sc", []content.Operation{op("cs", ast.PdfName("DeviceRGB")), op("sc", one, one, one)}, true},
		// cs resets the colour to the space's initial one (ISO 32000-1, 8.6.8).
		{"cs resets white", []content.Operation{op("g", one), op("cs", ast.PdfName("DeviceGray"))}, false},
		{"resource-named device space", []content.Operation{op("cs", ast.PdfName("CS0")), op("sc", one, one, one)}, false},
		{"pattern", []content.Operation{op("cs", ast.PdfName("Pattern")), op("scn", ast.PdfName("P1"))}, false},
		{"white after pattern", []content.Operation{op("cs", ast.PdfName("Pattern")), op("scn", ast.PdfName("P1")), op("rg", one, one, one)}, true},
		{"mode 3", []content.Operation{op("Tr", ast.PdfInt(3))}, true},
		{"mode 3 black", []content.Operation{op("Tr", ast.PdfInt(3)), op("g", zero)}, true},
		// ISO 32000-1, 9.3.6, Table 106: mode 7 only clips, and what the
		// page paints next shows through the glyphs.
		{"mode 7", []content.Operation{op("Tr", ast.PdfInt(7))}, false},
		{"mode 7 white", []content.Operation{op("Tr", ast.PdfInt(7)), op("g", one)}, false},
		{"mode 4 white", []content.Operation{op("Tr", ast.PdfInt(4)), op("g", one)}, true},
		// Modes 1 and 5 stroke alone: the stroke decides.
		{"mode 1 white stroke", []content.Operation{op("Tr", one), op("G", one)}, true},
		{"mode 1 white fill black stroke", []content.Operation{op("Tr", one), op("g", one)}, false},
		{"mode 5 white stroke black fill", []content.Operation{op("Tr", ast.PdfInt(5)), op("G", one)}, true},
		// Modes 2 and 6 fill and stroke: white only when both are.
		{"mode 2 white fill black stroke", []content.Operation{op("Tr", ast.PdfInt(2)), op("g", one)}, false},
		{"mode 2 white fill and stroke", []content.Operation{op("Tr", ast.PdfInt(2)), op("g", one), op("G", one)}, true},
		{"mode 6 black fill white stroke", []content.Operation{op("Tr", ast.PdfInt(6)), op("G", one)}, false},
		// A zero line width still strokes a hairline (8.4.3.2).
		{"mode 2 zero-width black stroke", []content.Operation{op("Tr", ast.PdfInt(2)), op("g", one), op("w", zero)}, false},
		// A stroke at CA 0 leaves no mark (11.6.4.4): the fill decides.
		{"mode 2 transparent stroke", []content.Operation{op("Tr", ast.PdfInt(2)), op("g", one), op("gs", ast.PdfName("NoStroke"))}, true},
		{"mode 2 transparent fill", []content.Operation{op("Tr", ast.PdfInt(2)), op("G", one), op("gs", ast.PdfName("NoFill"))}, true},
		{"mode 1 white stroke pattern fill", []content.Operation{op("Tr", one), op("cs", ast.PdfName("Pattern")), op("scn", ast.PdfName("P1")), op("G", one)}, true},
		{"mode 1 pattern stroke", []content.Operation{op("Tr", one), op("CS", ast.PdfName("Pattern")), op("SCN", ast.PdfName("P1"))}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := graphics.NewMachine([4]float64{0, 0, 612, 792})
			m.Resources = resources
			if err := m.Execute(context.Background(), tt.ops); err != nil {
				t.Fatal(err)
			}
			if got := textIsInvisible(m.Current()); got != tt.want {
				t.Errorf("textIsInvisible = %v, want %v (fill %+v)", got, tt.want, m.Current().Fill)
			}
			// Only the white judgement is a renderer's to overturn: mode 3
			// paints nothing whatever lies under it.
			if got, want := paintsOnlyWhite(m.Current()), tt.want && m.Current().Text.Tmode != 3; got != want {
				t.Errorf("paintsOnlyWhite = %v, want %v", got, want)
			}
		})
	}
}
