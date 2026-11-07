// Package office holds what the Office document converters share: the
// options [docx.Convert] and [odt.Convert] take and the errors they
// return. A converter reads its document model, builds a layout.Node tree
// in the vocabulary of css.Style, and lays it out with layout.Layout; it
// never touches CSS syntax.
package office
