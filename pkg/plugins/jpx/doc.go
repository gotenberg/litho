// Package jpx implements litho's JPXDecode filter plugin for JPEG 2000
// image streams. It wraps libopenjp2 through CGO and satisfies
// [plugins.FilterDecoder]. litho decodes JPXDecode natively, so the plugin is
// optional: registered with [pdf.WithFilterPlugin], it takes precedence over
// the built-in decoder.
//
// In builds without CGO the [Decoder.Decode] method returns an error that
// wraps [plugins.ErrUnsupportedFilter], so callers can fall back via
// [errors.Is] without a runtime check on build tags.
package jpx
