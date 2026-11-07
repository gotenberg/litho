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
