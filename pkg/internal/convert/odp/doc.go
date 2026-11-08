// Package odp converts an OpenDocument presentation, an ODP or flat FODP file (ODF 1.3 Part 3, 10, the drawing shapes and the presentation elements; Part 2 for the package) to the composition model: it reads the package, the master pages and their placeholders, the pages with their shapes, frames, pictures, charts, tables and text into a slides.Document, which
// slides.Convert lays out as Impress prints a presentation. LibreOffice is
// the reference wherever the specification leaves room or LibreOffice
// departs from it; see [Convert].
package odp
