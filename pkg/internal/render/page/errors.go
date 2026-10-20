package page

import "errors"

// ErrTooLarge reports a render past a budget: a page whose pixels at the
// requested resolution exceed the render pixel budget, the clips one render
// keeps past their share of it, or more operators than one render runs.
var ErrTooLarge = errors.New("render exceeds its budget")

// ErrInvalidOptions reports a resolution that is not positive and finite.
var ErrInvalidOptions = errors.New("invalid render options")
