package html

import "testing"

func TestChromiumPaper_CeilsToPointsThenDots(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ in, want float64 }{
		{598.2, 599.04}, {599, 599.04}, {1087.53, 1087.92}, {384, 384}, {595.276, 595.92}, {841.89, 841.92},
	} {
		if got := chromiumPaper(tt.in); !near6(got, tt.want) {
			t.Errorf("chromiumPaper(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
