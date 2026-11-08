package odp

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// styledRect returns a document of one rectangle in the automatic
// graphic style of the graphic properties props, over office:styles
// holding drawing styles; its parent, a common style setting nothing,
// keeps the built-in "standard" style out.
func styledRect(drawStyles, props string) flat {
	return flat{
		styles: drawStyles + `<style:style style:name="base" style:family="graphic"/>`,
		auto:   `<style:style style:name="s" style:family="graphic" style:parent-style-name="base"><style:graphic-properties ` + props + `/></style:style>`,
		body:   page(`<draw:rect draw:style-name="s" svg:width="5cm" svg:height="5cm"/>`),
	}
}

func TestRead_Fill(t *testing.T) {
	red, blue := css.Color{R: 1, A: 1}, css.Color{B: 1, A: 1}
	grads := `<draw:gradient draw:name="lin" draw:style="linear" draw:start-color="#ff0000" draw:end-color="#0000ff" draw:angle="90deg" draw:border="40%"/>` +
		`<draw:gradient draw:name="ax" draw:style="axial" draw:start-color="#ff0000" draw:end-color="#0000ff" draw:angle="0deg" draw:border="50%"/>` +
		`<draw:gradient draw:name="rad" draw:style="radial" draw:start-color="#ff0000" draw:end-color="#0000ff" draw:border="50%"/>` +
		`<draw:gradient draw:name="int" draw:style="linear" draw:start-color="#ff0000" draw:end-color="#0000ff" draw:start-intensity="50%"/>` +
		`<draw:hatch draw:name="h" draw:style="double" draw:color="#000000" draw:distance="0.1cm" draw:rotation="45deg"/>` +
		`<draw:fill-image draw:name="img"><office:binary-data>iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==</office:binary-data></draw:fill-image>`
	tests := []struct {
		name, props string
		want        slides.Fill
	}{
		{"pool default", ``, slides.Fill{Kind: slides.FillSolid, Color: cssColor(poolFill)}},
		{"none", `draw:fill="none"`, slides.Fill{}},
		{"solid with opacity", `draw:fill="solid" draw:fill-color="#ff0000" draw:opacity="25%"`, slides.Fill{Kind: slides.FillSolid, Color: css.Color{R: 1, A: 0.25}}},
		// Measured on the pinned LibreOffice: a linear gradient at 90
		// degrees runs left to right, its border in the start colour.
		{"linear", `draw:fill="gradient" draw:fill-gradient-name="lin"`, slides.Fill{Kind: slides.FillGradient, Gradient: &slides.Gradient{
			Stops: []slides.Stop{{Position: 0, Color: red}, {Position: 0.4, Color: red}, {Position: 1, Color: blue}}}}},
		// An axial gradient puts the start colour in the middle and its
		// border in the end colour at both edges.
		{"axial", `draw:fill="gradient" draw:fill-gradient-name="ax"`, slides.Fill{Kind: slides.FillGradient, Gradient: &slides.Gradient{Angle: 90,
			Stops: []slides.Stop{{Position: 0, Color: blue}, {Position: 0.25, Color: blue}, {Position: 0.5, Color: red}, {Position: 0.75, Color: blue}, {Position: 1, Color: blue}}}}},
		// A radial gradient has the end colour at the centre, its border
		// in the start colour outside.
		{"radial", `draw:fill="gradient" draw:fill-gradient-name="rad"`, slides.Fill{Kind: slides.FillGradient, Gradient: &slides.Gradient{Radial: true,
			Stops: []slides.Stop{{Position: 0, Color: blue}, {Position: 0.5, Color: red}, {Position: 1, Color: red}}}}},
		{"intensity", `draw:fill="gradient" draw:fill-gradient-name="int"`, slides.Fill{Kind: slides.FillGradient, Gradient: &slides.Gradient{Angle: 90,
			Stops: []slides.Stop{{Position: 0, Color: css.Color{R: 128.0 / 255, A: 1}}, {Position: 1, Color: blue}}}}},
		{"hatch", `draw:fill="hatch" draw:fill-hatch-name="h" draw:fill-hatch-solid="true" draw:fill-color="#ffffff"`,
			slides.Fill{Kind: slides.FillPattern, Pattern: "diagCross", Color: css.Color{A: 1}, Background: css.Color{R: 1, G: 1, B: 1, A: 1}}},
	}
	t.Run("standard", func(t *testing.T) {
		s := firstShape(t, flat{body: page(`<draw:rect svg:width="1cm" svg:height="1cm"/>`)})
		green := css.Color{R: 0x18 / 255.0, G: 0xa3 / 255.0, B: 0x03 / 255.0, A: 1}
		if s.Fill.Kind != slides.FillSolid || s.Fill.Color != green || !near(s.Line.Width, cm(0.106)) {
			t.Errorf("a shape of no style = %+v, want the built-in standard style's", s)
		}
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstShape(t, styledRect(grads, tt.props)).Fill
			if got.Kind != tt.want.Kind || got.Color != tt.want.Color || got.Background != tt.want.Background || got.Pattern != tt.want.Pattern {
				t.Fatalf("fill = %+v, want %+v", got, tt.want)
			}
			if tt.want.Gradient != nil {
				g, w := got.Gradient, tt.want.Gradient
				if g.Angle != w.Angle || g.Radial != w.Radial || !slices.EqualFunc(g.Stops, w.Stops, func(a, b slides.Stop) bool {
					return near(a.Position, b.Position) && a.Color == b.Color
				}) {
					t.Errorf("gradient = %+v, want %+v", *g, *w)
				}
			}
		})
	}
	t.Run("bitmap", func(t *testing.T) {
		got := firstShape(t, styledRect(grads, `draw:fill="bitmap" draw:fill-image-name="img" style:repeat="stretch"`)).Fill
		if got.Kind != slides.FillPicture || got.Picture == nil || got.Tile {
			t.Errorf("fill = %+v", got)
		}
	})
}

