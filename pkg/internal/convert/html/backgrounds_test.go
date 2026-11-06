package html

import (
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
