package css

// InheritsAlike reports whether a and b hold the same values of every
// inherited property (CSS Cascade 4, 3.3): what a box's children take
// from it. The properties litho declares beyond CSS (the Office ones)
// do not count.
func InheritsAlike(a, b *Style) bool {
	if a == b {
		return true
	}
	x, y := Initial(), Initial()
	for i := range props {
		if p := &props[i]; p.inherited && p.copyFrom != nil {
			p.copyFrom(x, a)
			p.copyFrom(y, b)
		}
	}
	return equalStyle(x, y)
}
