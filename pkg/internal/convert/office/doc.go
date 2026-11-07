// Package office holds what the Office document converters share: the
// options [docx.Convert] and [odt.Convert] take, the errors they return,
// and the capped zip reading of their packages ([OpenPackage]). A converter
// reads its document model, builds a layout.Node tree in the vocabulary of
// css.Style, and lays it out with layout.Layout; it never touches CSS
// syntax.
package office
