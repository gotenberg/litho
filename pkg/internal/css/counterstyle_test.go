package css

import (
	"context"
	"testing"
)

// TestCounterStyles_Predefined locks the predefined counter styles of CSS
// Counter Styles 3, 6 and 7 as Chromium spells them, its departures
// included: armenian past 9999, hebrew 0, the extended range of the
// Chinese and Korean styles.
//
// # Corpus evidence
//
//   - probe, html: every value below is the text Chromium of
//     gotenberg/gotenberg:9.4.1 writes for counter(c, <style>).
func TestCounterStyles_Predefined(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		style string
		v     int
		want  string
	}{
		{"decimal-leading-zero", -3, "-3"},
		{"decimal-leading-zero", 1, "01"},
		{"arabic-indic", 1234, "١٢٣٤"},
		{"armenian", 10000, "Ա̂"},
		{"armenian", 78010092, "Ւ̂Պ̂Ա̂ՂԲ"},
		{"lower-armenian", 1234, "ռմլդ"},
		{"bengali", -3, "-৩"},
		{"cjk-decimal", 1234, "一二三四"},
		{"cjk-decimal", -3, "-3"},
		{"georgian", 10000, "ჵ"},
		{"hebrew", 0, "אפס"},
		{"hebrew", 19, "יט"},
		{"lower-roman", 1234, "mccxxxiv"},
		{"lower-roman", 10000, "10000"},
		{"lower-alpha", 78010092, "fnrknd"},
		{"lower-greek", 10000, "ρθπ"},
		{"hiragana", 1234, "のめ"},
		{"katakana-iroha", 10000, "ニウア"},
		{"cjk-earthly-branch", 11, "戌"},
		{"cjk-earthly-branch", 19, "一九"},
		{"cjk-heavenly-stem", 0, "〇"},
		{"japanese-informal", -3, "マイナス三"},
		{"japanese-informal", 1234, "千二百三十四"},
		{"japanese-informal", 10000, "一〇〇〇〇"},
		{"japanese-formal", 1234, "壱阡弐百参拾四"},
		{"korean-hangul-formal", -3, "마이너스 삼"},
		{"korean-hangul-formal", 78010092, "칠천팔백일만 구십이"},
		{"korean-hanja-informal", 12345, "萬 二千三百四十五"},
		{"korean-hanja-informal", 100100000, "億 十萬"},
		{"korean-hanja-informal", 110, "百十"},
		{"korean-hanja-informal", 10000, "萬"},
		{"korean-hanja-informal", 78010092, "七千八百萬 九十二"},
		{"korean-hanja-formal", 1234, "壹仟貳百參拾四"},
		{"simp-chinese-informal", 11, "十一"},
		{"simp-chinese-informal", 110, "一百一十"},
		{"simp-chinese-informal", 78010092, "七千八百零一万零九十二"},
		{"simp-chinese-formal", -3, "负叁"},
		{"trad-chinese-informal", 10000, "一萬"},
		{"trad-chinese-formal", 78010092, "柒仟捌佰零壹萬零玖拾貳"},
		{"cjk-ideographic", 101, "一百零一"},
		{"ethiopic-numeric", 1234, "፲፪፻፴፬"},
		{"ethiopic-numeric", 10000, "፼"},
		{"ethiopic-numeric", 78010092, "፸፰፻፩፼፺፪"},
		{"ethiopic-numeric", 0, "0"},
		{"unknown-style", 7, "7"},
	} {
		if got := (*CounterStyles)(nil).Counter(tc.style, tc.v); got != tc.want {
			t.Errorf("%s(%d) = %q, want %q", tc.style, tc.v, got, tc.want)
		}
	}
	if got := (*CounterStyles)(nil).Marker("cjk-earthly-branch", 3); got != "寅、" {
		t.Errorf("cjk-earthly-branch marker %q", got)
	}
}

