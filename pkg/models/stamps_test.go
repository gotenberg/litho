package models

import "testing"

// TestStampPosition_String pins the name of each position.
func TestStampPosition_String(t *testing.T) {
	t.Parallel()

	for p, want := range map[StampPosition]string{
		PositionNone:            "",
		PositionCenter:          "center",
		PositionTopLeft:         "top-left",
		PositionTop:             "top",
		PositionTopRight:        "top-right",
		PositionLeft:            "left",
		PositionRight:           "right",
		PositionBottomLeft:      "bottom-left",
		PositionBottom:          "bottom",
		PositionBottomRight:     "bottom-right",
		PositionBottomRight + 1: "StampPosition(10)",
	} {
		if got := p.String(); got != want {
			t.Errorf("StampPosition(%d).String() = %q, want %q", p, got, want)
		}
	}
}
