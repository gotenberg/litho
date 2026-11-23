package odp

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
)

func TestFontwork_WarpsTheTextAndDrawsNoGeometry(t *testing.T) {
	shape := func(mode string) *slides.Shape {
		return firstShape(t, flat{
			auto: `<style:style style:name="gr1" style:family="graphic"><style:graphic-properties draw:fill="solid" draw:fill-color="#ff0000"/></style:style>`,
			body: `<draw:page draw:name="p" draw:master-page-name="Default"><draw:custom-shape draw:style-name="gr1" svg:x="1cm" svg:y="1cm" svg:width="6cm" svg:height="3cm"><text:p>Fontwork</text:p>` +
				`<draw:enhanced-geometry svg:viewBox="0 0 21600 21600" draw:text-path="true" draw:text-path-mode="` + mode + `" draw:type="fontwork-wave" ` +
				`draw:enhanced-path="M 0 0 L 21600 0 N M 0 21600 L 21600 21600 N"/></draw:custom-shape></draw:page>`,
		})
	}
	s := shape("shape")
	if s.Text == nil || s.Text.Warp == nil || s.Text.Warp.Custom == nil || !s.Text.WarpFit {
		t.Fatalf("text %+v, want a fitted warp", s.Text)
	}
	if s.Fill.Kind != slides.FillNone || len(s.Geometry.Custom.Paths) != 0 {
		t.Errorf("the Fontwork geometry is drawn: fill %v, %d paths", s.Fill.Kind, len(s.Geometry.Custom.Paths))
	}
	if c := s.Text.Paragraphs[0].Runs[0].Style.Color; c.R != 1 || c.G != 0 {
		t.Errorf("text colour %+v, want the shape's red fill", c)
	}
	if s := shape("normal"); s.Text.Warp != nil {
		t.Error("a Fontwork shape of mode normal warped its text")
	}
}

// TestFontwork_KeepsTheShapesAreaAndLine checks that a Fontwork shape's
// gradient fill and line are kept for its warped glyphs.
func TestFontwork_KeepsTheShapesAreaAndLine(t *testing.T) {
	s := firstShape(t, flat{
		styles: `<draw:gradient draw:name="G" draw:style="linear" draw:start-color="#000000" draw:end-color="#8d281e" draw:angle="0deg" draw:border="0%"/>`,
		auto:   `<style:style style:name="gr1" style:family="graphic"><style:graphic-properties draw:fill="gradient" draw:fill-gradient-name="G" draw:stroke="solid" svg:stroke-color="#ffbf00" svg:stroke-width="0.1cm"/></style:style>`,
		body: `<draw:page draw:name="p" draw:master-page-name="Default"><draw:custom-shape draw:style-name="gr1" svg:x="1cm" svg:y="1cm" svg:width="6cm" svg:height="3cm"><text:p>Fontwork</text:p>` +
			`<draw:enhanced-geometry svg:viewBox="0 0 21600 21600" draw:text-path="true" draw:text-path-mode="shape" draw:type="fontwork-wave" ` +
			`draw:enhanced-path="M 0 0 L 21600 0 N M 0 21600 L 21600 21600 N"/></draw:custom-shape></draw:page>`,
	})
	if s.Text.WarpFill == nil || s.Text.WarpFill.Kind != slides.FillGradient {
		t.Errorf("warp fill %+v, want the shape's gradient", s.Text.WarpFill)
	}
	if s.Text.WarpLine == nil || s.Text.WarpLine.Color.R != 1 {
		t.Errorf("warp line %+v, want the shape's amber line", s.Text.WarpLine)
	}
	if s.Fill.Kind != slides.FillNone || s.Line.Color.A != 0 {
		t.Error("the Fontwork shape keeps its own fill or line")
	}
}
