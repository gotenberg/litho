package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// regionFlags collects repeated --region page:x,y,width,height values.
type regionFlags []string

func (f *regionFlags) String() string { return strings.Join(*f, ", ") }
func (f *regionFlags) Set(val string) error {
	*f = append(*f, val)
	return nil
}

// redactResult is the JSON report, naming what was removed per vector.
//
// Redaction is destructive and security-critical, so the report enumerates
// what went rather than reporting a bare success: a caller has to be able to
// check that what it meant to remove is what was removed.
type redactResult struct {
	File                    string        `json:"file"`
	RemovedText             []string      `json:"removedText"`
	BlankedImages           []string      `json:"blankedImages,omitempty"`
	RemovedPaths            int           `json:"removedPaths"`
	RemovedAnnotations      []int         `json:"removedAnnotations,omitempty"`
	DroppedPageEntries      []string      `json:"droppedPageEntries,omitempty"`
	FontsFullyRemovedOnPage map[int][]int `json:"fontsFullyRemovedOnPage,omitempty"`
	ScrubbedAnnotations     []int         `json:"scrubbedAnnotations,omitempty"`
	ScrubbedFormFields      []int         `json:"scrubbedFormFields,omitempty"`
	ScrubbedOutlineItems    []int         `json:"scrubbedOutlineItems,omitempty"`
}

// parseRedactRegion reads one "page:x,y,width,height" argument.
//
// The flag takes x, y, width and height because that is how a box is measured
// off a page, while [pdf.RedactRegion.Rect] is [llx, lly, urx, ury], the shape
// of an annotation /Rect (ISO 32000-1, Table 166). The conversion happens here,
// once and explicitly: passing a width where an upper-right coordinate belongs
// would clear the wrong area, and on a security operation silently redacting
// the wrong box is the worst failure available.
func parseRedactRegion(spec string) (pdf.RedactRegion, error) {
	pageText, rectText, found := strings.Cut(spec, ":")
	if !found {
		return pdf.RedactRegion{}, fmt.Errorf("region %q: want page:x,y,width,height", spec)
	}

	page, err := strconv.Atoi(strings.TrimSpace(pageText))
	if err != nil || page < 1 {
		return pdf.RedactRegion{}, fmt.Errorf("region %q: page must be a positive whole number", spec)
	}

	parts := strings.Split(rectText, ",")
	if len(parts) != 4 {
		return pdf.RedactRegion{}, fmt.Errorf("region %q: want four numbers x,y,width,height", spec)
	}
	var n [4]float64
	for i, part := range parts {
		value, convErr := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if convErr != nil {
			return pdf.RedactRegion{}, fmt.Errorf("region %q: %q is not a number", spec, part)
		}
		n[i] = value
	}
	if n[2] <= 0 || n[3] <= 0 {
		return pdf.RedactRegion{}, fmt.Errorf("region %q: width and height must be greater than zero", spec)
	}

	return pdf.RedactRegion{
		Page: page,
		Rect: [4]float64{n[0], n[1], n[0] + n[2], n[1] + n[3]},
	}, nil
}

