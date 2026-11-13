package css

import (
	"strings"
	"testing"
)

func TestMask_Longhands(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "mask-image: url(a.png), linear-gradient(red, blue), none; mask-mode: luminance, alpha; mask-repeat: no-repeat;"+
		"mask-position: right 10% bottom 0%; mask-size: 10px 20%, contain; mask-origin: content-box; mask-clip: no-clip, padding-box; mask-composite: intersect")
	l := s.MaskLayers()
	if len(l) != 3 {
		t.Fatalf("%d layers", len(l))
	}
	if !strings.HasSuffix(l[0].Image, "a.png") || l[1].Gradient == nil || l[2].Image != "" || l[2].Gradient != nil {
		t.Errorf("images %+v", l)
	}
	if l[0].Mode != MaskLuminance || l[1].Mode != MaskAlpha || l[2].Mode != MaskLuminance {
		t.Errorf("modes repeat cyclically: %d %d %d", l[0].Mode, l[1].Mode, l[2].Mode)
	}
	if l[1].Repeat != [2]Repeat{RepeatNoRepeat, RepeatNoRepeat} || l[0].Position != [2]Dimension{pct(0.9), pct(1)} {
		t.Errorf("repeat %v position %v", l[1].Repeat, l[0].Position)
	}
	if l[0].Size.Width != pts(7.5) || l[0].Size.Height != pct(0.2) || l[1].Size.Kind != SizeContain || l[2].Size.Width != pts(7.5) {
		t.Errorf("sizes %+v %+v %+v", l[0].Size, l[1].Size, l[2].Size)
	}
	if l[2].Origin != GeoContent || !l[0].NoClip || l[1].NoClip || l[1].Clip != GeoPadding || l[2].Composite != CompositeIntersect {
		t.Errorf("boxes and composite %+v", l)
	}
}

func TestMask_Initial(t *testing.T) {
	t.Parallel()
	if l := styleOf(t, "").MaskLayers(); l != nil {
		t.Errorf("no mask: %+v", l)
	}
	if l := styleOf(t, "mask-image: url(a.png); mask-image: none").MaskLayers(); l != nil {
		t.Errorf("mask-image none: %+v", l)
	}
	l := styleOf(t, "mask-image: linear-gradient(red, blue)").MaskLayers()
	if len(l) != 1 || l[0].Mode != MaskMatchSource || l[0].Repeat != [2]Repeat{RepeatRepeat, RepeatRepeat} ||
		l[0].Position != [2]Dimension{pct(0), pct(0)} || l[0].Size.Width.Kind != Auto || l[0].Origin != GeoBorder || l[0].Clip != GeoBorder || l[0].NoClip {
		t.Errorf("initial layer %+v", l)
	}
}

func TestMask_Shorthand(t *testing.T) {
	t.Parallel()
	l := styleOf(t, "mask: url(#m) center / 10px no-repeat content-box padding-box exclude luminance, linear-gradient(red, blue) no-clip").MaskLayers()
	if len(l) != 2 {
		t.Fatalf("%d layers", len(l))
	}
	a, b := l[0], l[1]
	if !strings.HasSuffix(a.Image, "#m") || a.Position != [2]Dimension{pct(0.5), pct(0.5)} || a.Size.Width != pts(7.5) || a.Size.Height.Kind != Auto ||
		a.Repeat != [2]Repeat{RepeatNoRepeat, RepeatNoRepeat} || a.Origin != GeoContent || a.Clip != GeoPadding || a.Composite != CompositeExclude || a.Mode != MaskLuminance {
		t.Errorf("first layer %+v", a)
	}
	if b.Gradient == nil || !b.NoClip || b.Origin != GeoBorder || b.Composite != CompositeAdd || b.Mode != MaskMatchSource {
		t.Errorf("second layer %+v", b)
	}
	if l := styleOf(t, "mask: url(#m); mask: none").MaskLayers(); l != nil {
		t.Errorf("mask none: %+v", l)
	}
	if l := styleOf(t, "-webkit-mask-image: linear-gradient(red, blue)").MaskLayers(); len(l) != 1 {
		t.Errorf("legacy alias: %+v", l)
	}
	for _, v := range []string{"url(a) url(b)", "center / foo", "add add", "luminance alpha", "margin-box"} {
		if l := styleOf(t, "mask: url(#k); mask: "+v).MaskLayers(); len(l) != 1 || !strings.HasSuffix(l[0].Image, "#k") {
			t.Errorf("%q accepted: %+v", v, l)
		}
	}
}

// TestMask_InitialListsComputeAsInitial locks that a mask longhand written
// as its initial value computes as the initial keyword does: to no list.
func TestMask_InitialListsComputeAsInitial(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "mask-mode: match-source; mask-repeat: repeat; mask-position: 0% 0%; mask-size: auto;"+
		"mask-origin: border-box; mask-clip: border-box; mask-composite: add")
	if m := s.Mask; m.Modes != nil || m.Repeats != nil || m.Positions != nil || m.Sizes != nil || m.Origins != nil || m.Clips != nil || m.Composites != nil {
		t.Errorf("%+v", m)
	}
}
