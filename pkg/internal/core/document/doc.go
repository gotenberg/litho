// Package document provides the high-level PDF document model. It opens a PDF
// from an io.ReaderAt, builds the cross-reference index, resolves indirect
// objects with caching, walks the page tree, accesses the catalog, initializes
// encryption handlers, and supports linearized (fast web view) documents.
package document
