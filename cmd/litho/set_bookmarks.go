package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

func runSetBookmarks(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("set-bookmarks", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	fromJSON := fs.String("from-json", "", "JSON file with bookmark definitions (required unless --generate)")
	generate := fs.Bool("generate", false, "Auto-generate bookmarks from detected headings (instead of --from-json)")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	if fs.NArg() < 1 || *output == "" || (*fromJSON == "" && !*generate) {
		fmt.Fprintln(stderr, "Usage: litho set-bookmarks <file> (--from-json bookmarks.json | --generate) -o out.pdf")
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	w := pdf.NewWriter(r)
	if *generate {
		w.GenerateBookmarks(defaultBookmarkGenOptions(), pdf.BookmarkReplace)
	} else {
		data, err := os.ReadFile(*fromJSON)
		if err != nil {
			fmt.Fprintf(stderr, "reading bookmark file: %v\n", err)
			return exitError
		}

		var bookmarks []models.Bookmark
		if err := json.Unmarshal(data, &bookmarks); err != nil {
			fmt.Fprintf(stderr, "parsing bookmark JSON: %v\n", err)
			return exitError
		}
		w.SetBookmarks(bookmarks)
	}

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := w.WriteTo(ctx, dst); err != nil {
		closeDst() //nolint:errcheck // already returning an error; a close failure would mask its cause
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	return exitOK
}
