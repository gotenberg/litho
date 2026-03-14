package main

import (
	"fmt"
	"os"

	"github.com/gotenberg/litho/pkg/pdf"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: litho <file.pdf>")
		os.Exit(1)
	}

	reader, file, err := pdf.OpenFile(os.Args[1], "")
	if err != nil {
		fmt.Printf("Failed to open PDF: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = file.Close() //nolint:errcheck
	}()

	metadata, _ := reader.GetMetadata()
	fmt.Println("=== Metadata ===")
	for k, v := range metadata {
		fmt.Printf("%s: %s\n", k, v)
	}

	numPages, _ := reader.NumPages()
	fmt.Printf("\n=== Pages: %d ===\n", numPages)

	if numPages > 0 {
		text, _ := reader.ExtractText(1)
		fmt.Println("\n=== Page 1 Text ===")
		fmt.Println(text)
	}

	formData, _ := reader.GetFormData()
	if len(formData) > 0 {
		fmt.Println("\n=== Form Data ===")
		for k, v := range formData {
			fmt.Printf("%s: %s\n", k, v)
		}
	}
}
