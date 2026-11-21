package html

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// anchorHead is the head of the anchor positioning test documents: pages
// of 800 by 400 CSS pixels without margins, positioned boxes of 30 by 20.
const anchorHead = `<style>@page { size: 800px 400px; margin: 0 } html, body { margin: 0 }
.c { position: relative; width: 600px; height: 300px; margin: 40px 0 0 100px }
.a { position: absolute; width: 40px; height: 40px; background: blue }
.p { position: absolute; width: 30px; height: 20px }
.r { background: red } .g { background: lime } .m { background: fuchsia } .k { background: aqua } .y { background: yellow }
</style>`

var anchorColors = map[string]typeset.Color{
	"r": {R: 1, A: 1}, "g": {G: 1, A: 1}, "m": {R: 1, B: 1, A: 1}, "k": {G: 1, B: 1, A: 1}, "y": {R: 1, G: 1, A: 1},
}

// pxBoxes returns, page by page, the bounding box in CSS pixels of the
// fills of each colour of anchorColors, by its class name, "" for none.
func pxBoxes(doc *typeset.Document) []map[string]string {
	out := make([]map[string]string, len(doc.Pages))
	for i, pg := range doc.Pages {
		out[i] = map[string]string{}
		for name, c := range anchorColors {
			x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
			for _, r := range filledOn(pg, c) {
				x0, y0, x1, y1 = min(x0, r.X), min(y0, r.Y), max(x1, r.X+r.Width), max(y1, r.Y+r.Height)
			}
			if x0 <= x1 {
				k := 4.0 / 3
				out[i][name] = fmt.Sprintf("%g,%g+%gx%g", math.Round(x0*k), math.Round(y0*k), math.Round((x1-x0)*k), math.Round((y1-y0)*k))
			}
		}
	}
	return out
}

// filledOn returns the bounding boxes of the paths of pg filled with c,
// group transforms applied.
func filledOn(pg *typeset.Page, c typeset.Color) []typeset.Rect {
	var out []typeset.Rect
	var walk func([]typeset.Fragment, typeset.Matrix)
	walk = func(frags []typeset.Fragment, m typeset.Matrix) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Group:
				t := v.Transform
				if t == (typeset.Matrix{}) {
					t = typeset.Identity
				}
				walk(v.Content, typeset.Matrix{
					A: m.A*t.A + m.C*t.B, B: m.B*t.A + m.D*t.B,
					C: m.A*t.C + m.C*t.D, D: m.B*t.C + m.D*t.D,
					E: m.A*t.E + m.C*t.F + m.E, F: m.B*t.E + m.D*t.F + m.F,
				})
			case *typeset.Path:
				if v.Fill == nil || v.Fill.Color != c {
					continue
				}
				x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
				for _, op := range v.Ops {
					if op.Kind == typeset.ClosePath {
						continue
					}
					p := op.P[0]
					if op.Kind == typeset.CurveTo {
						p = op.P[2]
					}
					x, y := m.A*p.X+m.C*p.Y+m.E, m.B*p.X+m.D*p.Y+m.F
					x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
				}
				out = append(out, typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0})
			}
		}
	}
	walk(pg.Content, typeset.Identity)
	return out
}

// checkAnchored converts body under anchorHead and compares the boxes of
// each colour on each page with want, "page:class" to "x,y+wxh" in CSS
// pixels, "" for a class that draws nothing there.
func checkAnchored(t *testing.T, style, body string, want map[string]string) {
	t.Helper()
	doc := convertDoc(t, anchorHead+"<style>"+style+"</style>"+body, Options{PreferCSSPageSize: true})
	got := pxBoxes(doc)
	for k, w := range want {
		var page int
		var class string
		if _, err := fmt.Sscanf(strings.Replace(k, ":", " ", 1), "%d %s", &page, &class); err != nil {
			t.Fatalf("bad key %q", k)
		}
		g := ""
		if page-1 < len(got) {
			g = got[page-1][class]
		}
		if g != w {
			t.Errorf("page %d, %s: %q, want %q", page, class, g, w)
		}
	}
}

