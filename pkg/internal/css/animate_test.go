package css

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// animKeyframes are the @keyframes rules of the animation tests: fades,
// rules missing a keyframe, a keyframe's own easing, colour, visibility,
// display, offsets and transforms, a custom property, two rules of one
// name, two blocks at one offset, !important in a keyframe, a string
// name, a -webkit- rule and a rule under @media screen.
const animKeyframes = `
@keyframes fade { from { opacity: 0 } to { opacity: 1 } }
@keyframes unfade { from { opacity: 1 } to { opacity: 0 } }
@keyframes tohalf { to { opacity: 0 } }
@keyframes fromq { from { opacity: .25 } }
@keyframes kt { from { opacity: 0; animation-timing-function: steps(2, jump-start) } to { opacity: 1 } }
@keyframes bg { from { background-color: #fff } to { background-color: #000 } }
@keyframes show { from { visibility: hidden } to { visibility: visible } }
@keyframes hide { from { visibility: visible } to { visibility: hidden } }
@keyframes dn { from { display: none } to { display: none } }
@keyframes mv { from { transform: translateX(300px) } to { transform: none } }
@keyframes mvl { from { margin-left: 300px } to { margin-left: 0 } }
@keyframes cv { from { --c: #fff } to { --c: #fff } }
@keyframes dup { from { opacity: 0 } }
@keyframes dup { from { opacity: .5 } }
@keyframes same { 0% { opacity: .2 } 0% { opacity: .6 } to { opacity: 1 } }
@keyframes imp { from { opacity: 0 !important } to { opacity: 1 !important } }
@keyframes "f a" { from { opacity: 0 } }
@-webkit-keyframes wk { from { opacity: 0 } }
@media screen { @keyframes scr { from { opacity: 0 } } }
@keyframes widthk { from { width: 20px } }
html { scroll-timeline: --page }
`

// animCase is a black box's style and the opacity, or other value, it
// prints with.
type animCase struct {
	name, style string
	opacity     float64
}

// TestAnimationsAtTimeZero locks what an animation shows at time zero,
// the moment Chromium prints a page (Web Animations 1, 4.7): the first
// keyframe, or the progress a negative delay gives; a backwards fill
// during a positive delay; nothing during a delay without one; the last
// keyframe of a zero duration filled forwards.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints each
//     case's box at the opacity given, three runs alike, measured as the
//     mean darkness of a black box over white at 96 dpi.
func TestAnimationsAtTimeZero(t *testing.T) {
	t.Parallel()
	for _, tc := range []animCase{
		{"no delay", "animation: fade 100s linear", 0},
		{"negative delay", "animation: fade 100s linear -25s", 0.25},
		{"reverse", "animation: fade 100s linear reverse", 1},
		{"alternate", "animation: fade 100s linear alternate infinite", 0},
		{"alternate-reverse", "animation: fade 100s linear alternate-reverse infinite", 1},
		{"alternate, second iteration", "animation: fade 100s linear alternate infinite -125s", 0.75},
		{"no from keyframe", "opacity: .5; animation: tohalf 100s linear", 0.5},
		{"no to keyframe", "animation: fromq 100s linear -50s", 0.625},
		{"steps jump-start", "animation: fade 100s steps(4, jump-start)", 0.25},
		{"steps", "animation: fade 100s steps(4)", 0},
		{"step-start", "animation: fade 100s step-start", 1},
		{"cubic-bezier", "animation: fade 100s cubic-bezier(.5, -1, .5, 2) -50s", 0.5},
		{"zero duration forwards", "animation: fade 0s forwards", 1},
		{"zero duration", "animation: fade 0s", 1},
		{"zero duration in a backwards delay", "animation: fade 0s 100s backwards", 0},
		{"zero duration reversed in a backwards delay", "animation: fade 0s 100s backwards reverse", 1},
		{"infinite", "animation: fade 100s linear infinite -250s", 0.5},
		{"no iteration, forwards", "animation: fade 100s linear 0 forwards", 0},
		{"2.5 iterations ended, forwards", "animation: fade 100s linear 2.5 forwards -1000s", 0.5},
		{"timeline none", "animation: fade 100s linear; animation-timeline: none", 0},
		{"timeline of no name", "animation: fade 100s linear; animation-timeline: --nope", 0},
		{"named scroll timeline", "animation: fade 100s linear; animation-timeline: --page", 0},
		{"scroll()", "animation: fade 100s linear; animation-timeline: scroll()", 0},
		{"scroll() past its delay", "animation: fade 100s linear 50s; animation-timeline: scroll()", 1},
		{"scroll() in a backwards delay", "animation: fade 100s linear 50s backwards; animation-timeline: scroll()", 0},
		{"scroll() before its range", "animation: fade linear; animation-timeline: scroll(); animation-range: 100px 500px", 1},
		{"scroll() before its range, filled", "animation: fade linear both; animation-timeline: scroll(); animation-range: 100px 500px", 0},
		{"view() below the screen", "animation: fade 100s linear; animation-timeline: view()", 1},
		{"view() below the screen, filled", "animation: fade linear both; animation-timeline: view()", 0},
		{"keyframe easing", "animation: kt 100s linear", 0.5},
		{"last animation wins", "animation: fade 100s linear, unfade 100s linear", 1},
		{"!important wins", "opacity: 1 !important; animation: fade 100s linear", 1},
		{"two rules of a name", "animation: dup 100s linear", 0.5},
		{"two blocks at an offset", "animation: same 100s linear", 0.6},
		{"!important in a keyframe", "animation: imp 100s linear", 1},
		{"string name", `animation: "f a" 100s linear`, 0},
		{"names are case-sensitive", "animation: FADE 100s linear", 1},
		{"-webkit-keyframes", "animation: wk 100s linear", 0},
		{"keyframes under @media screen", "animation: scr 100s linear", 1},
		{"paused", "animation: fade 100s linear -50s paused", 0.5},
		{"paused in a backwards delay", "animation: fade 100s linear 100s backwards paused", 0},
		{"delay without fill", "animation: fade 100s linear 100s", 1},
		{"linear()", "animation: fade 100s linear(0, .2 50%, 1) -50s", 0.2},
		{"ease-in", "animation: fade 100s ease-in -50s", 0.3153},
		{"ease", "animation: fade 100s -50s", 0.8024},
		{"ended without fill", "animation: fade 100s linear -150s", 1},
		{"ended, forwards", "animation: unfade 100s linear -150s forwards", 0},
		{"alternate twice, forwards", "animation: unfade 100s linear 2 alternate forwards -500s", 1},
		{"-webkit-animation", "-webkit-animation: fade 100s linear", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := compute(t, `<div id=t></div>`, animKeyframes+`#t { `+tc.style+` }`).of("t")
			if math.Abs(st.Opacity-tc.opacity) > 0.002 {
				t.Errorf("opacity %v, want %v", st.Opacity, tc.opacity)
			}
		})
	}
}

