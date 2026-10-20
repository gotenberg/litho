package colorconv

import "errors"

// Sentinel errors of colour conversion.
var (
	// ErrPaintsNothing reports a Separation or DeviceN space over the
	// colorant None only: painting in it leaves the page unchanged
	// (ISO 32000-1, 8.6.6.4 and 8.6.6.5), so the caller skips the paint.
	ErrPaintsNothing = errors.New("colorconv: colour space paints nothing")

	// ErrPattern reports a Pattern space with no base space: its colour is a
	// pattern, which the caller paints, not a colour to convert
	// (ISO 32000-1, 8.6.6.2).
	ErrPattern = errors.New("colorconv: pattern colour space has no colour")

	// ErrUnresolved reports a colour space the graphics machine could not
	// resolve, such as a missing /ColorSpace resource.
	ErrUnresolved = errors.New("colorconv: unresolved colour space")

	// ErrUnsupported reports a colour space that cannot be converted: an
	// unknown family, an indexed space with no readable colour table, or a
	// tinted space whose tint transform or alternate space cannot be read.
	ErrUnsupported = errors.New("colorconv: unsupported colour space")
)
