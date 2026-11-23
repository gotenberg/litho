package css

import (
	"strings"
	"testing"
)

// TestBackgroundLayers_ListsRepeatToTheImages locks CSS Backgrounds 3,
// 2.2: the number of layers is the number of background-image values,
// a shorter list is repeated and a longer one cut.
func TestBackgroundLayers_ListsRepeatToTheImages(t *testing.T) {
	t.Parallel()
	st := styleOf(t, "background-image: linear-gradient(red, red), url(a.png), none; "+
		"background-repeat: no-repeat, repeat-x; background-position: 1px 2px, 3px 4px, 5px 6px, 7px 8px; "+
		"background-clip: content-box; background-origin: border-box, content-box; "+
		"background-attachment: fixed, scroll; background-blend-mode: multiply, screen; "+
		"background-size: cover, 10px")
	if n := st.BackgroundLayerCount(); n != 3 {
		t.Fatalf("%d layers, want 3", n)
	}
	l0, l1, l2 := st.BackgroundLayer(0), st.BackgroundLayer(1), st.BackgroundLayer(2)
	if l0.Gradient == nil || !strings.HasSuffix(l1.Image, "a.png") || l2.HasImage() {
		t.Errorf("images %+v, %q, %+v", l0.Gradient, l1.Image, l2)
	}
	if l0.Repeat != [2]Repeat{RepeatNoRepeat, RepeatNoRepeat} || l1.Repeat != [2]Repeat{RepeatRepeat, RepeatNoRepeat} || l2.Repeat != l0.Repeat {
		t.Errorf("repeats %v %v %v, want the list of two repeated", l0.Repeat, l1.Repeat, l2.Repeat)
	}
	if !dimEq(l2.Position[0], pts(3.75)) || !dimEq(l2.Position[1], pts(4.5)) {
		t.Errorf("third position %+v, want 5px 6px", l2.Position)
	}
	for i, l := range []BackgroundLayer{l0, l1, l2} {
		if l.Clip != ContentEdge {
			t.Errorf("layer %d clip %v, want content-box", i, l.Clip)
		}
	}
	if l0.Origin != BorderEdge || l1.Origin != ContentEdge || l2.Origin != BorderEdge {
		t.Errorf("origins %v %v %v", l0.Origin, l1.Origin, l2.Origin)
	}
	if l0.Attachment != AttachFixed || l1.Attachment != AttachScroll || l2.Attachment != AttachFixed {
		t.Errorf("attachments %v %v %v", l0.Attachment, l1.Attachment, l2.Attachment)
	}
	if l0.Blend != BlendMultiply || l1.Blend != BlendScreen || l2.Blend != BlendMultiply {
		t.Errorf("blend modes %v %v %v", l0.Blend, l1.Blend, l2.Blend)
	}
	if l0.Size.Kind != SizeCover || !dimEq(l1.Size.Width, pts(7.5)) || l2.Size.Kind != SizeCover {
		t.Errorf("sizes %+v %+v %+v", l0.Size, l1.Size, l2.Size)
	}
}

// TestBackgroundLayers_Shorthand locks CSS Backgrounds 3, 3.10: each
// layer of the background shorthand sets its own entry of every list,
// what a layer leaves out its initial value, and only the final layer
// names the colour.
func TestBackgroundLayers_Shorthand(t *testing.T) {
	t.Parallel()
	st := styleOf(t, "background: url(a.png) right 10px top / 20px no-repeat fixed content-box, "+
		"linear-gradient(red, blue) padding-box border-box green")
	if n := st.BackgroundLayerCount(); n != 2 {
		t.Fatalf("%d layers, want 2", n)
	}
	l0, l1 := st.BackgroundLayer(0), st.BackgroundLayer(1)
	if !strings.HasSuffix(l0.Image, "a.png") || l1.Gradient == nil {
		t.Errorf("images %q and %v", l0.Image, l1.Gradient)
	}
	if l0.Repeat != [2]Repeat{RepeatNoRepeat, RepeatNoRepeat} || l1.Repeat != [2]Repeat{} {
		t.Errorf("repeats %v and %v", l0.Repeat, l1.Repeat)
	}
	if l0.Position[0].Kind != Calc || l1.Position[0].Kind != Percent || l1.Position[0].Value != 0 {
		t.Errorf("positions %+v and %+v", l0.Position, l1.Position)
	}
	if !dimEq(l0.Size.Width, pts(15)) || l1.Size.Width.Kind != Auto || l1.Size.Height.Kind != Auto {
		t.Errorf("sizes %+v and %+v", l0.Size, l1.Size)
	}
	if l0.Origin != ContentEdge || l0.Clip != ContentEdge || l1.Origin != PaddingEdge || l1.Clip != BorderEdge {
		t.Errorf("boxes %v %v and %v %v", l0.Origin, l0.Clip, l1.Origin, l1.Clip)
	}
	if l0.Attachment != AttachFixed || l1.Attachment != AttachScroll {
		t.Errorf("attachments %v and %v", l0.Attachment, l1.Attachment)
	}
	if st.BackgroundColor != (Color{G: 128.0 / 255, A: 1}) {
		t.Errorf("colour %+v, want green", st.BackgroundColor)
	}
	if bad := styleOf(t, "background: red url(a.png), none"); bad.HasBackgroundImage() || bad.BackgroundColor.A != 0 {
		t.Errorf("a colour in a layer other than the final one was accepted")
	}
}

