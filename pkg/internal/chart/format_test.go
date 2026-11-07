package chart

import "testing"

func TestFormatLabel_Codes(t *testing.T) {
	tests := []struct {
		code string
		v    float64
		want string
	}{
		{"", 0.5, "0.5"},
		{"General", 1234567, "1234567"},
		{"General", -2, "-2"},
		{"General", 1e12, "1E+12"},
		{"0", 2.5, "3"},
		{"0.00", 1.005, "1.01"},
		{"#,##0", 1234567.8, "1,234,568"},
		{"#,##0.00", -1234.5, "-1,234.50"},
		{"0%", 0.256, "26%"},
		{"0.0%", 1, "100.0%"},
		{`"$"#,##0`, 123, "$123"},
		{`[$€-407] #,##0.00`, 3, "€ 3.00"},
		{"0.00E+00", 12345, "1.23E+04"},
		{"0;(0)", -5, "(5)"},
		{"0;-0;\"zero\"", 0, "zero"},
		{"#,##0,\"K\"", 25000, "25K"},
		{"yyyy-mm-dd", 45000, "2023-03-15"},
		{"d-mmm", 45000, "15-Mar"},
		{"mmm yy", 45000, "Mar 23"},
		{"h:mm AM/PM", 0.75, "6:00 PM"},
		{"0.0##", 2.5, "2.5"},
		{"#", 0, ""},
	}
	for _, tt := range tests {
		if got := formatLabel(tt.code, tt.v); got != tt.want {
			t.Errorf("formatLabel(%q, %v) = %q, want %q", tt.code, tt.v, got, tt.want)
		}
	}
}
