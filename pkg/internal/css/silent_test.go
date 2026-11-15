package css

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestSilentProperties_NoEffectOnPaper reads the properties a printed page
// gives no effect without a warning: forced-color-adjust (CSS Color
// Adjustment 1, 3.2, no forced colors on paper), overflow-anchor (CSS
// Scroll Anchoring 1, 3, nothing scrolls) and window-drag with its legacy
// name app-region (CSS UI 4, 8.1, no window).
func TestSilentProperties_NoEffectOnPaper(t *testing.T) {
	t.Parallel()
	for _, decl := range []string{"forced-color-adjust: none", "overflow-anchor: none", "window-drag: move", "app-region: drag"} {
		c := compute(t, `<body><div id=t></div></body>`, "#t { "+decl+" }")
		if ws := c.c.Warnings(); hasWarning(ws, warn.CSSUnsupported, "") {
			t.Errorf("%s warns: %v", decl, ws)
		}
	}
}

// TestSilentProperties_SVGHints reads without a warning the SVG properties
// Chromium prints nothing for: color-interpolation (SVG 2, 13.9), which it
// parses and never applies, and the hints color-rendering (SVG 1.1,
// 11.7.2) and buffered-rendering (SVG Tiny 1.2, 11.10.5).
func TestSilentProperties_SVGHints(t *testing.T) {
	t.Parallel()
	for _, decl := range []string{"color-interpolation: linearRGB", "color-rendering: optimizeSpeed", "buffered-rendering: static"} {
		c := compute(t, `<body><svg><rect id=t /></svg></body>`, "#t { "+decl+" }")
		if ws := c.c.Warnings(); hasWarning(ws, warn.CSSUnsupported, "") {
			t.Errorf("%s warns: %v", decl, ws)
		}
	}
}
