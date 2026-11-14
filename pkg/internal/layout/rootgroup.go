package layout

import (
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// rootGroup reports a root element whose opacity, clip-path, mask or
// filter makes the root group of CSS Compositing 1, 9.1 draw through an
// effect: the root's background, which paints the canvas, is part of the
// root group, so the effect applies to the canvas with the rest of the
// root's rendering, and only the page under it shows outside it.
//
// Chromium shows that on screen and prints the canvas untouched by the
// root's effects, its content alone through them: litho keeps the clause,
// which the screen agrees with.
//
// # Corpus evidence
//
//   - wpt: compositing/root-element-opacity, root-element-filter,
//     root-element-background-margin-opacity and
//     css-masking/clip-path/clip-path-document-element: Chromium 156's
//     screenshots draw each as its reference; its PDFs draw the canvas
//     #BBB at full opacity, black under filter: invert(1), and red outside
//     the clip-path.
func rootGroup(rb *box) bool {
	if rb == nil || rb.node == nil || !rb.laidOut {
		return false
	}
	st := rb.st
	return st.Opacity < 1 || !st.ClipPath.None() || len(st.Mask.Images) > 0 || len(st.Filter) > 0
}

// rootGroupOf returns the root group of page k: content, the canvas and
// the root's slice of the flow in page coordinates, drawn through the
// root's filter, then its clip-path, mask and opacity (Filter Effects 1,
// 6; CSS Masking 1, 4), the clip and mask in flow coordinates, which flow
// maps to the page.
func (e *engine) rootGroupOf(rb *box, content []typeset.Fragment, flow typeset.Matrix, k int) *typeset.Group {
	p := &painter{e: e, mono: -1}
	p.dx, p.dy = offset(rb)
	if len(rb.st.Filter) > 0 {
		box := p.borderRect(rb)
		box.X, box.Y = float64(box.X+flow.E), float64(box.Y+flow.F)
		it := &ditem{group: true, fx: &groupFx{opacity: 1, filter: rb.st.Filter, box: box, zoom: 1}}
		content = e.filtered(it, content, nil, k)
	}
	inv := typeset.Matrix{A: 1, D: 1, E: -flow.E, F: -flow.F}
	inner := &typeset.Group{Transform: inv, Opacity: 1, Content: content}
	clips, visible := p.clipPathOf(rb)
	if !visible {
		clips = []*typeset.Path{{Ops: rectOps(typeset.Rect{}), Fill: &typeset.Fill{}}}
	}
	for i := len(clips) - 1; i >= 1; i-- {
		inner = &typeset.Group{Transform: typeset.Identity, Clip: clips[i], Opacity: 1, Content: []typeset.Fragment{inner}}
	}
	op := min(max(clamp(rb.st.Opacity), 0), 1)
	g := &typeset.Group{Transform: flow, Opacity: op, Content: []typeset.Fragment{inner}}
	if len(clips) > 0 {
		g.Clip = clips[0]
	}
	if mask := p.maskOf(rb); mask != nil || op < 1 {
		g.Transparency = &typeset.TransparencyGroup{Isolated: true, Mask: mask}
	}
	return g
}
