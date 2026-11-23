package css

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestProperty_RegistersInitialAndInheritance checks registered custom
// properties (CSS Properties and Values API 1, 2.4 and 3): an undeclared
// one takes its initial value, so that a var() of it substitutes; one
// that does not inherit takes its initial value on every element its
// parent's value would reach; a value its syntax refuses, and the initial
// keyword, compute as unset and initial.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints box-shadow: var(--a), var(--b)
//     with only --b declared and --a registered of initial 0 0 #0000 as
//     --b's shadow, a non-inheriting --c declared red on a parent as its
//     initial green on the child, an inheriting one as red, a <length>
//     declared red as its initial 4px, and --c: initial as green.
func TestProperty_RegistersInitialAndInheritance(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><div id=shadow></div><div id=p><div id=inh></div><div id=noinh></div></div><div id=typed></div><div id=init></div><div id=plain></div></body>`, `
@property --a { syntax: "*"; inherits: false; initial-value: 0 0 #0000 }
@property --inh { syntax: "<color>"; inherits: true; initial-value: green }
@property --noinh { syntax: "<color>"; inherits: false; initial-value: green }
@property --len { syntax: "<length>"; inherits: true; initial-value: 4px }
#shadow { --b: 0 4px 6px red; box-shadow: var(--a), var(--b) }
#p { --inh: red; --noinh: red }
#inh { background-color: var(--inh) }
#noinh { background-color: var(--noinh) }
#typed { --len: red; border: var(--len) solid black }
#init { --inh: initial; background-color: var(--inh) }
#plain { --u: red; background-color: var(--undeclared, var(--u)) }
`)
	if s := c.of("shadow"); len(s.BoxShadow) != 2 {
		t.Errorf("box-shadow with an undeclared registered property: %v", s.BoxShadow)
	}
	red, green := rgb(255, 0, 0), rgb(0, 128, 0)
	for id, want := range map[string]Color{"inh": red, "noinh": green, "init": green, "plain": red} {
		if got := c.of(id).BackgroundColor; got != want {
			t.Errorf("#%s: background %v, want %v", id, got, want)
		}
	}
	if s := c.of("typed"); s.Border[Top].Width != 3 {
		t.Errorf("a <length> declared red: border width %v, want its initial 4px", s.Border[Top].Width)
	}
}

// TestProperty_RefusesInvalidRules checks the validity of an @property
// rule (CSS Properties and Values API 1, 3 and 5): syntax and inherits
// are needed, initial-value is needed unless the syntax is universal and
// must then match the syntax and be computationally independent; an
// invalid rule registers nothing and warns.
func TestProperty_RefusesInvalidRules(t *testing.T) {
	t.Parallel()
	for _, rule := range []string{
		`@property --x { inherits: false; initial-value: red }`,
		`@property --x { syntax: "<color>"; initial-value: red }`,
		`@property --x { syntax: "<color>"; inherits: false }`,
		`@property --x { syntax: "<color>"; inherits: false; initial-value: 4px }`,
		`@property --x { syntax: "<length>"; inherits: false; initial-value: 2em }`,
		`@property --x { syntax: "<colour>"; inherits: false; initial-value: red }`,
		`@property --x { syntax: "<transform-list>+"; inherits: false; initial-value: none }`,
		`@property --x { syntax: "<color>"; inherits: maybe; initial-value: red }`,
		`@property --x { syntax: <color>; inherits: false; initial-value: red }`,
		`@property x { syntax: "*"; inherits: false }`,
	} {
		sh := mustParse(t, rule)
		if len(sh.properties) != 0 {
			t.Errorf("%s: registered", rule)
		}
		if !hasWarning(sh.Warnings(), warn.CSSInvalid, "@property") {
			t.Errorf("%s: no warning", rule)
		}
	}
	for _, rule := range []string{
		`@property --x { syntax: "*"; inherits: false }`,
		`@property --x { syntax: "<length> | auto"; inherits: true; initial-value: auto }`,
		`@property --x { syntax: "<length>+"; inherits: true; initial-value: 1px 2px }`,
		`@property --x { syntax: "<color>#"; inherits: true; initial-value: red, blue }`,
		`@property --x { syntax: "<transform-list>"; inherits: false; initial-value: rotate(10deg) scale(2) }`,
		`@property --x { syntax: "<length-percentage>"; inherits: false; initial-value: 50% }`,
		`@property --x { syntax: "<angle>"; inherits: false; initial-value: 45deg }`,
	} {
		if sh := mustParse(t, rule); len(sh.properties) != 1 || len(sh.Warnings()) != 0 {
			t.Errorf("%s: registered %d, warnings %v", rule, len(sh.properties), sh.Warnings())
		}
	}
}

// TestProperty_LastRuleWins checks that the last valid @property rule of
// a name registers it (CSS Properties and Values API 1, 3), and that one
// inside @media registers only where its media match.
func TestProperty_LastRuleWins(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><div id=t></div></body>`, `
@property --c { syntax: "<color>"; inherits: false; initial-value: red }
@property --c { syntax: "<color>"; inherits: false; initial-value: green }
@property --c { syntax: "<color>"; inherits: false; initial-value: 4px }
@media screen { @property --c { syntax: "<color>"; inherits: false; initial-value: blue } }
#t { background-color: var(--c) }
`)
	if got := c.of("t").BackgroundColor; got != rgb(0, 128, 0) {
		t.Errorf("background %v, want green", got)
	}
}

