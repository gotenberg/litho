package panicerr

import (
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
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
	// logged records that [Log] has logged the panic, so an error that
	// crosses several layers holding a logger is logged once.
	logged atomic.Bool
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

// Log logs, at Error through logger, the worker panic err carries, with the
// recovered value and the worker's stack, and returns err unchanged. A panic
// is logged once however many layers pass it to Log, and an error carrying
// none, or a nil logger, logs nothing. A layer holding the document logger
// calls it on what a codec below the document returns, since the codec's
// worker has no logger to report its panic with (CONTRIBUTING "Goroutine
// lifecycle").
func Log(logger *slog.Logger, err error) error {
	var pe *Error
	if logger == nil || !errors.As(err, &pe) || pe.logged.Swap(true) {
		return err
	}
	logger.Error("recovered panic in a worker goroutine",
		slog.String("worker", pe.Where),
		slog.Any("panic", pe.Value),
		slog.String("stack", string(pe.Stack)),
	)
	return err
}
