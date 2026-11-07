package css

import "testing"

func TestMatchMedia_EvaluatesAQueryList(t *testing.T) {
	a4 := Media{Type: "print", Width: 595, Height: 842, Features: map[string]string{"prefers-color-scheme": "dark"}}
	tests := []struct {
		q    string
		want bool
	}{
		{"", true},
		{"print", true},
		{"screen", false},
		{"screen, print", true},
		{"(min-width: 500pt)", true},
		{"(min-width: 800pt)", false},
		{"not (min-width: 800pt)", true},
		{"(orientation: portrait) and (prefers-color-scheme: dark)", true},
		{"(prefers-color-scheme: light)", false},
		{"print {}", false},
		{"print;", false},
		{"'print'", false},
		{"garbage(, print", false},
	}
	for _, tt := range tests {
		if got := MatchMedia(tt.q, a4); got != tt.want {
			t.Errorf("MatchMedia(%q) = %v, want %v", tt.q, got, tt.want)
		}
	}
}
