package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestObjectViewBox_ScalesThePartToTheBox(t *testing.T) {
	// CSS Images 5, 4.1: the view box becomes the picture's natural size;
	// the whole picture is drawn scaled as the view box is into the
	// object's rectangle, and clipped to it.
	pic := &Picture{Source: &typeset.ImageSource{Width: 4, Height: 2}, Width: 100, Height: 50}
	vb := &css.Shape{Kind: css.ShapeInset, Inset: [4]css.Dimension{css.Pts(10), {Kind: css.Percent, Value: 0.5}, css.Pts(0), css.Pts(0)}}
	got := viewBoxPicture(pic, vb)
	if got.Width != 50 || got.Height != 40 {
		t.Fatalf("natural size %v by %v, want 50 by 40", got.Width, got.Height)
	}
	f := (&engine{}).pictureFrag(got, typeset.Rect{X: 0, Y: 0, Width: 100, Height: 80}, nil)
	g, ok := f.(*typeset.Group)
	if !ok || g.Clip == nil || len(g.Content) != 1 {
		t.Fatalf("fragment %#v, want a clipped group", f)
	}
	img, ok := g.Content[0].(*typeset.Image)
	if !ok || img.Rect != (typeset.Rect{X: 0, Y: -20, Width: 200, Height: 100}) {
		t.Errorf("picture at %+v, want 0, -20, 200 by 100", g.Content[0])
	}
	if viewBoxPicture(pic, &css.Shape{Kind: css.ShapeInset, Inset: [4]css.Dimension{css.Pts(60), css.Pts(0), css.Pts(0), css.Pts(0)}}) != pic {
		t.Error("an empty view box changed the picture")
	}
}
