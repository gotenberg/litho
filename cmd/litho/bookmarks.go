package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
)

type bookmarksResult struct {
	File      string            `json:"file"`
	Bookmarks []models.Bookmark `json:"bookmarks"`
}

func runBookmarks(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bookmarks", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho bookmarks [-f json] [-p password] <file> [file...]")
		return exitError
	}

	var results []bookmarksResult
	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		bms, err := r.Bookmarks(ctx)
		if err != nil {
			closer()
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		results = append(results, bookmarksResult{File: path, Bookmarks: bms})
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
			if len(res.Bookmarks) == 0 {
				fmt.Fprintln(stdout, "(no bookmarks)")
			} else {
				printBookmarks(stdout, res.Bookmarks, 0)
			}
		}
	}
	return exitOK
}

func printBookmarks(w io.Writer, bookmarks []models.Bookmark, depth int) {
	indent := strings.Repeat("  ", depth)
	for _, bm := range bookmarks {
		fmt.Fprintf(w, "%s%s (page %d)\n", indent, bm.Title, bm.PageNumber)
		if len(bm.Children) > 0 {
			printBookmarks(w, bm.Children, depth+1)
		}
	}
}
