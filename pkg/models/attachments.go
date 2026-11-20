package models

import (
	"context"
	"io"
)

// EmbeddedFile represents a file embedded within a PDF document.
// Content is accessed lazily: call Open to obtain an [io.ReadCloser] that
// decodes the stream through the PDF filter pipeline on read. The caller
// must close the returned reader.
// Concurrent reads of a value are safe; a write needs synchronization.
// Open may be called from several goroutines at once, and alongside
// other reads of the Reader that returned the value, while that Reader stays
// open.
type EmbeddedFile struct {
	// Name is the file name the file specification's /UF or /F entry holds,
	// as the PDF holds it: it may carry a path, such as "../evil.png". It is
	// "embedded" when the specification has neither.
	Name string `json:"name"`
	// MIMEType is the stream's /Subtype name, such as application/pdf, or
	// empty when the stream has none.
	MIMEType string `json:"mimeType,omitempty"`
	// Relationship is the file specification's /AFRelationship, such as Source,
	// Data, Alternative, Supplement or Unspecified (ISO 32000-2, Table 43), or
	// empty when the specification has none.
	Relationship string `json:"relationship,omitempty"`
	// Open reads the file's decoded content. Each call returns a new reader.
	Open func(ctx context.Context) (io.ReadCloser, error) `json:"-"`
}
