package css

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

func TestHTMLUserAgent_ParsesClean(t *testing.T) {
	t.Parallel()
	ua := HTMLUserAgent()
	if ua != HTMLUserAgent() {
		t.Errorf("the UA sheet is parsed more than once")
	}
	if w := ua.Warnings(); len(w) != 0 {
		t.Errorf("ua.css warnings: %v", w)
	}
	if len(ua.rules) < 50 {
		t.Errorf("ua.css has %d rules", len(ua.rules))
	}
}

func TestHTMLUserAgent_Rendering(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body id=body><h2 id=h2>x</h2><section><h1 id=h1>x</h1></section><pre id=pre>x</pre><blockquote id=bq>q</blockquote>
<hr id=hr><code id=code>c</code><b id=b>b</b><sub id=sub>s</sub><p id=hid hidden>h</p><center id=center>c</center>
<marquee id=mq>m</marquee><table><caption id=cap>c</caption><tr id=tr><th id=th>h</th></tr></table>
<p id=rtl dir=RTL>r</p><input id=hiddenin type=hidden><s id=s>s</s><dd id=dd>d</dd></body>`, ``)
	tests := []struct {
		id    string
		check func(*Style) bool
	}{
		{"body", func(s *Style) bool { return s.Margin[Top] == pts(6) && s.Display == Block }},
		{"h2", func(s *Style) bool {
			return s.FontSize == 18 && s.FontWeight == 700 && dimEq(s.Margin[Top], pts(18*0.83))
		}},
		// An h1 inside sectioning content keeps its size: the HTML Standard
		// dropped the smaller nested sizes in 2025, and Chromium with it.
		{"h1", func(s *Style) bool { return s.FontSize == 24 && dimEq(s.Margin[Top], pts(24*0.67)) }},
		{"pre", func(s *Style) bool { return s.WhiteSpace == Pre && s.FontFamilies[0] == "monospace" }},
		{"bq", func(s *Style) bool { return s.Margin[Left] == pts(30) && s.Margin[Top] == pts(12) }},
		{"hr", func(s *Style) bool {
			return s.Border[Top].Style == Inset && s.Border[Top].Width == 0.75 && s.Margin[Left].Kind == Auto && s.Overflow == OverflowHidden
		}},
		{"code", func(s *Style) bool { return s.FontFamilies[0] == "monospace" && s.Display == Inline }},
		{"b", func(s *Style) bool { return s.FontWeight == 700 }},
		{"sub", func(s *Style) bool { return s.VerticalAlign.Kind == Sub && near(s.FontSize, 10) }},
		{"hid", func(s *Style) bool { return s.Display == DisplayNone }},
		{"center", func(s *Style) bool { return s.Display == Block && s.TextAlign == AlignCenter }},
		{"mq", func(s *Style) bool { return s.Display == Block }},
		{"cap", func(s *Style) bool { return s.Display == TableCaption && s.TextAlign == AlignCenter }},
		{"tr", func(s *Style) bool { return s.Display == TableRow && s.VerticalAlign.Kind == Middle }},
		{"th", func(s *Style) bool {
			return s.Display == TableCell && s.FontWeight == 700 && s.Padding[Top] == pts(0.75)
		}},
		{"rtl", func(s *Style) bool { return s.Direction == RTL && s.UnicodeBidi == Isolate }},
		{"hiddenin", func(s *Style) bool { return s.Display == DisplayNone }},
		{"s", func(s *Style) bool { return s.TextDecoration == LineThrough }},
		{"dd", func(s *Style) bool { return s.Margin[Left] == pts(30) }},
	}
	for _, tt := range tests {
		if !tt.check(c.of(tt.id)) {
			t.Errorf("#%s: wrong UA style %+v", tt.id, *c.of(tt.id))
		}
	}
	if w := c.c.Warnings(); len(w) != 0 {
		t.Errorf("warnings: %v", w)
	}
}

func TestHTMLHints_Attributes(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body link="#00ff00" bgcolor="black" marginwidth=10>
<a id=a href=x>x</a>
<div id=div align=center>d</div>
<img id=img width=100 height="50%" hspace=5 vspace=3 align=left border=2>
<img id=img2 align=middle>
<table id=t align=center cellspacing=0 width=0><tr id=tr valign=top align=right><td id=td width=0 height=20 nowrap bgcolor="chucknorris">c</td></tr></table>
<font id=f1 size="+2" face="Arial, sans-serif" color="#f00">a</font><font id=f2 size=-5>b</font><font id=f3 size=9>c</font>
<hr id=hr size=1 noshade align=left width=50%>
<br id=br clear=all>
<ol id=ol type=A></ol><ul id=ul type=SQUARE></ul><ol id=ol2 type=square></ol>
<caption id=cap align=bottom></caption>
<p id=p align=justify>p</p>
</body>`, ``)
	tests := []struct {
		id    string
		check func(*Style) bool
	}{
		{"a", func(s *Style) bool { return colorEq(s.Color, rgb(0, 255, 0)) }},
		{"div", func(s *Style) bool { return s.TextAlign == AlignCenter }},
		{"img", func(s *Style) bool {
			return s.Width == pts(75) && s.Height == pct(0.5) && s.Margin[Left] == pts(3.75) && s.Margin[Top] == pts(2.25) &&
				s.Float == FloatLeft && s.Border[Top].Width == 1.5 && s.Border[Top].Style == Solid && s.Display == Block
		}},
		{"img2", func(s *Style) bool { return s.VerticalAlign.Kind == Middle }},
		{"t", func(s *Style) bool {
			return s.Margin[Left].Kind == Auto && s.Margin[Right].Kind == Auto && s.BorderSpacing == [2]float64{0, 0} && s.Width.Kind == Auto
		}},
		{"tr", func(s *Style) bool { return s.VerticalAlign.Kind == AlignTop && s.TextAlign == AlignRight }},
		{"td", func(s *Style) bool {
			return s.Width.Kind == Auto && s.Height == pts(15) && s.WhiteSpace == Nowrap && colorEq(s.BackgroundColor, rgb(0xc0, 0, 0)) &&
				s.VerticalAlign.Kind == AlignTop && s.TextAlign == AlignRight
		}},
		{"f1", func(s *Style) bool {
			return s.FontSize == 18 && s.FontFamilies[0] == "Arial" && colorEq(s.Color, rgb(255, 0, 0))
		}},
		{"f2", func(s *Style) bool { return s.FontSize == 7.5 }}, // x-small, 10px in Chromium's table
		{"f3", func(s *Style) bool { return s.FontSize == 36 }},
		{"hr", func(s *Style) bool {
			return s.Border[Bottom].Width == 0 && s.Border[Top].Style == Solid && s.Margin[Left] == pts(0) && s.Width == pct(0.5)
		}},
		{"br", func(s *Style) bool { return s.Clear == ClearBoth }},
		{"ol", func(s *Style) bool { return s.ListStyleType == UpperAlpha }},
		{"ul", func(s *Style) bool { return s.ListStyleType == Square }},
		{"ol2", func(s *Style) bool { return s.ListStyleType == Decimal }},
		{"p", func(s *Style) bool { return s.TextAlign == AlignJustify }},
	}
	for _, tt := range tests {
		if !tt.check(c.of(tt.id)) {
			t.Errorf("#%s: wrong hinted style %+v", tt.id, *c.of(tt.id))
		}
	}
	body := c.c.Style(byTag(c.doc, "body"))
	if !colorEq(body.BackgroundColor, rgb(0, 0, 0)) || body.Margin[Left] != pts(7.5) || body.Margin[Top] != pts(6) {
		t.Errorf("body = %+v %+v", body.BackgroundColor, body.Margin)
	}
	// An author rule with any specificity beats a hint.
	c = compute(t, `<div id=d align=center>x</div>`, `* { text-align: left }`)
	if c.of("d").TextAlign != AlignLeft {
		t.Errorf("a universal author rule loses to a hint")
	}
}

