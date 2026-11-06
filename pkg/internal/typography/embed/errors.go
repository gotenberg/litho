package embed

import "errors"

// ErrCollection is returned by [Load] for a TrueType or OpenType Collection.
// A collection holds several faces: extract the one to embed first, with
// truetype.ExtractFace.
var ErrCollection = errors.New("font collection, not a single font program")
