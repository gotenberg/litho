package css

import "testing"

// TestTextBox_Parse holds text-box-trim, text-box-edge and the text-box
// shorthand to CSS Inline 3, 5: one edge names both sides where it can,
// the under side taking text otherwise; text-box takes either in any
// order, an edge alone trimming both sides, normal resetting both.
func TestTextBox_Parse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl string
		trim TextBoxTrim
		edge TextBoxEdge
	}{
		{"text-box-trim: trim-start", TrimStart, TextBoxEdge{}},
		{"text-box-edge: cap alphabetic", TrimNone, TextBoxEdge{EdgeCap, EdgeAlphabetic}},
		{"text-box-edge: cap", TrimNone, TextBoxEdge{EdgeCap, EdgeText}},
		{"text-box-edge: ideographic", TrimNone, TextBoxEdge{EdgeIdeographic, EdgeIdeographic}},
		{"text-box-edge: alphabetic", TrimNone, TextBoxEdge{}},
		{"text-box: trim-end ex", TrimEnd, TextBoxEdge{EdgeEx, EdgeText}},
		{"text-box: cap alphabetic", TrimBoth, TextBoxEdge{EdgeCap, EdgeAlphabetic}},
		{"text-box: cap alphabetic trim-start", TrimStart, TextBoxEdge{EdgeCap, EdgeAlphabetic}},
		{"text-box: trim-both cap alphabetic; text-box: normal", TrimNone, TextBoxEdge{}},
		{"text-box: cap trim-start alphabetic", TrimNone, TextBoxEdge{}},
	}
	for _, tt := range tests {
		s := styleOf(t, tt.decl)
		if s.TextBoxTrim != tt.trim || s.TextBoxEdge != tt.edge {
			t.Errorf("%s = %v %v, want %v %v", tt.decl, s.TextBoxTrim, s.TextBoxEdge, tt.trim, tt.edge)
		}
	}
}