// An iframe of frameborder zero or not an integer loses its border (HTML
// Standard, 15.4.3); any other integer keeps it.
func TestHTMLHints_FrameBorder(t *testing.T) {
	t.Parallel()
	c := compute(t, `<iframe id=zero frameborder=0></iframe><iframe id=word frameborder=no></iframe><iframe id=zeros frameborder=" -00x"></iframe>
<iframe id=one frameborder=1></iframe><iframe id=minus frameborder=-1></iframe><iframe id=none></iframe>`, ``)
	for id, want := range map[string]float64{"zero": 0, "word": 0, "zeros": 0, "one": 1.5, "minus": 1.5, "none": 1.5} {
		if got := c.of(id).Border[Top].Width; got != want {
			t.Errorf("#%s: border width %v, want %v", id, got, want)
		}
	}
}

func TestLegacyColor_HTMLRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"chucknorris", "#c00000", true},
		{"ninjaturtle", "#00a000", true},
		{"crap", "#c0a000", true},
		{"  red ", "#ff0000", true},
		{"#abc", "#abc", true},
		{"fff", "#0f0f0f", true},
		{"#123456789", "#124578", true},
		{"transparent", "", false},
		{"", "", false},
		{"1", "#010000", true},
	}
	for _, tt := range tests {
		got, ok := legacyColor(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("legacyColor(%q) = %q %v, want %q %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestHint_AndParseDeclarations(t *testing.T) {
	t.Parallel()
	if d := Hint("color", "red"); !d.declares() || d.Property != "color" || d.Important {
		t.Errorf("Hint = %+v", d)
	}
	if d := Hint("color", "notacolor"); d.declares() {
		t.Errorf("an invalid hint declares")
	}
	if d := Hint("color", "red; display: none"); d.declares() {
		t.Errorf("a hint value cannot inject a declaration")
	}
	decls := ParseDeclarations(`color: red !important; --X: 1; bogus: 1; width: var(--w); ;;; margin: 1px 2px`, ParseOptions{})
	if len(decls) != 4 {
		t.Fatalf("decls = %+v", decls)
	}
	if !decls[0].Important || decls[1].Property != "--X" || decls[2].Property != "width" || decls[3].Property != "margin" {
		t.Errorf("decls = %+v", decls)
	}
	if !hasWarning(decls[2].warns, warn.CSSUnsupported, "bogus") {
		t.Errorf("the bogus warning is lost: %+v", decls)
	}
	if d := ParseDeclarations(`bogus: 1`, ParseOptions{}); len(d) != 1 || d[0].declares() || len(d[0].warns) != 1 {
		t.Errorf("a lone bogus declaration = %+v", d)
	}
}

func TestHTMLHints_DirAuto(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><p id=rtl dir=auto>&rlm;<b>א</b> a</p><p id=ltr dir=auto>1 <bdi>א</bdi> a</p>
<p id=none dir=auto>123</p><div dir=rtl><p id=inh dir=auto>!</p></div><p id=skip dir=auto><span dir=rtl>א</span>a</p>
<p>a <bdi id=bdi>א b</bdi> <bdi id=bdiltr dir=ltr>א</bdi> <bdi id=bdibad dir=x>א</bdi></p>
<p id=deep dir=auto><span><i><b></b></i>1</span><em><script>a</script></em>א a</p><p id=deepin dir=auto><span><i>2<b>a</b></i></span>א</p></body>`, ``)
	tests := []struct {
		id   string
		want Direction
	}{
		{"rtl", RTL},
		{"ltr", LTR},
		{"none", LTR},
		{"inh", RTL},
		{"skip", LTR},
		{"bdi", RTL},
		{"bdiltr", LTR},
		{"bdibad", RTL},
		{"deep", RTL},
		{"deepin", LTR},
	}
	for _, tt := range tests {
		if got := c.of(tt.id).Direction; got != tt.want {
			t.Errorf("%s: direction %v, want %v", tt.id, got, tt.want)
		}
	}
}

// TestHTMLHints_ListItemValue locks the value attribute of li as a
// counter-set of list-item at the author origin, below every author rule
// (HTML Standard, 4.4.8; CSS Lists 3, appendix A).
func TestHTMLHints_ListItemValue(t *testing.T) {
	t.Parallel()
	c := compute(t, `<ol><li id=a value=" +7x"><li id=b value="-3"><li id=c value="99999999999"><li id=d value="x">
<li id=e value=4 style="counter-set: c 1"><li id=f value=4 class=set></ol>`, `.set { counter-set: list-item 99 }`)
	for id, want := range map[string][]CounterOp{
		"a": {{Name: "list-item", Value: 7}},
		"b": {{Name: "list-item", Value: -3}},
		"c": {{Name: "list-item", Value: MaxInteger}},
		"d": nil,
		"e": {{Name: "c", Value: 1}},
		"f": {{Name: "list-item", Value: 99}},
	} {
		if got := c.of(id).CounterSet; !slices.Equal(got, want) {
			t.Errorf("#%s: counter-set %v, want %v", id, got, want)
		}
	}
}

// TestHTMLHints_TableRulesAndFrame locks the rules and frame attributes
// of a table (HTML Standard, 15.3.8): rules collapses the table's hidden
// border and gives its cells, row groups, column groups and rows their
// borders; frame draws the named sides solid, as wide as the border
// attribute or 1 pixel, as Chromium draws them.
func TestHTMLHints_TableRulesAndFrame(t *testing.T) {
	t.Parallel()
	c := compute(t, `<table id=all rules=all><tr><td id=allc>a</td></tr></table>
<table id=groups rules=groups><colgroup id=cg><col></colgroup><tbody id=tb><tr id=gr><td id=gc>a</td></tr></tbody></table>
<table rules=rows><tr id=rr><td>a</td></tr></table>
<table rules=cols><tr><td id=cc>a</td></tr></table>
<table id=hs frame=hsides><tr><td>a</td></tr></table>
<table id=b5 frame=box border=5><tr><td>a</td></tr></table>`, ``)
	px := 0.75
	tests := []struct {
		id    string
		check func(*Style) bool
	}{
		{"all", func(s *Style) bool { return s.BorderCollapse == CollapseBorders && s.Border[Top].Style == BorderHidden }},
		{"allc", func(s *Style) bool { return s.Border[Left].Style == Solid && s.Border[Left].Width == px }},
		{"gc", func(s *Style) bool { return s.Border[Top].Style == BorderNone }},
		{"tb", func(s *Style) bool { return s.Border[Top].Style == Solid && s.Border[Left].Style == BorderNone }},
		{"cg", func(s *Style) bool { return s.Border[Left].Style == Solid && s.Border[Top].Style == BorderNone }},
		{"gr", func(s *Style) bool { return s.Border[Top].Style == BorderNone }},
		{"rr", func(s *Style) bool { return s.Border[Top].Style == Solid && s.Border[Bottom].Width == px }},
		{"cc", func(s *Style) bool { return s.Border[Left].Style == Solid && s.Border[Top].Style == BorderNone }},
		{"hs", func(s *Style) bool {
			return s.Border[Top].Style == Solid && s.Border[Top].Width == px && s.Border[Left].Style == BorderHidden
		}},
		{"b5", func(s *Style) bool { return s.Border[Right].Style == Solid && s.Border[Right].Width == 5*px }},
	}
	for _, tc := range tests {
		if !tc.check(c.of(tc.id)) {
			t.Errorf("#%s: %+v", tc.id, c.of(tc.id).Border)
		}
	}
}

// TestUserAgent_ThCentresUnderStart locks that a th centres its text when
// its parent's computed text-align is start, the initial value, and takes
// the parent's otherwise (HTML Standard, 15.3.8), as Chromium computes it.
func TestUserAgent_ThCentresUnderStart(t *testing.T) {
	t.Parallel()
	c := compute(t, `<table><tr><th id=a>A</th></tr>
		<tr style="text-align:start"><th id=b>B</th></tr>
		<tr style="text-align:right"><th id=c>C</th></tr>
		<tr style="text-align:end"><th id=d>D</th></tr>
		<tr><th id=e style="text-align:inherit">E</th></tr></table>`, "")
	for id, want := range map[string]TextAlign{"a": AlignCenter, "b": AlignCenter, "c": AlignRight, "d": AlignEnd, "e": AlignStart} {
		if got := c.of(id).TextAlign; got != want {
			t.Errorf("th %s: text-align %v, want %v", id, got, want)
		}
	}
}

// TestAuthor_RefusesInternalValues locks that -internal-th-center, a value
// of the user agent sheet, is an invalid value in an author sheet and in a
// style attribute, whose declaration is dropped: Chromium parses its
// -internal- values in its own sheet alone.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints a p at the
//     left edge, where its parent's initial text-align puts it, when the
//     p's author rule or style attribute sets text-align to
//     -internal-center or -internal-th-center, and centres it under
//     -webkit-center.
func TestAuthor_RefusesInternalValues(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div><p id=a class=x>A</p>
		<p id=b style="text-align:-internal-th-center">B</p></div>`, `.x { text-align: -internal-th-center }`)
	for id, want := range map[string]TextAlign{"a": AlignStart, "b": AlignStart} {
		if got := c.of(id).TextAlign; got != want {
			t.Errorf("#%s: text-align %v, want %v", id, got, want)
		}
	}
}
