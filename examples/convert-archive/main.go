// Command convert-archive converts a document to a PDF/A-2b file that is
// also PDF/UA-1, in one call, and prints what the two profiles still find in
// it. The format follows the file extension: .txt, .html, .md, .docx, .odt,
// .xlsx, .ods, .csv, .pptx or .odp.
//
//	go run ./examples/convert-archive out.pdf report.docx
//
// A document that names no language gets "en". The file is written and
// claims both profiles even when something is left, such as a picture with
// no alternate text: the report, not the claim, says whether it conforms,
// and the command exits 2 then.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// formats maps a file extension to the format pdf.Convert reads.
var formats = map[string]pdf.Format{
	".txt": pdf.FormatText, ".html": pdf.FormatHTML, ".htm": pdf.FormatHTML, ".md": pdf.FormatMarkdown,
	".docx": pdf.FormatDOCX, ".odt": pdf.FormatODT, ".xlsx": pdf.FormatXLSX, ".ods": pdf.FormatODS,
	".csv": pdf.FormatCSV, ".pptx": pdf.FormatPPTX, ".odp": pdf.FormatODP,
}

func main() {
	conforms, err := run(context.Background(), os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	if !conforms {
		os.Exit(2)
	}
}

func run(ctx context.Context, args []string) (bool, error) {
	if len(args) != 2 {
		return false, errors.New("usage: convert-archive out.pdf input")
	}
	format, ok := formats[strings.ToLower(filepath.Ext(args[1]))]
	if !ok {
		return false, fmt.Errorf("%s: unknown extension", args[1])
	}

	in, err := os.Open(args[1]) //nolint:gosec // The input path is the example's own argument.
	if err != nil {
		return false, err
	}
	defer in.Close()

	out, err := os.Create(args[0]) //nolint:gosec // The output path is the example's own argument.
	if err != nil {
		return false, err
	}
	opts := []pdf.ConvertOption{
		pdf.WithConvertPDFA(models.PDFA2b),
		pdf.WithConvertPDFUA(models.PDFUA1),
		pdf.WithDefaultLanguage("en"),
	}
	if format == pdf.FormatHTML || format == pdf.FormatMarkdown {
		// The document reads its stylesheets and images from its own
		// directory, and nothing outside it.
		root, err := os.OpenRoot(filepath.Dir(args[1]))
		if err != nil {
			return false, err
		}
		defer root.Close()
		opts = append(opts, pdf.WithResources(root.FS()))
	}
	report, err := pdf.Convert(ctx, in, format, out, opts...)
	if err = errors.Join(err, out.Close()); err != nil {
		return false, err
	}

	fmt.Printf("%d page(s), %d fix(es)\n", report.Pages, len(report.Conformance.Fixed))
	for _, v := range report.Conformance.ResidualViolations() {
		fmt.Printf("%s %s: %s [%s]\n", v.Specification, v.Clause, v.Description, v.Context)
	}
	return len(report.Conformance.Residual) == 0, nil
}
