package page

import "errors"

// ErrTooLarge reports a render past a budget: a page whose pixels at the
// requested resolution exceed the render pixel budget, the clips one render
// keeps past their share of it, or more operators than one render runs.
// pkg/pdf re-exports it as pdf.ErrRenderTooLarge.
var ErrTooLarge = errors.New("page render exceeds its budget")

// ErrInvalidOptions reports a resolution that is not positive and finite, or
// a CMYK device profile that is not one ([Options.CMYKProfile]). pkg/pdf
// re-exports it as pdf.ErrInvalidRenderOptions.
var ErrInvalidOptions = errors.New("invalid render options")
