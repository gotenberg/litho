package blend

import "errors"

// Sentinel errors for group compositing and soft-mask derivation.
var (
	// ErrTransfer indicates a /TR transfer function that is not one input to
	// one output (ISO 32000-1, Table 144) or that fails to evaluate.
	ErrTransfer = errors.New("blend: invalid transfer function")
)