// TestAnchor_FunctionsResolve holds anchor() and anchor-size() (CSS
// Anchor Positioning 1, 3.2 and 4.1): an inset puts its edge of the
// containing block on the anchor's side, a percentage or center between
// the start and end sides, and anchor-size() measures the anchor; a
// function whose anchor is missing takes its fallback, and without one
// its declaration is invalid: the inset is auto.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints these
//     boxes where the test expects them, and so does its screen.
func TestAnchor_FunctionsResolve(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		#a { anchor-name: --a; left: 300px; top: 100px; width: 100px; height: 60px }
		#p1 { left: anchor(--a 25%); top: anchor(--a center); width: anchor-size(--a width); height: calc(anchor-size(--a height) / 3) }
		#p2 { top: anchor(--a bottom); left: anchor(--a right) }
		#p3 { left: anchor(--nope right, 10px); top: anchor(--nope top) }
		#p4 { right: anchor(--a left); bottom: anchor(--a top); margin-right: anchor-size(--a height, 0px) }`,
		`<div class=c><div class=a id=a></div><div class="p r" id=p1></div><div class="p g" id=p2></div>
		<div class="p m" id=p3></div><div class="p k" id=p4></div></div>`,
		map[string]string{
			"1:r": "425,170+100x20", "1:g": "500,200+30x20", "1:m": "110,40+30x20", "1:k": "310,120+30x20",
		})
}

// TestAnchor_PositionAreaAligns holds position-area (CSS Anchor
// Positioning 1, 3.1): the area of the grid the anchor's edges and the
// containing block's draw becomes the containing block, auto insets and
// margins are 0, and normal aligns toward the anchor, centres in the
// centre track and anchor-centres across all three (4.1).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints these
//     boxes where the test expects them, and so does its screen.
func TestAnchor_PositionAreaAligns(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		#a { anchor-name: --a; left: 200px; top: 120px }
		#p1 { position-anchor: --a; position-area: top left }
		#p2 { position-anchor: --a; position-area: span-x-start bottom; width: 20px }
		#p3 { position-anchor: --a; position-area: bottom; justify-self: anchor-center; width: 60px; margin-top: 30px }
		#p4 { position-anchor: --a; position-area: center right; align-self: stretch; height: auto }
		#p5 { position-anchor: --a; position-area: top span-right; margin: 5px }`,
		`<div class=c><div class=a id=a></div><div class="p r" id=p1></div><div class="p g" id=p2></div>
		<div class="p m" id=p3></div><div class="p k" id=p4></div><div class="p y" id=p5></div></div>`,
		map[string]string{
			"1:r": "270,140+30x20", "1:g": "320,200+20x20", "1:m": "290,230+60x20", "1:k": "340,160+30x40", "1:y": "305,135+30x20",
		})
}

// TestAnchor_TryFallbacks holds the position options (CSS Anchor
// Positioning 1, 5): the first option whose margin box fits its
// inset-modified containing block wins, from a try tactic, a
// @position-try rule or a position-area; and the default overflow
// alignment shifts an anchor-centred box back into its containing block
// (4.1).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints these
//     boxes where the test expects them, and so does its screen.
func TestAnchor_TryFallbacks(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		#a { anchor-name: --a; left: 550px; top: 250px }
		#b { anchor-name: --b; left: 5px; top: 20px; width: 20px; height: 20px }
		#p1 { position-anchor: --a; position-area: right; position-try-fallbacks: flip-inline }
		#p2 { position-anchor: --a; position-area: bottom; position-try-fallbacks: --above }
		@position-try --above { position-area: top; margin-bottom: 4px }
		#p3 { position-anchor: --a; position-area: bottom right; position-try: flip-block flip-inline }
		#p4 { position-anchor: --b; position-area: bottom; justify-self: anchor-center; width: 100px }
		#p5 { position-anchor: --a; position-area: left; position-try-fallbacks: flip-start; width: 80px; height: 30px }`,
		`<div class=c><div class=a id=a></div><div class=a id=b></div><div class="p r" id=p1></div>
		<div class="p g" id=p2></div><div class="p m" id=p3></div><div class="p k" id=p4></div><div class="p y" id=p5></div></div>`,
		map[string]string{
			"1:r": "620,300+30x20", "1:g": "655,266+30x20", "1:m": "620,270+30x20", "1:k": "100,80+100x20", "1:y": "570,295+80x30",
		})
}

