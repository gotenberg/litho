// Package pptx converts a PresentationML presentation, a PPTX file (ECMA-376 Part 1, 19; Part 2 for the package), a macro-enabled PPTM, a template POTX or a slide show PPSX alike to the composition model: it reads the package, the presentation with its slide size and slide list, the masters, layouts and slides with their placeholders, shapes, pictures, charts, tables and text into a slides.Document, which
// slides.Convert lays out as Impress prints a presentation. LibreOffice is
// the reference wherever the specification leaves room or LibreOffice
// departs from it; see [Convert].
package pptx
