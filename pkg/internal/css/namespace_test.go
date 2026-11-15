package css

import "testing"

// TestNamespaces_PrefixesBindToTheirRules locks CSS Namespaces 3: a
// prefix an @namespace rule declares selects the elements of its
// namespace, a prefix no rule declares makes the selector invalid, and a
// rule out of its place, after another rule or inside a block, declares
// nothing.
func TestNamespaces_PrefixesBindToTheirRules(t *testing.T) {
	t.Parallel()
	doc := `<p id=a>a</p><p id=b class=b>b</p><p id=c class=c>c</p><p id=d class=d>d</p>
<p id=e class=e>e</p><p id=f class=f>f</p><svg><rect id=r /></svg>`
	sheet := `
@namespace h "http://www.w3.org/1999/xhtml";
@namespace s url(http://www.w3.org/2000/svg);
@namespace o "http://www.w3.org/";
@media print {}
@namespace late "http://www.w3.org/1999/xhtml";
h|p { font-size: 10px }
s|p, s|rect { font-size: 11px }
.b, o|p { font-size: 12px }
.c, late|p { font-size: 13px }
.d, nope|p { font-size: 14px }
@media print {
  @namespace inner "http://www.w3.org/1999/xhtml";
  .e { font-size: 15px }
  .e:not(inner|p) { font-size: 16px }
}
*|p.f { font-size: 17px }
`
	c := compute(t, doc, sheet)
	for _, tt := range []struct {
		id   string
		want float64
	}{
		{"a", 7.5},   // h|p: the XHTML namespace holds HTML elements
		{"b", 9},     // o|p matches nothing, .b does
		{"c", 7.5},   // late is declared after @media: the rule is invalid
		{"d", 7.5},   // nope is declared nowhere: the rule is invalid
		{"e", 11.25}, // inner is declared inside a block: :not(inner|p) is invalid
		{"f", 12.75},
		{"r", 8.25}, // s|rect
	} {
		if got := c.of(tt.id).FontSize; got != tt.want {
			t.Errorf("#%s font-size %v, want %v", tt.id, got, tt.want)
		}
	}
}

// TestNamespaces_Default locks the default namespace (Selectors 4, 5.1
// and 5.2): it narrows a compound without a prefix, and *| lifts it.
func TestNamespaces_Default(t *testing.T) {
	t.Parallel()
	doc := `<p id=p>p</p><svg><rect id=r /><g id=g /></svg>`
	c := compute(t, doc, `
@namespace url(http://www.w3.org/2000/svg);
p { font-size: 10px }
rect { font-size: 11px }
*|g { font-size: 12px }
`)
	if got := c.of("p").FontSize; got == 7.5 {
		t.Errorf("p matched an unprefixed type selector of another default namespace")
	}
	if got := c.of("r").FontSize; got != 8.25 {
		t.Errorf("rect font-size %v, want 8.25", got)
	}
	if got := c.of("g").FontSize; got != 9 {
		t.Errorf("g font-size %v, want 9", got)
	}
}

// TestNamespaces_SupportsSelector locks selector() under @namespace (CSS
// Conditional 4, 2; CSS Namespaces 3, 4): a prefix declared before the
// condition is supported, one the sheet does not declare is not.
func TestNamespaces_SupportsSelector(t *testing.T) {
	t.Parallel()
	c := compute(t, `<p id=p>p</p><p id=q>q</p>`, `
@namespace x "http://www.w3.org/1999/xlink";
@supports (color: red) {}
@namespace y "http://www.w3.org/1999/xlink";
@supports selector(x|y) { #p { font-size: 10px } }
@supports selector(y|x) { #q { font-size: 10px } }
`)
	if got := c.of("p").FontSize; got != 7.5 {
		t.Errorf("selector(x|y) font-size %v, want 7.5", got)
	}
	if got := c.of("q").FontSize; got == 7.5 {
		t.Errorf("selector(y|x) applied, its prefix undeclared")
	}
}
