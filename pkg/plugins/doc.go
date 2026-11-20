// Package plugins defines the interfaces a caller implements to extend
// litho: the decoder of a PDF stream filter ([FilterDecoder]), the programs
// of the fonts a document shows without embedding them ([FontProvider] and
// [FontProviderFunc]), the score of a line as a heading ([HeadingScorer]),
// a signature ([Signer]) and the text of a scanned page ([OCREngine]). The
// values they take and return are the data types of
// [github.com/gotenberg/litho/pkg/models]; an option of
// [github.com/gotenberg/litho/pkg/pdf] registers an implementation.
//
// A filter plugin supplies the decoder for one PDF stream filter. litho
// decodes every standard filter natively, JBIG2Decode and JPXDecode included,
// so a plugin either makes a filter litho does not know decodable or replaces
// a built-in decoder: a registered plugin takes precedence for its filter
// name. A plugin declines a stream by returning [ErrUnsupportedFilter], which
// hands that stream back to the built-in decoder.
//
// Usage:
//
//	reader, err := pdf.OpenFile(ctx, "doc.pdf", pdf.WithFilterPlugin(myDecoder))
package plugins
