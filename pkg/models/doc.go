// Package models declares the data types of litho's public API, each once:
// the values a caller passes to [github.com/gotenberg/litho/pkg/pdf] and the
// values it gets back.
//
// They are plain values: what a [github.com/gotenberg/litho/pkg/pdf.Reader]
// extracts from a document (pages, text, annotations, form fields,
// bookmarks, images, signatures), the reports of a conversion, a
// remediation, a redaction, a render or a pipeline, and the values a caller
// passes in, such as a paper size, resource limits, encryption settings or
// document metadata. They hold no handle, no plugin and no object of the PDF
// parser, so a caller builds and copies them without opening a document. A
// value that holds a handle or a plugin, such as
// [github.com/gotenberg/litho/pkg/pdf.BookmarkOptions], is declared in
// package pdf.
//
// The package imports the standard library alone and does no I/O. The
// interfaces a caller implements are in
// [github.com/gotenberg/litho/pkg/plugins], which takes and returns these
// types.
package models
