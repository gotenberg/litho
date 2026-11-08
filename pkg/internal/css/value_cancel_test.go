package css

import "testing"

func TestCalc_CancelledLengthLeavesPercentage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decls string
		want  Dimension
	}{
		{`margin-top: calc(50% - 0px)`, Dimension{Kind: Percent, Value: 0.5}},
		{`margin-top: calc(10% + 2px - 2px)`, Dimension{Kind: Percent, Value: 0.1}},
		{`margin-top: calc(10% + 2px)`, Pts(0)},
	}
	for _, tt := range tests {
		if got := styleOf(t, tt.decls).Margin[Top]; got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.decls, got, tt.want)
		}
	}
}
