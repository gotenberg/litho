package html

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

func TestFieldset_RenderedLegendAndContentBox(t *testing.T) {
	t.Parallel()
	// HTML Standard, 15.3.13: the first legend child that is neither
	// floated nor absolutely positioned is the rendered legend, the first
	// child box; the anonymous fieldset content box holds the rest, the
	// pseudo-elements included.
	for _, tc := range []struct {
		name, src, legend, content string
	}{
		{"first", `<legend>L1</legend>C1`, "L1", "C1"},
		{"after text", `T1<legend>L1</legend>C1`, "L1", "T1C1"},
		{"second legend", `<legend>L1</legend><legend>L2</legend>`, "L1", "L2"},
		{"floated", `<legend style="float: left">L1</legend><legend>L2</legend>`, "L2", "L1"},
		{"positioned", `<legend style="position: absolute">L1</legend>C1`, "", "L1C1"},
		{"hidden", `<legend style="display: none">L1</legend><legend>L2</legend>`, "L2", ""},
		{"none", `C1`, "", "C1"},
		{"under display: contents", `<div style="display: contents">T1<legend>L1</legend></div>C1`, "L1", "T1C1"},
		{"pseudo-elements", `<style>fieldset::before { content: "B1" } fieldset::after { content: "A1" }</style><legend>L1</legend>`, "L1", "B1A1"},
	} {
		p := prep(t, `<!DOCTYPE html><fieldset id=f>`+tc.src+`</fieldset>`, Options{})
		f := byID(t, p.root, "f")
		if f.Fieldset != layout.FieldsetBox || len(f.Children) == 0 {
			t.Fatalf("%s: fieldset part %v, %d children", tc.name, f.Fieldset, len(f.Children))
		}
		content := f.Children[len(f.Children)-1]
		if content.Fieldset != layout.FieldsetContent {
			t.Fatalf("%s: last child part %v, want the content box", tc.name, content.Fieldset)
		}
		legend := ""
		if len(f.Children) == 2 {
			if f.Children[0].Fieldset != layout.FieldsetLegend {
				t.Errorf("%s: first child part %v, want the legend", tc.name, f.Children[0].Fieldset)
			}
			legend = nodeText(f.Children[0])
		}
		if strings.TrimSpace(legend) != tc.legend {
			t.Errorf("%s: legend %q, want %q", tc.name, legend, tc.legend)
		}
		if got := strings.Join(strings.Fields(nodeText(content)), ""); got != tc.content {
			t.Errorf("%s: content %q, want %q", tc.name, got, tc.content)
		}
	}
}

func TestFieldset_ContentBoxTakesTheLayoutProperties(t *testing.T) {
	t.Parallel()
	// HTML Standard, 15.3.13: the fieldset establishes a block formatting
	// context, an inline block when it is inline-level; its content box
	// is a flex or a grid container as its display says and takes its
	// overflow; a rendered legend is blockified.
	src := `<!DOCTYPE html><fieldset id=f style="display: inline-flex; overflow: hidden; padding: 9px; list-style: inside">` +
		`<legend style="display: inline">L1</legend>C1</fieldset><fieldset id=g style="display: list-item"></fieldset>`
	p := prep(t, src, Options{})
	f := byID(t, p.root, "f")
	if f.Style.Display != css.InlineBlock || f.Style.Overflow != css.OverflowVisible {
		t.Errorf("fieldset display %v, overflow %v; want an inline block, visible", f.Style.Display, f.Style.Overflow)
	}
	if f.Style.Padding[css.Top] != css.Pts(9*css.Pt) {
		t.Errorf("fieldset padding %+v, want its 9px, which the layout gives its content box", f.Style.Padding[css.Top])
	}
	legend, content := f.Children[0], f.Children[1]
	if legend.Style.Display != css.Block {
		t.Errorf("legend display %v, want block", legend.Style.Display)
	}
	if content.Style.Display != css.Flex || content.Style.Overflow != css.OverflowHidden || content.Style.Padding[css.Top] != css.Pts(0) {
		t.Errorf("content box display %v, overflow %v, padding %+v; want flex, hidden, none of its own", content.Style.Display, content.Style.Overflow, content.Style.Padding[css.Top])
	}
	if g := byID(t, p.root, "g"); g.Style.Display != css.FlowRoot || g.Marker != nil {
		t.Errorf("list-item fieldset display %v, marker %v; want a flow root without a marker", g.Style.Display, g.Marker)
	}
}

func TestFieldset_LegendAlignIsATextAlignHint(t *testing.T) {
	t.Parallel()
	// Chromium places a legend by text-align, which its align attribute
	// maps to; an author's text-align overrides the attribute.
	p := prep(t, `<!DOCTYPE html><fieldset><legend id=a align=center>L1</legend></fieldset><fieldset><legend id=b align=right style="text-align: left">L2</legend></fieldset>`, Options{})
	if got := byID(t, p.root, "a").Style.TextAlign; got != css.AlignCenter {
		t.Errorf("align=center: text-align %v, want center", got)
	}
	if got := byID(t, p.root, "b").Style.TextAlign; got != css.AlignLeft {
		t.Errorf("align=right under text-align: left: text-align %v, want left", got)
	}
}
