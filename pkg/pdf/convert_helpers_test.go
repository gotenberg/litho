package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/models"
)

// testFormat names a source format for the tests that run several formats
// through one call.
type testFormat string

const (
	formatText     testFormat = "text"
	formatHTML     testFormat = "html"
	formatMarkdown testFormat = "markdown"
	formatDOCX     testFormat = "docx"
	formatODT      testFormat = "odt"
	formatXLSX     testFormat = "xlsx"
	formatODS      testFormat = "ods"
	formatCSV      testFormat = "csv"
	formatPPTX     testFormat = "pptx"
	formatODP      testFormat = "odp"
)

// fromFormat returns the conversion of src as format with opts, through the
// constructor of that format. It panics on a format it does not name: the
// tables of the tests list the formats.
func fromFormat(format testFormat, src io.Reader, opts ...ConvertOption) *Conversion {
	switch format {
	case formatText:
		return FromText(src, optionsAs[TextOption](opts)...)
	case formatHTML:
		return FromHTML(src, optionsAs[HTMLOption](opts)...)
	case formatMarkdown:
		return FromMarkdown(src, optionsAs[MarkdownOption](opts)...)
	case formatDOCX:
		return FromDOCX(src, optionsAs[WordProcessingOption](opts)...)
	case formatODT:
		return FromODT(src, optionsAs[WordProcessingOption](opts)...)
	case formatXLSX:
		return FromXLSX(src, optionsAs[SpreadsheetOption](opts)...)
	case formatODS:
		return FromODS(src, optionsAs[SpreadsheetOption](opts)...)
	case formatCSV:
		return FromCSV(src, optionsAs[CSVOption](opts)...)
	case formatPPTX:
		return FromPPTX(src, optionsAs[PresentationOption](opts)...)
	case formatODP:
		return FromODP(src, optionsAs[PresentationOption](opts)...)
	}
	panic(fmt.Sprintf("unknown test format %q", format))
}

// optionsAs returns opts as the option type of one constructor, which every
// [ConvertOption] implements.
func optionsAs[T any](opts []ConvertOption) []T {
	out := make([]T, len(opts))
	for i, o := range opts {
		out[i] = o.(T)
	}
	return out
}

// withSpool forces the spool files of a conversion on (a hold of 1 byte)
// or off (-1).
func withSpool(held int64) ConvertOption {
	return convertOption(func(c *convertConfig) { c.spool = held })
}

// convertAndRemediate runs c in a pipeline and remediates its output in an
// Apply stage: to pdfa, paired with pdfua when both are given, or to pdfua
// alone, with lang as the document language when not empty. The output is
// written in full at version, the version c converts at. opts reach the
// pipeline's write after those two.
func convertAndRemediate(ctx context.Context, c *Conversion, version string, pdfa models.PDFAProfile, pdfua models.PDFUAProfile, lang string, opts ...WriteOption) ([]byte, *RemediationReport, error) {
	var remOpts []RemediateOption
	if lang != "" {
		remOpts = append(remOpts, WithLanguage(lang))
	}
	var report *RemediationReport
	var out bytes.Buffer
	_, err := NewPipeline().Convert(c).Apply(func(ctx context.Context, w *Writer) error {
		var err error
		if pdfa != "" {
			if pdfua != "" {
				remOpts = append(remOpts, WithPDFUA(pdfua))
			}
			report, err = w.RemediatePDFA(ctx, pdfa, remOpts...)
		} else {
			report, err = w.RemediatePDFUA(ctx, pdfua, remOpts...)
		}
		return err
	}).WriteTo(ctx, &out, append([]WriteOption{WithFullRewrite(), WithVersion(version)}, opts...)...)
	return out.Bytes(), report, err
}