// TestProperty_AbsolutizedBeforeInheriting computes a registered length
// on the element that declares it (CSS Properties and Values API 1, 2.4),
// so a child of another font size inherits the declaring element's
// pixels, as Chromium 156 printed it: 2em at 10px is 20px on a 20px
// child, a list of lengths and the length part of a <length-percentage>
// too, a <color> of currentcolor resolving on the child.
func TestProperty_AbsolutizedBeforeInheriting(t *testing.T) {
	t.Parallel()
	c := compute(t, `<!DOCTYPE html><div id=a><div id=b></div><div id=d></div></div>`, `
@property --w { syntax: '<length>'; inherits: true; initial-value: 0px }
@property --l { syntax: '<length>+'; inherits: true; initial-value: 0px }
@property --lp { syntax: '<length-percentage>'; inherits: true; initial-value: 0px }
@property --c { syntax: '<color>'; inherits: true; initial-value: black }
#a { font-size: 10px; --w: 2em; --l: 1em 2em; --lp: calc(1em + 10%); --c: currentcolor; color: red }
#b { font-size: 20px; width: var(--w); margin: var(--l); color: green; border-left: 1px solid var(--c) }
#d { font-size: 30px; width: var(--lp) }`)
	b := c.of("b")
	if b.Width != Pts(20*Pt) || b.Margin[Top] != Pts(10*Pt) || b.Margin[Right] != Pts(20*Pt) {
		t.Errorf("b: width %v margins %v, want 20px and 10px 20px", b.Width, b.Margin)
	}
	if b.Border[Left].Color != rgb(0, 128, 0) {
		t.Errorf("b: border colour %v, want its own green", b.Border[Left].Color)
	}
	if d := c.of("d").Width; d.Kind != Calc || !near(d.Value, 10*Pt) || !near(d.Fraction, 0.1) {
		t.Errorf("d: width %+v, want calc(10%% + 10px)", d)
	}
}

// TestProperty_ShadowTreeRegistersNothing leaves an @property rule of a
// shadow tree out, as Chromium 156 reads it: var() of the name takes its
// fallback in the tree and outside it.
func TestProperty_ShadowTreeRegistersNothing(t *testing.T) {
	t.Parallel()
	doc := parseHTML(t, `<!DOCTYPE html><div id=h></div><p id=out></p>`)
	root := NewShadowRoot()
	sheet := mustParse(t, `@property --s { syntax: '<color>'; inherits: false; initial-value: rgb(0, 0, 77) }`)
	r := NewResolver(a4, []Sheet{
		{Sheet: HTMLUserAgent(), Origin: UserAgent},
		{Sheet: mustParse(t, `#out { color: var(--s, rgb(0, 0, 1)) }`), Origin: Author},
		{Sheet: sheet, Origin: Author, Scope: root},
	})
	got, err := r.Compute(context.Background(), doc, ComputeOptions{Hints: HTMLHints})
	if err != nil {
		t.Fatal(err)
	}
	if c := got.Style(byID(doc, "out")).Color; c != rgb(0, 0, 1) {
		t.Errorf("out: colour %v, want the fallback", c)
	}
}
