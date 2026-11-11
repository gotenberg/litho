// Command convert-merge converts several documents, merges them in the order
// given into one PDF/A-2b file, and prints the pages each one brought. One
// pipeline per input converts it, or opens it when it is already a PDF; a
// MergePipelines stage runs them, several at once, and merges their outputs;
// an Apply stage remediates the merge. The format follows the file
// extension: .pdf, .txt, .html, .md, .docx, .odt, .xlsx, .ods, .csv, .pptx or
// .odp.
//
//	go run ./examples/convert-merge out.pdf cover.html report.docx annex.pdf
//
// The merged file is the same whatever the number of inputs converted at
// once. It claims PDF/A-2b even when something is left: the command exits 2
// then.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	if len(args) < 2 {
		return false, errors.New("usage: convert-merge out.pdf input [input ...]")
	}

	// Every input stays open until the pipeline is written: a conversion
	// reads its source as it runs, and the merge reads an opened PDF.
	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			if err := c.Close(); err != nil {
				log.Print(err)
			}
		}
	}()
	pipelines := make([]*pdf.Pipeline, 0, len(args)-1)
	for _, path := range args[1:] {
		p, closer, err := pipelineOf(ctx, path)
		if err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
		}
		closers = append(closers, closer)
		pipelines = append(pipelines, p)
	}

	out, err := os.Create(args[0]) //nolint:gosec // The output path is the example's own argument.
	if err != nil {
		return false, err
	}
	var report *pdf.RemediationReport
	result, err := pdf.NewPipeline().
		MergePipelines(pipelines).
		Apply(func(ctx context.Context, w *pdf.Writer) error {
			var err error
			report, err = w.RemediatePDFA(ctx, models.PDFA2b)
			return err
		}).
		WriteTo(ctx, out, pdf.WithPipelineConcurrency(4))
	if err = errors.Join(err, out.Close()); err != nil {
		return false, err
	}

	for i, r := range result.Stages[0].Pipelines {
		if conversion := r.Stages[0].Conversion; conversion != nil {
			fmt.Printf("%s: %d page(s) converted\n", args[i+1], conversion.Pages)
			continue
		}
		fmt.Printf("%s: opened\n", args[i+1])
	}
	fmt.Printf("%d fix(es), %d residual violation(s)\n", len(report.Fixed), len(report.Residual))
	return len(report.Residual) == 0, nil
}

// pipelineOf returns the pipeline of the input at path: it opens a PDF, and
// converts any other format at PDF 1.7, the version PDF/A-2 builds on. The
// closer releases the file once the pipeline is written.
func pipelineOf(ctx context.Context, path string) (*pdf.Pipeline, io.Closer, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".pdf" {
		r, err := pdf.OpenFile(ctx, path)
		if err != nil {
			return nil, nil, err
		}
		return pdf.NewPipeline().Open(r), r, nil
	}
	in, err := os.Open(path) //nolint:gosec // The input path is the example's own argument.
	if err != nil {
		return nil, nil, err
	}
	lang, version := pdf.WithDefaultLanguage("en"), pdf.WithConvertVersion("1.7")
	var c *pdf.Conversion
	switch ext {
	case ".txt":
		c = pdf.FromText(in, lang, version)
	case ".html", ".htm":
		c = pdf.FromHTML(in, lang, version)
	case ".md":
		c = pdf.FromMarkdown(in, lang, version)
	case ".docx":
		c = pdf.FromDOCX(in, lang, version)
	case ".odt":
		c = pdf.FromODT(in, lang, version)
	case ".xlsx":
		c = pdf.FromXLSX(in, lang, version)
	case ".ods":
		c = pdf.FromODS(in, lang, version)
	case ".csv":
		c = pdf.FromCSV(in, lang, version)
	case ".pptx":
		c = pdf.FromPPTX(in, lang, version)
	case ".odp":
		c = pdf.FromODP(in, lang, version)
	default:
		return nil, nil, errors.Join(errors.New("unknown extension"), in.Close())
	}
	return pdf.NewPipeline().Convert(c), in, nil
}
