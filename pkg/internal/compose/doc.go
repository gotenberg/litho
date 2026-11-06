// Package compose holds the paged document model every converter produces
// and the backend that writes that model as a PDF.
//
// A converter (plain text, HTML, DOCX, ...) lays its input out into a
// [Document]: pages of positioned fragments, links, an outline and an
// optional structure tree. [Write] turns the document into PDF bytes through
// litho's object writer, embedding the fonts the fragments use, so every
// converter shares one backend and one set of guarantees: valid output,
// deterministic bytes, bounded resources.
//
// # Coordinates
//
// Lengths are PDF points, 1/72 of an inch. A page's origin is its top-left
// corner, x grows to the right and y grows downward, as in CSS and in word
// processors. The backend flips to PDF's bottom-left origin when it writes.
//
// # Ownership
//
// A [Document] and everything it points to belong to the converter that
// built it until [Write] returns. [Write] reads the document and never
// mutates it. Fragments shared between pages are allowed: an [ImageSource]
// or a [Face] placed on several pages is written once.
package compose
