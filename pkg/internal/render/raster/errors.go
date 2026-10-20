package raster

import "errors"

var (
	// ErrInvalidPath reports a path holding a coordinate that is not finite,
	// or a flattening tolerance that is not positive.
	ErrInvalidPath = errors.New("invalid path")

	// ErrTooComplex reports a path that flattens to more segments than the
	// rasterizer accepts in one call.
	ErrTooComplex = errors.New("path too complex")

	// ErrSizeMismatch reports a raster size that is not positive, or a clip
	// mask whose size differs from the raster it is used with.
	ErrSizeMismatch = errors.New("raster size mismatch")
)
