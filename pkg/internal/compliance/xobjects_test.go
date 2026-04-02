package compliance

import "testing"

func TestHasInlineImageInterpolateTrue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		block []byte
		want  bool
	}{
		{"full name true", []byte("/Interpolate true /W 100"), true},
		{"full name false", []byte("/Interpolate false /W 100"), false},
		{"abbreviated /I true", []byte("/I true /W 100"), true},
		{"abbreviated /I false", []byte("/I false /W 100"), false},
		{"no interpolate key", []byte("/W 100 /H 200"), false},
		{"/IM key not confused", []byte("/IM true"), false},
		{"/Intent key not confused", []byte("/Intent true"), false},
		{"abbreviated with newline", []byte("/I\ntrue"), true},
		{"abbreviated with tab", []byte("/I\ttrue"), true},
		{"empty block", nil, false},
		{"full name with spaces", []byte("/Interpolate   true"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasInlineImageInterpolateTrue(tc.block); got != tc.want {
				t.Errorf("hasInlineImageInterpolateTrue(%q) = %v, want %v", tc.block, got, tc.want)
			}
		})
	}
}
