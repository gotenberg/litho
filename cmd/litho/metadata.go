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
	rawXMP := fs.Bool("xmp", false, "Dump the raw XMP metadata packet instead of the Info dictionary")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho metadata [-f json] [-p password] [--xmp] <file> [file...]")
		return exitError
	}

	if *rawXMP {
		return dumpXMP(ctx, files, *password, *format, stdout, stderr)
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

// dumpXMP streams the raw XMP metadata packet for each file. An absent
// packet prints "(no XMP packet)" in text mode and an empty xmp field in
// JSON mode.
func dumpXMP(ctx context.Context, files []string, password, format string, stdout, stderr io.Writer) int {
	type xmpResult struct {
		File string `json:"file"`
		XMP  string `json:"xmp"`
	}

	var results []xmpResult
	for _, path := range files {
		r, closer, err := openPDF(ctx, path, password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		open, err := r.XMPMetadata(ctx)
		if err != nil {
			closer()
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		var packet string
		if open != nil {
			rc, err := open(ctx)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
			data, readErr := io.ReadAll(rc)
			if cerr := rc.Close(); cerr != nil && readErr == nil {
				readErr = cerr
			}
			if readErr != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, readErr)
				return exitError
			}
			packet = string(data)
		}

		results = append(results, xmpResult{File: path, XMP: packet})
		closer()
	}

	switch format {
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
			if res.XMP == "" {
				fmt.Fprintln(stdout, "(no XMP packet)")
			} else {
				fmt.Fprintln(stdout, res.XMP)
			}
		}
	}
	return exitOK
}
