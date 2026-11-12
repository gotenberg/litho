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
	f.Add([]byte("@page { background: linear-gradient(45deg, red 10%, 40%, blue) 0 0 / 50% auto } p { background: radial-gradient(circle 4px at left, red, blue 2px 3px) }"))
	f.Fuzz(func(t *testing.T, data []byte) {
		fetch := func(context.Context, string) ([]byte, string, error) { return data, "", nil }
		s, err := Parse(context.Background(), data, ParseOptions{BaseURL: "https://e.com/s.css", Fetch: fetch})
		if s == nil {
			t.Fatalf("nil stylesheet (err %v)", err)
		}
		r := NewResolver(a4, []Sheet{{Sheet: s, Origin: Author}})
		r.Page(PageSelector{First: true, Left: true})
		r.PageIn(PageSelector{Name: "n", Right: true, Blank: true}, nil)
		r.FontFaces()
	})
}

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
		m := newMatcher("en", 1<<20)
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

// FuzzCompute splits its input at the first NUL byte into an HTML
// document and a stylesheet, and computes the styles.
func FuzzCompute(f *testing.F) {
	f.Add([]byte(cascadeDoc + "\x00" + cascadeCSS))
	f.Add([]byte(selectorDoc + "\x00" + fuzzSheet))
	f.Add([]byte("<p style='--a: var(--a); width: var(--a)'>x\x00p { color: var(--b, red) }"))
	f.Add([]byte("<span id=a>x</span>\x00#a { position: absolute; z-index: 2; transform: scale(2) translateX(1em); transform-origin: 1px 2px 3px }"))
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
