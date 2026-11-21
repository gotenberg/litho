package css

import (
	"context"
	"testing"

	"golang.org/x/net/html"
)

// TestAnchorLonghandsParse holds the value grammars of CSS Anchor
// Positioning 1: anchor-name and anchor-scope (2.1, 2.1.1),
// position-anchor (2.3), position-try-fallbacks (5.1), position-try-order
// (5.2) and position-visibility (5.5), and anchor-center, which Chromium
// takes after an overflow position as a <self-position>.
func TestAnchorLonghandsParse(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		decl string
		ok   bool
	}{
		{"anchor-name: --a", true},
		{"anchor-name: --a, --b", true},
		{"anchor-name: none", true},
		{"anchor-name: a", false},
		{"anchor-name: --a --b", false},
		{"anchor-scope: all", true},
		{"anchor-scope: --a, --b", true},
		{"anchor-scope: --a all", false},
		{"position-anchor: --a", true},
		{"position-anchor: match-parent", true},
		{"position-anchor: auto", true},
		{"position-anchor: --a --b", false},
		{"position-anchor: a", false},
		{"position-try-fallbacks: flip-block, --r flip-inline, flip-start flip-x flip-y, top left", true},
		{"position-try-fallbacks: flip-block --r", true},
		{"position-try-fallbacks: flip-block --r flip-inline", false},
		{"position-try-fallbacks: flip-block flip-block", false},
		{"position-try-fallbacks: --a --b", false},
		{"position-try-fallbacks: none, flip-block", false},
		{"position-try-order: most-block-size", true},
		{"position-try-order: most", false},
		{"position-try: most-width flip-block, --r", true},
		{"position-try: flip-block", true},
		{"position-try: most-width", false},
		{"position-visibility: anchor-valid no-overflow", true},
		{"position-visibility: always", true},
		{"position-visibility: always no-overflow", false},
		{"position-visibility: no-overflow no-overflow", false},
		{"position-visibility: anchors-visible", false},
		{"justify-self: anchor-center", true},
		{"align-self: anchor-center", true},
		{"place-self: anchor-center", true},
		{"align-self: safe anchor-center", true},
		{"align-self: safe normal", true},
		{"justify-self: unsafe normal", true},
		{"place-self: unsafe normal safe normal", true},
		{"align-items: safe normal", false},
		{"justify-items: unsafe normal", false},
		{"justify-self: unsafe anchor-center", true},
		{"place-self: safe anchor-center", true},
		{"justify-items: safe anchor-center", false},
		{"align-self: anchor-center safe", false},
		{"align-items: anchor-center", false},
	} {
		d := ParseDeclarations(c.decl, ParseOptions{})
		if got := len(d) == 1 && len(d[0].entries) > 0; got != c.ok {
			t.Errorf("%s: parsed %v, want %v", c.decl, got, c.ok)
		}
	}
}

// TestPositionAreaGrammar holds the <position-area> grammar and its axis
// rules (CSS Anchor Positioning 1, 3.1.2): two keywords of one
// alternative naming two axes; an ambiguous keyword takes the axis the
// other leaves, the first of two the block axis; one unambiguous keyword
// spans the other axis, one ambiguous keyword is repeated.
func TestPositionAreaGrammar(t *testing.T) {
	t.Parallel()
	span := func(axis AreaAxis, ref AreaRef, tracks uint8) AreaSpan {
		return AreaSpan{Axis: axis, Ref: ref, Tracks: tracks}
	}
	for _, c := range []struct {
		value string
		want  PositionArea
		ok    bool
	}{
		{"top left", PositionArea{span(AreaY, AreaPhysical, TrackStart), span(AreaX, AreaPhysical, TrackStart)}, true},
		{"span-x-start top", PositionArea{span(AreaX, AreaLogical, TrackStart|TrackCenter), span(AreaY, AreaPhysical, TrackStart)}, true},
		{"top", PositionArea{span(AreaY, AreaPhysical, TrackStart), span(AreaX, AreaPhysical, TrackAll)}, true},
		{"center", PositionArea{span(AreaBlock, AreaLogical, TrackCenter), span(AreaInline, AreaLogical, TrackCenter)}, true},
		{"block-start center", PositionArea{span(AreaBlock, AreaLogical, TrackStart), span(AreaInline, AreaLogical, TrackCenter)}, true},
		{"span-all start", PositionArea{span(AreaBlock, AreaLogical, TrackAll), span(AreaInline, AreaLogical, TrackStart)}, true},
		{"self-end span-self-start", PositionArea{span(AreaBlock, AreaSelf, TrackEnd), span(AreaInline, AreaSelf, TrackStart|TrackCenter)}, true},
		{"inline-end", PositionArea{span(AreaInline, AreaLogical, TrackEnd), span(AreaBlock, AreaLogical, TrackAll)}, true},
		{"right center", PositionArea{span(AreaX, AreaPhysical, TrackEnd), span(AreaY, AreaPhysical, TrackCenter)}, true},
		{"top bottom", PositionArea{}, false},
		{"left block-start", PositionArea{}, false},
		{"start self-end", PositionArea{}, false},
		{"inline-start inline-end", PositionArea{}, false},
		{"left top right", PositionArea{}, false},
	} {
		l, _ := parseComponentValues(c.value)
		got, ok := parsePositionArea(significant(l))
		if ok != c.ok || ok && got != c.want {
			t.Errorf("%s: %+v %v, want %+v %v", c.value, got, ok, c.want, c.ok)
		}
	}
}

