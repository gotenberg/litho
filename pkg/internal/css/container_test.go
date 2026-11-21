package css

import (
	"context"
	"slices"
	"testing"

	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// computeSized computes doc under author with the containers of the ids
// of sizes measured at those content box sizes, in CSS pixels.
func computeSized(t *testing.T, doc, author string, sizes map[string][2]float64) computed {
	t.Helper()
	root := parseHTML(t, doc)
	r := NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: mustParse(t, author), Origin: Author}})
	size := func(n *html.Node) (float64, float64, bool) {
		id, _ := attr(n, "id")
		v, ok := sizes[id]
		return v[0] * Pt, v[1] * Pt, ok
	}
	c, err := r.Compute(context.Background(), root, ComputeOptions{Hints: HTMLHints, ContainerSize: size})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return computed{t: t, doc: root, c: c}
}

func TestContainerProperties_ParseAndCompute(t *testing.T) {
	t.Parallel()
	// CSS Conditional 5, 5.1 to 5.3: container-type, container-name and
	// the container shorthand, whose omitted type resets to normal.
	tests := []struct {
		decl  string
		typ   ContainerType
		names []string
	}{
		{"container-type: inline-size", ContainerInlineSize, nil},
		{"container-type: size scroll-state", ContainerSize | ContainerScrollState, nil},
		{"container-type: scroll-state inline-size", ContainerInlineSize | ContainerScrollState, nil},
		{"container-type: size inline-size", 0, nil},
		{"container-type: anchored", ContainerAnchored, nil},
		{"container-type: anchored inline-size scroll-state", ContainerAnchored | ContainerInlineSize | ContainerScrollState, nil},
		{"container-type: anchored anchored", 0, nil},
		{"container-type: normal", 0, nil},
		{"container-name: card Main", 0, []string{"card", "Main"}},
		{"container-name: none", 0, nil},
		{"container-name: and", 0, nil},
		{"container-name: card none", 0, nil},
		{"container: card / size", ContainerSize, []string{"card"}},
		{"container-type: size; container: card", 0, []string{"card"}},
		{"container: none / inline-size", ContainerInlineSize, nil},
		{"container: card /", 0, nil},
	}
	for _, tt := range tests {
		st := compute(t, `<div id=d></div>`, "#d { "+tt.decl+" }").of("d")
		if st.ContainerType != tt.typ || !slices.Equal(st.ContainerNames, tt.names) {
			t.Errorf("%s: type %v names %q, want %v %q", tt.decl, st.ContainerType, st.ContainerNames, tt.typ, tt.names)
		}
	}
}

func TestContainerType_AppliesContainment(t *testing.T) {
	t.Parallel()
	// CSS Conditional 5, 5.1: size applies size and style containment,
	// inline-size inline-size and style containment; neither applies
	// layout containment. CSS Anchor Positioning 2, 2.1: anchored applies
	// style containment.
	tests := []struct {
		decl string
		want Containment
	}{
		{"container-type: size", ContainSize | ContainStyle},
		{"container-type: inline-size", ContainInlineSize | ContainStyle},
		{"container-type: inline-size; contain: size", ContainSize | ContainStyle},
		{"container-type: scroll-state", 0},
		{"container-type: anchored", ContainStyle},
		{"contain: paint", ContainPaint},
	}
	for _, tt := range tests {
		st := compute(t, `<div id=d></div>`, "#d { "+tt.decl+" }").of("d")
		if got := st.Containment(); got != tt.want {
			t.Errorf("%s: containment %b, want %b", tt.decl, got, tt.want)
		}
	}
}

