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

// TestSilentProperties_ScrollingAndInteraction reads without a warning the
// properties that act on scrolling, triggers, transitions, the caret and
// mobile text inflation, none of which a printed page shows, and keeps the
// warning of the four a page prints: a mandatory snap container scrolls to
// its snap position and a scroll container to its initial target at
// layout (CSS Scroll Snap 1, 6.2; CSS Overflow 5), and a view transition
// name makes a stacking context.
func TestSilentProperties_ScrollingAndInteraction(t *testing.T) {
	t.Parallel()
	for _, decl := range []string{
		"scroll-margin-top: 80px", "scroll-margin: 1em 2em", "scroll-padding-inline-start: 4px",
		"scroll-padding: 10px", "scroll-snap-stop: always", "overscroll-behavior-y: contain",
		"overscroll-behavior-inline: none", "timeline-trigger: --t view()", "animation-trigger: --t play-forwards",
		"trigger-scope: all", "view-transition-class: card", "view-transition-group: contain",
		"view-transition-scope: auto", "interest-delay: 1s", "caret-shape: block", "caret-animation: manual",
		"interactivity: inert", "overlay: auto", "scroll-target-group: auto", "scroll-axis-lock: auto",
		"text-size-adjust: 100%", "-webkit-text-size-adjust: 100%", "dynamic-range-limit: standard",
	} {
		c := compute(t, `<!DOCTYPE html><body><div id=t></div></body>`, "#t { "+decl+" }")
		if ws := c.c.Warnings(); hasWarning(ws, warn.CSSUnsupported, "") {
			t.Errorf("%s warns: %v", decl, ws)
		}
	}
	for _, decl := range []string{
		"scroll-snap-type: x mandatory", "scroll-snap-align: start",
		"scroll-initial-target: nearest", "view-transition-name: card",
	} {
		c := compute(t, `<!DOCTYPE html><body><div id=t></div></body>`, "#t { "+decl+" }")
		if ws := c.c.Warnings(); !hasWarning(ws, warn.CSSUnsupported, "") {
			t.Errorf("%s prints and must warn", decl)
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