// TestAnchorFunctionsParse holds the grammars of anchor() and
// anchor-size() (CSS Anchor Positioning 1, 3.2 and 4.1): anchor() in the
// inset properties alone, anchor-size() in the inset, sizing and margin
// properties, either alone or in a math function.
func TestAnchorFunctionsParse(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		decl string
		ok   bool
	}{
		{"top: anchor(--a bottom)", true},
		{"top: anchor(bottom --a)", true},
		{"left: anchor(50%)", true},
		{"left: anchor(center, 10px)", true},
		{"left: anchor(--a center, 10%)", true},
		{"left: calc(anchor(--a right) + 1em)", true},
		{"left: max(anchor(--a right), anchor(--b right, 0px))", true},
		{"left: anchor(--a right, anchor(--b left))", true},
		{"inset-inline-start: anchor(--a self-end)", true},
		{"left: anchor(--a)", false},
		{"left: anchor(--a --b left)", false},
		{"left: anchor(--a left,)", false},
		{"left: anchor(--a left, 1px, 2px)", false},
		{"left: anchor(--a left, 3)", false},
		{"left: anchor(--a left, 0)", true},
		{"width: anchor(--a left)", false},
		{"margin-left: anchor(--a left)", false},
		{"top: anchor-size(--a height)", true},
		{"width: anchor-size()", true},
		{"width: anchor-size(--a)", true},
		{"width: anchor-size(width --a, 20px)", true},
		{"width: anchor-size(20px)", true},
		{"min-height: calc(anchor-size(self-block) / 2)", true},
		{"max-width: anchor-size(--a inline)", true},
		{"margin-top: anchor-size(--a height)", true},
		{"width: anchor-size(left)", false},
		{"padding-left: anchor-size(width)", false},
	} {
		d := ParseDeclarations(c.decl, ParseOptions{})
		if got := len(d) == 1 && len(d[0].entries) > 0; got != c.ok {
			t.Errorf("%s: parsed %v, want %v", c.decl, got, c.ok)
		}
	}
}

