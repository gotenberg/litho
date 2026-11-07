// Package odt converts an OpenDocument text document, an ODT file (ODF
// 1.3, Part 3 for the document, Part 2 for the package), to the
// composition model: it reads the package, resolves the automatic and
// common styles, the page layouts and master pages, builds a layout.Node
// tree in the vocabulary of css.Style, and lays it out with layout.Layout.
// See [Convert] for what is read and what is not.
package odt
