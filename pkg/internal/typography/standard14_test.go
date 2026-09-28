package typography

import "testing"

func TestStandard14Width(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		font, glyph string
		want        float64
		ok          bool
	}{
		{"Helvetica", "A", 667, true},
		{"ABCDEF+Helvetica,Bold", "A", 722, true},
		{"Arial,Bold", "A", 722, true},
		{"TimesNewRoman", "a", 444, true},
		{"CourierNew,Italic", "a", 600, true},
		{"Courier", ".notdef", 600, false},
		{"Helvetica", "nosuchglyph", 0, false},
		{"ArialMT", "A", 0, false},
		{"Calibri", "A", 0, false},
	} {
		got, ok := Standard14Width(tc.font, tc.glyph)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Standard14Width(%q, %q) = %v, %v, want %v, %v", tc.font, tc.glyph, got, ok, tc.want, tc.ok)
		}
	}
	if !IsStandard14("TimesNewRoman,BoldItalic") || IsStandard14("Verdana") {
		t.Errorf("IsStandard14 misreads the alternative names")
	}
}