// TestContainerQueries_SizeFeatures locks the size features and the
// query syntax against a container whose content box is 300 by 100 CSS
// pixels: CSS Conditional 5, 5.4 and 6.1.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 applies each
//     rule marked true here, and none marked false, to a child of a
//     container 300px wide, of type inline-size, or of type size and
//     100px tall.
func TestContainerQueries_SizeFeatures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		typ, query string
		want       bool
	}{
		{"inline-size", "(width > 200px)", true},
		{"inline-size", "(min-width: 400px)", false},
		{"inline-size", "(max-width: 300px)", true},
		{"inline-size", "(200px < width <= 300px)", true},
		{"inline-size", "(300px >= width)", true},
		{"inline-size", "(width: 300px)", true},
		{"inline-size", "(width>200px)", true},
		{"inline-size", "(WIDTH > 200PX)", true},
		{"inline-size", "(inline-size >= 300px)", true},
		{"inline-size", "(width > 20em)", false},
		{"inline-size", "(width > 0)", true},
		{"inline-size", "(width)", true},
		// An inline-size container answers no block-axis feature: unknown.
		{"inline-size", "(block-size > 50px)", false},
		{"inline-size", "(height > 0)", false},
		{"inline-size", "(orientation: landscape)", false},
		{"inline-size", "not (height > 0)", false},
		{"inline-size", "(width > 100px) and (width < 200px)", false},
		{"inline-size", "(width > 1000px) or (width < 400px)", true},
		{"inline-size", "not (width > 1000px)", true},
		{"inline-size", "(width > 200px) and not (width > 250px)", false},
		{"inline-size", "((width > 200px))", true},
		// An unknown feature selects no container (5.4).
		{"inline-size", "(unknown-feature > 0)", false},
		{"inline-size", "not (unknown-feature > 0)", false},
		{"inline-size", "(width > 200px) or foo(bar)", false},
		{"size", "(block-size > 50px)", true},
		{"size", "(height > 50px)", true},
		{"size", "(orientation: landscape)", true},
		{"size", "(orientation: portrait)", false},
		{"size", "(aspect-ratio > 2)", true},
		{"size", "(aspect-ratio: 3/1)", true},
		{"size", "(min-aspect-ratio: 4/1)", false},
	}
	for _, tt := range tests {
		css := "#c { container-type: " + tt.typ + " } @container " + tt.query + " { #p { margin-left: 100px } }"
		got := computeSized(t, `<div id=c><p id=p></p></div>`, css, map[string][2]float64{"c": {300, 100}}).of("p").Margin[Left].Value != 0
		if got != tt.want {
			t.Errorf("%s @container %s applied %v, want %v", tt.typ, tt.query, got, tt.want)
		}
	}
}

// TestContainerQueries_SelectTheContainer locks which container a
// condition queries: the nearest ancestor whose name matches and that
// answers every feature, never the element itself, the originating
// element for a pseudo-element, unknown without one (CSS Conditional 5,
// 5 and 5.4).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 applies
//     @container card (width > 400px) to a child of an unnamed 300px
//     container inside a 500px one named card, @container (width >
//     200px) to the ::before of a container's child, and no @container
//     rule to the container itself or to an element outside any.
func TestContainerQueries_SelectTheContainer(t *testing.T) {
	t.Parallel()
	doc := `<div id=outer><div id=inner><p id=p></p><p id=q class=b></p></div></div><p id=out></p>`
	css := `#outer { container: card / inline-size } #inner { container-type: inline-size }
@container card (width > 400px) { #p { margin-left: 100px } }
@container (width > 400px) { #q { margin-left: 100px } }
@container card { #out, #inner { margin-left: 100px } }
@container nosuch { #q { padding-top: 10px } }
@container (width < 400px) { .b::before { content: "x" } }
@container nosuch (width > 0), (width > 0) { #p { padding-top: 10px } }
@container none (width > 0) { #p { margin-right: 10px } }`
	c := computeSized(t, doc, css, map[string][2]float64{"outer": {500, 0}, "inner": {300, 0}})
	if c.of("p").Margin[Left].Value == 0 {
		t.Error("a named condition skipped the nearer unnamed container")
	}
	if c.of("q").Margin[Left].Value != 0 {
		t.Error("an unnamed condition skipped the nearest container")
	}
	if c.of("out").Margin[Left].Value != 0 || c.of("q").Padding[Top].Value != 0 {
		t.Error("a condition with no eligible container applied")
	}
	if c.of("inner").Margin[Left].Value == 0 {
		t.Error("a name alone did not select its container")
	}
	if ps := c.c.Pseudo(byID(c.doc, "q"), PseudoBefore); ps == nil || len(ps.Content.Items) == 0 {
		t.Error("a pseudo-element did not query its originating element's container")
	}
	if c.of("p").Padding[Top].Value == 0 {
		t.Error("a list of conditions is not true when one is")
	}
	if c.of("p").Margin[Right].Value != 0 {
		t.Error("an @container rule named none was kept")
	}
	if !c.c.QueriesContainers() {
		t.Error("QueriesContainers is false after size queries")
	}
}