// TestAnchor_AcceptableAnchors holds the target anchor element (CSS
// Anchor Positioning 1, 2.2): the nearest ancestor of a name, else the
// last box of it in tree order that is laid out before the positioned
// box; a positioned box later in tree order is not, nor a box outside
// the positioned box's containing block; anchor-scope keeps a name to a
// subtree (2.1.1).
func TestAnchor_AcceptableAnchors(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		.n { anchor-name: --n; position: absolute; width: 40px; height: 40px; background: blue }
		#n1 { left: 10px; top: 10px } #n2 { left: 100px; top: 10px }
		#p1, #p2, #p3, #p4 { position-anchor: --n; position-area: bottom span-right }
		#n3 { left: 300px; top: 200px }
		.s { anchor-scope: --n; position: absolute; left: 400px; top: 100px; width: 100px; height: 100px }
		#n4 { left: 0; top: 0 }`,
		`<div class=c><div class=n id=n1></div><div class="p r" id=p1></div><div class=n id=n2></div>
		<div class="p g" id=p2></div><div class=s><div class=n id=n4></div><div class="p m" id=p3></div></div></div>
		<div class=n id=n3></div><div class="p k" id=p4></div>`,
		map[string]string{
			"1:r": "110,90+30x20", "1:g": "200,90+30x20", "1:m": "500,180+30x20", "1:k": "300,240+30x20",
		})
}

// TestAnchor_PositionVisibility holds position-visibility (CSS Anchor
// Positioning 1, 5.5): the initial anchor-visible hides a box whose
// default anchor an overflow clip between them hides; no-overflow hides
// a box that overflows after its options; anchor-valid hides one whose
// default anchor is missing; always hides none.
func TestAnchor_PositionVisibility(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		#c { overflow: hidden; position: absolute; left: 100px; top: 50px; width: 100px; height: 50px }
		#a1 { anchor-name: --a1; position: static; display: block; width: 30px; height: 30px; margin-top: 80px }
		#p1 { position-anchor: --a1; position-area: right; left: 300px }
		#p2 { position-anchor: --a1; position-area: right; position-visibility: always }
		#v { position: relative; left: 300px; top: 150px; width: 200px; height: 100px }
		#a2 { anchor-name: --a2; left: 120px; top: 30px }
		#p3 { position-anchor: --a2; position-area: right; position-visibility: no-overflow; width: 50px }
		#p4 { position-anchor: --nope; position-area: bottom; position-visibility: anchor-valid; left: 10px; top: 300px }
		#p5 { position-anchor: --nope; position-area: bottom; left: 60px; top: 300px }`,
		`<div id=c><div class=a id=a1></div></div><div class="p r" id=p1></div><div class="p g" id=p2></div>
		<div id=v><div class=a id=a2></div><div class="p m" id=p3></div></div><div class="p k" id=p4></div><div class="p y" id=p5></div>`,
		map[string]string{
			"1:r": "", "1:g": "130,135+30x20", "1:m": "", "1:k": "", "1:y": "60,300+30x20",
		})
}

