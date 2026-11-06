package css

import "testing"

// TestMedia_ViewportApartFromFeatures locks that ViewportWidth and
// ViewportHeight size the viewport units while the media features keep
// Width and Height, and that Width and Height size both without them.
func TestMedia_ViewportApartFromFeatures(t *testing.T) {
	t.Parallel()
	doc := `<!doctype html><p id=x>x</p>`
	author := `#x { width: 100vw; height: 50vh } @media (min-width: 90pt) { #x { color: red } }`
	tests := []struct {
		name         string
		m            Media
		w, h         float64
		featureMatch bool
	}{
		{"apart", Media{Type: "print", Width: 100, Height: 100, ViewportWidth: 60, ViewportHeight: 40}, 60, 20, true},
		{"together", Media{Type: "print", Width: 80, Height: 100}, 80, 50, false},
	}
	for _, tt := range tests {
		st := computeMedia(t, doc, author, tt.m).of("x")
		if st.Width != Pts(tt.w) || st.Height != Pts(tt.h) {
			t.Errorf("%s: 100vw by 50vh = %v by %v, want %v by %v", tt.name, st.Width, st.Height, tt.w, tt.h)
		}
		if got := st.Color == (Color{R: 1, A: 1}); got != tt.featureMatch {
			t.Errorf("%s: (min-width: 90pt) matched = %v, want %v", tt.name, got, tt.featureMatch)
		}
	}
}