// TestContainerQueries_Nesting locks @container inside and around
// @media, @supports, @layer and style rules (CSS Conditional 5, 5.4;
// CSS Nesting 1, 3.2): a rule applies when every condition around it
// does, and the global rules inside apply whatever the condition.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 applies a rule
//     under @media print and @container, either order, under @supports
//     and @container, under a nested @container in a style rule, and
//     under two nested @container rules only when both are true.
func TestContainerQueries_Nesting(t *testing.T) {
	t.Parallel()
	css := `#c { container-type: inline-size }
@media print { @container (width > 200px) { #a { padding-left: 1px } } }
@media screen { @container (width > 200px) { #a { padding-right: 1px } } }
@container (width > 200px) { @media print { #a { padding-top: 1px } } }
@supports (display: grid) { @container (width > 200px) { #a { padding-bottom: 1px } } }
@container (width > 200px) { @supports (display: grid) { #b { padding-left: 1px } } }
@container (width > 200px) { @layer x { #b { margin-left: 1px } } }
#b { @container (width > 200px) { padding-right: 1px } }
@container (width > 200px) { @container (width < 250px) { #b { padding-top: 1px } } }
@container (width > 200px) { @container (width > 250px) { #b { padding-bottom: 1px } } }
@container (width > 1000px) { @font-face { font-family: Q; src: local(Q) } }`
	sheet := mustParse(t, css)
	c := computeSized(t, `<div id=c><p id=a></p><p id=b></p></div>`, css, map[string][2]float64{"c": {300, 0}})
	a, b := c.of("a"), c.of("b")
	if a.Padding[Left].Value == 0 || a.Padding[Right].Value != 0 || a.Padding[Top].Value == 0 || a.Padding[Bottom].Value == 0 {
		t.Errorf("#a paddings %+v", a.Padding)
	}
	if b.Padding[Left].Value == 0 || b.Margin[Left].Value == 0 || b.Padding[Right].Value == 0 || b.Padding[Top].Value != 0 || b.Padding[Bottom].Value == 0 {
		t.Errorf("#b margins %+v paddings %+v", b.Margin, b.Padding)
	}
	if len(sheet.faces) != 1 {
		t.Errorf("an @font-face inside @container: %d faces, want 1", len(sheet.faces))
	}
}

func TestContainerQueries_InvalidPreludeDropsTheRule(t *testing.T) {
	t.Parallel()
	// CSS Conditional 5, 5.4: the prelude must match
	// <container-condition>#, a name excluding none, and, not and or; a
	// parenthesised block that is no size feature is <general-enclosed>,
	// valid and unknown.
	for _, prelude := range []string{"", "none", "and (width > 0)", "card card", "(width > 0) and", "(width > 0) and (width < 9px) or (width: 1px)"} {
		sheet := mustParse(t, "@container "+prelude+" { p { color: red } }")
		if len(sheet.rules) != 0 {
			t.Errorf("@container %q kept its rule", prelude)
		}
		if prelude != "" && !slices.ContainsFunc(sheet.Warnings(), func(w Warning) bool { return w.Code == warn.CSSInvalid }) {
			t.Errorf("@container %q dropped without a warning", prelude)
		}
	}
	for _, prelude := range []string{"card", "card (width > 0)", "(width > 0), style(--x)", "foo(bar)", "(width > 0) or (a b c)", "(min-orientation: portrait)", "(width < 1px > 2px)"} {
		if sheet := mustParse(t, "@container "+prelude+" { p { color: red } }"); len(sheet.rules) != 1 {
			t.Errorf("@container %q dropped its rule", prelude)
		}
	}
}

// TestContainerQueries_StyleFeatures locks the style queries: custom
// properties of the nearest ancestor compared as tokens, a name alone
// true when set, ranges of one type, standard properties unknown (CSS
// Conditional 5, 6.2, as Chromium answers them).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 applies the
//     rules marked true and none marked false, under a parent of --theme:
//     dark, --n: 5, --len: 10px and colour red.
func TestContainerQueries_StyleFeatures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		query string
		want  bool
	}{
		{"style(--theme: dark)", true},
		{"style(--theme:dark)", true},
		{"style(--theme: light)", false},
		{"style(--theme)", true},
		{"style(--nosuch)", false},
		{"not style(--nosuch)", true},
		{"not style(--theme: light)", true},
		{"style(--theme: dark) and style(--n: 5)", true},
		{"style((--theme: dark) or (--n: 9))", true},
		{"style(--n > 3)", true},
		{"style(--n < 3)", false},
		{"style(2 < --n < 9)", true},
		{"style(--len >= 10px)", true},
		{"style(--len > 7.5pt)", false},
		{"style(--theme > 3)", false},
		{"style(--len: 10px)", true},
		{"style(--n: 5.0)", false},
		{"style(--theme: initial)", false},
		{"style(--nosuch: initial)", true},
		{"style(color: red)", false},
		{"not style(color: red)", false},
		{"style(display: block)", false},
		{"named style(--theme: dark)", false},
	}
	for _, tt := range tests {
		css := "#v { --theme: dark; --n: 5; --len: 10px; color: red } @container " + tt.query + " { #p { margin-left: 100px } }"
		got := compute(t, `<div id=v><div><p id=p></p></div></div>`, css).of("p").Margin[Left].Value != 0
		if got != tt.want {
			t.Errorf("@container %s applied %v, want %v", tt.query, got, tt.want)
		}
	}
	c := compute(t, `<div id=v><p id=p></p></div>`, `#v { --theme: dark } @container style(--theme: dark) { #p { margin-left: 1px } }`)
	if c.c.QueriesContainers() {
		t.Error("a style query reads a container's size")
	}
}

