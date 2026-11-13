package css

import "testing"

// CSS Text 4, 3.4: white-space is the shorthand of white-space-collapse
// and text-wrap-mode, and either longhand set on its own keeps the other,
// inherited, as Chromium computes them.
func TestWhiteSpace_Longhands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		parent, child string
		want          WhiteSpace
	}{
		{"", "white-space: pre", Pre},
		{"", "white-space: preserve nowrap", Pre},
		{"", "white-space: nowrap preserve-breaks", PreLineNowrap},
		{"", "white-space: break-spaces nowrap", BreakSpacesNowrap},
		{"", "white-space: wrap", WhiteSpaceNormal},
		{"", "white-space: normal nowrap", WhiteSpaceNormal},
		{"", "white-space: pre; text-wrap-mode: wrap", PreWrap},
		{"", "white-space-collapse: preserve-breaks", PreLine},
		{"white-space: pre", "text-wrap-mode: wrap", PreWrap},
		{"white-space: nowrap", "white-space-collapse: preserve", Pre},
		{"white-space: pre-line", "text-wrap: nowrap", PreLineNowrap},
		{"white-space: pre", "white-space-collapse: initial", Nowrap},
	}
	for _, tt := range tests {
		c := compute(t, `<body><div id=p><div id=t></div></div></body>`, `#p { `+tt.parent+` } #t { `+tt.child+` }`)
		if got := c.of("t").WhiteSpace; got != tt.want {
			t.Errorf("%q under %q: white-space %v, want %v", tt.child, tt.parent, got, tt.want)
		}
	}
}

// CSS Text 4, 6.3: text-wrap is the shorthand of text-wrap-mode and
// text-wrap-style.
func TestTextWrap_Shorthand(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "white-space: pre-wrap; text-wrap: balance")
	if s.TextWrapStyle != WrapStyleBalance || s.WhiteSpace != PreWrap {
		t.Errorf("text-wrap: balance gives %v and %v", s.TextWrapStyle, s.WhiteSpace)
	}
	s = styleOf(t, "text-wrap: nowrap pretty")
	if s.TextWrapStyle != WrapStylePretty || s.WhiteSpace != Nowrap {
		t.Errorf("text-wrap: nowrap pretty gives %v and %v", s.TextWrapStyle, s.WhiteSpace)
	}
}
