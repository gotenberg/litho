package svg

import "testing"

// TestGeometry_PropertiesOverAttributes checks the geometry properties of
// SVG 2, 6.7 and 10: a style rule or a style attribute that sets one wins
// over the attribute of the same name; a text's x and y stay attributes.
func TestGeometry_PropertiesOverAttributes(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		probes []probe
	}{
		{
			"circle", `<svg width="100" height="100"><style>circle { cx: 30px; cy: 40px; r: 20 }</style><circle cx="80" cy="80" r="5" fill="red"/></svg>`,
			[]probe{{30, 40, red}, {80, 80, white}},
		},
		{
			"rect", `<svg width="100" height="100"><rect x="0" y="0" width="5" height="5" style="x: 10px; y: 20px; width: 50%; height: 30px" fill="red"/></svg>`,
			[]probe{{30, 30, red}, {2, 2, white}, {65, 30, white}},
		},
		{
			"rect-auto-width", `<svg width="100" height="100"><style>rect { width: auto }</style><rect x="10" y="10" width="50" height="50" fill="red"/></svg>`,
			[]probe{{30, 30, white}},
		},
		{
			"ellipse-auto", `<svg width="100" height="100"><style>ellipse { cx: 50px; cy: 50px; rx: 40px; ry: auto }</style><ellipse rx="5" ry="10" fill="lime"/></svg>`,
			[]probe{{50, 15, lime}, {50, 50, lime}},
		},
		{
			"path", `<svg width="100" height="100"><style>path { d: path("M 10 10 L 90 10 L 90 90 Z") }</style><path d="M 0 0 L 5 0 L 0 5 Z" fill="blue"/></svg>`,
			[]probe{{80, 20, blue}, {20, 80, white}},
		},
		{
			"path-none", `<svg width="100" height="100"><style>path { d: none }</style><path d="M 0 0 L 100 0 L 0 100 Z" fill="blue"/></svg>`,
			[]probe{{10, 10, white}},
		},
		{
			"important", `<svg width="100" height="100"><style>rect { width: 80px !important }</style><rect x="0" y="0" width="10" height="10" style="width: 30px" fill="red"/></svg>`,
			[]probe{{70, 5, red}},
		},
		{
			"invalid-value", `<svg width="100" height="100"><style>circle { r: red }</style><circle cx="50" cy="50" r="30" fill="lime"/></svg>`,
			[]probe{{50, 50, lime}, {75, 50, lime}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkProbes(t, draw(t, tt.src, 100, 100), 40, tt.probes)
		})
	}
}