// TestAnchor_AcrossPages holds the page-crossing rule: a box whose
// containing block is in the flow sees its anchor in the flow and follows
// it to its page, earlier or later than its own place in the document,
// and fragments with it; a fixed box sees its anchor on the anchor's page
// and is drawn from that page on.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints each box
//     where the test expects it, the fixed one too.
func TestAnchor_AcrossPages(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		.b { width: 100px; height: 40px; background: blue }
		#a1 { anchor-name: --a1; margin: 300px 0 0 100px }
		#p1 { top: anchor(--a1 top); left: anchor(--a1 right) }
		#a2 { anchor-name: --a2; margin-left: 300px }
		#p2 { top: anchor(--a2 top); left: anchor(--a2 right) }
		#a3 { anchor-name: --a3; height: 300px }
		#p3 { top: anchor(--a3 top); bottom: anchor(--a3 bottom); left: anchor(--a3 right); height: auto }
		#p4 { position: fixed; top: anchor(--a2 top); left: anchor(--a2 left) }`,
		`<div class="p g" id=p2></div><div class=b id=a1></div><div style="height: 900px"></div>
		<div class=b id=a2></div><div class=b id=a3></div><div class="p r" id=p1></div><div class="p m" id=p3></div>
		<div class="p k" id=p4></div>`,
		map[string]string{
			"1:r": "200,300+30x20", "4:g": "400,40+30x20", "4:m": "100,80+30x300",
			"1:k": "", "3:k": "", "4:k": "300,40+30x20",
		})
}

// TestAnchor_PseudoElements holds the anchors of pseudo-elements: a
// ::before or ::after takes its originating element as its implicit
// anchor (CSS Anchor Positioning 1, 2.3.1), match-parent its parent's
// default anchor, and a pseudo-element names an anchor as any box. The
// originating element is no acceptable anchor when it is the pseudo-
// element's containing block (2.2): it is laid out before it, not
// strictly before.
func TestAnchor_PseudoElements(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		.x { margin: 100px 0 0 100px; width: 50px; height: 30px; background: blue }
		.x::after { content: ""; position: absolute; position-area: right; position-anchor: auto; width: 30px; height: 20px; background: red }
		.y2 { anchor-name: --y; position: absolute; left: 300px; top: 200px; width: 50px; height: 30px; background: blue }
		.z { position-anchor: --y }
		.z::before { content: ""; position: absolute; position-anchor: match-parent; position-area: bottom; width: 30px; height: 20px; background: lime }
		.w { position: absolute; left: 500px; top: 50px; width: 40px; height: 40px }
		.w::before { content: ""; position: absolute; anchor-name: --wb; left: 0; top: 50px; width: 10px; height: 10px; background: blue }
		.q { position: absolute; position-anchor: --wb; position-area: right; width: 30px; height: 20px; background: fuchsia }`,
		`<div class=x></div><div class=y2></div><div class=z></div><div class=w></div><div class=q></div>`,
		map[string]string{"1:r": "150,105+30x20", "1:g": "310,230+30x20", "1:m": "510,95+30x20"})
	checkAnchored(t, `
		.x { position: absolute; left: 100px; top: 100px; width: 50px; height: 30px; background: blue }
		.x::after { content: ""; position: absolute; position-area: right; position-anchor: auto; width: 30px; height: 20px; background: red }`,
		`<div class=x></div>`, map[string]string{"1:r": "100,100+30x20"})
}

// TestAnchor_TransformsAndColumns holds the anchor box of an anchor under
// a transform or in columns, seen from a containing block outside them
// (CSS Anchor Positioning 1, 2): the axis-aligned bounding box of what
// the transform makes of it, and of its fragments in the columns.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints both
//     boxes where the test expects them.
func TestAnchor_TransformsAndColumns(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		#t { transform: translate(50px, 20px) rotate(10deg); width: 200px; margin: 40px 0 0 40px }
		#a1 { anchor-name: --a1; width: 100px; height: 40px; margin-left: 20px }
		#p1 { top: anchor(--a1 bottom); left: anchor(--a1 left) }
		#mc { columns: 3; column-gap: 0; width: 600px; margin: 60px 0 0 100px; height: 120px; column-fill: auto }
		#mc div { height: 100px }
		#a2 { anchor-name: --a2; height: 50px !important; width: 50px }
		#p2 { top: anchor(--a2 bottom); left: anchor(--a2 left) }`,
		`<div id=t><div id=a1></div></div><div class="p r" id=p1></div>
		<div id=mc><div></div><div></div><div id=a2></div></div><div class="p g" id=p2></div>`,
		map[string]string{"1:r": "108,103+30x20", "1:g": "300,260+30x20"})
}

