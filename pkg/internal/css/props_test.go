package css

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// styleOf computes one <div id=t> styled by decls, inside a 20px body.
func styleOf(t *testing.T, decls string) *Style {
	t.Helper()
	c := compute(t, `<body><div id=t></div></body>`, `body { font-size: 20px; color: blue } #t { `+decls+` }`)
	return c.of("t")
}

func TestProperties_Shorthands(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "margin: 1px 2px 3px; padding: 4px 8px; inset: 1px auto")
	if !reflect.DeepEqual(s.Margin, [4]Dimension{pts(0.75), pts(1.5), pts(2.25), pts(1.5)}) {
		t.Errorf("margin = %+v", s.Margin)
	}
	if !reflect.DeepEqual(s.Padding, [4]Dimension{pts(3), pts(6), pts(3), pts(6)}) {
		t.Errorf("padding = %+v", s.Padding)
	}
	if !dimEq(s.Top, pts(0.75)) || s.Right.Kind != Auto || !dimEq(s.Bottom, pts(0.75)) || s.Left.Kind != Auto {
		t.Errorf("inset = %+v %+v %+v %+v", s.Top, s.Right, s.Bottom, s.Left)
	}

	s = styleOf(t, "border: thick dashed red; border-left: none; border-top-width: thin; border-bottom-style: hidden")
	if s.Border[Top] != (Border{Width: 0.75, Style: Dashed, Color: rgb(255, 0, 0)}) {
		t.Errorf("top = %+v", s.Border[Top])
	}
	// The computed width keeps its length under none and hidden (CSS
	// Backgrounds 3, 4.3 as browsers implement it): border-left: none
	// resets it to medium.
	if s.Border[Right].Width != 3.75 || s.Border[Left].Width != 2.25 || s.Border[Bottom].Width != 3.75 {
		t.Errorf("widths = %v %v %v", s.Border[Right].Width, s.Border[Left].Width, s.Border[Bottom].Width)
	}
	if !colorEq(s.Border[Left].Color, rgb(0, 0, 255)) {
		t.Errorf("border-left: none resets the colour to currentColor, got %+v", s.Border[Left].Color)
	}

	s = styleOf(t, "border-style: solid; border-color: red green")
	if s.Border[Top].Width != 2.25 || !colorEq(s.Border[Right].Color, rgb(0, 128, 0)) {
		t.Errorf("medium default width %v, right colour %+v", s.Border[Top].Width, s.Border[Right].Color)
	}

	s = styleOf(t, "border-radius: 10px 5% / 20px")
	if s.BorderRadius[TopLeft] != (Radius{H: pts(7.5), V: pts(15)}) || s.BorderRadius[TopRight] != (Radius{H: pct(0.05), V: pts(15)}) {
		t.Errorf("radius = %+v", s.BorderRadius)
	}

	s = styleOf(t, `font: italic small-caps bold 12px/1.2 "Times New Roman", serif`)
	if s.FontStyle != FontItalic || s.FontVariant != SmallCaps || s.FontWeight != 700 || s.FontSize != 9 ||
		s.LineHeight != (Dimension{Kind: Number, Value: 1.2}) || !reflect.DeepEqual(s.FontFamilies, []string{"Times New Roman", "serif"}) {
		t.Errorf("font = %+v %v %d %v %+v %q", s.FontStyle, s.FontVariant, s.FontWeight, s.FontSize, s.LineHeight, s.FontFamilies)
	}
	s = styleOf(t, "font-weight: bold; line-height: 3; font: 10px monospace")
	if s.FontWeight != 400 || s.LineHeight.Kind != Normal || s.FontSize != 7.5 {
		t.Errorf("font resets: weight %d line-height %+v", s.FontWeight, s.LineHeight)
	}

	s = styleOf(t, "background: url(a.png) no-repeat right 20% / cover padding-box content-box, #ff0000")
	if s.BackgroundImage != "a.png" || s.BackgroundRepeat != [2]Repeat{RepeatNoRepeat, RepeatNoRepeat} ||
		s.BackgroundPosition != [2]Dimension{pct(1), pct(0.2)} || s.BackgroundSize.Kind != SizeCover ||
		s.BackgroundOrigin != PaddingEdge || s.BackgroundClip != ContentEdge || !colorEq(s.BackgroundColor, rgb(255, 0, 0)) {
		t.Errorf("background = %+v", s)
	}
	s = styleOf(t, "background: center / 50% auto repeat-y")
	if s.BackgroundPosition != [2]Dimension{pct(0.5), pct(0.5)} || s.BackgroundSize.Width != pct(0.5) || s.BackgroundSize.Height.Kind != Auto ||
		s.BackgroundRepeat != [2]Repeat{RepeatNoRepeat, RepeatRepeat} || s.BackgroundOrigin != PaddingEdge {
		t.Errorf("background = %+v %+v %v", s.BackgroundPosition, s.BackgroundSize, s.BackgroundRepeat)
	}
	s = styleOf(t, "background-position: right 10% bottom 5%")
	if s.BackgroundPosition != [2]Dimension{pct(0.9), pct(0.95)} {
		t.Errorf("4-value position = %+v", s.BackgroundPosition)
	}

	s = styleOf(t, "list-style: square inside url(m.png)")
	if s.ListStyleType != Square || s.ListStylePosition != Inside || s.ListStyleImage != "m.png" {
		t.Errorf("list-style = %v %v %q", s.ListStyleType, s.ListStylePosition, s.ListStyleImage)
	}
	if s = styleOf(t, "list-style: none"); s.ListStyleType != ListNone || s.ListStyleImage != "" {
		t.Errorf("list-style: none = %v", s.ListStyleType)
	}
	if s = styleOf(t, "text-decoration: underline overline dotted red"); s.TextDecoration != Underline|Overline {
		t.Errorf("text-decoration = %v", s.TextDecoration)
	}
	s = styleOf(t, "page-break-before: always; page-break-after: avoid; page-break-inside: avoid; word-wrap: break-word")
	if s.BreakBefore != Page || s.BreakAfter != BreakAvoid || s.BreakInside != InsideAvoid || s.OverflowWrap != BreakWord {
		t.Errorf("legacy aliases = %v %v %v %v", s.BreakBefore, s.BreakAfter, s.BreakInside, s.OverflowWrap)
	}
	if s = styleOf(t, "word-break: break-word"); s.WordBreak != WordBreakNormal || s.OverflowWrap != Anywhere {
		t.Errorf("word-break: break-word = %v %v", s.WordBreak, s.OverflowWrap)
	}
	if s = styleOf(t, "overflow: scroll hidden"); s.Overflow != OverflowHidden {
		t.Errorf("overflow = %v", s.Overflow)
	}
	// A scroll container prints clipped, as Chromium prints it.
	if s = styleOf(t, "overflow: auto"); s.Overflow != OverflowHidden {
		t.Errorf("overflow auto = %v", s.Overflow)
	}
	if s = styleOf(t, "overflow: visible"); s.Overflow != OverflowVisible {
		t.Errorf("overflow visible = %v", s.Overflow)
	}
	if s = styleOf(t, "margin-inline: 1px 2px; padding-block-start: 3px"); !dimEq(s.Margin[Left], pts(0.75)) || !dimEq(s.Margin[Right], pts(1.5)) || !dimEq(s.Padding[Top], pts(2.25)) {
		t.Errorf("logical = %+v %+v", s.Margin, s.Padding)
	}
}

