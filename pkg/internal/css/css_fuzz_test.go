package css

import (
	"bytes"
	"context"
	"testing"

	"golang.org/x/net/html"
)

const fuzzSheet = `@import url(a.css) print;
@media print and (min-width: 100px) { p > a:not(.x)[href^="http"] { color: rgb(1 2 3 / 50%); margin: calc(1em + 2px) auto } }
@page :first { size: A4 landscape; margin: 2cm }
@font-face { font-family: X; src: url(x.woff2) format("woff2"); unicode-range: U+0-7F }
@supports (display: grid) and (not (gap: 1px)) { div { display: grid } }
:root { --c: red; --w: calc(var(--n, 2) * 1px) }
li:nth-child(2n+1 of .a), :is(h1, h2)::before { font: italic bold 12px/1.5 "A B", serif; color: var(--c) }
table td { border: 1px solid; background: url(x.png) no-repeat center / cover, #fff }
`

func FuzzParse(f *testing.F) {
	f.Add([]byte(fuzzSheet))
	f.Add([]byte(fuzzSheet[:len(fuzzSheet)/2]))
	f.Add([]byte("p{a:(((((((((("))
	f.Add([]byte(`@page { --x: "b"; @top-center { content: var(--x, "a") } }`))
	f.Add([]byte("@page { background: linear-gradient(45deg, red 10%, 40%, blue) 0 0 / 50% auto } p { background: radial-gradient(circle 4px at left, red, blue 2px 3px) }"))
	f.Add([]byte("p { float: left; shape-outside: shape(evenodd from 0 0, curve by 5px 5px with 1px 0 / 2px 2px, arc to 10% 10% of 4px cw large rotate 9deg, hline to right, close) border-box; shape-margin: 5%; shape-image-threshold: 40% }"))
	f.Add([]byte(fuzzFeatureValues))
	f.Add([]byte(fuzzFeatureValues[:len(fuzzFeatureValues)/2]))
	f.Add([]byte("input { appearance: menulist-button; -webkit-appearance: textfield; accent-color: light-dark(red, currentColor); resize: auto; align-content: unsafe center }"))
	f.Fuzz(func(t *testing.T, data []byte) {
		fetch := func(context.Context, string) ([]byte, string, error) { return data, "", nil }
		s, err := Parse(context.Background(), data, ParseOptions{BaseURL: "https://e.com/s.css", Fetch: fetch})
		if s == nil {
			t.Fatalf("nil stylesheet (err %v)", err)
		}
		r := NewResolver(a4, []Sheet{{Sheet: s, Origin: Author}})
		r.pageOf(t, PageSelector{First: true, Left: true})
		r.pageInOf(t, PageSelector{Name: "n", Right: true, Blank: true}, nil)
		r.FontFaces()
		r.FontFeatureValues("a b")
	})
}

// fuzzFeatureValues seeds the @font-feature-values rule and the font
// feature descriptors and properties.
const fuzzFeatureValues = `@font-feature-values "A B", c { @styleset { x: 1 2 99; y: 100 } @character-variant { z: 3 2 } @swash { s: 1 } }
@font-face { font-family: A; src: local(A); font-feature-settings: "liga" 0, "ss01"; size-adjust: 80%; ascent-override: normal }
p { font-variant: small-caps styleset(x, y) character-variant(z) historical-forms tabular-nums jis04 ruby super emoji;
  font-feature-settings: "smcp" 2; font-language-override: "TRK"; font-size-adjust: cap-height from-font }`

