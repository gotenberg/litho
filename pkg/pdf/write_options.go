package pdf

import "log/slog"

// WriteOption configures the behavior of [Writer.WriteTo].
type WriteOption func(*writeOpts)

type writeOpts struct {
	fullRewrite bool
	version     string
	xrefStream  bool
	logger      *slog.Logger
}

// WithFullRewrite forces a clean rewrite of the entire file instead of an
// incremental update. Required for operations that touch every object
// (encrypt, decrypt, flatten).
func WithFullRewrite() WriteOption {
	return func(o *writeOpts) {
		o.fullRewrite = true
	}
}

// WithVersion sets the PDF version in the output header (e.g., "2.0").
// Only applies to full rewrites.
func WithVersion(version string) WriteOption {
	return func(o *writeOpts) {
		o.version = version
	}
}

// WithXRefStream forces cross-reference stream format (PDF 1.5+) instead
// of a traditional xref table.
func WithXRefStream() WriteOption {
	return func(o *writeOpts) {
		o.xrefStream = true
	}
}

// WithWriteLogger injects a structured logger for write-phase diagnostics.
func WithWriteLogger(logger *slog.Logger) WriteOption {
	return func(o *writeOpts) {
		o.logger = logger
	}
}
