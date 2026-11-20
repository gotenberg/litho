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

// AssociatedFile describes an embedded file for PDF/A-3+ compliance with the
// /AFRelationship and document-level /AF array required by Factur-X / ZUGFeRD.
// Concurrent reads of a value are safe; a write needs synchronization.
type AssociatedFile struct {
	// Name is the displayed file name.
	Name string
	// Data is the file content. It is retained, not copied: the file is
	// written when [github.com/gotenberg/litho/pkg/pdf.Writer.WriteTo] drains
	// the queue, so leave the slice untouched until then.
	Data []byte
	// MimeType is the embedded file /Subtype (e.g., "application/xml").
	MimeType string
	// Relationship is the /AFRelationship value. Valid per
	// ISO 32000-2 §7.11.3: Source, Data, Alternative, Supplement,
	// EncryptedPayload, FormData, Schema, Unspecified.
	Relationship string
}
