package panicerr

import (
	"errors"
	"fmt"
)

// ErrInternalPanic is the sentinel a recovered panic wraps. The public API
// boundary returns it, and so does a worker goroutine that recovers its own
// panic, since the boundary's recover runs on the calling goroutine and never
// sees one raised on another. The document package re-exports it as
// document.ErrInternalPanic, and pkg/pdf as pdf.ErrInternalPanic: the three
// names hold one value, so [errors.Is] matches across them.
var ErrInternalPanic = errors.New("internal panic recovered while processing the document")

// Error is a panic a worker goroutine recovered. It wraps [ErrInternalPanic]
// and keeps the recovered value and the worker's stack, so the caller holding
// the document logger logs them: a package below the document has no logger
// of its own.
type Error struct {
	// Where names the worker that panicked.
	Where string
	// Value is what recover returned.
	Value any
	// Stack is the worker's stack at the recover, from runtime/debug.Stack.
	Stack []byte
}

// Error returns the sentinel's message, the worker and the recovered value.
// It leaves the stack out: a logger reads it from the Stack field.
func (e *Error) Error() string {
	return fmt.Sprintf("%v in %s: %v", ErrInternalPanic, e.Where, e.Value)
}

// Unwrap returns [ErrInternalPanic].
func (e *Error) Unwrap() error {
	return ErrInternalPanic
}
