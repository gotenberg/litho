package html

import (
	"strings"
	"testing"
)

// TestDraw_ClipPathURL locks a clip-path naming an SVG clipPath element of
// the document (CSS Masking 1, 5.1): its user space starts at the
// element's border box in CSS pixels, objectBoundingBox units take that
// box, and a url naming no clipPath element leaves the element unclipped.
func TestDraw_ClipPathURL(t *testing.T) {
	t.Parallel()
	const defs = `<svg width="0" height="0" style="position:absolute"><clipPath id="u"><rect x="1" y="1" width="2" height="2"/></clipPath>` +
		`<clipPath id="b" clipPathUnits="objectBoundingBox"><rect width="0.5" height="1"/></clipPath><mask id="m"/></svg>`
	tests := []struct {
		name, ref, want string
	}{
		{"user space", "url(#u)", "______|_RR___|_RR___|______"},
		{"bounding box", "url(#b)", "RR____|RR____|RR____|RR____"},
		{"no such element", "url(#none)", "RRRR__|RRRR__|RRRR__|RRRR__"},
		{"a mask", "url(#m)", "RRRR__|RRRR__|RRRR__|RRRR__"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := `<body style="margin:0">` + defs + `<div style="width:4px;height:4px;background:red;clip-path:` + tt.ref + `"></div>`
			if got := strings.Join(drawPage(t, src, 6, 4, Options{}), "|"); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}
