package css

import (
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
