package varfont

import "errors"

var (
	// ErrInvalid reports variation data that breaks the OpenType
	// specification: a count or an offset past its table, a version litho
	// does not know, a value out of its range.
	ErrInvalid = errors.New("invalid font variation data")

	// ErrUnsupported reports a variable font litho does not instance: CFF2
	// outlines, 'avar' version 2.
	ErrUnsupported = errors.New("unsupported font variations")

	// ErrTooComplex reports a font whose instance would cost more work
	// than [maxInstanceWork] allows.
	ErrTooComplex = errors.New("font variations past the work bound")
)
