package chart

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/numfmt"
)

func TestFormatLabel_Codes(t *testing.T) {
	tests := []struct {
		code string
		v    float64
		want string
	}{
		{"", 0.5, "0.5"},
		{"General", 1234567, "1234567"},
		{"General", -2, "-2"},
		{"General", 1e12, "1000000000000"},
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
		if got := formatLabel(tt.code, tt.v, numfmt.Locale{}); got != tt.want {
			t.Errorf("formatLabel(%q, %v) = %q, want %q", tt.code, tt.v, got, tt.want)
		}
	}
}

func TestFormatLabel_Locale(t *testing.T) {
	hu := numfmt.LocaleOf("hu-HU")
	tests := []struct {
		code string
		v    float64
		loc  numfmt.Locale
		want string
	}{
		{"0.00%", 0.2143, hu, "21,43%"},
		{"", 0.5, numfmt.LocaleOf("de-DE"), "0,5"},
		{"#,##0.0", 1234.5, numfmt.LocaleOf("de-DE"), "1.234,5"},
		// A code naming its own locale keeps it.
		{"[$-409]0.0", 0.5, hu, "0.5"},
		{"0.0", 0.5, numfmt.Locale{}, "0.5"},
	}
	for _, tt := range tests {
		if got := formatLabel(tt.code, tt.v, tt.loc); got != tt.want {
			t.Errorf("formatLabel(%q, %v, %+v) = %q, want %q", tt.code, tt.v, tt.loc, got, tt.want)
		}
	}
}

func TestData_LabelAsAxisFormat(t *testing.T) {
	d := Data{Values: []Value{Num(43831), Str("x")}, Format: "0.0"}
	tests := []struct {
		i          int
		code, want string
	}{
		{0, "", "43831.0"},
		{0, "yyyy-mm-dd", "2020-01-01"},
		{1, "yyyy-mm-dd", "x"},
	}
	for _, tt := range tests {
		if got := d.labelAs(tt.i, tt.code, numfmt.Locale{}); got != tt.want {
			t.Errorf("labelAs(%d, %q) = %q, want %q", tt.i, tt.code, got, tt.want)
		}
	}
}

func TestTruncateLabel(t *testing.T) {
	tests := []struct {
		s         string
		w, excess float64
		want      string
	}{
		{"Dddddd dddddd", 130, 55, "Dddd..."},
		{"Bbbbbbb", 70, 5, "Bbb"},
		{"Ab", 20, 30, "Ab"},
		// Within a character of fitting, a label still loses one.
		{"Bbbbbbb", 72.07, -3.39, "Bbb"},
		{"Eeiieeee", 70.06, -5.4, "Eeii..."},
		{"Aaaa", 42.04, -33.4, "Aaaa"},
		{"", 0, 10, ""},
	}
	for _, tt := range tests {
		if got := truncateLabel(tt.s, tt.w, tt.excess); got != tt.want {
			t.Errorf("truncateLabel(%q, %v, %v) = %q, want %q", tt.s, tt.w, tt.excess, got, tt.want)
		}
	}
}
