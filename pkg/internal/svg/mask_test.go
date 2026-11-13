package svg

import "testing"

func TestDraw_MaskAndBlending(t *testing.T) {
	cyan := [3]uint8{0, 255, 255}
	yellow := [3]uint8{255, 255, 0}
	tests := []struct {
		name, src string
		probes    []probe
	}{
		{
			// A mask by luminance: white keeps, black masks out, its
			// region the bounding box grown by 10% each side.
			"mask-luminance", `<svg width="100" height="100"><mask id="m"><rect width="50" height="100" fill="white"/><rect x="50" width="50" height="100" fill="black"/></mask><rect width="100" height="100" fill="red" mask="url(#m)"/></svg>`,
			[]probe{{25, 50, red}, {75, 50, white}},
		},
		{
			"mask-type-alpha", `<svg width="100" height="100"><mask id="m" mask-type="alpha"><rect x="50" width="50" height="100" fill="black"/></mask><rect width="100" height="100" fill="red" mask="url(#m)"/></svg>`,
			[]probe{{25, 50, white}, {75, 50, red}},
		},
		{
			"mask-units", `<svg width="100" height="100"><mask id="m" maskUnits="userSpaceOnUse" x="0" y="0" width="30" height="100"><rect width="100" height="100" fill="white"/></mask><rect width="100" height="100" fill="red" mask="url(#m)"/></svg>`,
			[]probe{{15, 50, red}, {60, 50, white}},
		},
		{
			"mask-content-units", `<svg width="100" height="100"><mask id="m" maskContentUnits="objectBoundingBox"><rect width="0.5" height="1" fill="white"/></mask><rect x="20" width="80" height="100" fill="red" mask="url(#m)"/></svg>`,
			[]probe{{40, 50, red}, {80, 50, white}},
		},
		{
			// Multiply blends with what lies beneath in the drawing.
			"mix-blend-mode", `<svg width="100" height="100"><rect width="100" height="100" fill="yellow"/><rect width="50" height="100" fill="cyan" style="mix-blend-mode:multiply"/></svg>`,
			[]probe{{25, 50, lime}, {75, 50, yellow}},
		},
		{
			// An isolated group blends its content with nothing beneath it.
			"isolation", `<svg width="100" height="100"><rect width="100" height="100" fill="yellow"/><g style="isolation:isolate"><rect width="50" height="100" fill="cyan" style="mix-blend-mode:multiply"/></g></svg>`,
			[]probe{{25, 50, cyan}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkProbes(t, draw(t, tt.src, 100, 100), 40, tt.probes)
		})
	}
}