func TestRead_Line(t *testing.T) {
	dashes := `<draw:stroke-dash draw:name="d1" draw:style="rect" draw:dots1="1" draw:dots1-length="0.5cm" draw:dots2="1" draw:dots2-length="0.1cm" draw:distance="0.2cm"/>` +
		`<draw:stroke-dash draw:name="d2" draw:style="rect" draw:dots1="2" draw:dots1-length="200%" draw:distance="100%"/>` +
		`<draw:stroke-dash draw:name="dot" draw:style="round" draw:dots1="1" draw:distance="0.05cm"/>` +
		`<draw:marker draw:name="Arrow" svg:viewBox="0 0 20 30" svg:d="M10 0l-10 30h20z"/>` +
		`<draw:marker draw:name="Circle" svg:viewBox="0 0 10 10" svg:d="M0 0"/>`
	tests := []struct {
		name, props string
		want        slides.Line
	}{
		{"none", `draw:stroke="none"`, slides.Line{}},
		{"pool default hairline", ``, slides.Line{Width: hairline, Color: cssColor(poolStroke)}},
		{"solid", `draw:stroke="solid" svg:stroke-width="0.1cm" svg:stroke-color="#ff0000" svg:stroke-linecap="square" draw:stroke-linejoin="miter"`,
			slides.Line{Width: cm(0.1), Color: css.Color{R: 1, A: 1}, Cap: slides.CapSquare, Join: slides.JoinMiter}},
		// Measured on the pinned LibreOffice: d1 at 0.1 cm prints the dash
		// array [14.173 5.669 2.835 5.669], d2 at 0.2 cm [11.338 5.669
		// 11.338 5.669].
		{"absolute dash", `draw:stroke="dash" draw:stroke-dash="d1" svg:stroke-width="0.1cm" svg:stroke-color="#000000"`,
			slides.Line{Width: cm(0.1), Color: css.Color{A: 1}, Dash: []float64{cm(0.5), cm(0.2), cm(0.1), cm(0.2)}}},
		{"relative dash", `draw:stroke="dash" draw:stroke-dash="d2" svg:stroke-width="0.2cm" svg:stroke-color="#000000"`,
			slides.Line{Width: cm(0.2), Color: css.Color{A: 1}, Dash: []float64{cm(0.4), cm(0.2), cm(0.4), cm(0.2)}}},
		{"dots", `draw:stroke="dash" draw:stroke-dash="dot" svg:stroke-width="0.1cm" svg:stroke-color="#000000"`,
			slides.Line{Width: cm(0.1), Color: css.Color{A: 1}, Dash: []float64{cm(0.1), cm(0.05)}, Cap: slides.CapRound}},
		{"markers", `draw:stroke="solid" svg:stroke-width="0.1cm" svg:stroke-color="#000000" draw:marker-start="Arrow" draw:marker-start-width="0.6cm" draw:marker-end="Circle"`,
			slides.Line{Width: cm(0.1), Color: css.Color{A: 1}, Head: slides.Arrow{Kind: slides.ArrowTriangle, Width: 2, Length: 3}, Tail: slides.Arrow{Kind: slides.ArrowOval, Width: 1, Length: 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstShape(t, styledRect(dashes, tt.props)).Line
			w := tt.want
			if !near(got.Width, w.Width) || got.Color != w.Color || !nearAll(got.Dash, w.Dash) || got.Cap != w.Cap || got.Join != w.Join ||
				got.Head.Kind != w.Head.Kind || !near(got.Head.Width, w.Head.Width) || !near(got.Head.Length, w.Head.Length) || got.Tail != w.Tail {
				t.Errorf("line = %+v, want %+v", got, w)
			}
		})
	}
}

func TestRead_TextBodyProps(t *testing.T) {
	tests := []struct {
		name, props string
		shape       string
		want        slides.TextBody
	}{
		{"frame defaults", ``, "frame", slides.TextBody{Wrap: true, Autofit: slides.AutofitGrow}},
		{"frame", `fo:padding="0.1cm" fo:padding-left="0.2cm" draw:textarea-vertical-align="bottom" draw:auto-grow-height="false" style:shrink-to-fit="true" fo:column-count="2" fo:column-gap="0.5cm" style:writing-mode="tb-rl"`, "frame",
			slides.TextBody{Insets: [4]float64{cm(0.1), cm(0.1), cm(0.1), cm(0.2)}, Anchor: slides.AnchorBottom, Wrap: true, Autofit: slides.AutofitShrink, Columns: 2, ColumnGap: cm(0.5), Vertical: css.VerticalRL}},
		{"shape no wrap", `fo:wrap-option="no-wrap" draw:textarea-vertical-align="middle"`, "shape", slides.TextBody{Anchor: slides.AnchorMiddle}},
		{"shape grows", `draw:auto-grow-height="true" fo:wrap-option="wrap"`, "shape", slides.TextBody{Wrap: true, Autofit: slides.AutofitGrow}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `<draw:frame draw:style-name="s" svg:width="5cm" svg:height="2cm"><draw:text-box><text:p>x</text:p></draw:text-box></draw:frame>`
			if tt.shape == "shape" {
				body = `<draw:custom-shape draw:style-name="s" svg:width="5cm" svg:height="2cm"><text:p>x</text:p><draw:enhanced-geometry draw:type="ellipse"/></draw:custom-shape>`
			}
			// An automatic style with a parent of its own takes no
			// built-in "standard", whose padding would show here.
			f := flat{
				styles: `<style:style style:name="base" style:family="graphic"/>`,
				auto:   `<style:style style:name="s" style:family="graphic" style:parent-style-name="base"><style:graphic-properties ` + tt.props + `/></style:style>`,
				body:   page(body),
			}
			tb := firstShape(t, f).Text
			w := tt.want
			if tb == nil || !nearAll(tb.Insets[:], w.Insets[:]) || tb.Anchor != w.Anchor || tb.Wrap != w.Wrap || tb.Autofit != w.Autofit ||
				tb.Columns != w.Columns || !near(tb.ColumnGap, w.ColumnGap) || tb.Vertical != w.Vertical {
				t.Errorf("text body = %+v, want %+v", tb, w)
			}
		})
	}
}
