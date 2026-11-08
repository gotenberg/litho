package pptx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
)

func TestTable_BackgroundUnderTheCells(t *testing.T) {
	t.Parallel()
	frame := `<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="3" name="T"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="0" y="0"/><a:ext cx="254000" cy="254000"/></p:xfrm>` +
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"><a:tbl><a:tblPr bandRow="1"/><a:tblGrid><a:gridCol w="127000"/></a:tblGrid>` +
		`<a:tr h="127000"><a:tc><a:tcPr/></a:tc></a:tr><a:tr h="127000"><a:tc><a:tcPr/></a:tc></a:tr><a:tr h="127000"><a:tc><a:tcPr><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill></a:tcPr></a:tc></a:tr>` +
		`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`
	style := func(bg string) string {
		return `<a:tblStyleLst xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" def="{AB}"><a:tblStyle styleId="{AB}" styleName="x">` + bg +
			`<a:wholeTbl><a:tcStyle><a:fill><a:noFill/></a:fill></a:tcStyle></a:wholeTbl>` +
			`<a:band1H><a:tcStyle><a:fill><a:solidFill><a:srgbClr val="FF0000"><a:alpha val="40000"/></a:srgbClr></a:solidFill></a:fill></a:tcStyle></a:band1H></a:tblStyle></a:tblStyleLst>`
	}
	tests := []struct {
		name                string
		bg                  string
		band1, band2, owned uint32
	}{
		// Red at 40% over white; the band without a fill shows white; the
		// cell's own opaque fill stays.
		{"solid", `<a:tblBg><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill></a:tblBg>`, 0xFF9999, 0xFFFFFF, 0x00FF00},
		// The theme's second fill style, a gradient of two stops, paints
		// its first stop: accent1 4F81BD tinted to 50%.
		{"fillRef", `<a:tblBg><a:fillRef idx="2"><a:schemeClr val="accent1"/></a:fillRef></a:tblBg>`, 0, 0, 0x00FF00},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := readDeck(t, testDeck{slides: []testSlide{{tree: frame}}, parts: map[string]string{"ppt/tableStyles.xml": style(tt.bg)}})
			rows := doc.Slides[0].Shapes[0].Table.Rows
			b1, b2, own := rows[0].Cells[0].Fill, rows[1].Cells[0].Fill, rows[2].Cells[0].Fill
			for _, f := range []slides.Fill{b1, b2, own} {
				if f.Kind != slides.FillSolid || f.Color.A != 1 {
					t.Fatalf("fill %+v, want opaque solid", f)
				}
			}
			if tt.band1 != 0 && (!sameColor(b1.Color, rgb(tt.band1)) || !sameColor(b2.Color, rgb(tt.band2))) {
				t.Errorf("bands %+v %+v", b1.Color, b2.Color)
			}
			if tt.band1 == 0 && (sameColor(b2.Color, rgb(0xFFFFFF)) || b1.Color == b2.Color) {
				t.Errorf("fillRef background %+v under band %+v", b2.Color, b1.Color)
			}
			if !sameColor(own.Color, rgb(tt.owned)) {
				t.Errorf("own fill %+v", own.Color)
			}
		})
	}
}