func FuzzParseDeclarations(f *testing.F) {
	f.Add("color: red !important; margin: 1px 2px; --x: { a }; width: var(--x, 3px); background: url(a) 50% / 10px")
	f.Add("font: bold 1")
	f.Add("transform: rotate(1turn) translate(1px, 2%) skewY(3deg) matrix(1, 2, 3, 4, 5, 6); z-index: -3; transform-origin: left 1px 2px")
	f.Add("background: repeating-radial-gradient(ellipse closest-side at 10% 20px, red 0 1em, 30%, blue 2px) no-repeat; background-image: linear-gradient(to top left, currentcolor, 1turn)")
	f.Add("border: 1px solid rgb(")
	f.Add("border-image: url(a.png) 10% fill 2 / 1 auto 3px / 0 2px round space; mask-border: linear-gradient(red, blue) 1 / / 1; border-image-slice: calc(1 + 2) fill")
	f.Add("line-clamp: 3 \"(…)\"; text-overflow: clip ellipsis; width: clamp(1px, max(2lh, 1rem), calc(3rlh - min(1em, 2px))); border-inline-start: 1px solid red; margin-inline: 1px 2px")
	f.Add("font: oblique condensed 12px serif; font-style: oblique -30deg; font-synthesis: weight small-caps; text-autospace: ideograph-alpha insert; text-shadow: 1px 2px 3px red, 0 0 blue; text-justify: distribute")
	f.Add("columns: 12em 3; column-rule: thin dotted red; column-span: all; column-fill: balance-all; column-height: 2em; column-wrap: wrap; break-before: column; break-inside: avoid-column; overflow: auto")
	f.Add("display: inline flex; flex: 2 1 10%; flex-flow: column wrap-reverse; place-content: safe center space-evenly; align-self: last baseline; gap: 1px 2%; order: -2; min-width: auto")
	f.Add("width: round(up, mod(-7px, 3em), 1vi); height: calc(sin(30deg) * hypot(3cap, 4rex) / 1px * 1px); margin: abs(10% - 2svh) clamp(1px, 50%, 2lh); z-index: calc(7 / 2); rotate: atan2(1, -1)")
	f.Add("top: calc(anchor(--a bottom, 10%) + max(anchor(50%), 1em)); width: anchor-size(--a self-inline, anchor-size(height)); position-try: most-width --r flip-block, span-top left; position-visibility: anchor-valid no-overflow")
	f.Add("-webkit-text-stroke: 1e30px red; -webkit-text-fill-color: color-mix(in srgb, currentcolor, red); paint-order: markers stroke fill")
	f.Add("offset: 10px 20% ray(1turn sides contain at right 5px top) calc(0 / 0 * 1%) reverse 1e30deg / left 1e30px; offset-path: path(evenodd, 'M 0 0 L 1e30 NaN') view-box")
	f.Fuzz(func(t *testing.T, src string) {
		ParseDeclarations(src, ParseOptions{BaseURL: "https://e.com/"})
		Hint("color", src)
		Hint("width", src)
	})
}

func FuzzSelector(f *testing.F) {
	f.Add("div > p.a#b[c~='d' i]:not(:nth-child(2n+1 of .x)) ~ span:lang(en)::before")
	f.Add(":is(a, b:where(c")
	f.Add("li:nth-last-of-type(-n+3), *|*, [x|=y s]")
	doc, err := html.Parse(bytes.NewReader([]byte(selectorDoc)))
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, sel string) {
		l, _ := parseComponentValues(sel)
		list, serr := parseSelectorList(trimWS(l), 0, false)
		if serr != nil {
			return
		}
		m := newMatcher("en", 1<<20, false, nil)
		var walk func(n *html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				m.anyMatches(list, n)
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(doc)
	})
}

// fuzzAnimationDoc and fuzzAnimationCSS seed the @keyframes rules, the
// animation, transition and timeline properties, and the interpolation of
// the animated values.
const fuzzAnimationDoc = `<div id=a><p id=b>x</p><p id=c>y</p></div>`

const fuzzAnimationCSS = `@keyframes k { from { opacity: 0; --c: red; transform: translateX(1em) } 40%, 60% { width: 10%; color: var(--c); animation-timing-function: steps(3, jump-both) } to { visibility: hidden; display: none } }
@-webkit-keyframes "s t" { 50% { margin: 1px 2% 3em; background-color: rgb(1 2 3 / 50%); border-top-left-radius: 1px 2% } }
#a { scroll-timeline: --s x; view-timeline: --v inline 1px auto; timeline-scope: --s }
#b { animation: k 2s linear(0, .3 20% 40%, 1) -1s infinite alternate both, "s t" .5s cubic-bezier(.1, 3, .9, -2) 3; animation-timeline: auto, --s; animation-composition: add }
#c { animation: k calc(1s / 3) -9e9s 2.5 reverse backwards paused; animation-range: entry 10% exit; transition: all 1s allow-discrete, color .2s }`

