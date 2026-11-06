package svg

import (
	"testing"
)

func TestDraw_Shapes(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		probes []probe
	}{
		{"rect", `<svg width="100" height="100"><rect x="10" y="10" width="50" height="30" fill="red"/></svg>`,
			[]probe{{30, 20, red}, {5, 5, white}, {70, 20, white}, {30, 50, white}}},
		{"rounded-rect", `<svg width="100" height="100"><rect x="10" y="10" width="80" height="80" rx="20" fill="blue"/></svg>`,
			[]probe{{50, 50, blue}, {12, 12, white}, {50, 12, blue}}},
		{"circle", `<svg width="100" height="100"><circle cx="50" cy="50" r="40" fill="lime"/></svg>`,
			[]probe{{50, 50, lime}, {15, 15, white}, {88, 50, lime}}},
		{"ellipse", `<svg width="100" height="100"><ellipse cx="50" cy="50" rx="40" ry="10" fill="lime"/></svg>`,
			[]probe{{50, 50, lime}, {50, 30, white}, {85, 50, lime}}},
		{"polygon", `<svg width="100" height="100"><polygon points="0,0 100,0 0,100" fill="red"/></svg>`,
			[]probe{{10, 10, red}, {90, 90, white}}},
		{"path-arc", `<svg width="100" height="100"><path d="M10 50 A40 40 0 0 1 90 50 Z" fill="blue"/></svg>`,
			[]probe{{50, 30, blue}, {50, 70, white}}},
		{"path-relative", `<svg width="100" height="100"><path d="m10 10 h80 v80 h-80 z" fill="red"/></svg>`,
			[]probe{{50, 50, red}, {5, 5, white}}},
		{"stroke", `<svg width="100" height="100"><line x1="0" y1="50" x2="100" y2="50" stroke="black" stroke-width="10"/></svg>`,
			[]probe{{50, 50, black}, {50, 47, black}, {50, 40, white}}},
		{"evenodd", `<svg width="100" height="100"><path fill-rule="evenodd" d="M0 0H100V100H0Z M25 25H75V75H25Z" fill="red"/></svg>`,
			[]probe{{10, 10, red}, {50, 50, white}}},
		{"viewbox", `<svg width="100" height="100" viewBox="0 0 10 10"><rect width="5" height="5" fill="red"/></svg>`,
			[]probe{{25, 25, red}, {75, 75, white}}},
		{"transform", `<svg width="100" height="100"><g transform="translate(50 50)"><rect width="20" height="20" fill="red"/></g></svg>`,
			[]probe{{60, 60, red}, {40, 40, white}}},
		{"style-element", `<svg width="100" height="100"><style>.a{fill:blue} #b{fill:lime}</style><rect class="a" width="50" height="100" fill="red"/><rect id="b" x="50" width="50" height="100"/></svg>`,
			[]probe{{25, 50, blue}, {75, 50, lime}}},
		{"style-attr", `<svg width="100" height="100"><rect width="100" height="100" fill="red" style="fill: blue"/></svg>`,
			[]probe{{50, 50, blue}}},
		{"inherit", `<svg width="100" height="100"><g fill="lime"><rect width="100" height="100"/></g></svg>`,
			[]probe{{50, 50, lime}}},
		{"use", `<svg width="100" height="100"><defs><rect id="r" width="20" height="20" fill="red"/></defs><use href="#r" x="50" y="50"/></svg>`,
			[]probe{{60, 60, red}, {10, 10, white}}},
		{"display-none", `<svg width="100" height="100"><rect width="100" height="100" fill="red" display="none"/></svg>`,
			[]probe{{50, 50, white}}},
		{"current-color", `<svg width="100" height="100" color="blue"><rect width="100" height="100" fill="currentColor"/></svg>`,
			[]probe{{50, 50, blue}}},
		{"linear-gradient", `<svg width="100" height="100"><linearGradient id="g"><stop offset="0" stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient><rect width="100" height="100" fill="url(#g)"/></svg>`,
			[]probe{{2, 50, red}, {97, 50, blue}}},
		{"radial-gradient", `<svg width="100" height="100"><radialGradient id="g"><stop offset="0" stop-color="red"/><stop offset="1" stop-color="blue"/></radialGradient><rect width="100" height="100" fill="url(#g)"/></svg>`,
			[]probe{{50, 50, red}, {2, 2, blue}}},
		{"clip-path", `<svg width="100" height="100"><clipPath id="c"><rect width="50" height="50"/></clipPath><rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`,
			[]probe{{25, 25, red}, {75, 75, white}}},
		{"group-opacity", `<svg width="100" height="100"><g opacity="0.5"><rect width="100" height="100" fill="blue"/></g></svg>`,
			[]probe{{50, 50, [3]uint8{128, 128, 255}}}},
		{"switch", `<svg width="100" height="100"><switch><rect width="100" height="100" fill="red" requiredExtensions="x"/><rect width="100" height="100" fill="lime"/></switch></svg>`,
			[]probe{{50, 50, lime}}},
		{"nested-svg", `<svg width="100" height="100"><svg x="50" y="50" width="50" height="50" viewBox="0 0 1 1"><rect width="2" height="2" fill="red"/></svg></svg>`,
			[]probe{{75, 75, red}, {25, 25, white}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := draw(t, tt.src, 100, 100)
			checkProbes(t, img, 40, tt.probes)
		})
	}
}

