// Package plugins defines extension interfaces for the litho PDF reader.
//
// Filter plugins supply decoders for PDF stream filters backed by external C
// libraries: JPXDecode via libopenjp2 and JBIG2Decode via libjbig2dec. litho
// decodes both natively; a registered plugin takes precedence over the
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