func TestProperties_Longhands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl  string
		check func(*Style) bool
	}{
		{"display: inline-table", func(s *Style) bool { return s.Display == InlineTable }},
		{"display: block flow", func(s *Style) bool { return s.Display == Block }},
		{"display: inline flow-root", func(s *Style) bool { return s.Display == InlineBlock }},
		{"display: flow-root", func(s *Style) bool { return s.Display == FlowRoot }},
		{"display: block flow-root", func(s *Style) bool { return s.Display == FlowRoot }},
		// CSS Display 3, 2.3: a flow-root list item is a list item that
		// establishes a block formatting context.
		{"display: flow-root list-item", func(s *Style) bool { return s.Display == ListItem && s.ListItemRoot }},
		{"display: list-item", func(s *Style) bool { return s.Display == ListItem && !s.ListItemRoot }},
		// An inline-level one is an inline block with a marker, which
		// blockifies to a block-level one.
		{"display: inline flow-root list-item", func(s *Style) bool {
			return s.Display == InlineBlock && s.ListItemRoot && s.MakesListItem()
		}},
		{"display: list-item inline flow-root; float: left", func(s *Style) bool { return s.Display == ListItem && s.ListItemRoot }},
		{"display: inline list-item", func(s *Style) bool { return s.Display == InlineListItem && s.MakesListItem() }},
		{"display: inline-block", func(s *Style) bool { return !s.MakesListItem() }},
		{"display: inline-grid", func(s *Style) bool { return s.Display == InlineGrid }},
		{"position: sticky", func(s *Style) bool { return s.Position == Sticky }},
		{"opacity: 150%", func(s *Style) bool { return s.Opacity == 1 }},
		{"opacity: 0.25", func(s *Style) bool { return s.Opacity == 0.25 }},
		{"width: 50%; height: 2em", func(s *Style) bool { return s.Width == pct(0.5) && s.Height == pts(30) }},
		{"width: -1px", func(s *Style) bool { return s.Width.Kind == Auto }},
		{"width: max-content", func(s *Style) bool { return s.Width.Kind == MaxContent }},
		{"min-width: auto; max-height: none", func(s *Style) bool { return s.MinWidth == pts(0) && s.MaxHeight.Kind == None }},
		{"max-width: 10vw", func(s *Style) bool { return near(s.MaxWidth.Value, 59.528) }},
		{"line-height: normal", func(s *Style) bool { return s.LineHeight.Kind == Normal }},
		{"line-height: 20px", func(s *Style) bool { return s.LineHeight == pts(15) }},
		{"line-height: calc(100% + 2px)", func(s *Style) bool { return s.LineHeight == pts(16.5) }},
		{"font-size: 150%", func(s *Style) bool { return s.FontSize == 22.5 }},
		{"white-space: break-spaces", func(s *Style) bool { return s.WhiteSpace == BreakSpaces }},
		{"line-break: anywhere", func(s *Style) bool { return s.LineBreak == LineBreakAnywhere }},
		{"word-break: manual", func(s *Style) bool { return s.WordBreak == WordBreakManual }},
		{"word-space-transform: ideographic-space auto-phrase", func(s *Style) bool { return s.WordSpaceTransform == WordSpaceIdeographic }},
		{"word-space-transform: space space", func(s *Style) bool { return s.WordSpaceTransform == WordSpaceNone }},
		{"text-align: justify-all", func(s *Style) bool { return s.TextAlign == AlignJustifyAll && s.TextAlignLast == AlignAuto }},
		{"text-align-last: center", func(s *Style) bool { return s.TextAlignLast == AlignCenter }},
		{"text-decoration: underline dotted red 2px", func(s *Style) bool {
			return s.TextDecoration == Underline && s.TextDecorationStyle == DecorationDotted &&
				s.TextDecorationColor == rgb(255, 0, 0) && s.TextDecorationThickness == pts(1.5)
		}},
		{"text-decoration: line-through", func(s *Style) bool {
			return s.TextDecorationColor == rgb(0, 0, 255) && s.TextDecorationThickness.Kind == Auto
		}},
		{"text-decoration-thickness: from-font; text-underline-offset: 10%", func(s *Style) bool {
			return s.TextDecorationThickness.Kind == Normal && s.TextUnderlineOffset == pct(0.1)
		}},
		{"text-underline-position: right under", func(s *Style) bool { return s.TextUnderlinePosition == UnderUnder|UnderRight }},
		{"text-underline-position: from-font", func(s *Style) bool { return s.TextUnderlinePosition == UnderFromFont }},
		{"text-underline-position: left right", func(s *Style) bool { return s.TextUnderlinePosition == 0 }},
		{"text-underline-position: under from-font", func(s *Style) bool { return s.TextUnderlinePosition == 0 }},
		{"hyphens: none", func(s *Style) bool { return s.Hyphens == HyphensNone }},
		{"hyphens: auto", func(s *Style) bool { return s.Hyphens == HyphensAuto }},
		{`hyphenate-character: "=="`, func(s *Style) bool { return s.HyphenateCharacter == HyphenateCharacter{Given: true, Text: "=="} }},
		{"hyphenate-character: auto", func(s *Style) bool { return s.HyphenateCharacter == HyphenateCharacter{} }},
		{"text-decoration-skip-ink: all", func(s *Style) bool { return s.TextDecorationSkipInk == SkipInkAll }},
		{"text-decoration-skip-ink: none", func(s *Style) bool { return s.TextDecorationSkipInk == SkipInkNone }},
		{"text-decoration-skip-ink: from-font", func(s *Style) bool { return s.TextDecorationSkipInk == SkipInkAuto }},
		{"width: 2ch; height: 2ex", func(s *Style) bool { return s.Width == pts(15) && s.Height == pts(15) }},
		{"font-size: x-large", func(s *Style) bool { return s.FontSize == 18 }},
		{"font-size: larger", func(s *Style) bool { return s.FontSize == 18 }},
		{"font-size: smaller", func(s *Style) bool { return near(s.FontSize, 12.5) }},
		{"font-size: 2rem", func(s *Style) bool { return s.FontSize == 24 }},
		{"font-size: calc(1em + 10%)", func(s *Style) bool { return near(s.FontSize, 16.5) }},
		{"font-size: -1px", func(s *Style) bool { return s.FontSize == 15 }},
		{"font-weight: 550", func(s *Style) bool { return s.FontWeight == 550 }},
		{"font-weight: lighter", func(s *Style) bool { return s.FontWeight == 100 }},
		{"font-weight: 1001", func(s *Style) bool { return s.FontWeight == 400 }},
		{"font-style: oblique 10deg", func(s *Style) bool { return s.FontStyle == FontOblique }},
		{"font-family: Times New Roman, 'Arial Black', SERIF", func(s *Style) bool {
			return reflect.DeepEqual(s.FontFamilies, []string{"Times New Roman", "Arial Black", "serif"})
		}},
		{"text-align: justify", func(s *Style) bool { return s.TextAlign == AlignJustify }},
		{"text-indent: 2em hanging", func(s *Style) bool { return s.TextIndent == pts(30) && s.TextIndentHanging && !s.TextIndentEachLine }},
		{"text-indent: each-line hanging 1px", func(s *Style) bool { return s.TextIndent == pts(0.75) && s.TextIndentHanging && s.TextIndentEachLine }},
		{"text-indent: calc(10px + 10%)", func(s *Style) bool { return s.TextIndent.Kind == Calc }},
		{"text-transform: uppercase", func(s *Style) bool { return s.TextTransform == Uppercase }},
		{"letter-spacing: 2px; word-spacing: normal", func(s *Style) bool { return s.LetterSpacing == pts(1.5) && s.WordSpacing.Kind == Normal }},
		{"white-space: pre-wrap", func(s *Style) bool { return s.WhiteSpace == PreWrap }},
		{"tab-size: 4", func(s *Style) bool { return s.TabSize == Dimension{Kind: Number, Value: 4} }},
		{"tab-size: 20px", func(s *Style) bool { return s.TabSize == pts(15) }},
		{"vertical-align: -10%", func(s *Style) bool { return s.VerticalAlign == VerticalAlign{Kind: AlignLength, Length: pct(-0.1)} }},
		{"vertical-align: text-top", func(s *Style) bool { return s.VerticalAlign.Kind == TextTop }},
		{"direction: rtl; unicode-bidi: plaintext", func(s *Style) bool { return s.Direction == RTL && s.UnicodeBidi == Plaintext }},
		{"list-style-type: lower-latin", func(s *Style) bool { return s.ListStyleType == LowerAlpha }},
		{"list-style-type: hebrew", func(s *Style) bool { return s.ListStyleType == ListNamed && s.ListStyleString == "hebrew" }},
		{"border-collapse: collapse; border-spacing: 1px 2px", func(s *Style) bool {
			return s.BorderCollapse == CollapseBorders && s.BorderSpacing == [2]float64{0.75, 1.5}
		}},
		{"caption-side: bottom; empty-cells: hide; table-layout: fixed", func(s *Style) bool {
			return s.CaptionSide == CaptionBottom && s.EmptyCells == Hide && s.TableLayout == TableFixed
		}},
		{"break-before: recto; break-after: column; break-inside: avoid-page", func(s *Style) bool {
			return s.BreakBefore == RightPage && s.BreakAfter == Column && s.BreakInside == InsideAvoidPage
		}},
		{"orphans: 3; widows: 0", func(s *Style) bool { return s.Orphans == 3 && s.Widows == 2 }},
		{"color: currentColor", func(s *Style) bool { return colorEq(s.Color, rgb(0, 0, 255)) }},
		{"background-color: currentcolor; color: red", func(s *Style) bool { return colorEq(s.BackgroundColor, rgb(255, 0, 0)) }},
		{"background-image: linear-gradient(red, blue)", func(s *Style) bool { return s.BackgroundImage == "" }},
		{"visibility: collapse; box-sizing: border-box", func(s *Style) bool { return s.Visibility == VisibilityCollapse && s.BoxSizing == BorderBox }},
		{"float: right; clear: both", func(s *Style) bool { return s.Float == FloatRight && s.Clear == ClearBoth }},
	}
	for _, tt := range tests {
		if s := styleOf(t, tt.decl); !tt.check(s) {
			t.Errorf("%s: wrong computed value", tt.decl)
		}
	}
}

