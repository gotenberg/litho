// Package plugins defines extension interfaces for the litho PDF reader.
//
// Filter plugins allow opt-in support for PDF stream filters that require
// external C libraries (e.g., JBIG2Decode via libjbig2dec, JPXDecode via
// libopenjp2). Without plugins, these filters return ErrUnsupportedFilter.
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