// TestAnimationsAtTimeZero_OtherProperties locks the other animated
// values Chromium prints at time zero: an interpolated colour, the
// visibility and display rules, offsets, a transform, a width from its
// underlying value, a custom property, a pseudo-element.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints white to
//     black at half way as 50% grey (0.498 dark), visibility hidden to
//     visible hidden at 0 and visible half way, display: none from
//     keyframes, a margin-left of 300px at 0, translateX(150px) half way
//     from 300px, a width of 60px half way from 20px to 100px, a custom
//     property set in a keyframe, and a ::before faded from 0.
func TestAnimationsAtTimeZero_OtherProperties(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=bg></div><div id=sh></div><div id=sh2></div><div id=hi></div><div id=dn></div>`+
		`<div id=ml></div><div id=tx></div><div id=w></div><div id=cv></div><div id=pe></div><div id=fl></div>`,
		animKeyframes+`
#bg { background: #000; animation: bg 100s linear -50s }
#sh { animation: show 100s linear }
#sh2 { animation: show 100s linear -50s }
#hi { animation: hide 100s linear -50s }
#dn { animation: dn 100s linear }
#ml { animation: mvl 100s linear }
#tx { animation: mv 100s linear -50s }
#w { width: 100px; animation: widthk 100s linear -50s }
#cv { background: var(--c, #000); animation: cv 100s linear }
#pe::before { content: ""; animation: fade 100s linear }
#fl { direction: rtl; margin-inline-start: var(--m, 1px); animation: m 100s linear }
@keyframes m { from { --m: 7px } to { --m: 7px } }`)
	if bg := c.of("bg").BackgroundColor; math.Abs(bg.R-0.5) > 0.003 || bg.A != 1 {
		t.Errorf("background %v, want 50%% grey", bg)
	}
	if v := c.of("sh").Visibility; v != Hidden {
		t.Errorf("show at 0: visibility %v", v)
	}
	if v := c.of("sh2").Visibility; v != Visible {
		t.Errorf("show half way: visibility %v", v)
	}
	if v := c.of("hi").Visibility; v != Visible {
		t.Errorf("hide half way: visibility %v", v)
	}
	if d := c.of("dn").Display; d != DisplayNone {
		t.Errorf("display %v, want none", d)
	}
	if m := c.of("ml").Margin[Left]; m != Pts(300*Pt) {
		t.Errorf("margin-left %v, want 300px", m)
	}
	if tf := c.of("tx").Transform; len(tf) != 1 || tf[0].Kind != TransformTranslate || !near(tf[0].X.Value, 150*Pt) {
		t.Errorf("transform %+v, want translateX(150px)", tf)
	}
	if w := c.of("w").Width; w.Kind != Points || !near(w.Value, 60*Pt) {
		t.Errorf("width %v, want 60px", w)
	}
	if bg := c.of("cv").BackgroundColor; bg != (Color{R: 1, G: 1, B: 1, A: 1}) {
		t.Errorf("background %v, want white from the animated --c", bg)
	}
	if ps := c.c.Pseudo(byID(c.doc, "pe"), PseudoBefore); ps == nil || ps.Opacity != 0 {
		t.Errorf("::before %+v, want opacity 0", ps)
	}
	if m := c.of("fl").Margin; m[Right] != Pts(7*Pt) || m[Left] != Pts(0) {
		t.Errorf("margins %v, want the animated --m on the right, the inline start of rtl", m)
	}
}

// TestAnimationsAtEnd locks the internal end-state switch: each animation
// at the end of its active interval, or the element's own style when its
// fill mode does not keep it; the end of the first iteration of one that
// repeats forever; time zero for a paused animation and one off the
// document timeline.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 after
//     waitDelay=3s prints each finite case of 0.5 s at the opacity given;
//     the paused one, the one on the timeline none and the one on
//     scroll() at time zero's. An infinite fade of 0.7 s prints at 0.31
//     after 3 s and 0.74 after 3.3 s: the wait decides.
func TestAnimationsAtEnd(t *testing.T) {
	t.Parallel()
	for _, tc := range []animCase{
		{"forwards", "animation: fade .5s linear forwards", 1},
		{"no fill", "opacity: .5; animation: fade .5s linear", 0.5},
		{"backwards", "animation: fade .5s linear backwards", 1},
		{"reverse forwards", "animation: fade .5s linear reverse forwards", 0},
		{"alternate twice forwards", "animation: fade .5s linear 2 alternate forwards", 0},
		{"alternate thrice forwards", "animation: fade .5s linear 3 alternate forwards", 1},
		{"1.5 iterations forwards", "animation: fade .5s linear 1.5 forwards", 0.5},
		{"paused", "animation: fade .5s linear forwards paused", 0},
		{"timeline none", "animation: fade .5s linear forwards; animation-timeline: none", 0},
		{"scroll()", "animation: fade .5s linear forwards; animation-timeline: scroll()", 0},
		{"delayed forwards", "animation: fade .5s linear 1s forwards", 1},
		{"zero duration forwards", "animation: fade 0s forwards", 1},
		{"no iteration forwards", "animation: fade .5s linear 0 forwards", 0},
		{"fade out forwards", "animation: unfade .5s linear forwards", 0},
		{"infinite", "animation: fade .7s linear infinite", 1},
		{"infinite reverse", "animation: fade .7s linear infinite reverse", 0},
		{"infinite alternate-reverse", "animation: fade .7s linear infinite alternate-reverse", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := parseHTML(t, `<div id=t></div>`)
			r := NewResolver(a4, []Sheet{{Sheet: mustParse(t, animKeyframes+`#t { `+tc.style+` }`), Origin: Author}})
			c, err := r.Compute(context.Background(), root, ComputeOptions{AnimationTime: AnimationsAtEnd})
			if err != nil {
				t.Fatal(err)
			}
			if st := c.Style(byID(root, "t")); math.Abs(st.Opacity-tc.opacity) > 0.002 {
				t.Errorf("opacity %v, want %v", st.Opacity, tc.opacity)
			}
		})
	}
}

