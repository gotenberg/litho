// Package plugins defines extension interfaces for the litho PDF reader.
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
