// Command gen-quotes generates the quotation marks quotes: auto picks for
// a content language in the HTML converter of pkg/internal/convert/html.
//
// CSS Generated Content 3, 2.1 leaves the marks of quotes: auto to the
// user agent, to suit the content language. Chromium takes them from
// ICU's locale data, which mirrors the delimiters of the Unicode CLDR:
// quotationStart and quotationEnd for the outer pair,
// alternateQuotationStart and alternateQuotationEnd for the inner one.
// The generator downloads the cldr-misc-full package of the CLDR release
// the pinned Chromium's ICU carries, refuses an archive whose SHA-256
// differs from the pinned digest, applies the locales where the pinned
// Chromium prints other marks than that release holds, and writes the
// pairs of every locale as a Go map keyed by the lower-case BCP 47 tag.
//
// The CLDR data is under the Unicode License v3, which allows its
// redistribution with its notice: the generated file carries it.
//
// Usage:
//
//	go run ./cmd/gen-quotes                 # download the package
//	go run ./cmd/gen-quotes --src FILE.tgz  # read a local copy
//	go generate ./pkg/internal/convert/html/...
package main
