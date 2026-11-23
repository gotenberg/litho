package css

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestScope_RootsLimitsAndProximity applies @scope rules (CSS Cascade 6,
// 2.5) as Chromium 156 printed the same page: a rule matches only the
// elements in scope, from a root to its limits, which are descendants of
// the root; a selector without :scope or & is relative to the root, &
// stands for :scope; declarations directly in the block style the root;
// a nested rule's roots lie in its parent's scope; scope proximity orders
// declarations of one specificity, the nearer root winning and any root
// beating no scope, after specificity and before order of appearance
// (6.6).
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints each element of this test in the
//     colour the table states, read by getComputedStyle into its page.
func TestScope_RootsLimitsAndProximity(t *testing.T) {
	t.Parallel()
	c := compute(t, `<!DOCTYPE html>
<div class=c1><p id=p1>p1 in</p></div><p id=p1o>p1 out</p>
<div class=light><div class=dark><p class=t2 id=p2a>dark inner</p></div></div>
<div class=dark><div class=light><p class=t2 id=p2b>light inner</p></div></div>
<div class=a3><div class=b3><p class=x3 id=p3>spec vs prox</p></div></div>
<div class=a4><p class=t4 id=p4>scoped vs unscoped later</p></div>
<div class=c5><p id=p5a>c5 out of limit</p><div class=content><p id=p5b>c5 in limit</p></div></div>
<div class=c6 id=p6><div class=c6 id=p6b>nested c6</div></div>
<div class=c8><p id=p8a>c8 child</p><div><p id=p8b>c8 grandchild</p></div></div>
<div class=c9 id=p9>direct decls</div>
<div class=b10><p id=p10a>b10 outside a10</p></div><div class=a10><div class=b10><p id=p10b>b10 inside a10</p></div></div>
<div class=c11><div class=x><p id=p11a>under :scope > .x</p></div><div><div class=x><p id=p11b>under deep .x</p></div></div></div>
<div class=c12><p id=p12>c12 limit self</p></div>
<div class=c13><p id=p13>c13</p></div>
<div id=c14 class=y>c14</div>
<div class=d15><div class=c15><p id=p15>d15 outside root</p></div></div>
<div class=c16><p id=p16a>c16 child</p><div><p id=p16b>c16 grandchild</p></div></div>`, `
@scope (.c1) { p { color: rgb(0,0,100) } }
@scope (.light) { p.t2 { color: rgb(0,0,1) } }
@scope (.dark) { p.t2 { color: rgb(0,0,2) } }
@scope (.a3) { p.x3 { color: rgb(0,0,3) } }
@scope (.b3) { p { color: rgb(0,0,4) } }
@scope (.a4) { p.t4 { color: rgb(0,0,5) } }
p.t4 { color: rgb(0,0,6) }
@scope (.c5) to (.content) { p { color: rgb(0,0,7) } }
@scope (.c6) { :scope { color: rgb(0,0,8) } .c6 { color: rgb(0,0,9) } }
@scope (.c8) { & > p { color: rgb(0,0,10) } }
@scope (.c9) { color: rgb(0,0,11); }
@scope (.a10) { @scope (.b10) { p { color: rgb(0,0,12) } } }
@scope (.c11) to (:scope > .x) { p { color: rgb(0,0,13) } }
@scope (.c12) to (.c12) { p { color: rgb(0,0,14) } }
@scope (.c13) { :scope p { color: rgb(0,0,15) } }
@scope (#c14) { & { color: rgb(0,0,16) } }
@scope (#c14) { :scope.y { color: rgb(0,0,17) } }
@scope (.c15) { .d15 p { color: rgb(0,0,18) } }
@scope (.c16) { > p { color: rgb(0,0,19) } }`)
	for id, want := range map[string]int{
		"p1": 100, "p1o": 0, "p2a": 2, "p2b": 1, "p3": 3, "p4": 5, "p5a": 7, "p5b": 0, "p6": 8, "p6b": 8,
		"p8a": 10, "p8b": 0, "p9": 11, "p10a": 0, "p10b": 12, "p11a": 0, "p11b": 13, "p12": 14, "p13": 15,
		"c14": 17, "p15": 0, "p16a": 19, "p16b": 0,
	} {
		if got := c.of(id).Color; got != (Color{B: float64(want) / 255, A: 1}) {
			t.Errorf("%s: colour %v, want blue %d", id, got, want)
		}
	}
	if ws := c.c.Warnings(); hasWarning(ws, warn.CSSUnsupported, "") {
		t.Errorf("warnings %v, want none", ws)
	}
}

// TestScope_ImplicitRoot scopes an @scope rule without a prelude to the
// parent of the element that holds its style sheet (CSS Cascade 6,
// 2.5.1), as Chromium 156 printed it.
func TestScope_ImplicitRoot(t *testing.T) {
	t.Parallel()
	doc := parseHTML(t, `<!DOCTYPE html><div id=d><style></style><p id=in>in</p></div><p id=out>out</p>`)
	sheet := mustParse(t, `@scope { p { color: rgb(0,0,20) } } @scope { color: rgb(0,0,21) }`)
	r := NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: sheet, Origin: Author, Owner: byTag(doc, "style")}})
	got, err := r.Compute(context.Background(), doc, ComputeOptions{Hints: HTMLHints})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int{"in": 20, "out": 0, "d": 21} {
		if c := got.Style(byID(doc, id)).Color; c != (Color{B: float64(want) / 255, A: 1}) {
			t.Errorf("%s: colour %v, want blue %d", id, c, want)
		}
	}
}

// TestScope_Bounded drops an @scope rule nested past maxScopeNesting and
// one whose prelude does not parse, with a warning.
func TestScope_Bounded(t *testing.T) {
	t.Parallel()
	src := ""
	for range maxScopeNesting + 1 {
		src += "@scope (div) { "
	}
	src += "p { color: red }"
	for range maxScopeNesting + 1 {
		src += " }"
	}
	s := mustParse(t, src+` @scope (.a) junk { p { color: red } } @scope (::before) { p { color: red } }`)
	if len(s.rules) != 0 {
		t.Errorf("%d rules kept, want none", len(s.rules))
	}
	if !hasWarning(s.Warnings(), warn.LimitReached, "@scope") || !hasWarning(s.Warnings(), warn.CSSInvalid, "@scope") {
		t.Errorf("warnings %v, want the nesting cap and the invalid prelude", s.Warnings())
	}
}
