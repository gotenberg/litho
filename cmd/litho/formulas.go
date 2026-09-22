package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/models"
)

type formulasResult struct {
	File     string                  `json:"file"`
	Formulas []models.FormulaElement `json:"formulas"`
}

func runFormulas(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("formulas", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho formulas [-f json] [-p password] <file> [file...]")
		return exitError
	}

	var results []formulasResult
	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		formulas, err := r.ExtractFormulas(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			formulas = nil
		}
		results = append(results, formulasResult{File: path, Formulas: formulas})
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
			for _, f := range res.Formulas {
				if len(f.MCIDs) > 0 {
					fmt.Fprintf(stdout, "mcids %v\n", f.MCIDs)
				}
				fmt.Fprintln(stdout, f.MathML)
			}
		}
	}
	return exitOK
}
