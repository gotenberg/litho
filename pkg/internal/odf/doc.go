// Package odf reads OpenDocument packages (ODF 1.3, Part 2) and the
// parts of their XML documents (ODF 1.3, Part 3) the OpenDocument
// converters share: the zip package or the flat single-document form,
// the manifest, the content, styles and meta documents as element trees,
// the font-face declarations, the style families with their property
// resolution (default styles, parent chains, the automatic styles of both
// documents), the list styles, the page layouts and the master pages.
//
// What is specific to one document type, text, spreadsheet or
// presentation, stays in its converter. Every read is bounded: the
// package's entries and sizes, the documents' elements and nesting carry
// named caps, and no DTD is processed.
package odf
