package compliance

import "github.com/gotenberg/litho/pkg/internal/core/document"

// nopDoc returns a minimal Document with a no-op logger for tests that
// don't need a real PDF but call functions that log via doc.Logger.
func nopDoc() *document.Document {
	return &document.Document{Logger: document.NopLogger()}
}