// TestAnimationProperties_ParseAsChromium holds the animation, transition
// and timeline properties to the values Chromium parses.
//
// # Corpus evidence
//
//   - probe, html: CSS.supports in Chromium of gotenberg/gotenberg:9.4.1
//     answers each value as listed.
func TestAnimationProperties_ParseAsChromium(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		prop, value string
		ok          bool
	}{
		{"animation-name", "none", true}, {"animation-name", "a, none, b", true}, {"animation-name", `"str"`, true},
		{"animation-name", "default", false}, {"animation-name", "a b", false},
		{"animation-duration", "auto", true}, {"animation-duration", "1s", true}, {"animation-duration", "-1s", false},
		{"animation-duration", "0", false}, {"animation-duration", "100ms, 2s", true}, {"animation-duration", "calc(1s + 100ms)", true},
		{"animation-delay", "-1s", true}, {"animation-delay", "1s 2s", false},
		{"animation-delay-start", "1s", false}, {"animation-delay-end", "1s", false},
		{"animation-iteration-count", "infinite", true}, {"animation-iteration-count", "2.5", true},
		{"animation-iteration-count", "-1", false}, {"animation-iteration-count", "0", true},
		{"animation-direction", "alternate-reverse", true}, {"animation-fill-mode", "both", true}, {"animation-play-state", "paused", true},
		{"animation-timing-function", "steps(3, jump-none)", true}, {"animation-timing-function", "steps(1, jump-none)", false},
		{"animation-timing-function", "cubic-bezier(.1,2,.3,-1)", true}, {"animation-timing-function", "cubic-bezier(2,0,.3,1)", false},
		{"animation-timing-function", "linear(0, .5 25%, 1)", true}, {"animation-timing-function", "step-start, step-end", true},
		{"animation-timing-function", "steps(2, start)", true},
		{"animation-composition", "add", true}, {"animation-composition", "accumulate", true}, {"animation-composition", "replace, add", true},
		{"animation-timeline", "auto", true}, {"animation-timeline", "none", true}, {"animation-timeline", "--t", true},
		{"animation-timeline", "scroll()", true}, {"animation-timeline", "scroll(root inline)", true}, {"animation-timeline", "view()", true},
		{"animation-timeline", "view(block 10px auto)", true}, {"animation-timeline", "foo", false},
		{"animation-range", "normal", true}, {"animation-range", "entry 10% exit 90%", true}, {"animation-range", "cover", true},
		{"animation-range", "10px 50%", true}, {"animation-range-start", "entry 20%", true}, {"animation-range-end", "normal", true},
		{"animation-range-center", "normal", false},
		{"animation", "1s", true}, {"animation", "fade 1s ease-in 2s infinite reverse both paused", true}, {"animation", "none", true},
		{"animation", "a 1s, b 2s", true}, {"animation", "1s 2s 3s", false}, {"animation", "auto", true}, {"animation", "fade auto", true},
		{"animation", "fade 1s add", false}, {"animation", "fade 1s scroll()", false}, {"animation", "fade 1s --t", false},
		{"animation", "none, a", true}, {"animation", "a 1s linear 1s linear", false}, {"animation", "infinite infinite", true},
		{"animation", "forwards both", true}, {"animation", "reverse paused running", true},
		{"transition", "opacity 1s", true}, {"transition", "all 1s allow-discrete", true}, {"transition", "none", true},
		{"transition", "none, opacity 1s", false}, {"transition", "opacity 1s, none", false}, {"transition", "foo 1s", true},
		{"transition", "1s 2s 3s", false}, {"transition", "initial 1s", false},
		{"transition-property", "none", true}, {"transition-property", "opacity, none", false}, {"transition-property", "all, foo", true},
		{"transition-behavior", "allow-discrete", true}, {"transition-behavior", "normal, allow-discrete", true},
		{"transition-duration", "-1s", false}, {"transition-delay", "-1s", true}, {"transition-timing-function", "steps(0)", false},
		{"scroll-timeline", "--a block", true}, {"scroll-timeline", "--a x, --b", true}, {"scroll-timeline", "none", true},
		{"scroll-timeline-name", "--a", true}, {"scroll-timeline-name", "a", false}, {"scroll-timeline-axis", "inline", true},
		{"scroll-timeline-axis", "vertical", false},
		{"view-timeline", "--v block 10px", true}, {"view-timeline", "--v", true}, {"view-timeline-name", "none, --v", true},
		{"view-timeline-axis", "x", true}, {"view-timeline-inset", "auto 10%", true}, {"view-timeline-inset", "10px", true},
		{"timeline-scope", "--a, --b", true}, {"timeline-scope", "all", false}, {"timeline-scope", "none", true},
	} {
		decls := ParseDeclarations(tc.prop+": "+tc.value, ParseOptions{})
		got := len(decls) == 1 && decls[0].declares()
		if got != tc.ok {
			t.Errorf("%s: %s parses %v, want %v", tc.prop, tc.value, got, tc.ok)
		}
	}
}

