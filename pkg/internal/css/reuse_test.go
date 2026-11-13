package css

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// reuseDoc repeats elements whose cascades are equal under parents of
// equal and of different styles, beside elements whose cascades read
// more than their declarations: hints, style attributes, custom
// properties, var(), attr(), animations, display: contents and flex
// parents, the root, and pseudo-elements.
func reuseDoc() string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><body>`)
	for i := range 6 {
		fmt.Fprintf(&b, `<section class="s%d"><h2>Head</h2>`, i%3)
		b.WriteString(`<table><tr><td>a</td><td class="n">1</td></tr><tr><td>b</td><td class="n" align="center">2</td></tr><tr><td style="color: red">c</td><td class="n">3</td></tr></table>`)
		b.WriteString(`<ul><li>one</li><li class="v">two</li><li data-w="7">three</li><li class="anim">four</li></ul>`)
		b.WriteString(`<div class="flex"><span>x</span><span>y</span></div><div class="contents"><p>p</p><p>q</p></div>`)
		b.WriteString(`<div class="contents"><span>in block</span></div><div class="flex"><div class="contents"><span>in flex</span></div></div>`)
		b.WriteString(`<x-el class="contents">custom</x-el><canvas class="contents">canvas</canvas>`)
		b.WriteString(`<p class="lead">Text <em>em</em> <q>q</q></p><p>Plain</p></section>`)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}

const reuseCSS = `
:root { --gap: 3pt }
body { font: 11pt/1.4 serif }
.s1 { font-size: 13pt } .s2 { font-size: 80%; color: teal }
h2 { font-size: 1.4em; margin: 0.5em 0 }
td { padding: 2pt 4pt; border: 0.5pt solid } td.n { text-align: right }
tr:nth-child(even) td { background: #eee }
li { margin-left: 1em } li.v { margin-top: var(--gap) }
li[data-w]::after { content: attr(data-w) }
li.anim { animation: grow 1s }
@keyframes grow { from { letter-spacing: 2pt } to { letter-spacing: 0 } }
.flex { display: flex } .contents { display: contents }
.lead::first-letter { font-size: 2em } .lead::first-line { font-weight: bold }
q::before { content: open-quote } p { margin: 0 0 1ex }
`

// TestCompute_ReuseKeepsEveryStyle checks that reusing the style of an
// equal cascade computes every element and pseudo-element as computing
// each one does, with the same warnings, and that it reuses.
func TestCompute_ReuseKeepsEveryStyle(t *testing.T) {
	t.Parallel()
	doc := parseHTML(t, reuseDoc())
	r := NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: mustParse(t, reuseCSS), Origin: Author}})
	reused, err := r.Compute(context.Background(), doc, ComputeOptions{Hints: HTMLHints})
	if err != nil {
		t.Fatal(err)
	}
	each, err := r.Compute(context.Background(), doc, ComputeOptions{Hints: HTMLHints, noReuse: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(reused.styles) != len(each.styles) || len(reused.pseudo) != len(each.pseudo) {
		t.Fatalf("got %d styles and %d pseudo-element styles, want %d and %d", len(reused.styles), len(reused.pseudo), len(each.styles), len(each.pseudo))
	}
	for n, st := range each.styles {
		if !reflect.DeepEqual(reused.styles[n], st) {
			t.Errorf("style of <%s %v> differs when reused", n.Data, n.Attr)
		}
	}
	for k, st := range each.pseudo {
		if !reflect.DeepEqual(reused.pseudo[k], st) {
			t.Errorf("style of <%s>'s pseudo-element %d differs when reused", k.n.Data, k.which)
		}
	}
	if !reflect.DeepEqual(reused.Warnings(), each.Warnings()) {
		t.Errorf("warnings differ:\n%v\n%v", reused.Warnings(), each.Warnings())
	}
	for _, id := range []string{"FirstLetter", "FirstLine"} {
		for n := range each.styles {
			if !hasClass(n, "lead") {
				continue
			}
			em := byTag(n, "em")
			var a, b *Style
			if id == "FirstLetter" {
				a, b = reused.FirstLetter(n, reused.styles[em]), each.FirstLetter(n, each.styles[em])
			} else {
				a, b = reused.FirstLine(n, reused.styles[em]), each.FirstLine(n, each.styles[em])
			}
			if !reflect.DeepEqual(a, b) {
				t.Errorf("%s of <p class=lead> differs when reused", id)
			}
		}
	}

	// Two cascades of one rule's declarations under one parent: the
	// second reuses the first's style.
	c := &computeState{r: r, styles: NewStyles()}
	c.reuse.on = true
	rule := []cand{{d: &r.rules[0].rule.decls[0], ord: r.rules[0].order}}
	first, _ := c.cascade(slices.Clone(rule), initialStyle, nil, false, nil)
	second, _ := c.cascade(slices.Clone(rule), initialStyle, nil, false, nil)
	if first != second || c.reuse.hits != 1 {
		t.Errorf("second cascade: same style %v, %d reused, want true and 1", first == second, c.reuse.hits)
	}
	for _, x := range []cand{
		{d: &Declaration{}, ord: -1},
		{d: &Declaration{}, spec: styleAttrSpec},
		{d: &Declaration{isCustom: true}},
		{d: &Declaration{pending: []token{{typ: tIdent, value: "x"}}}},
		{d: &Declaration{entries: []entry{{id: pAnimationName}}}},
	} {
		if c.reusable([]cand{x}, nil) {
			t.Errorf("%+v is reusable", x)
		}
	}
	if c.reusable(nil, &firstLineProps) {
		t.Errorf("a restricted cascade is reusable")
	}
}

// hasClass reports whether the element n names class c.
func hasClass(n *html.Node, c string) bool {
	v, _ := attr(n, "class")
	return slices.Contains(strings.Fields(v), c)
}