// TestAnchor_KeepsItsOptionAcrossLayouts holds the last successful
// position option (CSS Anchor Positioning 1, 5.4.1.1): a document laid
// out again wider, to shrink it to its page, keeps the option its box
// took, which still fits, where the base styles would fit too.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints the box
//     on the anchor's left, its flipped option, on a page that a box past
//     its right edge shrinks.
func TestAnchor_KeepsItsOptionAcrossLayouts(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		#a1 { anchor-name: --a1; left: 760px; top: 20px; width: 20px; height: 20px }
		#p1 { position-anchor: --a1; position-area: right; position-try-fallbacks: flip-inline; width: 40px }
		#w { position: absolute; left: 780px; top: 100px; width: 40px; height: 20px }`,
		`<div class=a id=a1></div><div class="p r" id=p1></div><div class="p g" id=w></div>`,
		map[string]string{"1:r": "702,20+39x20"})
}

// TestAnchor_CenterOutsideAbsolutePositioning holds anchor-center on a box
// that is not absolutely positioned: it aligns as center (CSS Anchor
// Positioning 1, 4.2).
func TestAnchor_CenterOutsideAbsolutePositioning(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		.f { display: flex; height: 100px; width: 200px }
		.f > div { width: 30px; height: 20px }
		#r { align-self: anchor-center }
		.gr { display: grid; grid-template-columns: 200px; height: 60px }
		.gr > div { width: 30px; height: 20px; justify-self: anchor-center }`,
		`<div class=f><div class=r id=r></div></div><div class=gr><div class=g></div></div>`,
		map[string]string{"1:r": "0,40+30x20", "1:g": "85,100+30x20"})
}

// TestAnchor_FallbackStylesInherit holds the position option a box takes
// as its computed values (CSS Anchor Positioning 1, 5.4: the option is
// applied by interleaving, so it changes computed values): a child that
// inherits an accepted @position-try property takes the option's value,
// and a box that keeps its base styles hands down its own.
func TestAnchor_FallbackStylesInherit(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		#a { anchor-name: --a; left: 550px; top: 250px }
		#p1 { position-anchor: --a; position-area: right center; position-try-fallbacks: --f; height: auto }
		#p2 { position-anchor: --a; position-area: bottom center; position-try-fallbacks: --f; height: 10px; top: 0 }
		@position-try --f { position-area: left center; height: 40px }
		#p1 > div, #p2 > div { height: inherit }`,
		`<div class=c><div class=a id=a></div><div class=p id=p1><div class=g></div></div>
		<div class=p id=p2><div class=m></div></div></div>`,
		map[string]string{"1:g": "620,290+30x40", "1:m": "655,330+30x10"})
}

