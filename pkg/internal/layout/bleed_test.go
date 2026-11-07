package layout

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestBleed_Draw(t *testing.T) {
	t.Parallel()
	red2 := sty(func(s *css.Style) { s.BackgroundColor = red })
	tests := []struct {
		name string
		pb   PageBox
		want string
	}{
		{"background over the bleed", PageBox{Width: px(2), Height: px(2), Bleed: px(1), Background: red2}, `
			RRRR
			RRRR
			RRRR
			RRRR`},
		{"crop marks", PageBox{Width: px(4), Height: px(4), Bleed: px(1), Marks: css.MarksCrop}, `
			KK__KK
			K____K
			______
			______
			K____K
			KK__KK`},
		{"cross marks", PageBox{Width: px(4), Height: px(4), Bleed: px(1), Marks: css.MarksCross}, `
			__KK__
			______
			K____K
			K____K
			______
			__KK__`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertPixels(t, run(t, doc(), tt.pb), 1, tt.want)
		})
	}
}

func TestBleed_PageBoxes(t *testing.T) {
	t.Parallel()
	res := run(t, doc(el(sty(height(1), func(s *css.Style) { s.BackgroundColor = blue }))), PageBox{Width: px(4), Height: px(4), Bleed: px(1), Margin: [4]float64{px(1), 0, 0, 0}})
	pg := res.Pages[0]
	if pg.Width != px(6) || pg.Height != px(6) {
		t.Fatalf("sheet %v by %v", pg.Width, pg.Height)
	}
	if want := (compose.Rect{X: px(1), Y: px(1), Width: px(4), Height: px(4)}); pg.TrimBox == nil || *pg.TrimBox != want {
		t.Errorf("TrimBox %+v, want %+v", pg.TrimBox, want)
	}
	if want := (compose.Rect{Width: px(6), Height: px(6)}); pg.BleedBox == nil || *pg.BleedBox != want {
		t.Errorf("BleedBox %+v, want %+v", pg.BleedBox, want)
	}
	assertPixels(t, res, 1, `
		______
		______
		_BBBB_
		______
		______
		______`)
}

func TestBleed_Invalid(t *testing.T) {
	t.Parallel()
	for _, b := range []float64{-1, 8000} {
		if _, err := Layout(context.Background(), doc(), ahem(t), Options{Page: PageBox{Width: 100, Height: 100, Bleed: b}}); err == nil {
			t.Errorf("bleed %v: no error", b)
		}
	}
}