// fuzzAnchorDoc and fuzzAnchorCSS seed the anchor positioning properties:
// an anchor() nested in calc(), one in a custom property, two boxes each
// anchored to the other, and a @position-try rule.
const fuzzAnchorDoc = `<div id=a><p id=b>x</p><p id=c>y</p></div>`

const fuzzAnchorCSS = `@position-try --r { inset-block-start: anchor(--c end, 1px); margin: anchor-size(height); position-area: span-all self-end }
#a { --t: anchor(--b top); anchor-name: --a; anchor-scope: all }
#b { position: absolute; anchor-name: --b; top: var(--t); left: calc(anchor(--c right) * 2 - anchor(--a 25%)); position-try: most-height --r flip-start, flip-x }
#c { position: fixed; anchor-name: --c; position-anchor: --b; top: anchor(--b bottom); width: anchor-size(--b width, 10%); justify-self: anchor-center }`

// FuzzCompute splits its input at the first NUL byte into an HTML
// document and a stylesheet, and computes the styles.
func FuzzCompute(f *testing.F) {
	f.Add([]byte(cascadeDoc + "\x00" + cascadeCSS))
	f.Add([]byte(selectorDoc + "\x00" + fuzzSheet))
	f.Add([]byte("<p style='--a: var(--a); width: var(--a)'>x\x00p { color: var(--b, red) }"))
	f.Add([]byte("<span id=a>x</span>\x00#a { position: absolute; z-index: 2; transform: scale(2) translateX(1em); transform-origin: 1px 2px 3px }"))
	f.Add([]byte("<p id=a>x<b>y</b></p>\x00#a { -webkit-text-stroke: thick; paint-order: stroke; offset: auto inset(1px round 2px) margin-box 50% } b { offset-distance: calc(1px * sign(-0)); -webkit-text-stroke-color: currentcolor; field-sizing: content }"))
	f.Add([]byte("<p id=a>x</p>\x00#a { width: min(100%, round(10em, 3vi)); margin: mod(5%, 3%) calc(1px / 0); font-size: clamp(1rem, 2cap, 3ric) }"))
	f.Add([]byte(fuzzAnchorDoc + "\x00" + fuzzAnchorCSS))
	f.Add([]byte(fuzzAnchorDoc + "\x00" + fuzzAnchorCSS[:len(fuzzAnchorCSS)/2]))
	f.Add([]byte("<html><frameset id=a cols='1*,2*' rows=50%,* border=9 bordercolor=red><frame><frameset cols=x frameborder=no><frame></frameset><frame><frame><frame></frameset>\x00frame { box-shadow: none }"))
	f.Add([]byte(fuzzAnimationDoc + "\x00" + fuzzAnimationCSS))
	f.Add([]byte(fuzzAnimationDoc + "\x00" + fuzzAnimationCSS[:len(fuzzAnimationCSS)/2]))
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, sheet, _ := bytes.Cut(data, []byte{0})
		root, err := html.Parse(bytes.NewReader(doc))
		if err != nil {
			return
		}
		s, _ := Parse(context.Background(), sheet, ParseOptions{})
		r := NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: s, Origin: Author}})
		c, err := r.Compute(context.Background(), root, ComputeOptions{Hints: HTMLHints, Lang: "en"})
		if err != nil {
			return
		}
		var walk func(n *html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				st := c.Style(n)
				if st == nil {
					t.Fatalf("no style for <%s>", n.Data)
				}
				if st.FontSize < 0 || st.FontSize > maxFontSize || st.FontWeight < 100 || st.FontWeight > 900 {
					t.Fatalf("out-of-range font %v %d", st.FontSize, st.FontWeight)
				}
			}
			for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
				walk(ch)
			}
		}
		walk(root)
	})
}

func FuzzMatchMedia(f *testing.F) {
	m := Media{Type: "print", Width: 595, Height: 842}
	f.Add("print and (min-width: 100px), not screen and (orientation: landscape)")
	f.Add("print and (min-wi")
	f.Fuzz(func(_ *testing.T, q string) {
		MatchMedia(q, m)
	})
}
