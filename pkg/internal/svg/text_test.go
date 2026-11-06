package svg

import "testing"

func TestDraw_Text(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		probes []probe
	}{
		{"plain", `<svg width="100" height="100"><text x="10" y="50" font-family="Ahem" font-size="20" fill="blue">X</text></svg>`,
			[]probe{{20, 44, blue}, {5, 44, white}, {35, 44, white}, {20, 30, white}}},
		{"anchor-middle", `<svg width="100" height="100"><text x="50" y="50" font-family="Ahem" font-size="20" text-anchor="middle">XX</text></svg>`,
			[]probe{{45, 44, black}, {55, 44, black}, {25, 44, white}, {75, 44, white}}},
		{"anchor-end", `<svg width="100" height="100"><text x="50" y="50" font-family="Ahem" font-size="20" text-anchor="end">X</text></svg>`,
			[]probe{{40, 44, black}, {55, 44, white}}},
		{"tspan-dx", `<svg width="100" height="100"><text x="0" y="50" font-family="Ahem" font-size="10">X<tspan dx="20" fill="red">X</tspan></text></svg>`,
			[]probe{{5, 46, black}, {15, 46, white}, {35, 46, red}}},
		{"per-char-x", `<svg width="100" height="100"><text x="0 50" y="50" font-family="Ahem" font-size="10">XX</text></svg>`,
			[]probe{{5, 46, black}, {15, 46, white}, {55, 46, black}}},
		{"spaces-collapse", `<svg width="100" height="100"><text x="0" y="50" font-family="Ahem" font-size="10">  X
		    X</text></svg>`,
			[]probe{{5, 46, black}, {15, 46, white}, {25, 46, black}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkProbes(t, draw(t, tt.src, 100, 100), 40, tt.probes)
		})
	}
}
