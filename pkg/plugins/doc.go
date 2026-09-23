// Package plugins defines extension interfaces for the litho PDF reader.
//
// Filter plugins allow opt-in support for PDF stream filters that require
// external C libraries: JPXDecode via libopenjp2, which returns
// ErrUnsupportedFilter without its plugin. JBIG2Decode is decoded natively; a
// registered jbig2 plugin, backed by libjbig2dec, takes precedence over the
// built-in decoder.
//
// Usage:
//
//	import (
//	    "github.com/gotenberg/litho/pkg/pdf"
//	    "github.com/gotenberg/litho/pkg/plugins/jbig2"
//	    "github.com/gotenberg/litho/pkg/plugins/jpx"
//	)
//
//	reader, err := pdf.OpenFile(ctx, "doc.pdf",
//	    pdf.WithFilterPlugin(jbig2.New()),
//	    pdf.WithFilterPlugin(jpx.New()),
//	)
package plugins
