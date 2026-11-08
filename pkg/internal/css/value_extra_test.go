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
		{`margin-top: calc(10% + 2px)`, Dimension{Kind: Calc, Value: 1.5, Fraction: 0.1}},
	}
	for _, tt := range tests {
		if got := styleOf(t, tt.decls).Margin[Top]; got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.decls, got, tt.want)
		}
	}
}

func TestOverflowClipMargin_Parses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decls string
		want  ClipMargin
	}{
		{`overflow-clip-margin: 10pt`, ClipMargin{Margin: 10}},
		{`overflow-clip-margin: border-box`, ClipMargin{Box: ClipBorderBox}},
		{`overflow-clip-margin: 4pt content-box`, ClipMargin{Box: ClipContentBox, Margin: 4}},
		{`overflow-clip-margin: -4pt`, ClipMargin{}},
		{`overflow-clip-margin: border-box padding-box`, ClipMargin{}},
	}
	for _, tt := range tests {
		if got := styleOf(t, tt.decls).OverflowClipMargin; got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.decls, got, tt.want)
		}
	}
}

func TestUnits_IcIsAnEm(t *testing.T) {
	t.Parallel()
	if got := styleOf(t, `font-size: 10pt; width: calc(10pt + 2ic)`).Width; got != Pts(30) {
		t.Errorf("width %+v, want 30pt", got)
	}
}

func TestTextOverflow_StringSegmentBreak(t *testing.T) {
	t.Parallel()
	if got := styleOf(t, `text-overflow: "Hi\A ワ\D\A x"`).TextOverflow.Text; got != "Hi ワ x" {
		t.Errorf("text %q, want the breaks as spaces", got)
	}
}
