// Package pdf provides a high-level API for reading, extracting data from,
// and producing PDF documents.
//
// litho is a pure-Go implementation of ISO 32000 (PDF 1.4 - 2.0) with no
// C dependencies. The package groups its functionality into three types:
//
//   - [Reader]: open an existing PDF, query metadata, extract text and
//     other content. See [Open], [OpenFile].
//   - [Writer]: queue mutations (rotate pages, stamp, encrypt, sign, ...)
//     against a Reader and emit a new PDF via [Writer.WriteTo].
//   - [Pipeline]: chain Split, Merge, Apply and WriteTo operations with
//     managed intermediate storage. See [NewPipeline].
//
// Specialised PDF 2.0 extractors sit on [Reader] alongside the core
// text, image, and annotation methods: [Reader.ExtractGeoData],
// [Reader.ExtractMeasurements], [Reader.Extract3DModels],
// [Reader.ExtractPronunciationLexicons], [Reader.ExtractFormulas], and
// [Reader.ExtractRedactedText].
//
// # Quick start
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"log"
//		"os"
//
//		"github.com/gotenberg/litho/pkg/pdf"
//	)
//
//	func main() {
//		ctx := context.Background()
//
//		// Read
//		reader, err := pdf.OpenFile(ctx, "input.pdf")
//		if err != nil {
//			log.Fatal(err)
//		}
//		defer reader.Close()
//
//		text, _ := reader.ExtractText(ctx, 1)
//		fmt.Println(text)
//
//		// Mutate and write
//		writer := pdf.NewWriter(reader)
//		writer.SetMetadata(pdf.Metadata{Title: "Redacted copy"})
//		if err := writer.RotatePages(90, 1); err != nil {
//			log.Fatal(err)
//		}
//
//		out, err := os.Create("output.pdf")
//		if err != nil {
//			log.Fatal(err)
//		}
//		defer out.Close()
//
//		if err := writer.WriteTo(ctx, out); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// # PDF/A and PDF/UA
//
// Use [Reader.ValidatePDFA] or [Reader.ValidatePDFUA] to report compliance
// against a profile, and [Writer.RemediatePDFA] or [Writer.RemediatePDFUA]
// to queue automatic repairs for the violations the engine can fix.
//
// # Digital signatures
//
// [Reader.ExtractSignatures] and [Reader.VerifySignatures] inspect existing
// signatures. [Writer.Sign] queues a new signature; [WriteTo] produces a
// signed incremental update.
//
// # Acknowledgments
//
// litho's text extraction pipeline, CMap parser, and font resolution logic
// were heavily inspired by Mozilla's PDF.js
// (https://github.com/mozilla/pdf.js). The PDF/A and PDF/UA validation
// rule set and clause numbering draw extensively from veraPDF
// (https://github.com/veraPDF). Grateful acknowledgment to both
// communities for their pioneering open-source work on PDF parsing
// and conformance.
package pdf
