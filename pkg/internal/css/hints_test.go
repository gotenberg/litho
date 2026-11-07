package css

import (
	"testing"
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
	if !hasWarning(decls[2].warns, "unsupported-css-property", "bogus") {
		t.Errorf("the bogus warning is lost: %+v", decls)
	}
	if d := ParseDeclarations(`bogus: 1`, ParseOptions{}); len(d) != 1 || d[0].declares() || len(d[0].warns) != 1 {
		t.Errorf("a lone bogus declaration = %+v", d)
	}
}

func TestHTMLHints_DirAuto(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><p id=rtl dir=auto>&rlm;<b>א</b> a</p><p id=ltr dir=auto>1 <bdi>א</bdi> a</p>
<p id=none dir=auto>123</p><div dir=rtl><p id=inh dir=auto>!</p></div><p id=skip dir=auto><span dir=rtl>א</span>a</p></body>`, ``)
	tests := []struct {
		id   string
		want Direction
	}{
		{"rtl", RTL},
		{"ltr", LTR},
		{"none", LTR},
		{"inh", RTL},
		{"skip", LTR},
	}
	for _, tt := range tests {
		if got := c.of(tt.id).Direction; got != tt.want {
			t.Errorf("%s: direction %v, want %v", tt.id, got, tt.want)
		}
	}
}
