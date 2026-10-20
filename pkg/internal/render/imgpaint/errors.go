package imgpaint

import "errors"

// ErrBadMask reports an [Alpha] plane or colour-key description that does
// not describe a raster: a non-positive size, too few values, or ranges that
// do not match the components.
var ErrBadMask = errors.New("bad image mask")

// ErrBadClip reports a clip whose size differs from the canvas it clips.
var ErrBadClip = errors.New("clip and canvas sizes differ")