func runRedact(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("redact", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	format := fs.String("f", "text", "Report format: text or json")
	// Redaction re-encodes an image it cannot keep in its own colour space,
	// such as an indexed image over DeviceCMYK, as RGB, and this profile is
	// what its CMYK colours are read in when the document names none.
	cmykProfile := fs.String("cmyk-profile", "", "ICC profile DeviceCMYK is read in when the document names none (default: built-in SWOP profile)")

	var regionSpecs regionFlags
	fs.Var(&regionSpecs, "region", "Region to clear, page:x,y,width,height (repeatable, required)")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || len(regionSpecs) == 0 {
		fmt.Fprintln(stderr, "Usage: litho redact <file> --region page:x,y,width,height [--region ...] [--cmyk-profile profile.icc] -o out.pdf")
		return exitError
	}

	regions := make([]pdf.RedactRegion, 0, len(regionSpecs))
	for _, spec := range regionSpecs {
		region, err := parseRedactRegion(spec)
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return exitError
		}
		regions = append(regions, region)
	}

	var extra []pdf.OpenOption
	if *cmykProfile != "" {
		profile, err := os.ReadFile(filepath.Clean(*cmykProfile))
		if err != nil {
			fmt.Fprintf(stderr, "reading the CMYK profile: %v\n", err)
			return exitError
		}
		extra = append(extra, pdf.WithDefaultCMYKProfile(profile))
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password, extra...)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	// Redaction is the case that matters most: the caller proved they hold the
	// password, removed confidential content, and got back a document anyone
	// can open. This command has no encryption flag, so it is never encrypting.
	warnEncryptionDropped(stderr, false, r)

	w := pdf.NewWriter(r)
	defer releaseWriter(w, stderr)

	// Before the output file exists: a refused redaction must leave nothing
	// behind, and a half-redacted file on disk is the failure this command
	// exists to prevent.
	report, err := w.Redact(ctx, regions)
	if err != nil {
		fmt.Fprintf(stderr, "redacting: %v\n", err)
		if errors.Is(err, pdf.ErrRedactionIncomplete) && report != nil {
			for _, hit := range report.SurvivingCopies {
				fmt.Fprintf(stderr, "  copy of %q survives in object %d (%s)\n",
					hit.Token, hit.ObjectNum, hit.Where)
			}
			for _, objNum := range report.UnreadableObjects {
				fmt.Fprintf(stderr, "  object %d could not be read, so it is not proved clean\n", objNum)
			}
		}
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

	// -o - writes the document to stdout, so the report goes to stderr rather
	// than landing inside the bytes the caller is capturing. See reportWriter.
	return printRedactReport(reportWriter(*output, stdout, stderr), stderr, *format, fs.Arg(0), report)
}

// printRedactReport writes the report in the requested format.
func printRedactReport(stdout, stderr io.Writer, format, file string, report *pdf.RedactReport) int {
	result := redactResult{
		File:                    file,
		RemovedText:             report.RemovedText,
		BlankedImages:           report.BlankedImages,
		RemovedPaths:            report.RemovedPaths,
		RemovedAnnotations:      report.RemovedAnnotations,
		DroppedPageEntries:      report.DroppedPageEntries,
		FontsFullyRemovedOnPage: report.FontsFullyRemovedOnPage,
		ScrubbedAnnotations:     report.ScrubbedAnnotations,
		ScrubbedFormFields:      report.ScrubbedFormFields,
		ScrubbedOutlineItems:    report.ScrubbedOutlineItems,
	}

	switch format {
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(stderr, "encoding report: %v\n", err)
			return exitError
		}
	case "text":
		fmt.Fprintf(stdout, "Removed %d text run(s), %d path(s), %d image(s)\n",
			len(result.RemovedText), result.RemovedPaths, len(result.BlankedImages))
		if n := len(result.RemovedAnnotations); n > 0 {
			fmt.Fprintf(stdout, "Removed %d annotation(s) inside the regions\n", n)
		}
		if n := len(result.ScrubbedAnnotations) + len(result.ScrubbedFormFields) + len(result.ScrubbedOutlineItems); n > 0 {
			fmt.Fprintf(stdout, "Scrubbed %d document-wide copy holder(s)\n", n)
		}
		// Naming these is the point: litho cannot re-subset the embedded
		// program, so its outlines still describe the removed glyphs. Pages
		// print in ascending order: ranging the map printed them in a
		// different order on every run (CONTRIBUTING "Deterministic output").
		for _, page := range slices.Sorted(maps.Keys(result.FontsFullyRemovedOnPage)) {
			fmt.Fprintf(stdout, "Page %d no longer draws with font object(s) %v; their embedded outlines remain\n",
				page, result.FontsFullyRemovedOnPage[page])
		}
	default:
		fmt.Fprintf(stderr, "unknown format %q. Valid: text, json\n", format)
		return exitError
	}
	return exitOK
}
