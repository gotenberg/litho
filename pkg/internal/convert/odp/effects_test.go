package odp

import (
	"math"
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestRead_Effects checks the shadow, glow and soft edge read from a
// shape's graphic properties, with Impress's pool defaults.
func TestRead_Effects(t *testing.T) {
	cm := 72 / 2.54
	round := func(s *slides.Shadow) *slides.Shadow {
		if s == nil {
			return nil
		}
		c := *s
		for _, v := range []*float64{&c.OffsetX, &c.OffsetY, &c.Blur, &c.Opacity} {
			*v = math.Round(*v*1e6) / 1e6
		}
		return &c
	}
	for _, tc := range []struct {
		name, props string
		shadow      *slides.Shadow
		glow        *slides.Glow
		soft        float64
	}{
		{"hidden", `draw:shadow="hidden" draw:shadow-offset-x="1cm"`, nil, nil, 0},
		{"pool shadow", `draw:shadow="visible"`, &slides.Shadow{Color: css.Color{R: 128.0 / 255, G: 128.0 / 255, B: 128.0 / 255, A: 1}, Opacity: 1, OffsetX: 0.2 * cm, OffsetY: 0.2 * cm, ScaleX: 1, ScaleY: 1}, nil, 0},
		{
			"blurred shadow", `draw:shadow="visible" draw:shadow-offset-x="1cm" draw:shadow-offset-y="-0.5cm" draw:shadow-color="#ff0000" draw:shadow-opacity="40%" loext:shadow-blur="0.5cm"`,
			&slides.Shadow{Color: css.Color{R: 1, A: 1}, Opacity: 0.4, OffsetX: cm, OffsetY: -0.5 * cm, ScaleX: 1, ScaleY: 1, Blur: 0.5 * cm}, nil, 0,
		},
		{"glow", `loext:glow-radius="1cm" loext:glow-color="#00ff00" loext:glow-transparency="60%"`, nil, &slides.Glow{Color: css.Color{G: 1, A: 1}, Opacity: 0.4, Radius: cm}, 0},
		{"black glow by default", `loext:glow-radius="1cm"`, nil, &slides.Glow{Color: css.Color{A: 1}, Opacity: 1, Radius: cm}, 0},
		{"no glow radius", `loext:glow-radius="0cm" loext:glow-color="#00ff00"`, nil, nil, 0},
		{"soft edge", `loext:softedge-radius="0.5cm"`, nil, nil, 0.5 * cm},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := firstShape(t, styledRect("", tc.props))
			if got := round(s.Shadow); !reflect.DeepEqual(got, round(tc.shadow)) {
				t.Errorf("shadow %+v, want %+v", got, tc.shadow)
			}
			if !reflect.DeepEqual(s.Glow, tc.glow) {
				if s.Glow == nil || tc.glow == nil || s.Glow.Color != tc.glow.Color || math.Abs(s.Glow.Opacity-tc.glow.Opacity) > 1e-9 || math.Abs(s.Glow.Radius-tc.glow.Radius) > 1e-9 {
					t.Errorf("glow %+v, want %+v", s.Glow, tc.glow)
				}
			}
			if math.Abs(s.SoftEdge-tc.soft) > 1e-9 {
				t.Errorf("soft edge %v, want %v", s.SoftEdge, tc.soft)
			}
		})
	}
}
