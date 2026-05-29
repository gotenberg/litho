package pdf

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// recoverToError converts an unexpected panic in a public entry point
// into a wrapped [ErrInternalPanic], logged at Error with the recovered
// value and a stack trace. Internal code must not panic; this boundary
// is defense-in-depth so a crafted or corrupt document cannot crash the
// host process. It is deferred at the top of public methods that process
// document bytes:
//
//	func (r *Reader) ExtractText(...) (_ string, err error) {
//		defer recoverToError(r.logger(), "Reader.ExtractText", &err)
//		...
//	}
//
// logger may be nil, in which case the panic is still converted to an
// error but not logged. errp must point at the method's named error
// return; the conversion only fires when no error is already in flight.
func recoverToError(logger *slog.Logger, name string, errp *error) {
	rec := recover()
	if rec == nil {
		return
	}
	if logger != nil {
		logger.Error("recovered panic at public API boundary",
			slog.String("entry", name),
			slog.Any("panic", rec),
			slog.String("stack", string(debug.Stack())),
		)
	}
	if errp != nil && *errp == nil {
		*errp = fmt.Errorf("%w in %s: %v", ErrInternalPanic, name, rec)
	}
}

// logger returns the document logger for panic-boundary reporting. It is
// never nil for a constructed Reader.
func (r *Reader) logger() *slog.Logger {
	if r == nil || r.doc == nil {
		return nil
	}
	return r.doc.Logger
}

// logger returns the backing document's logger for panic-boundary
// reporting on the Writer.
func (w *Writer) logger() *slog.Logger {
	if w == nil || w.reader == nil {
		return nil
	}
	return w.reader.logger()
}