// TestAnimationShorthand_Expands checks how animation assigns its
// keywords and times (CSS Animations 1, 4.9): the first time is the
// duration, the second the delay, and a keyword goes to the name only
// once every other property that takes it is set.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 computes
//     animation: ease ease to the name ease, animation: none 1s none to
//     the name none, animation: auto to an auto duration.
func TestAnimationShorthand_Expands(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=a></div><div id=b></div><div id=c></div><div id=d></div>`, `
#a { animation: fade 1s ease-in 2s infinite reverse both paused }
#b { animation: ease ease }
#c { animation: a 1s, b }
#d { animation: infinite infinite }`)
	a := c.of("a").Animation
	if a == nil || a.Names[0] != "fade" || a.Durations[0] != 1 || a.Delays[0] != 2 || !math.IsInf(a.Iterations[0], 1) ||
		a.Directions[0] != DirectionReverse || a.Fills[0] != FillBoth || !a.Paused[0] || a.Easings[0].X1 != 0.42 {
		t.Errorf("#a: %+v", a)
	}
	if b := c.of("b").Animation; b == nil || len(b.Names) != 1 || b.Names[0] != "ease" {
		t.Errorf("#b: %+v", b)
	}
	if x := c.of("c").Animation; x == nil || len(x.Names) != 2 || x.Durations[0] != 1 || x.Durations[1] != -1 {
		t.Errorf("#c: %+v", x)
	}
	if x := c.of("d").Animation; x == nil || x.Names[0] != "infinite" || !math.IsInf(x.Iterations[0], 1) {
		t.Errorf("#d: %+v", x)
	}
}

