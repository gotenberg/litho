// Package pdf provides a high-level API for reading and extracting data from PDF documents.
//
// The litho engine is a pure Go implementation of the PDF 1.4 - 2.0 specifications.
// It bypasses the need for external C-dependencies (like poppler or mutool) and
// provides native tools for decrypting files, extracting text using spatial heuristics,
// evaluating embedded interactive Javascript forms, and stripping out hidden content.
//
// # Acknowledgment
//
// litho's text extraction pipeline, CMap parser, and font resolution logic were
// heavily inspired by Mozilla's PDF.js (https://github.com/mozilla/pdf.js).
// Grateful acknowledgment to the PDF.js contributors for their pioneering work on
// open-source PDF parsing.
//
// # Example Usage
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"log"
//		"github.com/gotenberg/litho/pkg/pdf"
//	)
//
//	func main() {
//		ctx := context.Background()
//
//		// Open the document
//		reader, err := pdf.OpenFile(ctx, "document.pdf")
//		if err != nil {
//			log.Fatal(err)
//		}
//		defer reader.Close()
//
//		// Check page count
//		pages, _ := reader.PageCount(ctx)
//		fmt.Printf("PDF %s, %d pages\n", reader.Version(), pages)
//
//		// Extract clean, reconstructed text from the first page
//		if pages > 0 {
//			text, _ := reader.ExtractText(ctx, 1)
//			fmt.Println(text)
//		}
//	}
package pdf
