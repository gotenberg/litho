package limits

import "errors"

// ErrInvalid is returned by [Limits.Validate] when a budget is negative or
// above its ceiling. pkg/pdf re-exports it as pdf.ErrInvalidLimits.
var ErrInvalid = errors.New("invalid resource limits")
