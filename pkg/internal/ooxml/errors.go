package ooxml

import "errors"

// ErrUnsupportedPicture reports a picture format the decoders do not
// draw, such as EMF or WMF. The error message names the format.
var ErrUnsupportedPicture = errors.New("ooxml: unsupported picture format")

// ErrNoPart reports a part the package does not hold.
var ErrNoPart = errors.New("ooxml: no such part")