// TestKeyframes_Parse checks the @keyframes rules litho keeps (CSS
// Animations 1, 3): none and initial are no names, a selector out of
// [0%, 100%] or without its percent sign drops its block, and the
// animation properties but the timing function drop out of a block.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 drops
//     @keyframes none, @keyframes initial and @-moz-keyframes, keeps
//     @-webkit-keyframes, and keeps of @keyframes k5 { 0 {} 120% {}
//     -10% {} 10% { animation-name; animation-timing-function;
//     animation-duration; opacity } } the 10% block with its timing
//     function and opacity.
func TestKeyframes_Parse(t *testing.T) {
	t.Parallel()
	s := mustParse(t, `
@keyframes none { 0% { opacity: 0 } }
@keyframes initial { 0% { opacity: 0 } }
@-moz-keyframes k3 { 0% { opacity: 0 } }
@-webkit-keyframes k2 { 0% { opacity: 0 } }
@keyframes k5 { 0 { opacity: 0 } 120% { opacity: .2 } -10% { opacity: .3 }
  10% { animation-name: foo; animation-timing-function: steps(2); animation-duration: 3s; opacity: .4 } }`)
	var names []string
	for _, k := range s.keyframes {
		names = append(names, k.name)
	}
	if len(names) != 2 || names[0] != "k2" || names[1] != "k5" {
		t.Fatalf("rules %v, want [k2 k5]", names)
	}
	k5 := s.keyframes[1]
	if len(k5.blocks) != 1 || k5.blocks[0].offsets[0] != 0.1 || k5.blocks[0].easing == nil || len(k5.blocks[0].decls) != 1 {
		t.Errorf("k5 blocks %+v", k5.blocks)
	}
	if len(s.Warnings()) != 0 {
		t.Errorf("warnings %v", s.Warnings())
	}
}

