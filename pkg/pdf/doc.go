// Package pdf provides a high-level API for reading and extracting data from PDF documents.
//
// The litho engine is a pure Go implementation of the PDF 1.4 - 2.0 specifications.
// It bypasses the need for external C-dependencies (like poppler or mutool) and
// provides native tools for decrypting files, extracting text using spatial heuristics,
// evaluating embedded interactive Javascript forms, and stripping out hidden content.
//
// # Example Usage
//
//	package main
//
//	import (
//		"fmt"
//		"log"
//		"github.com/gotenberg/litho/pkg/pdf"
//	)
//
//	func main() {
//		// Open the document
//		reader, file, err := pdf.OpenFile("document.pdf", "")
//		if err != nil {
//			log.Fatal(err)
//		}
//		defer file.Close()
//
//		// Check page count
//		pages, _ := reader.NumPages()
//		fmt.Printf("Total Pages: %d\n", pages)
//
//		// Extract clean, reconstructed text from the first page
//		if pages > 0 {
//			text, _ := reader.ExtractText(1)
//			fmt.Println(text)
//		}
//	}
package pdf