// TestAnchorFunctionComputedValue holds the computed-value rules of
// anchor() and anchor-size() (CSS Anchor Positioning 1, 3.2.1 and
// 4.1.1): on an absolutely positioned box they stay for the layout to
// resolve; on another box, or as an anchor() whose physical side is of the
// other axis, they compute to their fallback, and without one the
// declaration is invalid at computed-value time: the property takes its
// initial value.
func TestAnchorFunctionComputedValue(t *testing.T) {
	t.Parallel()
	doc := `<div id=a></div><div id=b></div><div id=c></div><div id=d></div><div id=e></div>`
	s := compute(t, doc, `
		#a { position: absolute; top: anchor(--x bottom, 7px); width: anchor-size(--x width) }
		#b { position: relative; top: anchor(--x bottom, 7px); left: anchor(--x right); width: anchor-size(--x width, 3px); margin-left: anchor-size(width) }
		#c { position: absolute; top: anchor(--x left, 5px); left: anchor(--x top) }
		#d { position: absolute; top: calc(anchor(--x top) + 2px) }
		#e { position: fixed; top: anchor(--x bottom) }`)
	a := s.of("a")
	if !a.Top.Anchored() || !a.Width.Anchored() {
		t.Errorf("#a: top %+v, width %+v: want anchor functions kept", a.Top, a.Width)
	}
	b := s.of("b")
	if b.Top != Pts(7*Pt) || b.Left.Kind != Auto || b.Width != Pts(3*Pt) || b.Margin[Left] != Pts(0) {
		t.Errorf("#b: top %+v, left %+v, width %+v, margin-left %+v: want the fallbacks or the initial values", b.Top, b.Left, b.Width, b.Margin[Left])
	}
	c := s.of("c")
	if c.Top != Pts(5*Pt) || c.Left.Kind != Auto {
		t.Errorf("#c: top %+v, left %+v: want the fallback and auto for a side of the other axis", c.Top, c.Left)
	}
	if d := s.of("d"); !d.Top.Anchored() {
		t.Errorf("#d: top %+v: want the calculation kept", d.Top)
	}
	if e := s.of("e"); !e.Top.Anchored() {
		t.Errorf("#e: top %+v: want a fixed box to keep its anchor function", e.Top)
	}
}

// TestResolveAnchors holds the substitution the layout engine makes: each
// function takes the length it resolves to, else its fallback, else the
// declaration is invalid (CSS Anchor Positioning 1, 3.2.1).
func TestResolveAnchors(t *testing.T) {
	t.Parallel()
	s := compute(t, `<div id=a></div>`, `#a { position: absolute; left: calc(anchor(--x right) + 10px); top: anchor(--y top, 20%); right: anchor(--z left) }`)
	st := s.of("a")
	got := func(a AnchorFn) (float64, bool) {
		if a.Name == "--x" && a.Side == AnchorRight {
			return 100, true
		}
		return 0, false
	}
	l, ok := st.Left.ResolveAnchors(got)
	if !ok || l.Resolve(0) != 100+10*Pt {
		t.Errorf("left %+v %v: want 100pt plus 10px", l, ok)
	}
	top, ok := st.Top.ResolveAnchors(got)
	if !ok || top.Resolve(200) != 40 {
		t.Errorf("top %+v %v: want the 20%% fallback", top, ok)
	}
	if _, ok := st.Right.ResolveAnchors(got); ok {
		t.Error("right: an unresolved function without a fallback resolved")
	}
	flipped := st.Left.MapAnchors(func(a AnchorFn) AnchorFn { a.Side = AnchorLeft; return a })
	if l, ok := flipped.ResolveAnchors(got); ok {
		t.Errorf("mapped left resolved to %+v: want the mapped side looked up", l)
	}
}

// TestAnchorLonghandsCompute holds the computed values of the longhands
// held in [Style.Anchor], and that all-initial values leave it nil.
func TestAnchorLonghandsCompute(t *testing.T) {
	t.Parallel()
	doc := `<div id=a></div><div id=b></div>`
	s := compute(t, doc, `
		#a { anchor-name: --n, --m; anchor-scope: all; position-anchor: --n; position-area: span-top left;
		     position-try: most-height flip-block, --nope; position-visibility: no-overflow }
		#b { anchor-name: none; position-anchor: normal; position-visibility: anchor-visible; position-try-fallbacks: none }`)
	a := s.of("a").AnchorPositioning()
	if len(a.Names) != 2 || !a.ScopeAll || a.Default != (PositionAnchor{Kind: AnchorNamed, Name: "--n"}) ||
		a.Area.A.Tracks != TrackStart|TrackCenter || a.Order != TryMostHeight || len(a.Fallbacks) != 2 ||
		a.Fallbacks[0].Tactics[0] != FlipBlock || a.Fallbacks[1].Try != nil || a.Visibility != VisibilityNoOverflow {
		t.Errorf("#a: %+v", a)
	}
	if b := s.of("b"); b.Anchor != nil {
		t.Errorf("#b: %+v, want nil for initial values", b.Anchor)
	}
	if !s.of("b").AnchorPositioning().Visibility.Has(VisibilityAnchorVisible) {
		t.Error("the initial position-visibility is not anchor-visible")
	}
}

