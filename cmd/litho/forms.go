package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

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
					fmt.Fprintf(stdout, "%s%s: %s\n", f.Name, formFieldTraits(f), f.Value)
				}
			}
		}
	}
	return exitOK
}

// formFieldTraits returns what the text output says of f beside its name: its
// type and the pages its widgets are on, such as " (radio, pages 1,2)", or ""
// when it knows neither.
func formFieldTraits(f models.FormField) string {
	var pages []string
	for _, w := range f.Widgets {
		if p := strconv.Itoa(w.PageNumber); !slices.Contains(pages, p) {
			pages = append(pages, p)
		}
	}
	var traits []string
	if f.Type != "" {
		traits = append(traits, string(f.Type))
	}
	switch len(pages) {
	case 0:
	case 1:
		traits = append(traits, "page "+pages[0])
	default:
		traits = append(traits, "pages "+strings.Join(pages, ","))
	}
	if len(traits) == 0 {
		return ""
	}
	return " (" + strings.Join(traits, ", ") + ")"
}
