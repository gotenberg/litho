package html

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

func TestOmitBackgrounds_DropsTheCanvasAlone(t *testing.T) {
	t.Parallel()
	src := `<html style="background: blue"><style>
	div { background: lime } .img { background: url(x.png) } .grad { background: linear-gradient(red, blue) }
	@page { background: yellow }
	</style><body style="background: red url(y.png)">
	<div id=plain>a</div><div id=img class=img></div><div id=grad class=grad></div></body>`
	for _, tt := range []struct {
		name string
		omit bool
	}{{"printed", false}, {"omitted", true}} {
		p := prep(t, src, Options{OmitBackgrounds: tt.omit})
		root, body := p.root, bodyNode(p.root)
		if body == nil {
			t.Fatalf("%s: no body", tt.name)
		}
		for _, n := range []*layout.Node{root, body} {
			st := n.Style
			if has := st.BackgroundColor.A > 0 || st.BackgroundImage != "" || st.BackgroundGradient != nil; has == tt.omit {
				t.Errorf("%s: html or body background %+v %q, kept %v", tt.name, st.BackgroundColor, st.BackgroundImage, has)
			}
			for _, k := range n.Children {
				if k.Kind == layout.Text && k.Style != n.Style {
					t.Errorf("%s: a text node lost its element's style", tt.name)
				}
			}
		}
		if st := byID(t, p.root, "plain").Style; st.BackgroundColor != (css.Color{G: 1, A: 1}) {
			t.Errorf("%s: element background %+v, want kept", tt.name, st.BackgroundColor)
		}
		if st := byID(t, p.root, "img").Style; st.BackgroundImage == "" {
			t.Errorf("%s: element background image dropped", tt.name)
		}
		if st := byID(t, p.root, "grad").Style; st.BackgroundGradient == nil {
			t.Errorf("%s: element gradient dropped", tt.name)
		}
		if p.layout.Page.Background == nil || p.layout.Page.Background.BackgroundColor.A == 0 {
			t.Errorf("%s: page background dropped", tt.name)
		}
	}
}

func TestOmitBackgrounds_LeavesSharedStylesAlone(t *testing.T) {
	t.Parallel()
	st := &css.Style{BackgroundColor: css.Color{R: 1, A: 1}, BackgroundImage: "a.png"}
	out := withoutBackground(st)
	if out == st || out.BackgroundColor.A != 0 || out.BackgroundImage != "" {
		t.Errorf("withoutBackground returned %+v", out)
	}
	if st.BackgroundColor.A != 1 || st.BackgroundImage != "a.png" {
		t.Errorf("the shared style changed: %+v", st)
	}
	bare := &css.Style{}
	if withoutBackground(bare) != bare {
		t.Errorf("a style without a background was copied")
	}
}

// bodyNode returns the child of root built from the body element.
func bodyNode(root *layout.Node) *layout.Node {
	for _, k := range root.Children {
		if k.Kind == layout.Element {
			return k
		}
	}
	return nil
}

func TestOmitBackgrounds_DropsEconomyElements(t *testing.T) {
	t.Parallel()
	// CSS Color Adjust 1, 3.1: an element of print-color-adjust: economy
	// loses its background when backgrounds are not printed; the root's
	// exact, which Gotenberg's route forces, reaches every other element.
	src := `<html style="print-color-adjust: economy"><body>
	<div id=plain style="background: lime">a</div>
	<div id=eco style="print-color-adjust: economy; background: lime">b</div>
	<section style="-webkit-print-color-adjust: economy"><div id=inner style="background: lime">c</div></section></body>`
	for _, tt := range []struct {
		omit bool
		kept map[string]bool
	}{
		{false, map[string]bool{"plain": true, "eco": true, "inner": true}},
		{true, map[string]bool{"plain": true, "eco": false, "inner": false}},
	} {
		p := prep(t, src, Options{OmitBackgrounds: tt.omit})
		for id, want := range tt.kept {
			if got := byID(t, p.root, id).Style.BackgroundColor.A > 0; got != want {
				t.Errorf("omit %v: %s background kept %v, want %v", tt.omit, id, got, want)
			}
		}
	}
}

// TestBackgroundAttribute_SetsTheImage checks the background attribute
// (HTML Standard, 15.3.2 and 15.3.8): on body and on the table elements
// its URL, stripped of white space and resolved against the document's
// base URL, is the background image; an empty value and the attribute on
// any other element set none, and a style sheet's background-image wins.
func TestBackgroundAttribute_SetsTheImage(t *testing.T) {
	t.Parallel()
	p := prep(t, `<!DOCTYPE html><base href="img/"><style>#styled { background-image: url(s.png) }</style>
<body id=body background="b.png"><table id=table background=" t.png "><tr id=tr background="r.png">
<td id=td background="d.png">x<th id=empty background="">y<td id=styled background="d.png">z</table>
<div id=div background="v.png">v</div></body>`, Options{})
	dir, ok := strings.CutSuffix(byID(t, p.root, "styled").Style.BackgroundImage, "img/s.png")
	if !ok {
		t.Fatalf("#styled: background image %q", byID(t, p.root, "styled").Style.BackgroundImage)
	}
	for id, want := range map[string]string{
		"body": "img/b.png", "table": "img/t.png", "tr": "img/r.png", "td": "img/d.png", "empty": "", "div": "",
	} {
		if want != "" {
			want = dir + want
		}
		if got := byID(t, p.root, id).Style.BackgroundImage; got != want {
			t.Errorf("#%s: background image %q, want %q", id, got, want)
		}
	}
}