// TestAnchor_GridAreaShiftsInPaddingBox holds the original containing
// block of a box a grid container holds: the grid area its placement
// names is an adjustment of the containing block (CSS Position 3, 2.1.1),
// so a box that overflows its position-area shifts to stay inside the
// container's padding box, not the grid area (CSS Box Alignment 3,
// 4.4.1.2).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 shows a box in a
//     zero-width track left of the grid shifted to the padding box's
//     left edge, and a box right of the anchor, outside its grid area but
//     inside the padding box, where the area puts it.
func TestAnchor_GridAreaShiftsInPaddingBox(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		.gd { display: grid; grid: 60px 60px / 100px 100px; padding: 20px; position: relative; margin: 0 0 10px 100px; width: 200px }
		.gd > .a { position: static; width: auto; height: auto }
		#a1 { grid-area: 1/1/2/2; anchor-name: --a1 }
		#p1 { grid-area: 2/2/3/3; position-anchor: --a1; position-area: bottom left }
		#a2 { grid-area: 2/2/3/3; anchor-name: --a2 }
		#p2 { grid-area: 1/1/2/2; position-anchor: --a2; position-area: top right }`,
		`<div class=gd style="width: 200px"><div class=a id=a1></div><div class="p r" id=p1></div></div>
		<div class=gd style="width: 300px"><div class=a id=a2></div><div class="p g" id=p2></div></div>`,
		map[string]string{"1:r": "100,80+30x20", "1:g": "320,230+30x20"})
}

// TestAnchor_CenterOverflow holds anchor-center's overflow (CSS Anchor
// Positioning 1, 4.2; CSS Box Alignment 3, 4.4): the box is sized in its
// inset-modified containing block and centred on the anchor; one larger
// than the room centred there shifts back into its containing block by
// default, and safe aligns it at the start of that room instead, the
// start of the initial containing block taken from the principal writing
// mode, a body's direction (CSS Writing Modes 3, 8).
//
// # Corpus evidence
//
//   - wpt: css/css-anchor-position/anchor-center-safe and
//     anchor-center-safe-rtl draw their references in Chromium of
//     gotenberg/gotenberg:9.4.1.
func TestAnchor_CenterOverflow(t *testing.T) {
	t.Parallel()
	style := `
		#a { anchor-name: --a; left: 560px; top: 0 }
		#b { anchor-name: --b; left: 560px; top: 100px }
		#p1 { position-anchor: --a; top: anchor(bottom); justify-self: safe anchor-center; width: 100px }
		#p2 { position-anchor: --a; top: calc(anchor(bottom) + 30px); justify-self: anchor-center; width: 100px }
		#p3 { position-anchor: --b; top: anchor(bottom); justify-self: anchor-center; width: auto; height: auto; font-size: 0; line-height: 0 }
		#p3 span { display: inline-block; width: 50px; height: 10px }`
	checkAnchored(t, style,
		`<div class=c><div class=a id=a></div><div class=a id=b></div><div class="p r" id=p1></div>
		<div class="p g" id=p2></div><div class="p k" id=p3><span></span><span></span></div></div>`,
		map[string]string{"1:r": "660,80+100x20", "1:g": "600,110+100x20", "1:k": "600,180+100x10"})
	checkAnchored(t, `body { direction: rtl }
		#f { position: fixed; anchor-name: --f; right: 0; top: 0 }
		#q { position: fixed; position-anchor: --f; top: anchor(bottom); right: 0; justify-self: safe anchor-center; width: 100px }`,
		`<div class=a id=f></div><div class="p g" id=q></div>`,
		map[string]string{"1:g": "700,40+100x20"})
}

// TestAbsoluteSelfAlignment holds the self-alignment of an absolutely
// positioned box whose two insets of an axis are set (CSS Position 3,
// 4.3): the box is aligned in its inset-modified containing block as
// justify-self and align-self say, where CSS 2.1 would put it at the left
// or top inset; normal keeps CSS 2.1's placement.
func TestAbsoluteSelfAlignment(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		#p1 { inset: 0; justify-self: end; align-self: end }
		#p2 { inset: 10px; justify-self: center; align-self: start }
		#p3 { inset: 0; justify-self: normal }
		#p4 { left: 0; right: 0; top: 0; justify-self: end; align-self: end }`,
		`<div class=c><div class="p r" id=p1></div><div class="p g" id=p2></div><div class="p m" id=p3></div>
		<div class="p k" id=p4></div></div>`,
		map[string]string{"1:r": "670,320+30x20", "1:g": "385,50+30x20", "1:m": "100,40+30x20", "1:k": "670,40+30x20"})
}