// TestAnimations_Warnings checks what an animation reports: nothing for
// the animation and transition properties, @starting-style or a
// keyframe it computes; once for a property that is not animatable, a
// view timeline, a composite operation computed as replace, and a value
// interpolated in steps.
func TestAnimations_Warnings(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=a></div><div id=b></div><div id=c></div><div id=d></div><div id=e></div>`, animKeyframes+`
@keyframes wm { from { writing-mode: vertical-rl; opacity: 0 } }
@keyframes sh { from { text-shadow: 1px 1px red } to { text-shadow: 3px 3px red } }
@starting-style { #a { opacity: 0 } }
#a { animation: fade 1s; transition: opacity 1s allow-discrete }
#b { animation: wm 1s }
#c { animation: fade 1s; animation-timeline: view() }
#d { animation: fade 1s; animation-composition: add }
#e { animation: sh 100s linear -50s }`)
	ws := c.c.Warnings()
	for _, want := range []string{`"writing-mode" is not animatable`, "view timeline", "composition", `"text-shadow" is animated in discrete steps`} {
		if !hasWarning(ws, warn.CSSUnsupported, want) {
			t.Errorf("no warning %q: %v", want, ws)
		}
	}
	if len(ws) != 4 {
		t.Errorf("%d warnings, want 4: %v", len(ws), ws)
	}
}

// TestAnimationTypesAgainstWebref holds the longhands litho animates in
// discrete steps without a warning, and those it does not animate, to the
// animation types of their definitions, as the pinned @webref/css data
// gathers them.
func TestAnimationTypesAgainstWebref(t *testing.T) {
	t.Parallel()
	byName := webrefAnimationTypes(t)
	for i := range props {
		def := &props[i]
		typ, ok := byName[def.name]
		if !ok {
			continue
		}
		if got, want := discreteAnimation[i], typ == "discrete"; got != want {
			t.Errorf("%s: discrete %v, webref %q", def.name, got, typ)
		}
		if got, want := notAnimatable[i], typ == "not animatable"; got != want && def.name != "animation-delay" {
			t.Errorf("%s: not animatable %v, webref %q", def.name, got, typ)
		}
	}
}

// TestEasing_Output checks the easing functions against CSS Easing 2:
// steps() at its boundaries and under the before flag, linear()'s
// canonical control points, and cubic-bezier() extended past [0, 1].
func TestEasing_Output(t *testing.T) {
	t.Parallel()
	parse := func(s string) Easing {
		t.Helper()
		v, _ := parseComponentValues(s)
		e, ok := parseEasing(significant(v)[0])
		if !ok {
			t.Fatalf("%s does not parse", s)
		}
		return e
	}
	for _, tc := range []struct {
		fn     string
		x      float64
		before bool
		want   float64
	}{
		{"steps(4, jump-start)", 0, false, 0.25},
		{"steps(4, jump-start)", 0, true, 0},
		{"steps(4)", 0.25, false, 0.25},
		{"steps(4)", 0.9999, false, 0.75},
		{"steps(3, jump-none)", 0.5, false, 0.5},
		{"steps(3, jump-both)", 0, false, 0.25},
		{"steps(2)", 1, false, 1},
		{"linear(0, 0.25 75%, 1)", 0.375, false, 0.125},
		{"linear(0, 0.25 25% 75%, 1)", 0.5, false, 0.25},
		{"linear(0 20%, 0.5 10%, 1)", 0.6, false, 0.75},
		{"linear(0, 1)", 1.5, false, 1.5},
		{"linear", 0.3, false, 0.3},
		{"ease-in", 0.5, false, 0.3153},
		{"ease", 0.5, false, 0.8024},
		{"cubic-bezier(0.25, 0.1, 0.25, 1)", -1, false, -0.4},
		{"cubic-bezier(0, 0, 1, 0.5)", 2, false, 2},
	} {
		e := parse(tc.fn)
		if got := e.at(tc.x, tc.before); math.Abs(got-tc.want) > 1e-3 {
			t.Errorf("%s at %v (before %v) = %v, want %v", tc.fn, tc.x, tc.before, got, tc.want)
		}
	}
}

// TestAnimations_StepBudget checks that the work of animations counts
// against the cascade's step budget: a sheet that animates many
// properties through many animations stops with ErrTooComplex once the
// budget is spent.
func TestAnimations_StepBudget(t *testing.T) {
	t.Parallel()
	var css, names, doc strings.Builder
	css.WriteString("@keyframes k { from {")
	for _, p := range []string{"opacity", "width", "height", "margin-left", "margin-top", "padding-left", "left", "top", "font-size", "letter-spacing"} {
		css.WriteString(p + ": 1px; ")
	}
	css.WriteString("} }\n")
	for i := range maxListItems {
		if i > 0 {
			names.WriteString(", ")
		}
		names.WriteString("k 100s linear -50s")
	}
	css.WriteString("p { animation: " + names.String() + " }")
	for range 10 {
		doc.WriteString("<p>x</p>")
	}
	root := parseHTML(t, doc.String())
	r := NewResolver(a4, []Sheet{{Sheet: mustParse(t, css.String()), Origin: Author}})
	// Ten elements of a thousand animations interpolating ten properties
	// each take about 10^8 steps; matching and cascading them without the
	// animations takes about 10^4.
	if _, err := r.Compute(context.Background(), root, ComputeOptions{maxSteps: 1 << 20}); !errors.Is(err, ErrTooComplex) {
		t.Errorf("Compute: %v, want ErrTooComplex", err)
	}
}
