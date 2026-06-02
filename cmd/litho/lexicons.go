package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/models"
)

type lexiconsResult struct {
	File     string                        `json:"file"`
	Lexicons []models.PronunciationLexicon `json:"lexicons"`
}

func runLexicons(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lexicons", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho lexicons [-f json] [-p password] <file> [file...]")
		return exitError
	}

	var results []lexiconsResult
	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		lex, err := r.ExtractPronunciationLexicons(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			lex = nil
		}
		results = append(results, lexiconsResult{File: path, Lexicons: lex})
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
			for _, l := range res.Lexicons {
				fmt.Fprintln(stdout, l.XML)
			}
		}
	}
	return exitOK
}