// TestAnchor_FieldsetPseudoElement holds the implicit anchor of a
// fieldset's ::before (CSS Anchor Positioning 1, 2.3.1): its originating
// element is the fieldset, whose border box the position-area grid takes,
// not the anonymous content box that holds the pseudo-element.
func TestAnchor_FieldsetPseudoElement(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		fieldset { margin: 0; padding: 0; border: 10px solid blue; width: 200px; height: 50px }
		fieldset::before { content: ""; display: block; position: absolute; width: 10px; height: 10px; background: lime;
			position-anchor: auto; position-area: bottom right }`,
		`<fieldset></fieldset>`,
		map[string]string{"1:g": "220,70+10x10"})
}

// TestAnchor_VisibleThroughEscapedClip holds position-visibility:
// anchors-visible (CSS Anchor Positioning 1, 5.5) for an anchor inside an
// absolutely positioned box that escapes a box clipping its overflow:
// that box clips nothing of it (CSS Overflow 3, 3), so the anchor is
// visible and the box shows; an anchor the clip does reach hides it.
func TestAnchor_VisibleThroughEscapedClip(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		.clip { overflow: hidden; height: 10px }
		.esc { position: absolute; top: 50px; left: 0; width: 100px; height: 100px }
		.in { position: relative; top: 50px }
		#a1 { anchor-name: --a1; height: 10px } #a2 { anchor-name: --a2; height: 10px }
		#p1 { position-anchor: --a1; left: anchor(left); top: anchor(bottom) }
		#p2 { position-anchor: --a2; left: anchor(left); top: anchor(bottom) }`,
		`<div class=clip><div class=esc><div id=a1></div></div></div>
		<div class=clip><div class=in><div id=a2></div></div></div>
		<div class="p g" id=p1></div><div class="p r" id=p2></div>`,
		map[string]string{"1:g": "0,60+30x20", "1:r": ""})
}

// TestAnchor_DefaultAnchorInItsContainingBlock holds the default anchor's
// box in the containing block's coordinates (CSS Anchor Positioning 1,
// 2): the offsets of the box that generates the containing block move
// the anchor and the positioned box alike, so they leave the anchor box
// where it is within it.
func TestAnchor_DefaultAnchorInItsContainingBlock(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		.rel { position: relative; top: 30px; width: 200px; height: 100px }
		#z { anchor-name: --z; left: 50px; top: 10px }
		#p1 { position-anchor: --z; top: anchor(bottom); left: anchor(left) }`,
		`<div class=rel><div class=a id=z></div><div class="p g" id=p1></div></div>`,
		map[string]string{"1:g": "50,80+30x20"})
}

// TestAnchor_InlineBoxes holds anchors and inline boxes (CSS Anchor
// Positioning 1, 2 and 2.2): an anchor inside a relatively positioned
// inline box, the containing block of the box positioned from it, is
// laid out before that box, so it is an acceptable anchor element, inside
// a block split out of the inline box too (CSS 2.1, 9.2.1.1); and the
// anchor box of an inline box split around a block holds that block,
// with the anonymous block box that wraps it, as wide as the line.
//
// # Corpus evidence
//
//   - wpt: css/css-anchor-position/anchor-position-inline-005, -006,
//     -007, -008 and position-visibility-anchors-visible-inline-cb draw
//     their references in Chromium of gotenberg/gotenberg:9.4.1.
func TestAnchor_InlineBoxes(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		.line { width: 300px; font-size: 0; line-height: 0 }
		.ib { display: inline-block; height: 10px; width: 50px }
		.rel { position: relative }
		#a { anchor-name: --a; width: 40px }
		#p1 { left: anchor(--a left); top: anchor(--a bottom) }
		#d { anchor-name: --d; width: 20px; height: 10px; margin-left: 100px }
		#p2 { left: anchor(--d left); top: anchor(--d bottom) }
		#b { anchor-name: --b }
		#p3 { position-anchor: --b; top: anchor(top); left: anchor(left); width: anchor-size(width); height: anchor-size(height) }`,
		`<div class=line><span class=rel><span class=ib></span><span class=ib id=a></span><span class="p g" id=p1></span></span></div>
		<div class=line><span class=rel><div><div id=d></div></div><span class="p m" id=p2></span></span></div>
		<div class=line style="margin-top: 50px"><span id=b><div style="width: 50px; height: 30px"></div></span></div>
		<div class="p k" id=p3></div>`,
		map[string]string{"1:g": "50,10+30x20", "1:m": "100,20+30x20", "1:k": "0,70+300x30"})
}