func TestProperties_CSSWideKeywords(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=p><span id=c>x</span></div>`, `
#p { color: red; border: 1px solid; margin: 5px; font-size: 30px; display: table }
#c { color: initial; border: inherit; margin: unset; font-size: revert; display: inherit; text-align: inherit }`)
	ch := c.of("c")
	if !colorEq(ch.Color, rgb(0, 0, 0)) {
		t.Errorf("color: initial = %+v", ch.Color)
	}
	// The parent's border colour is currentColor, a keyword the child
	// resolves against its own color (CSS Color 4, 4.4).
	if ch.Border[Top].Width != 0.75 || ch.Border[Top].Style != Solid || !colorEq(ch.Border[Top].Color, rgb(0, 0, 0)) {
		t.Errorf("border: inherit = %+v", ch.Border[Top])
	}
	if !dimEq(ch.Margin[Top], pts(0)) || ch.FontSize != 22.5 || ch.Display != Table {
		t.Errorf("unset margin %+v, revert font-size %v, inherit display %v", ch.Margin[Top], ch.FontSize, ch.Display)
	}
}

func TestProperties_DisplayFixups(t *testing.T) {
	t.Parallel()
	c := compute(t, `<span id=f>a</span><span id=a>b</span><span id=t style="display: inline-table; float: left">c</span><td id=cell></td><span id=n style="display:none; float: left"></span>`,
		`#f { float: left } #a { position: absolute; float: right } html { display: inline }`)
	if s := c.of("f"); s.Display != Block {
		t.Errorf("float display = %v", s.Display)
	}
	if s := c.of("a"); s.Display != Block || s.Float != FloatNone {
		t.Errorf("absolute display %v float %v", s.Display, s.Float)
	}
	if s := c.of("t"); s.Display != Table {
		t.Errorf("floated inline-table = %v", s.Display)
	}
	if s := c.of("n"); s.Display != DisplayNone {
		t.Errorf("display none = %v", s.Display)
	}
	if s := c.c.Style(byTag(c.doc, "html")); s.Display != Block {
		t.Errorf("root display = %v", s.Display)
	}
}

