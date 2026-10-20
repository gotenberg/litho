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
		})
	}
}
