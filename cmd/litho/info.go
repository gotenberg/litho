package main

import (
	"context"
	"flag"
	"fmt"
	"io"
)

type infoResult struct {
	File        string `json:"file"`
	Version     string `json:"version"`
	Pages       int    `json:"pages"`
	Encrypted   bool   `json:"encrypted"`
	Linearized  bool   `json:"linearized"`
	HasForms    bool   `json:"has_forms"`
	IsPortfolio bool   `json:"is_portfolio"`
}

func runInfo(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")

	if err := fs.Parse(args); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho info [-f json] [-p password] <file> [file...]")
		return exitError
	}

	var results []infoResult
	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		pageCount, _ := r.PageCount(ctx)
		hasForms := r.HasAcroForm(ctx)
		isPortfolio, _ := r.IsPortfolio(ctx)

		results = append(results, infoResult{
			File:        path,
			Version:     r.Version(),
			Pages:       pageCount,
			Encrypted:   r.IsEncrypted(),
			Linearized:  r.IsLinearized(),
			HasForms:    hasForms,
			IsPortfolio: isPortfolio,
		})

		closer()
	}

	switch *format {
	case "json":
		if len(results) == 1 {
			return jsonOut(stdout, stderr, results[0])
		}
		return jsonOut(stdout, stderr, results)
	default:
		for _, res := range results {
			if len(results) > 1 {
				fmt.Fprintf(stdout, "--- %s ---\n", res.File)
			}
			fmt.Fprintf(stdout, "Version:     %s\n", res.Version)
			fmt.Fprintf(stdout, "Pages:       %d\n", res.Pages)
			fmt.Fprintf(stdout, "Encrypted:   %t\n", res.Encrypted)
			fmt.Fprintf(stdout, "Linearized:  %t\n", res.Linearized)
			fmt.Fprintf(stdout, "Has forms:   %t\n", res.HasForms)
			fmt.Fprintf(stdout, "Portfolio:   %t\n", res.IsPortfolio)
		}
	}
	return exitOK
}

func jsonOut(stdout, stderr io.Writer, v any) int {
	if err := outputJSON(stdout, v); err != nil {
		fmt.Fprintf(stderr, "encoding JSON: %v\n", err)
		return exitError
	}
	return exitOK
}