// TestBackgroundLayers_OneLayerHoldsNoLists locks that a background of
// one layer, whatever sets it, keeps BackgroundRest nil: it costs a style
// nothing more.
func TestBackgroundLayers_OneLayerHoldsNoLists(t *testing.T) {
	t.Parallel()
	for _, decls := range []string{
		"background: url(a.png) no-repeat 10px 10px / cover fixed padding-box content-box red",
		"background-image: url(a.png); background-repeat: no-repeat; background-clip: text",
		"background-position: 1px 2px; background-position-x: 3px; background-size: 4px",
		"background-blend-mode: multiply; background-attachment: local; background-origin: border-box",
		"background: url(a.png), none; background: red",
	} {
		if st := styleOf(t, decls); st.BackgroundRest != nil || st.BackgroundLayerCount() != 1 {
			t.Errorf("%s: lists %+v", decls, st.BackgroundRest)
		}
	}
}

// TestBackgroundLayers_CapsTheLayers locks maxBackgroundLayers: a list
// longer than the cap is invalid and leaves the earlier value.
func TestBackgroundLayers_CapsTheLayers(t *testing.T) {
	t.Parallel()
	list := func(n int) string { return strings.TrimSuffix(strings.Repeat("linear-gradient(red, red), ", n), ", ") }
	if st := styleOf(t, "background-image: "+list(maxBackgroundLayers)); st.BackgroundLayerCount() != maxBackgroundLayers {
		t.Errorf("%d layers at the cap, want %d", st.BackgroundLayerCount(), maxBackgroundLayers)
	}
	st := styleOf(t, "background-image: url(a.png); background-image: "+list(maxBackgroundLayers+1))
	if st.BackgroundLayerCount() != 1 || st.BackgroundImage == "" {
		t.Errorf("%d layers past the cap, want the earlier value of one", st.BackgroundLayerCount())
	}
	if st := styleOf(t, "background: "+list(maxBackgroundLayers+1)); st.HasBackgroundImage() {
		t.Errorf("the shorthand took a list past the cap")
	}
}

// TestBackgroundLayers_Inherit locks that an inherited background list
// is the parent's whole list (CSS Cascade 4, 7.3.3), the child's own
// image list still counting the layers.
func TestBackgroundLayers_Inherit(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=p><div id=c></div></div>`,
		`#p { background-image: none, none; background-clip: content-box, padding-box } `+
			`#c { background-image: none, none, none; background-clip: inherit }`)
	st := c.of("c")
	if got := st.BackgroundColorClip(); got != ContentEdge {
		t.Errorf("colour clip %v, want content-box: the inherited list of two repeated to three layers", got)
	}
	if l := st.BackgroundLayer(1); l.Clip != PaddingEdge {
		t.Errorf("second layer clip %v, want padding-box", l.Clip)
	}
}

// TestBackgroundLayers_BlendModeTakesNoPlusModes locks that
// background-blend-mode is a list of <blend-mode> (CSS Compositing 1,
// 9.1): plus-lighter and plus-darker, which Compositing 2 gives
// mix-blend-mode alone and which no PDF blend mode draws (ISO 32000-1,
// 11.3.5), make the list invalid.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 answers @supports (background-blend-mode:
//     plus-lighter) and (background-blend-mode: plus-darker) false,
//     (background-blend-mode: multiply, screen) and (mix-blend-mode:
//     plus-lighter) true.
func TestBackgroundLayers_BlendModeTakesNoPlusModes(t *testing.T) {
	t.Parallel()
	for _, m := range []string{"plus-lighter", "plus-darker", "multiply, plus-lighter"} {
		if st := styleOf(t, "background-blend-mode: screen; background-blend-mode: "+m); st.BackgroundBlendMode != BlendScreen || st.BackgroundRest != nil {
			t.Errorf("%s: blend mode %v, want the earlier screen", m, st.BackgroundBlendMode)
		}
	}
}

// TestBackground_ShorthandTextClip checks the text keyword of the
// background shorthand (CSS Backgrounds 4: a layer takes <visual-box> ||
// <bg-clip>, and text is a <bg-clip>): it sets the clip alone, a box
// beside it the origin, and a second box beside it is invalid.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 paints the gradient of background:
//     linear-gradient(...) text through the text alone, its origin the
//     padding box, and with content-box text from the content box.
func TestBackground_ShorthandTextClip(t *testing.T) {
	t.Parallel()
	if s := styleOf(t, "background: linear-gradient(red, blue) text"); s.BackgroundClip != TextEdge || s.BackgroundOrigin != PaddingEdge || s.BackgroundGradient == nil {
		t.Errorf("text: clip %v, origin %v", s.BackgroundClip, s.BackgroundOrigin)
	}
	if s := styleOf(t, "background: content-box text red"); s.BackgroundClip != TextEdge || s.BackgroundOrigin != ContentEdge {
		t.Errorf("content-box text: clip %v, origin %v", s.BackgroundClip, s.BackgroundOrigin)
	}
	if s := styleOf(t, "background: text border-box"); s.BackgroundClip != TextEdge || s.BackgroundOrigin != BorderEdge {
		t.Errorf("text border-box: clip %v, origin %v", s.BackgroundClip, s.BackgroundOrigin)
	}
	for _, v := range []string{"content-box padding-box text", "text text", "text content-box padding-box"} {
		if s := styleOf(t, "background: "+v); s.BackgroundClip == TextEdge {
			t.Errorf("%s: parsed", v)
		}
	}
}