func TestDraw_Painting(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		probes []probe
	}{
		{"dash", `<svg width="100" height="100"><line x1="0" y1="10" x2="100" y2="10" stroke="black" stroke-width="10" stroke-dasharray="20"/></svg>`,
			[]probe{{10, 10, black}, {30, 10, white}, {50, 10, black}}},
		{"dash-offset", `<svg width="100" height="100"><line x1="0" y1="10" x2="100" y2="10" stroke="black" stroke-width="10" stroke-dasharray="20" stroke-dashoffset="20"/></svg>`,
			[]probe{{10, 10, white}, {30, 10, black}}},
		{"use-symbol", `<svg width="100" height="100"><symbol id="s" viewBox="0 0 10 10"><rect width="10" height="10" fill="red"/></symbol><use href="#s" x="50" y="50" width="50" height="50"/></svg>`,
			[]probe{{75, 75, red}, {25, 25, white}}},
		{"xlink-href", `<svg width="100" height="100" xmlns:xlink="http://www.w3.org/1999/xlink"><rect id="r" width="20" height="20" fill="red"/><use xlink:href="#r" x="50" y="50"/></svg>`,
			[]probe{{60, 60, red}}},
		{"gradient-user-space", `<svg width="100" height="100"><linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="0" y2="100"><stop offset="0" stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient><rect width="100" height="100" fill="url(#g)"/></svg>`,
			[]probe{{50, 2, red}, {50, 97, blue}}},
		{"gradient-href", `<svg width="100" height="100"><linearGradient id="a"><stop offset="0" stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient><linearGradient id="b" href="#a" x2="0" y2="1"/><rect width="100" height="100" fill="url(#b)"/></svg>`,
			[]probe{{50, 2, red}, {50, 97, blue}}},
		{"gradient-missing-fallback", `<svg width="100" height="100"><rect width="100" height="100" fill="url(#nope) lime"/></svg>`,
			[]probe{{50, 50, lime}}},
		{"stop-opacity", `<svg width="100" height="100"><linearGradient id="g"><stop offset="0" stop-color="blue" stop-opacity="0"/><stop offset="1" stop-color="blue"/></linearGradient><rect width="100" height="100" fill="url(#g)"/></svg>`,
			[]probe{{1, 50, white}, {98, 50, blue}}},
		{"fill-opacity", `<svg width="100" height="100"><rect width="100" height="100" fill="blue" fill-opacity="0.5"/></svg>`,
			[]probe{{50, 50, [3]uint8{128, 128, 255}}}},
		{"visibility", `<svg width="100" height="100"><g visibility="hidden"><rect width="50" height="100" fill="red"/><rect x="50" width="50" height="100" fill="lime" visibility="visible"/></g></svg>`,
			[]probe{{25, 50, white}, {75, 50, lime}}},
		{"important", `<svg width="100" height="100"><style>rect{fill:lime !important}</style><rect width="100" height="100" style="fill:red"/></svg>`,
			[]probe{{50, 50, lime}}},
		{"child-selector", `<svg width="100" height="100"><style>g > rect{fill:lime} rect{fill:red}</style><g><rect width="100" height="100"/></g></svg>`,
			[]probe{{50, 50, lime}}},
		{"clip-bbox-units", `<svg width="100" height="100"><clipPath id="c" clipPathUnits="objectBoundingBox"><rect width="0.5" height="1"/></clipPath><rect x="20" width="80" height="100" fill="red" clip-path="url(#c)"/></svg>`,
			[]probe{{40, 50, red}, {70, 50, white}}},
		{"units", `<svg width="1in" height="1in"><rect width="0.5in" height="0.5in" fill="red"/></svg>`,
			[]probe{{40, 40, red}, {60, 60, white}}},
		{"percent", `<svg width="100" height="100"><rect width="50%" height="100%" fill="red"/></svg>`,
			[]probe{{40, 50, red}, {60, 50, white}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := draw(t, tt.src, 100, 100)
			checkProbes(t, img, 40, tt.probes)
		})
	}
}

func TestDraw_TransformedLeaves(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		probes []probe
	}{
		{"skewed-stroke", `<svg width="100" height="100"><g transform="skewX(45)"><line x1="0" y1="50" x2="50" y2="50" stroke="black" stroke-width="10"/></g></svg>`,
			[]probe{{75, 50, black}, {25, 50, white}}},
		{"rotated-stroke", `<svg width="100" height="100"><g transform="rotate(90 50 50)"><line x1="0" y1="50" x2="100" y2="50" stroke="black" stroke-width="10"/></g></svg>`,
			[]probe{{50, 10, black}, {10, 50, white}}},
		{"scaled-gradient", `<svg width="100" height="100"><linearGradient id="g" gradientUnits="userSpaceOnUse" x2="10"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient><g transform="scale(10)"><rect width="10" height="10" fill="url(#g)"/></g></svg>`,
			[]probe{{2, 50, red}, {97, 50, blue}}},
		{"scaled-text", `<svg width="100" height="100"><g transform="scale(2)"><text x="5" y="25" font-family="Ahem" font-size="10">X</text></g></svg>`,
			[]probe{{20, 40, black}, {35, 40, white}}},
		{"flipped-image-free", `<svg width="100" height="100"><g transform="translate(100 0) scale(-1 1)"><rect width="30" height="100" fill="red"/></g></svg>`,
			[]probe{{85, 50, red}, {15, 50, white}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkProbes(t, draw(t, tt.src, 100, 100), 40, tt.probes)
		})
	}
}
