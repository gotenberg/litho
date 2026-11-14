package css

import "testing"

func TestBackgroundClip_Text(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decls string
		want  BoxEdge
	}{
		{"background-clip: text", TextEdge},
		{"-webkit-background-clip: text", TextEdge},
		{"background-clip: -webkit-text", TextEdge},
		{"background-clip: padding-box", PaddingEdge},
		{"background-clip: text; background: red", BorderEdge},
	}
	for _, tt := range tests {
		t.Run(tt.decls, func(t *testing.T) {
			t.Parallel()
			if got := styleOf(t, tt.decls).BackgroundClip; got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBackgroundClip_ColorTakesTheBottomLayer locks that background-color
// is clipped by the background-clip of the bottom layer (CSS Backgrounds
// 3, 3.10), the entry for the last background-image layer once the list
// is repeated or truncated to the number of layers (2.2), and that the
// first layer keeps its own clip.
func TestBackgroundClip_ColorTakesTheBottomLayer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decls       string
		first, colr BoxEdge
	}{
		{"background-clip: border-box, content-box, border-box; background-image: none, none", BorderEdge, ContentEdge},
		{"background-clip: padding-box, content-box", PaddingEdge, PaddingEdge},
		{"background-clip: content-box, padding-box; background-image: none, none, none", ContentEdge, ContentEdge},
		{"background: none border-box, green padding-box", BorderEdge, PaddingEdge},
		{"background: none content-box border-box, green", BorderEdge, BorderEdge},
		{"background-clip: padding-box; background-image: none, none", PaddingEdge, PaddingEdge},
		{"background-clip: padding-box, bogus", BorderEdge, BorderEdge},
	}
	for _, tt := range tests {
		t.Run(tt.decls, func(t *testing.T) {
			t.Parallel()
			st := styleOf(t, tt.decls)
			if st.BackgroundClip != tt.first || st.BackgroundColorClip() != tt.colr {
				t.Errorf("clips %v and %v, want %v and %v", st.BackgroundClip, st.BackgroundColorClip(), tt.first, tt.colr)
			}
		})
	}
}

// TestBackgroundImage_RefusesAnInvalidLayer locks that a background-image
// list is invalid when one of its layers is (CSS Backgrounds 3, 2.2).
func TestBackgroundImage_RefusesAnInvalidLayer(t *testing.T) {
	t.Parallel()
	st := styleOf(t, "background-image: linear-gradient(red, red); background-image: none, 12px")
	if st.BackgroundGradient == nil || st.BackgroundLayerCount() != 1 {
		t.Errorf("gradient %v, %d layers: the invalid list replaced the valid one", st.BackgroundGradient, st.BackgroundLayerCount())
	}
}