func TestProperties_UnknownWarnedOnce(t *testing.T) {
	t.Parallel()
	s := mustParse(t, `p { foo: 1; foo: 2; -webkit-foo: 1; cursor: pointer } div { foo: 3; display: -webkit-box; display: -webkit-box }`)
	n := 0
	for _, w := range s.Warnings() {
		if w.Code == warn.CSSUnsupported && strings.HasPrefix(w.Message, "property ") {
			n++
		}
	}
	if n != 1 || !hasWarning(s.Warnings(), warn.CSSUnsupported, "display: -webkit-box") {
		t.Errorf("warnings = %v", s.Warnings())
	}
}

func TestVars_Substitution(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=p><div id=c>x</div><div id=d>y</div></div>`, `
:root { --gap: 4px; --brand: rgb(0 128 0); --a: var(--b); --b: var(--a); --big: calc(var(--gap) * 2) }
#p { --side: 2px; margin: var(--side) var(--gap); color: var(--brand); width: var(--undefined, 50%); }
#c { --gap: 8px; padding: var(--big); height: var(--a, 7px); border: var(--nope); color: var(--brand, red) }
#d { width: var(--side) var(--side); --empty:; font-family: var(--empty) serif; background: var(--x, url(f.png) no-repeat) }
`)
	p := c.of("p")
	if !dimEq(p.Margin[Top], pts(1.5)) || !dimEq(p.Margin[Right], pts(3)) || !colorEq(p.Color, rgb(0, 128, 0)) || !dimEq(p.Width, pct(0.5)) {
		t.Errorf("p margins %+v color %+v width %+v", p.Margin, p.Color, p.Width)
	}
	ch := c.of("c")
	if !dimEq(ch.Padding[Top], pts(6)) {
		t.Errorf("--big inherits its computed value (4px * 2), got %+v", ch.Padding[Top])
	}
	if !dimEq(ch.Height, pts(5.25)) {
		t.Errorf("a cycle is guaranteed-invalid and takes the fallback, got %+v", ch.Height)
	}
	if ch.Border[Top].Style != BorderNone {
		t.Errorf("an invalid var() computes as unset, got %+v", ch.Border[Top])
	}
	d := c.of("d")
	if d.Width.Kind != Auto {
		t.Errorf("width with two lengths after substitution = %+v, want unset", d.Width)
	}
	if !reflect.DeepEqual(d.FontFamilies, []string{"serif"}) || d.BackgroundImage != "f.png" || d.BackgroundRepeat[0] != RepeatNoRepeat {
		t.Errorf("d families %q image %q", d.FontFamilies, d.BackgroundImage)
	}
	if !hasWarning(c.c.Warnings(), warn.CSSInvalid, `"width"`) {
		t.Errorf("warnings = %v", c.c.Warnings())
	}
}

func TestVars_DeclarationValueGrammar(t *testing.T) {
	t.Parallel()
	// CSS Variables 1, 2 and 3: a custom property value and a var()
	// fallback are <declaration-value>: a top-level "!" or ";" or an
	// unmatched ")" makes the declaration invalid at parse time, so the
	// earlier declaration stays.
	c := compute(t, `<p id=a>a</p><p id=b>b</p><p id=c>c</p><p id=d>d</p><p id=e>e</p>`, `
p { --g: rgb(0 128 0); --r: rgb(255 0 0) }
#a { --x: var(--g); --x: var(--r,!); color: var(--x) }
#b { --x: var(--g); --x: var(--r,!important); color: var(--x) }
#c { color: var(--x); --x: var(--g); --x: red) }
#d { color: var(--g); color: var(--r) !important !important }
#e { --x: var(--g); --x: var(--r,(!)); color: var(--x) }
`)
	for _, id := range []string{"a", "b", "c", "d"} {
		if got := c.of(id).Color; !colorEq(got, rgb(0, 128, 0)) {
			t.Errorf("#%s color = %+v, want the earlier declaration's green", id, got)
		}
	}
	if got := c.of("e").Color; !colorEq(got, rgb(255, 0, 0)) {
		t.Errorf("#e color = %+v: a nested \"!\" is valid", got)
	}
}

func TestVars_ExponentialGrowthIsCapped(t *testing.T) {
	t.Parallel()
	css := `:root { --a: x x x x x x x x x x; --b: var(--a) var(--a) var(--a) var(--a) var(--a) var(--a) var(--a) var(--a);
	--c: var(--b) var(--b) var(--b) var(--b) var(--b) var(--b) var(--b) var(--b);
	--d: var(--c) var(--c) var(--c) var(--c) var(--c) var(--c) var(--c) var(--c);
	--e: var(--d) var(--d) var(--d) var(--d) var(--d) var(--d) var(--d) var(--d);
	--f: var(--e) var(--e) var(--e) var(--e) var(--e) var(--e) var(--e) var(--e); }
	p { font-family: var(--f) }`
	doc := parseHTML(t, `<p id=p>`)
	r := NewResolver(a4, []Sheet{{Sheet: mustParse(t, css), Origin: Author}})
	c, err := r.Compute(context.Background(), doc, ComputeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(c.Warnings(), warn.LimitReached, "var()") {
		t.Errorf("warnings = %v", c.Warnings())
	}
	if got := c.Style(byID(doc, "p")).FontFamilies; got != nil {
		t.Errorf("families = %q, want the initial value, no list", got)
	}
}

func TestFontUnits_ResolveExAndCh(t *testing.T) {
	t.Parallel()
	root := parseHTML(t, `<body><div id=t style="font-family: f; width: 2ch; height: 2ex; font-size: 2ex"></div></body>`)
	r := NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: mustParse(t, `body { font-size: 20px }`), Origin: Author}})
	units := func(st *Style, _ string) FontUnits {
		if len(st.FontFamilies) > 0 && st.FontFamilies[0] == "f" {
			return FontUnits{XHeight: 0.25, Zero: 0.75}
		}
		return FontUnits{XHeight: 0.5, Zero: 0.5}
	}
	c, err := r.Compute(context.Background(), root, ComputeOptions{Hints: HTMLHints, FontUnits: units})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	s := c.Style(byID(root, "t"))
	// font-size takes the parent's ex: 2 * 0.5 * 20px = 20px; the other
	// lengths the element's own font.
	if s.FontSize != 15 || s.Width != pts(22.5) || s.Height != pts(7.5) {
		t.Errorf("font-size %v, width %+v, height %+v", s.FontSize, s.Width, s.Height)
	}
}

// TestFontUnits_MeasureInTheElementLanguage locks that ex and ch measure
// the face the element's language picks (CSS Values 4, 6.1.1: the first
// available font, whose generic family CSS Fonts 4, 2.1.5 lets depend on
// the language): an element's lengths in its own language, its font-size
// in its parent's, and two elements alike but for their language apart.
func TestFontUnits_MeasureInTheElementLanguage(t *testing.T) {
	t.Parallel()
	root := parseHTML(t, `<body><p id=a lang=ja></p><p id=b lang=en></p><div lang=ja><p id=c class=s lang=en></p></div><div lang=en><p id=d class=s lang=en></p></div></body>`)
	sheet := `p { font-size: 20px; width: 2ex } .s { font-size: 2ex }`
	r := NewResolver(a4, []Sheet{{Sheet: mustParse(t, sheet), Origin: Author}})
	units := func(_ *Style, lang string) FontUnits {
		if lang == "ja" {
			return FontUnits{XHeight: 0.5}
		}
		return FontUnits{XHeight: 0.25}
	}
	c, err := r.Compute(context.Background(), root, ComputeOptions{Lang: "en", FontUnits: units})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	for _, tt := range []struct {
		id          string
		size, width float64
	}{
		{"a", 15, 15},
		{"b", 15, 7.5},
		// 2ex of the 16px parent in Japanese: 16px, then 2ex in English.
		{"c", 12, 6},
		{"d", 6, 3},
	} {
		s := c.Style(byID(root, tt.id))
		if s.FontSize != tt.size || s.Width != pts(tt.width) {
			t.Errorf("#%s font-size %v width %+v, want %v and %vpt", tt.id, s.FontSize, s.Width, tt.size, tt.width)
		}
	}
}

// TestFontUnits_FollowFontSizeAdjust locks CSS Values 4, 6.1.1 with CSS
// Fonts 5, 2.6: ex and ch measure the used font, which font-size-adjust
// scales, whatever the order of the declarations; em does not.
func TestFontUnits_FollowFontSizeAdjust(t *testing.T) {
	t.Parallel()
	root := parseHTML(t, `<body><div id=a></div><div id=b></div><div id=c></div></body>`)
	sheet := `div { font-size: 20px; width: 4ch; height: 2ex; margin-left: 1em; font-size-adjust: 0.25 }
#b { font-size-adjust: ch-width 1 } #c { font-size-adjust: from-font }`
	r := NewResolver(a4, []Sheet{{Sheet: mustParse(t, sheet), Origin: Author}})
	units := func(*Style, string) FontUnits { return FontUnits{XHeight: 0.5, Zero: 0.5} }
	c, err := r.Compute(context.Background(), root, ComputeOptions{FontUnits: units})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	for id, want := range map[string][2]float64{"a": {15, 7.5}, "b": {60, 30}, "c": {30, 15}} {
		s := c.Style(byID(root, id))
		if s.Width != pts(want[0]) || s.Height != pts(want[1]) || s.Margin[Left] != pts(15) {
			t.Errorf("#%s width %+v height %+v margin %+v, want %vpt %vpt 15pt", id, s.Width, s.Height, s.Margin[Left], want[0], want[1])
		}
	}
}

