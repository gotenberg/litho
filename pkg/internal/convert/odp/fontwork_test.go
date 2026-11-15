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
