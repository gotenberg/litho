package odf

import (
	"context"
	"testing"
)

// TestLegacyShapeColours locks the default graphic colours LibreOffice
// gives the documents of older producers (see [LegacyShapeColours],
// which holds the measurements), and that a default style keeps the
// colours it states.
func TestLegacyShapeColours(t *testing.T) {
	tests := []struct {
		generator, fill, stroke string
	}{
		{"OpenOffice.org/3.3$Unix OpenOffice.org_project/330m20$Build-9567", "#99ccff", "#000000"},
		{"OpenOffice.org/2.4$Win32 OpenOffice.org_project/680m17$Build-9310", "#99ccff", "#000000"},
		{"OpenOffice/4.0.0$Unix OpenOffice.org_project/400m3$Build-9702", "#cfe7f5", "#808080"},
		{"OpenOffice/4.1.7$Win32 OpenOffice.org_project/417m1$Build-9800", "#cfe7f5", "#808080"},
		{"LibreOffice/4.0.2.2$Linux_X86_64 LibreOffice_project/4c82dcdd6efcd48b1d8bba66bfe1989deee49c3", "#99ccff", "#000000"},
		{"LibreOffice/3.6$Linux_X86_64 LibreOffice_project/abc", "#99ccff", "#000000"},
		{"LibreOffice/4.1.0.4$Linux_X86_64 LibreOffice_project/abc", "", ""},
		{"LibreOffice/7.4.2.3$Linux_X86_64 LibreOffice_project/30$Build-2", "", ""},
		{"StarOffice 7", "#99ccff", "#000000"},
		{"OpenOffice.org/1.1.5$Win32", "", ""},
		{"StarOffice/7$Linux OpenOffice.org_project/680m1", "", ""},
		{"Calligra/3.2", "", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		doc := `<office:document ` + drawNS + `><office:meta><meta:generator>` + tt.generator + `</meta:generator></office:meta>` +
			`<office:styles><style:default-style style:family="graphic"><style:graphic-properties svg:stroke-width="0cm"/></style:default-style></office:styles></office:document>`
		p, err := Open(context.Background(), []byte(doc), Options{})
		if err != nil {
			t.Fatal(err)
		}
		fill, stroke := LegacyShapeColours(p)
		if fill != tt.fill || stroke != tt.stroke {
			t.Errorf("%q: %q, %q, want %q, %q", tt.generator, fill, stroke, tt.fill, tt.stroke)
		}
		s := NewStyles(p)
		s.SetDefaultGraphicColours(fill, stroke)
		props := s.Resolve(ScopeContent, FamilyGraphic, "", GraphicProps)
		if props.Get("draw:fill-color") != tt.fill || props.Get("svg:stroke-color") != tt.stroke || props.Get("svg:stroke-width") != "0cm" {
			t.Errorf("%q: default style reads fill %q, stroke %q, width %q", tt.generator, props.Get("draw:fill-color"), props.Get("svg:stroke-color"), props.Get("svg:stroke-width"))
		}
	}
	doc := `<office:document ` + drawNS + `><office:styles><style:default-style style:family="graphic"><style:graphic-properties draw:fill-color="#ff0000"/></style:default-style></office:styles></office:document>`
	p, err := Open(context.Background(), []byte(doc), Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := NewStyles(p)
	s.SetDefaultGraphicColours("#cfe7f5", "#808080")
	props := s.Resolve(ScopeContent, FamilyGraphic, "", GraphicProps)
	if props.Get("draw:fill-color") != "#ff0000" || props.Get("svg:stroke-color") != "#808080" {
		t.Errorf("stated fill: fill %q, stroke %q", props.Get("draw:fill-color"), props.Get("svg:stroke-color"))
	}
}
