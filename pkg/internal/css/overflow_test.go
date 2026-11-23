package css

import "testing"

// TestOverflow_AxesComputeApart checks overflow-x and overflow-y as two
// longhands (CSS Overflow 3, 3.1): each cascades alone, visible beside a
// scrollable axis computes to auto and clip to hidden, and overflow-inline
// and overflow-block take their axis from the writing mode.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 computes
//     overflow-x: hidden to hidden auto, overflow-x: clip to clip visible,
//     overflow: hidden followed by overflow-y: visible to hidden auto,
//     overflow-x: scroll to scroll auto and overflow-block: clip in
//     vertical-rl to clip visible.
func TestOverflow_AxesComputeApart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl                  string
		x, y, scroll, clipOne bool
	}{
		{"overflow-x: hidden", true, true, true, false},
		{"overflow-x: clip", true, false, false, true},
		{"overflow-y: clip", false, true, false, true},
		{"overflow-x: clip; overflow-y: visible", true, false, false, true},
		{"overflow: clip; overflow-y: visible", true, false, false, true},
		{"overflow: hidden; overflow-y: visible", true, true, true, false},
		{"overflow-x: scroll", true, true, true, false},
		{"overflow-x: overlay", true, true, true, false},
		{"overflow-x: clip; overflow-y: hidden", true, true, false, false},
		{"overflow-x: hidden; overflow-y: clip", true, true, false, false},
		{"overflow-y: visible; overflow-x: visible", false, false, false, false},
		{"overflow-inline: clip", true, false, false, true},
		{"overflow-block: clip", false, true, false, true},
		{"writing-mode: vertical-rl; overflow-block: clip", true, false, false, true},
		{"overflow: clip hidden auto", false, false, false, false},
	}
	for _, tt := range tests {
		st := styleOf(t, tt.decl)
		if st.ClipsX() != tt.x || st.ClipsY() != tt.y || st.ScrollContainer() != tt.scroll || st.ClipOnly() != tt.clipOne {
			t.Errorf("%s: clips x %v, y %v, scroll container %v, clip only %v", tt.decl, st.ClipsX(), st.ClipsY(), st.ScrollContainer(), st.ClipOnly())
		}
	}
}

// TestOverflow_HiddenWithoutAxes checks a style built without the cascade,
// as the Office converters build theirs: an Overflow of OverflowHidden
// clips both axes.
func TestOverflow_HiddenWithoutAxes(t *testing.T) {
	t.Parallel()
	st := Initial()
	st.Overflow = OverflowHidden
	if !st.ClipsX() || !st.ClipsY() || st.ClipOnly() || st.ScrollContainer() {
		t.Errorf("hidden: clips x %v, y %v, clip only %v, scroll container %v", st.ClipsX(), st.ClipsY(), st.ClipOnly(), st.ScrollContainer())
	}
	st.SetClipOnly()
	if !st.ClipsX() || !st.ClipsY() || !st.ClipOnly() {
		t.Errorf("clip only: clips x %v, y %v, clip only %v", st.ClipsX(), st.ClipsY(), st.ClipOnly())
	}
}
