package main

import (
	"context"
	"flag"
	"fmt"
	"io"
)

type attachmentInfo struct {
	Name     string `json:"name"`
	MIMEType string `json:"mimeType,omitempty"`
}

type attachmentsResult struct {
	File  string           `json:"file"`
	Files []attachmentInfo `json:"files"`
}

func runAttachments(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("attachments", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho attachments [-f json] [-p password] <file> [file...]")
		return exitError
	}

	var results []attachmentsResult
	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		embedded, err := r.EmbeddedFiles(ctx)
		if err != nil {
			closer()
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		var infos []attachmentInfo
		for _, ef := range embedded {
			infos = append(infos, attachmentInfo{
				Name:     ef.Name,
				MIMEType: ef.MIMEType,
			})
		}

		results = append(results, attachmentsResult{File: path, Files: infos})
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
			if len(res.Files) == 0 {
				fmt.Fprintln(stdout, "(no attachments)")
			} else {
				for _, f := range res.Files {
					if f.MIMEType != "" {
						fmt.Fprintf(stdout, "%s  (%s)\n", f.Name, f.MIMEType)
					} else {
						fmt.Fprintln(stdout, f.Name)
					}
				}
			}
		}
	}
	return exitOK
}
