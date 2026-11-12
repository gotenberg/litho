package docx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestVMLHorizontalRule checks Word's horizontal lines: a width of 0 takes
// o:hrpct of the column, 100% without one; a shaded line draws its bar
// and two edges, an unshaded one its bar alone.
func TestVMLHorizontalRule(t *testing.T) {
	t.Parallel()
	hr := func(attrs string) *layout.Node {
		t.Helper()
		body := `<w:p><w:r><w:pict><v:rect style="width:0;height:4pt" o:hr="t" fillcolor="red" stroked="f" ` + attrs + `/></w:pict></w:r></w:p>`
		pics := find(build(t, body, nil).root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
		if len(pics) != 1 {
			t.Fatalf("%d replaced boxes", len(pics))
		}
		return pics[0]
	}
	full := hr("")
	if full.Style.Width != (css.Dimension{Kind: css.Percent, Value: 1}) || full.Style.Height != css.Pts(4) {
		t.Errorf("standard line %v by %v, want the column by 4 points", full.Style.Width, full.Style.Height)
	}
	if frags := full.Picture.Vector(t.Context(), typeset.Rect{Width: 100, Height: 4}); len(frags) != 3 || frags[0].(*typeset.Path).Fill.Color != (typeset.Color{R: 1, A: 1}) {
		t.Errorf("shaded line drew %d fragments", len(frags))
	}
	if half := hr(`o:hrpct="500"`); half.Style.Width != (css.Dimension{Kind: css.Percent, Value: 0.5}) {
		t.Errorf("hrpct 500 is %v wide", half.Style.Width)
	}
	if flat := hr(`o:hrnoshade="t"`); len(flat.Picture.Vector(t.Context(), typeset.Rect{Width: 100, Height: 4})) != 1 {
		t.Error("an unshaded line drew its edges")
	}
}