// TestCounterStyles_Rules locks the @counter-style rule (CSS Counter
// Styles 3, 3): each system, the descriptors, extends and fallback, the
// last rule of a name winning, and the rules that define nothing.
func TestCounterStyles_Rules(t *testing.T) {
	t.Parallel()
	sheet, err := Parse(context.Background(), []byte(`
		@counter-style cyc { system: cyclic; symbols: "*" "+"; suffix: ")" }
		@counter-style fix { system: fixed 3; symbols: a b c; fallback: upper-roman }
		@counter-style sym { system: symbolic; symbols: "*" "+"; range: 1 5 }
		@counter-style alp { system: alphabetic; symbols: x y; prefix: "(" }
		@counter-style num { system: numeric; symbols: "0" "1"; negative: "(" ")"; pad: 4 "0" }
		@counter-style add { system: additive; additive-symbols: 5 V, 1 I }
		@counter-style ext { system: extends add; suffix: "!" }
		@counter-style loop1 { system: extends loop2 }
		@counter-style loop2 { system: extends loop1 }
		@counter-style few { system: alphabetic; symbols: x }
		@counter-style bad-range { system: cyclic; symbols: o; range: 5 1 }
		@counter-style twice { system: cyclic; symbols: one }
		@counter-style twice { system: cyclic; symbols: two }
		@counter-style decimal { system: cyclic; symbols: no }
		@counter-style lower-roman { system: cyclic; symbols: R }
		@counter-style Mixed { system: cyclic; symbols: m }
		@counter-style my-circle { system: extends circle }
	`), ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cs := NewResolver(Media{}, []Sheet{{Sheet: sheet, Origin: Author}}).CounterStyles()
	for _, tc := range []struct {
		style string
		v     int
		want  string
	}{
		{"cyc", 3, "*"}, {"cyc", -1, "*"},
		{"fix", 3, "a"}, {"fix", 5, "c"}, {"fix", 6, "VI"},
		{"sym", 3, "**"}, {"sym", 6, "6"},
		{"alp", 3, "xx"},
		{"num", 5, "0101"}, {"num", -1, "(01)"},
		{"add", 7, "VII"}, {"add", 0, "0"},
		{"ext", 6, "VI"},
		{"loop1", 4, "4"},
		{"few", 2, "2"},
		{"bad-range", 9, "o"},
		{"twice", 1, "two"},
		{"decimal", 2, "2"},
		{"lower-roman", 2, "R"},
		{"Mixed", 1, "m"}, {"mixed", 1, "1"},
		{"my-circle", 4, "◦"}, {"square", 1, "▪"}, {"disc", 1, "•"},
	} {
		if got := cs.Counter(tc.style, tc.v); got != tc.want {
			t.Errorf("%s(%d) = %q, want %q", tc.style, tc.v, got, tc.want)
		}
	}
	for style, want := range map[string]string{"cyc": "*)", "alp": "(x. ", "ext": "I!", "fix": "VI. "} {
		v := 1
		if style == "fix" {
			v = 6
		}
		if got := cs.Marker(style, v); got != want {
			t.Errorf("%s marker %q, want %q", style, got, want)
		}
	}
	if !cs.Defines("lower-roman") || cs.Defines("decimal") || cs.Defines("few") {
		t.Errorf("Defines: lower-roman %v, decimal %v, few %v", cs.Defines("lower-roman"), cs.Defines("decimal"), cs.Defines("few"))
	}
}

// TestCounterStyles_Symbols locks symbols() (CSS Counter Styles 3, 4): its
// system, symbolic by default, a space as suffix, and the forms that are
// invalid.
func TestCounterStyles_Symbols(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		decl   string
		v      int
		marker string
	}{
		{`list-style-type: symbols("*" "†")`, 3, "** "},
		{`list-style-type: symbols(cyclic "a" "b")`, 3, "a "},
		{`list-style-type: symbols(numeric "0" "1")`, 2, "10 "},
		{`list-style-type: symbols(fixed "x")`, 2, "2 "},
		{`list-style-type: symbols(numeric "0")`, 2, ""},
		{`list-style-type: symbols(additive "x")`, 2, ""},
	} {
		decls := ParseDeclarations(tc.decl, ParseOptions{})
		var st Style
		if len(decls) == 1 && len(decls[0].entries) == 1 {
			if n, ok := decls[0].entries[0].v.(listName); ok {
				st.ListStyleType, st.ListStyleString = ListNamed, string(n)
			}
		}
		if tc.marker == "" {
			if st.ListStyleType == ListNamed {
				t.Errorf("%s: accepted, want invalid", tc.decl)
			}
			continue
		}
		if got := (*CounterStyles)(nil).Marker(st.ListStyleString, tc.v); st.ListStyleType != ListNamed || got != tc.marker {
			t.Errorf("%s: marker %q, want %q", tc.decl, got, tc.marker)
		}
	}
}
