package blend

import "errors"

// Sentinel errors for group compositing and soft-mask derivation.
var (
	// ErrTransfer indicates a /TR transfer function that is not one input to
	// one output (ISO 32000-1, Table 144) or that fails to evaluate.
	ErrTransfer = errors.New("blend: invalid transfer function")
	// ErrCoverage indicates a coverage holding fewer values than its
	// rectangle has pixels.
	ErrCoverage = errors.New("blend: coverage smaller than its rectangle")
	// ErrPlanes indicates component planes ([Group.PaintPlanes]) too few
	// for the group's blending colour space, or of unequal sizes.
	ErrPlanes = errors.New("blend: planes do not hold the group's components")
	// ErrNotCMYK indicates a group read as CMYK ([Group.CMYK]) that does
	// not blend in a CMYK space.
	ErrNotCMYK = errors.New("blend: group does not blend in CMYK")
)