// TestDisplay_RejectsStrayKeywords locks the two-keyword grammar of
// display (CSS Display 3, 2): an outer and an inner display type, nothing
// else, so an invalid value leaves the earlier declaration in force.
func TestDisplay_RejectsStrayKeywords(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"flex foo", "flow table", "inline flex block", "foo grid", "inline block", "flow flow-root"} {
		if s := styleOf(t, "display: inline-table; display: "+v); s.Display != InlineTable {
			t.Errorf("display: %s computes to %v, want it rejected", v, s.Display)
		}
	}
	for v, want := range map[string]Display{"flex inline": InlineFlex, "grid block": Grid, "inline list-item flow": InlineListItem} {
		if s := styleOf(t, "display: "+v); s.Display != want {
			t.Errorf("display: %s computes to %v, want %v", v, s.Display, want)
		}
	}
}

// TestPosition_RejectsUnknownKeywords locks the keywords of <position>
// (CSS Backgrounds 3, 3.6): an identifier other than left, right, top,
// bottom and center makes the declaration invalid.
func TestPosition_RejectsUnknownKeywords(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"foo", "foo 10px top", "left 10% foo", "foo top 10px"} {
		s := styleOf(t, "background-position: 10% 20%; background-position: "+v+"; object-position: 10% 20%; object-position: "+v)
		if s.BackgroundPosition != [2]Dimension{pct(0.1), pct(0.2)} || s.ObjectPosition != [2]Dimension{pct(0.1), pct(0.2)} {
			t.Errorf("%s: background %+v, object %+v, want it rejected", v, s.BackgroundPosition, s.ObjectPosition)
		}
	}
}