func TestContainerQueries_ScrollStateIsNoneOnPaper(t *testing.T) {
	t.Parallel()
	// CSS Conditional 5, 6.3: nothing scrolls, sticks or snaps on a
	// printed page; a scroll-state query needs a scroll-state container.
	//
	// # Corpus evidence
	//
	//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 applies
	//     scroll-state(stuck: none), not scroll-state(stuck: top) and
	//     scroll-state(scrollable: none) inside a scroll-state container.
	tests := []struct {
		typ, query string
		want       bool
	}{
		{"scroll-state", "scroll-state(stuck: none)", true},
		{"scroll-state", "not scroll-state(stuck: top)", true},
		{"scroll-state", "scroll-state(scrollable: none)", true},
		{"scroll-state", "scroll-state(snapped)", false},
		{"inline-size", "scroll-state(stuck: none)", false},
	}
	for _, tt := range tests {
		css := "#c { container-type: " + tt.typ + " } @container " + tt.query + " { #p { margin-left: 100px } }"
		got := compute(t, `<div id=c><p id=p></p></div>`, css).of("p").Margin[Left].Value != 0
		if got != tt.want {
			t.Errorf("%s @container %s applied %v, want %v", tt.typ, tt.query, got, tt.want)
		}
	}
}

// TestContainerQueries_UnknownBeforeMeasured locks that a size query of a
// container the layout has not measured is unknown, and that the result
// says a style read it.
func TestContainerQueries_UnknownBeforeMeasured(t *testing.T) {
	t.Parallel()
	css := "#c { container-type: inline-size } @container (width >= 0) { #p { margin-left: 1px } } @container not (width < 0) { #p { margin-right: 1px } }"
	c := compute(t, `<div id=c><p id=p></p></div>`, css)
	if p := c.of("p"); p.Margin[Left].Value != 0 || p.Margin[Right].Value != 0 {
		t.Errorf("a query of an unmeasured container applied: %+v", p.Margin)
	}
	if !c.c.QueriesContainers() {
		t.Error("QueriesContainers is false")
	}
	if compute(t, `<div><p></p></div>`, "@container (width >= 0) { p { margin-left: 1px } }").c.QueriesContainers() {
		t.Error("QueriesContainers is true without a container")
	}
}

// TestContainerUnits locks the container query units: 1% of the nearest
// container answering each axis, the small viewport's where none does
// (CSS Conditional 5, 7).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 sizes 10cqw at
//     40px in a 400px inline-size container, 10cqh there at a tenth of
//     the viewport's height, 10cqh at 20px inside a 200px tall size
//     container around a 200px inline-size one, 10cqmin at 20px there,
//     and 10cqw at a tenth of the page width outside any container.
func TestContainerUnits(t *testing.T) {
	t.Parallel()
	doc := `<div id=s><div id=i><p id=a></p><p id=b></p><p id=c></p><p id=d></p><p id=e></p></div></div><p id=f></p>`
	css := `#s { container-type: size } #i { container-type: inline-size }
#a { margin-left: 10cqw } #b { margin-left: 10cqh } #c { margin-left: 10cqmin } #d { margin-left: 10cqmax }
#e { margin-left: calc(5cqi + 20px); width: max(10cqb, 100px) } #f { margin-left: 10cqw; margin-right: 10cqh }`
	c := computeSized(t, doc, css, map[string][2]float64{"s": {400, 200}, "i": {200, 50}})
	vw, vh := a4.Width, a4.Height
	tests := []struct {
		id   string
		got  float64
		want float64
	}{
		{"a", c.of("a").Margin[Left].Value, 20 * Pt},
		{"b", c.of("b").Margin[Left].Value, 20 * Pt},
		{"c", c.of("c").Margin[Left].Value, 20 * Pt},
		{"d", c.of("d").Margin[Left].Value, 20 * Pt},
		{"e", c.of("e").Margin[Left].Value, 30 * Pt},
		{"e width", c.of("e").Width.Value, 100 * Pt},
		{"f", c.of("f").Margin[Left].Value, vw / 10},
		{"f right", c.of("f").Margin[Right].Value, vh / 10},
	}
	for _, tt := range tests {
		if d := tt.got - tt.want; d > 1e-9 || d < -1e-9 {
			t.Errorf("%s = %v, want %v", tt.id, tt.got, tt.want)
		}
	}
	if !c.c.QueriesContainers() {
		t.Error("QueriesContainers is false after container units")
	}
	plain := compute(t, `<p id=p></p>`, "#p { margin-left: 10cqw }")
	if plain.c.QueriesContainers() || plain.of("p").Margin[Left].Value != vw/10 {
		t.Error("a container unit outside any container is not the small viewport's")
	}
}
