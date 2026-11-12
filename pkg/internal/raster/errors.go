package raster

import "errors"

// ErrTooLarge is returned for a mask whose size is not positive or
// exceeds [MaxPixels].
var ErrTooLarge = errors.New("raster: mask size out of range")
