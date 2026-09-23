package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runStampImage(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stamp-image", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	imagePath := fs.String("image", "", "Path to image file (required)")
	mime := fs.String("mime", "", "MIME type (auto-detected from extension if omitted)")
	x := fs.Float64("x", 0, "X position in points from bottom-left")
	y := fs.Float64("y", 0, "Y position in points from bottom-left")
	width := fs.Float64("width", 0, "Width in points (0 = proportional or natural)")
	height := fs.Float64("height", 0, "Height in points (0 = proportional or natural)")
	opacity := fs.Float64("opacity", 1.0, "Opacity (0.0-1.0)")
	pages := fs.String("pages", "", "Page range (default: all)")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || *imagePath == "" {
		fmt.Fprintln(stderr, "Usage: litho stamp-image <file> --image logo.png [--mime image/png] [--x 100] [--y 100] [--width 200] [--height 200] [--opacity 1.0] [--pages RANGE] -o out.pdf")
		return exitError
	}

	data, err := os.ReadFile(*imagePath)
	if err != nil {
		fmt.Fprintf(stderr, "reading image file: %v\n", err)
		return exitError
	}

	mimeType := *mime
	if mimeType == "" {
		mimeType = mimeFromExt(*imagePath)
	}

	stamp := pdf.ImageStamp{
		Data:     data,
		MimeType: mimeType,
		X:        *x,
		Y:        *y,
		Width:    *width,
		Height:   *height,
		Opacity:  *opacity,
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	// This command has no encryption flag, so it can never be encrypting.
	warnEncryptionDropped(stderr, false, r)

	w := pdf.NewWriter(r)

	var pageList []int
	if *pages != "" {
		pageCount, err := r.PageCount(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "reading page count: %v\n", err)
			return exitError
		}
		pageList, err = parsePages(*pages, pageCount)
		if err != nil {
			fmt.Fprintf(stderr, "parsing pages: %v\n", err)
			return exitError
		}
	}

	if err := w.StampImage(stamp, pageList...); err != nil {
		fmt.Fprintf(stderr, "stamping image: %v\n", err)
		return exitError
	}

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := w.WriteTo(ctx, dst); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	return exitOK
}

// mimeFromExt returns a MIME type based on the file extension.
func mimeFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".pdf":
		return "application/pdf"
	case ".csv":
		return "text/csv"
	case ".xml":
		return "application/xml"
	case ".json":
		return "application/json"
	case ".txt":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}
