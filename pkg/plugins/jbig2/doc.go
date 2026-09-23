// Package jbig2 implements litho's JBIG2Decode filter plugin. It wraps
// libjbig2dec through CGO and satisfies [plugins.FilterDecoder]. litho decodes
// JBIG2Decode natively, so the plugin is optional: registered with
// [pdf.WithFilterPlugin], it takes precedence over the built-in decoder.
//
// In builds without CGO the [Decoder.Decode] method returns an error that
// wraps [plugins.ErrUnsupportedFilter], so callers can fall back via
// [errors.Is] without a runtime check on build tags.
package jbig2
