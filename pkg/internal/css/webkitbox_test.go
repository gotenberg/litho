package css

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestLegacyBox_ContainerIsAFlexContainer lays display: -webkit-box out
// as Chromium does: a flex container that never wraps, its axis from
// -webkit-box-orient and -webkit-box-direction, -webkit-box-pack and
// -webkit-box-align for justify-content and align-items, whose own values
// and flex-direction and flex-wrap it ignores; a vertical box that clamps
// its lines stays a block container.
func TestLegacyBox_ContainerIsAFlexContainer(t *testing.T) {
	t.Parallel()
	c := compute(t, `<!DOCTYPE html><div id=a></div><span id=b></span><div id=c></div><div id=d></div><div id=e></div>`, `
#a { display: -webkit-box; -webkit-box-pack: justify; -webkit-box-align: center; justify-content: end; align-items: start; flex-wrap: wrap; flex-direction: column }
#b { display: -webkit-inline-box; -webkit-box-direction: reverse }
#c { display: -webkit-box; -webkit-box-orient: vertical; -webkit-box-direction: reverse; -webkit-box-pack: end; -webkit-box-align: baseline }
#d { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2 }
#e { display: -webkit-box; -webkit-line-clamp: 2 }`)
	for _, tc := range []struct {
		id      string
		display Display
		dir     FlexDirection
		justify BoxAlign
		align   BoxAlign
	}{
		{"a", Flex, FlexRow, BoxSpaceBetween, BoxCenter},
		{"b", InlineFlex, FlexRowReverse, BoxFlexStart, BoxStretch},
		{"c", Flex, FlexColumnReverse, BoxFlexEnd, BoxBaseline},
		{"e", Flex, FlexRow, BoxFlexStart, BoxStretch},
	} {
		st := c.of(tc.id)
		if st.Display != tc.display || st.FlexDirection != tc.dir || st.FlexWrap != NoWrap || st.JustifyContent != tc.justify || st.AlignItems != tc.align {
			t.Errorf("%s: display %v direction %v wrap %v justify %v align %v, want %v %v nowrap %v %v",
				tc.id, st.Display, st.FlexDirection, st.FlexWrap, st.JustifyContent, st.AlignItems, tc.display, tc.dir, tc.justify, tc.align)
		}
	}
	if d := c.of("d"); d.Display != Block || d.Lines() != 2 {
		t.Errorf("d: display %v, %d lines, want a block clamped at 2", d.Display, d.Lines())
	}
	if ws := c.c.Warnings(); hasWarning(ws, warn.CSSUnsupported, "") {
		t.Errorf("warnings %v, want none", ws)
	}
}

// TestLegacyBox_ItemsTakeTheBoxFlex gives the items of a -webkit-box
// their flex factors and order from -webkit-box-flex and
// -webkit-box-ordinal-group, as Chromium does: one factor grows and
// shrinks, a negative one is 0, the ordinal group is 1 by default, and
// flex-grow, flex-shrink, order and align-self change nothing while
// flex-basis applies; an auto basis of an item of auto width in a
// horizontal box is fit-content, and no automatic minimum size holds an
// item.
func TestLegacyBox_ItemsTakeTheBoxFlex(t *testing.T) {
	t.Parallel()
	c := compute(t, `<!DOCTYPE html><div id=h><p id=a></p><p id=b></p><p id=c></p><p id=d></p></div>`+
		`<div id=v><p id=e></p></div><div id=f><p id=g></p></div>`, `
#h { display: -webkit-box } #v { display: -webkit-box; -webkit-box-orient: vertical } #f { display: flex }
#a { -webkit-box-flex: 2; -webkit-box-ordinal-group: 3; flex-grow: 5; order: 7; align-self: center }
#b { -webkit-box-flex: -1; width: 50px }
#c { flex: 1 }
#e { -webkit-box-flex: 1 }
#g { -webkit-box-flex: 1; -webkit-box-ordinal-group: 2 }`)
	for _, tc := range []struct {
		id           string
		grow, shrink float64
		order        int
		basis        DimKind
	}{
		{"a", 2, 2, 3, FitContent},
		{"b", 0, 0, 1, Auto},
		{"c", 0, 0, 1, Percent},
		{"d", 0, 0, 1, FitContent},
		{"e", 1, 1, 1, Auto},
		{"g", 0, 1, 0, Auto},
	} {
		st := c.of(tc.id)
		if st.FlexGrow != tc.grow || st.FlexShrink != tc.shrink || st.Order != tc.order || st.FlexBasis.Kind != tc.basis {
			t.Errorf("%s: grow %v shrink %v order %d basis %v, want %v %v %d %v",
				tc.id, st.FlexGrow, st.FlexShrink, st.Order, st.FlexBasis.Kind, tc.grow, tc.shrink, tc.order, tc.basis)
		}
	}
	if a := c.of("a"); a.AlignSelf != BoxAuto || a.MinWidthAuto || a.MinHeightAuto {
		t.Errorf("a: align-self %v, automatic minimums %v %v, want auto and none", a.AlignSelf, a.MinWidthAuto, a.MinHeightAuto)
	}
	if g := c.of("g"); !g.MinWidthAuto {
		t.Errorf("g: an item of a flex container keeps its automatic minimum size")
	}
}
