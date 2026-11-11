// Command convert-archive converts a document to a PDF/A-2b file that is
// also PDF/UA-1, in a pipeline: a Convert stage lays the document out, an
// Apply stage remediates it to both profiles. It prints what the two
// profiles still find in it. The format follows the file extension: .txt,
// .html, .md, .docx, .odt, .xlsx, .ods, .csv, .pptx or .odp.
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
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

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

	in, err := os.Open(args[1]) //nolint:gosec // The input path is the example's own argument.
	if err != nil {
		return false, err
	}
	defer in.Close()

	// The document reads its stylesheets and images from its own directory,
	// and nothing outside it.
	root, err := os.OpenRoot(filepath.Dir(args[1]))
	if err != nil {
		return false, err
	}
	defer root.Close()
	conversion, err := conversionOf(in, strings.ToLower(filepath.Ext(args[1])), root.FS())
	if err != nil {
		return false, fmt.Errorf("%s: %w", args[1], err)
	}

	out, err := os.Create(args[0]) //nolint:gosec // The output path is the example's own argument.
	if err != nil {
		return false, err
	}
	var report *pdf.RemediationReport
	result, err := pdf.NewPipeline().
		Convert(conversion).
		Apply(func(ctx context.Context, w *pdf.Writer) error {
			var err error
			report, err = w.RemediatePDFA(ctx, models.PDFA2b, pdf.WithPDFUA(models.PDFUA1), pdf.WithLanguage("en"))
			return err
		}).
		WriteTo(ctx, out)
	if err = errors.Join(err, out.Close()); err != nil {
		return false, err
	}

	fmt.Printf("%d page(s), %d fix(es)\n", result.Stages[0].Conversion.Pages, len(report.Fixed))
	for _, v := range report.ResidualViolations() {
		fmt.Printf("%s %s: %s [%s]\n", v.Specification, v.Clause, v.Description, v.Context)
	}
	return len(report.Residual) == 0, nil
}

// conversionOf returns the conversion of src, whose file extension is ext,
// at PDF 1.7, the version PDF/A-2 and PDF/UA-1 build on. An HTML or
// Markdown source reads its resources from resources.
func conversionOf(src io.Reader, ext string, resources fs.FS) (*pdf.Conversion, error) {
	lang, version := pdf.WithDefaultLanguage("en"), pdf.WithConvertVersion("1.7")
	switch ext {
	case ".txt":
		return pdf.FromText(src, lang, version), nil
	case ".html", ".htm":
		return pdf.FromHTML(src, lang, version, pdf.WithResources(resources)), nil
	case ".md":
		return pdf.FromMarkdown(src, lang, version, pdf.WithResources(resources)), nil
	case ".docx":
		return pdf.FromDOCX(src, lang, version), nil
	case ".odt":
		return pdf.FromODT(src, lang, version), nil
	case ".xlsx":
		return pdf.FromXLSX(src, lang, version), nil
	case ".ods":
		return pdf.FromODS(src, lang, version), nil
	case ".csv":
		return pdf.FromCSV(src, lang, version), nil
	case ".pptx":
		return pdf.FromPPTX(src, lang, version), nil
	case ".odp":
		return pdf.FromODP(src, lang, version), nil
	}
	return nil, errors.New("unknown extension")
}
