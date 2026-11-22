package odt

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// TestFrameFill locks the fills of a text frame: a colour made
// transparent by draw:opacity or style:background-transparency, a linear
// gradient from its start colour at the top, a radial one from its end
// colour at its centre, and a bitmap.
func TestFrameFill(t *testing.T) {
	d := doc{
		styles: `<draw:gradient draw:name="lin" draw:style="linear" draw:start-color="#ff0000" draw:end-color="#0000ff" draw:angle="0" draw:border="0%"/>` +
			`<draw:gradient draw:name="rad" draw:style="radial" draw:cx="30%" draw:cy="40%" draw:start-color="#800000" draw:end-color="#ffff00" draw:border="20%"/>` +
			`<draw:fill-image draw:name="pic" xlink:href="Pictures/p.png"/>`,
		contentAuto: `<style:style style:name="op" style:family="graphic"><style:graphic-properties draw:fill="solid" draw:fill-color="#008000" draw:opacity="55%"/></style:style>` +
			`<style:style style:name="tr" style:family="graphic"><style:graphic-properties draw:fill="solid" style:background-transparency="100%"/></style:style>` +
			`<style:style style:name="lin" style:family="graphic"><style:graphic-properties draw:fill="gradient" draw:fill-gradient-name="lin"/></style:style>` +
			`<style:style style:name="rad" style:family="graphic"><style:graphic-properties draw:fill="gradient" draw:fill-gradient-name="rad"/></style:style>` +
			`<style:style style:name="bmp" style:family="graphic"><style:graphic-properties draw:fill="bitmap" draw:fill-image-name="pic"/></style:style>`,
		body: `<text:p>a</text:p>`,
	}
	b := newTestBuilder(t, d)
	fill := func(name string) *css.Style {
		st := baseStyle()
		b.frameFill(st, b.styles.Resolve(odf.ScopeContent, odf.FamilyGraphic, name, odf.GraphicProps), 30, 40)
		return st
	}
	if c := fill("op").BackgroundColor; !near(c.A, 0.55, 1e-9) || !near(c.G, 128.0/255, 1e-9) {
		t.Errorf("colour at 55%% opacity: %+v", c)
	}
	if c := fill("tr").BackgroundColor; c.A != 0 {
		t.Errorf("colour of transparency 100%%: %+v, want transparent", c)
	}
	if g := fill("lin").BackgroundGradient; g == nil || g.Kind != css.LinearGradient || g.Angle != 180 || g.Stops[0].Color != (css.Color{R: 1, A: 1}) {
		t.Errorf("linear gradient: %+v, want red at the top", g)
	}
	g := fill("rad").BackgroundGradient
	if g == nil || g.Kind != css.RadialGradient || g.Stops[0].Color != (css.Color{R: 1, G: 1, A: 1}) || !near(g.Radius[0].Value, 25, 1e-9) ||
		!near(g.Center[0].Value, 0.3, 1e-9) || !near(g.Stops[1].Position.Value, 0.8, 1e-9) {
		t.Errorf("radial gradient: %+v, want yellow at (30%%, 40%%), maroon from 0.8 of a 25-point radius", g)
	}
	if st := fill("bmp"); st.BackgroundImage != pictureURL("Pictures/p.png") {
		t.Errorf("bitmap: %q", st.BackgroundImage)
	}
}
