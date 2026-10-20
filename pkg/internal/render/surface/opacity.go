package surface

// Opacity is a constant alpha, the CA or ca of a graphics state (ISO
// 32000-1, 11.6.4.4), in [0, 1]. Its zero value is opaque, the initial value
// Table 52 gives both, so a paint whose caller leaves it unset paints opaque
// rather than nothing. [NewOpacity] sets another value.
type Opacity struct {
	// transparency is 1 - alpha, so the zero value reads as alpha 1.
	transparency float32
}

// Opaque is the opacity of 1, the zero value.
var Opaque = Opacity{}

// NewOpacity returns the opacity a, clamped to [0, 1]; NaN reads as 0, as a
// constant alpha that is no number paints nothing.
func NewOpacity(a float64) Opacity {
	switch {
	case !(a > 0):
		return Opacity{transparency: 1}
	case a >= 1:
		return Opaque
	}
	return Opacity{transparency: 1 - float32(a)}
}

// Alpha returns the opacity as an alpha in [0, 1].
func (o Opacity) Alpha() float32 {
	return 1 - o.transparency
}

// Transparent reports whether the opacity paints nothing.
func (o Opacity) Transparent() bool {
	return !(o.Alpha() > 0)
}
