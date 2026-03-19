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

	metadata, _ := reader.GetMetadata(ctx)
	fmt.Println("=== Metadata ===")
	for k, v := range metadata {
		fmt.Printf("%s: %s\n", k, v)
	}

	numPages, _ := reader.NumPages(ctx)
	fmt.Printf("\n=== Pages: %d ===\n", numPages)

	if numPages > 0 {
		text, _ := reader.ExtractText(ctx, 1)
		fmt.Println("\n=== Page 1 Text ===")
		fmt.Println(text)
	}

	formData, _ := reader.GetFormData(ctx)
	if len(formData) > 0 {
		fmt.Println("\n=== Form Data ===")
		for k, v := range formData {
			fmt.Printf("%s: %s\n", k, v)
		}
	}
}
