package symbolfont

import "testing"

func TestOfficeAdvance(t *testing.T) {
	for _, tc := range []struct {
		family string
		code   rune
		want   float64
		ok     bool
	}{
		{"Wingdings", 0xA7, 0.458, true},
		{"Wingdings", 0xF0A7, 0.458, true},
		{"Wingdings 2", 0x97, 0.554, true},
		{" wingdings2 ", 0xF097, 0.554, true},
		{"Symbol", 0xB7, 0.46, true},
		{"Wingdings", 0x20, 0, false},
		{"Webdings", 0x21, 1, true},
		{"Wingdings 3", 0x7D, 0.53, true},
		{"Arial", 0x6C, 0, false},
		{"Wingdings", 0x2022, 0, false},
	} {
		got, ok := OfficeAdvance(tc.family, tc.code)
		if ok != tc.ok || got != tc.want {
			t.Errorf("OfficeAdvance(%q, %#x) = %v, %v; want %v, %v", tc.family, tc.code, got, ok, tc.want, tc.ok)
		}
	}
}
