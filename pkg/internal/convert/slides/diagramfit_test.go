package slides

import (
	"context"
	"testing"
)

// TestTextFitsCountsTheBulletPastTheIndent measures a bulleted paragraph
// whose bullet is wider than its hanging indent: the bullet pushes the
// first line's text past it as the slide draws it, so the word needs that
// much more room to fit unbroken.
func TestTextFitsCountsTheBulletPastTheIndent(t *testing.T) {
	fonts := pinnedFonts(t)
	shapeOf := func(w float64, bullet *Bullet) *Shape {
		p := para("second", arial(50))
		p.MarginLeft, p.Indent, p.Bullet = 22.5, -22.5, bullet
		s := box(0, 0, w, 200, p)
		s.Geometry = nil
		return s
	}
	fits := func(w float64, bullet *Bullet) bool {
		return TextFits(context.Background(), fonts, shapeOf(w, bullet))
	}
	// The narrowest whole width the word fits with a bullet that stays
	// within its indent.
	narrow := &Bullet{Kind: BulletChar, Char: "•"}
	w0 := 1.0
	for ; w0 < 1000 && !fits(w0, narrow); w0++ {
	}
	// A 100-point bullet, 35 points wide in Arial, reaches 12.5 points past
	// the 22.5-point indent.
	wide := &Bullet{Kind: BulletChar, Char: "•", Points: 100}
	if fits(w0, wide) {
		t.Errorf("a word that fills %v points fits beside a bullet 12.5 points past its indent", w0)
	}
	if !fits(w0+13, wide) {
		t.Errorf("a word that fills %v points does not fit %v points beside a bullet 12.5 points past its indent", w0, w0+13)
	}
}