// TestFont_ShorthandCoversKerning locks font-kerning among the longhands
// of font (CSS Fonts 4, 2.8): a CSS-wide keyword or a var() on the
// shorthand reaches it as an explicit value does.
func TestFont_ShorthandCoversKerning(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=p><div id=a></div><div id=b></div></div>`,
		`#p { font-kerning: none } #a { font-kerning: normal; font: inherit } #b { --f: 12px serif; font-kerning: none; font: var(--f) }`)
	if k := c.of("a").FontKerning; k != KerningNone {
		t.Errorf("font: inherit leaves font-kerning %v, want the parent's none", k)
	}
	if k := c.of("b").FontKerning; k != KerningAuto {
		t.Errorf("font: var() leaves font-kerning %v, want it reset to auto", k)
	}
}

// TestFont_StretchOnce locks the font shorthand grammar (CSS Fonts 4,
// 2.8): the stretch keyword appears at most once.
func TestFont_StretchOnce(t *testing.T) {
	t.Parallel()
	if s := styleOf(t, "font: 10px serif; font: condensed expanded 12px serif"); s.FontSize != 7.5 {
		t.Errorf("font size %v, want the declaration rejected", s.FontSize)
	}
}

// TestFontUnits_ZeroAdvanceCounts locks CSS Values 4, 6.1.1: ch and ic
// fall back to 0.5em and 1em only when the advance cannot be measured; a
// font whose "0" or "水" has no advance gives 0.
func TestFontUnits_ZeroAdvanceCounts(t *testing.T) {
	t.Parallel()
	if _, ch, _, ic := (FontUnits{}).sizes(20); ch != 10 || ic != 20 {
		t.Errorf("unmeasured ch %v ic %v, want 10 20", ch, ic)
	}
	if _, ch, _, ic := (FontUnits{ZeroMeasured: true, IdeographMeasured: true}).sizes(20); ch != 0 || ic != 0 {
		t.Errorf("zero-advance ch %v ic %v, want 0 0", ch, ic)
	}
}