// TestAnchor_ColumnsOfTheContainingBlock holds the anchor box of an anchor
// in the columns of a multi-column container that is itself the
// containing block (CSS Anchor Positioning 1, 2: the bounding box of the
// anchor's fragments): the box is laid out outside the columns (CSS
// Position 3, 3.5.3), so it sees each piece of the anchor where its column
// puts it, an inline anchor's pieces each in their own.
//
// # Corpus evidence
//
//   - wpt: css/css-anchor-position/anchor-position-inline-004,
//     anchor-name-multicol-003 and -004 draw their references in Chromium
//     of gotenberg/gotenberg:9.4.1.
func TestAnchor_ColumnsOfTheContainingBlock(t *testing.T) {
	t.Parallel()
	checkAnchored(t, `
		.mc { position: relative; columns: 2; column-gap: 0; column-fill: auto; width: 200px; height: 40px; font-size: 10px; line-height: 20px; orphans: 1; widows: 1 }
		.ib { display: inline-block; height: 10px }
		#m { anchor-name: --m }
		#p4 { position-anchor: --m; top: anchor(top); left: anchor(left); width: anchor-size(width); height: anchor-size(height) }`,
		`<div class=mc><div style="height: 20px"></div><span class=ib style="width: 50px"></span><span id=m><span class=ib style="width: 50px"></span><span class=ib style="width: 30px"></span></span>
		<div class="p y" id=p4></div></div>`,
		map[string]string{"1:y": "50,5+80x30"})
}

// TestAnchor_FixedBoxShiftsInScrollableArea holds the overflow limit of a
// fixed box whose default anchor scrolls with the document (CSS Box
// Alignment 3, 4.4.1.2): the document's scrollable overflow area, not the
// viewport, so a box that overflows the page but not the root stays
// where its position-area puts it, and one that overflows the root too
// shifts back inside it. A paged document scrolls nowhere: the page the
// anchor is on stands for the viewport.
//
// # Corpus evidence
//
//   - wpt: css/css-anchor-position/position-area-overflow-icb-004-ltr and
//     -rtl draw their references in Chromium of gotenberg/gotenberg:9.4.1,
//     and so does -003, where the root is no larger than the viewport;
//     -006, whose place-self: safe normal Chromium does not parse, a
//     value only the editor's draft of CSS Box Alignment 3 holds, it
//     draws as -004.
func TestAnchor_FixedBoxShiftsInScrollableArea(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ root, height, want string }{
		{"1000px", "100px", "105,340+30x100"},
		{"450px", "200px", "105,250+30x200"},
	} {
		checkAnchored(t, `html { height: `+c.root+` }
			#a { anchor-name: --a; left: 100px; top: 300px }
			#p1 { position: fixed; position-anchor: --a; position-area: bottom; height: `+c.height+` }`,
			`<div class=a id=a></div><div class="p g" id=p1></div>`,
			map[string]string{"1:g": c.want})
	}
}
