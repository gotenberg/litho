package main

import (
	"context"
	"fmt"
	"os"

	"github.com/gotenberg/litho/pkg/pdf"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: litho <file.pdf>")
		os.Exit(1)
	}

	ctx := context.Background()

	reader, err := pdf.OpenFile(ctx, os.Args[1])
	if err != nil {
		fmt.Printf("Failed to open PDF: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = reader.Close() //nolint:errcheck
	}()

	metadata, err := reader.GetMetadata(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to extract metadata: %v\n", err)
	}
	fmt.Println("=== Metadata ===")
	for k, v := range metadata {
		fmt.Printf("%s: %s\n", k, v)
	}

	numPages, err := reader.NumPages(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get page count: %v\n", err)
	}
	fmt.Printf("\n=== Pages: %d ===\n", numPages)

	if numPages > 0 {
		text, err := reader.ExtractText(ctx, 1)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to extract text from page 1: %v\n", err)
		}
		fmt.Println("\n=== Page 1 Text ===")
		fmt.Println(text)
	}

	formData, err := reader.GetFormData(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to extract form data: %v\n", err)
	}
	if len(formData) > 0 {
		fmt.Println("\n=== Form Data ===")
		for k, v := range formData {
			fmt.Printf("%s: %s\n", k, v)
		}
	}
}