// TestPositionTryRule holds @position-try (CSS Anchor Positioning 1, 5.3):
// a rule's accepted properties make a position option on top of the
// element's own declarations, flow-relative ones by its writing mode, a
// property the rule does not accept or one marked !important dropped, the
// last rule of a name winning, and an important declaration of the
// element winning over the rule.
func TestPositionTryRule(t *testing.T) {
	t.Parallel()
	s := compute(t, `<div id=a></div>`, `
		@position-try --r { top: 1px; left: 1px; color: red; width: 9px !important }
		@position-try --r { inset-inline-start: anchor(--x right); margin-top: 3px; position-area: bottom; height: 4px }
		#a { position: absolute; direction: rtl; top: 5px; margin-top: 6px !important; width: 7px; position-try-fallbacks: --r, --missing }`)
	st := s.of("a")
	opts := st.AnchorPositioning().Fallbacks
	if len(opts) != 2 || opts[0].Try == nil || opts[1].Try != nil {
		t.Fatalf("options %+v", opts)
	}
	o := opts[0].Try
	if !o.Inset[Right].Anchored() || o.Inset[Top] != Pts(5*Pt) || o.Margin[Top] != Pts(6*Pt) ||
		o.Width != Pts(7*Pt) || o.Height != Pts(4*Pt) || o.Area.A.Tracks != TrackEnd {
		t.Errorf("option %+v", o)
	}
	if st.Color != s.of("a").Color || st.Top != Pts(5*Pt) {
		t.Error("the option changed the element's own style")
	}
}

// TestAnchorSupports holds @supports for the module: litho answers by
// what it parses (CSS Conditional 3, 6.1), at-rule() by what it reads.
func TestAnchorSupports(t *testing.T) {
	t.Parallel()
	s := compute(t, `<div id=a></div>`, `
		@supports (anchor-name: --a) and (top: anchor(--a bottom)) and (width: anchor-size(width))
		  and (position-area: top) and (justify-self: anchor-center) and at-rule(@position-try) { #a { order: 1 } }
		@supports (width: anchor(--a left)) { #a { order: 2 } }`)
	if got := s.of("a").Order; got != 1 {
		t.Errorf("order %d, want 1", got)
	}
}

// TestPositionOptionComputes holds a position option a layout chose as
// the element's computed values (CSS Anchor Positioning 1, 5.4: applied
// by interleaving): a child inheriting an accepted @position-try property
// takes the option's value, the element keeps its own as its fallback
// base styles, and a style that inherits one from a box with position
// options is reported, so that a converter knows an option changes
// styles.
func TestPositionOptionComputes(t *testing.T) {
	t.Parallel()
	doc := `<div id=p><div id=k></div></div><div id=q><div id=j></div></div>`
	sheet := `@position-try --f { height: 9px; top: 1px }
		#p, #q { position: absolute; height: 5px; position-try-fallbacks: --f }
		#k { height: inherit } #j { color: inherit }`
	root := parseHTML(t, doc)
	r := NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: mustParse(t, sheet), Origin: Author}})
	plain, err := r.Compute(context.Background(), root, ComputeOptions{Hints: HTMLHints})
	if err != nil {
		t.Fatal(err)
	}
	if !plain.InheritsPositionOptions() {
		t.Error("height: inherit under a box with position options is not reported")
	}
	p := byID(root, "p")
	try := plain.Style(p).AnchorPositioning().Fallbacks[0].Try
	c, err := r.Compute(context.Background(), root, ComputeOptions{Hints: HTMLHints, PositionOption: func(n *html.Node) *TryStyle {
		if n == p {
			return try
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Style(byID(root, "k")).Height; got != Pts(9*Pt) {
		t.Errorf("child height %+v, want the option's 9px", got)
	}
	if got := c.Style(p).FallbackBase(); got.Height != Pts(5*Pt) || got.Top.Kind != Auto || got.Anchor.Base != nil {
		t.Errorf("fallback base %+v %+v, want the element's own 5px and auto", got.Height, got.Top)
	}
	if got := c.Style(byID(root, "j")).Height; got.Kind != Auto {
		t.Errorf("unchosen box's child height %+v", got)
	}
	other := compute(t, doc, `#p { position: absolute; position-anchor: --a; position-area: top } #k { height: inherit }
		#q { position: absolute; position-try-fallbacks: flip-start } #j { color: inherit }`)
	if other.c.InheritsPositionOptions() {
		t.Error("no style inherits an accepted property from a box with position options: reported anyway")
	}
}
