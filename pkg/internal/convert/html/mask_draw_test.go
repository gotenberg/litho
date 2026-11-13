package html

import (
	"strings"
	"testing"
)

// TestDraw_MaskElement locks a mask naming an SVG mask element of the
// document (CSS Masking 1, 7.1): by luminance, white keeps and black masks
// out; by alpha with mask-type alpha; its user space starts at the
// element's border box in CSS pixels.
func TestDraw_MaskElement(t *testing.T) {
	t.Parallel()
	const defs = `<svg width="0" height="0" style="position:absolute">` +
		`<mask id="l" maskUnits="userSpaceOnUse" x="0" y="0" width="6" height="4"><rect width="2" height="4" fill="white"/><rect x="2" width="2" height="4" fill="black"/></mask>` +
		`<mask id="a" mask-type="alpha" maskUnits="userSpaceOnUse" x="0" y="0" width="6" height="4"><rect x="1" width="2" height="4" fill="black"/></mask></svg>`
	tests := []struct {
		name, ref, want string
	}{
		{"luminance", "url(#l)", "RR____|RR____|RR____|RR____"},
		{"alpha", "url(#a)", "_RR___|_RR___|_RR___|_RR___"},
		{"no mask element", "url(#none)", "RRRR__|RRRR__|RRRR__|RRRR__"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := `<body style="margin:0">` + defs + `<div style="width:4px;height:4px;background:red;mask:` + tt.ref + `"></div>`
			if got := strings.Join(drawPage(t, src, 6, 4, Options{}), "|"); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}
