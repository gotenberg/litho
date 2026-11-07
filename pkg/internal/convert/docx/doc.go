// Package docx converts a WordprocessingML document, a DOCX file (ECMA-376
// Part 1, 17; Part 2 for the package), to the composition model: it reads
// the package, resolves the styles, numbering and sections, builds a
// layout.Node tree in the vocabulary of css.Style, and lays it out with
// layout.Layout. See [Convert] for what is read and what is not.
package docx
