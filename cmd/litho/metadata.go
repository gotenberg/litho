package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
)

func runMetadata(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("metadata", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")

	if err := fs.Parse(args); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho metadata [-f json] [-p password] <file> [file...]")
		return exitError
	}

	type metadataResult struct {
		File     string            `json:"file"`
		Metadata map[string]string `json:"metadata"`
	}

	var results []metadataResult
	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		meta, err := r.Metadata(ctx)
		if err != nil {
			closer()
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		results = append(results, metadataResult{File: path, Metadata: meta})
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
			keys := make([]string, 0, len(res.Metadata))
			for k := range res.Metadata {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(stdout, "%s: %s\n", k, res.Metadata[k])
			}
		}
	}
	return exitOK
}
