package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/models"
)

type formsResult struct {
	File   string             `json:"file"`
	Fields []models.FormField `json:"fields"`
}

func runForms(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("forms", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho forms [-f json] [-p password] <file> [file...]")
		return exitError
	}

	var results []formsResult
	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		fields, err := r.FormFields(ctx)
		if err != nil {
			closer()
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		results = append(results, formsResult{File: path, Fields: fields})
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
			if len(res.Fields) == 0 {
				fmt.Fprintln(stdout, "(no form fields)")
			} else {
				for _, f := range res.Fields {
					fmt.Fprintf(stdout, "%s: %s\n", f.Name, f.Value)
				}
			}
		}
	}
	return exitOK
}
