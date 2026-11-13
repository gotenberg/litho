package css

import "testing"

// TestMarginTrim_Parse holds margin-trim to the grammar of CSS Box 4, 3.3:
// none, block, or block-start and block-end in either order, each once;
// the inline keywords are refused and leave the initial none.
func TestMarginTrim_Parse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl string
		want MarginTrim
	}{
		{"margin-trim: none", 0},
		{"margin-trim: block", TrimBlockStart | TrimBlockEnd},
		{"margin-trim: block-start", TrimBlockStart},
		{"margin-trim: block-end", TrimBlockEnd},
		{"margin-trim: block-end block-start", TrimBlockStart | TrimBlockEnd},
		{"margin-trim: inline", 0},
		{"margin-trim: block-start inline-end", 0},
		{"margin-trim: block block-start", 0},
		{"margin-trim: block-start block-start", 0},
		{"margin-trim: block; margin-trim: none", 0},
	}
	for _, tt := range tests {
		if got := styleOf(t, tt.decl).MarginTrim; got != tt.want {
			t.Errorf("%s = %v, want %v", tt.decl, got, tt.want)
		}
	}
}
