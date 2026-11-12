package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// The inputs of `corpus cost`: the documents it converts and the PDFs it
// runs the PDF operations on, each by a name a reading keeps. A generated
// input is built by code from a fixed seed into the inputs directory, the
// same bytes on every build; a PDF input is a file of a provisioned corpus.

// costInput is one input of `corpus cost`.
type costInput struct {
	name string
	// file is the document's name in its directory, for a generated input.
	file string
	// write writes the document and its resources into a directory, nil
	// for a PDF of a corpus.
	write func(dir string) error
	// pdf is a PDF input's path under the testdata, "" for a generated one.
	pdf string
	// kind says what the input stands for.
	kind string
}

// costRow is one row of a reading: an operation on an input.
type costRow struct {
	op    string
	input string
}

// name names the row as a reading prints it: the input for a conversion,
// op/input for another operation.
func (r costRow) name() string {
	if r.op == "convert" {
		return r.input
	}
	return r.op + "/" + r.input
}

// Sizes of the generated measurement documents, small, medium and large.
var (
	// costReportSections are the sections of the reports in HTML, Markdown,
	// DOCX and ODT: each is about a page of prose, half of them a table.
	costReportSections = [3]int{10, 60, 300}
	// costSalesSheets and costSalesRows shape the workbooks: sheets of rows
	// of eight cells.
	costSalesSheets = [3]int{1, 4, 4}
	costSalesRows   = [3]int{500, 2500, 10_000}
	// costSlides are the slides of the decks.
	costSlides = [3]int{20, 100, 500}
	// costSizeNames name the three sizes.
	costSizeNames = [3]string{"s", "m", "l"}
)

// costWriteFile returns a write that writes data() as file.
func costWriteFile(file string, data func() ([]byte, error)) func(dir string) error {
	return func(dir string) error {
		b, err := data()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, file), b, 0o644) //nolint:gosec // testdata is world-readable
	}
}

// costBytes adapts a generator that cannot fail.
func costBytes(b func() []byte) func() ([]byte, error) {
	return func() ([]byte, error) { return b(), nil }
}

// costGenerated returns the generated inputs, in a fixed order.
func costGenerated() []costInput {
	var in []costInput
	add := func(name, file, kind string, data func() ([]byte, error)) {
		in = append(in, costInput{name: name, file: file, kind: kind, write: costWriteFile(file, data)})
	}
	add("text-line", "index.txt", "one line of text", costBytes(func() []byte { return []byte("The quick brown fox jumps over the lazy dog.\n") }))
	for _, mib := range []int{1, 4, 16} {
		add(fmt.Sprintf("text-%dmib", mib), "index.txt", "paragraphs of plain text", costBytes(func() []byte { return stressText(mib << 20) }))
	}
	for _, kib := range []int{256, 1024, 4096} {
		name := fmt.Sprintf("html-%dkib", kib)
		if kib >= 1024 {
			name = fmt.Sprintf("html-%dmib", kib>>10)
		}
		add(name, "index.html", "HTML paragraphs", costBytes(func() []byte { return stressHTML(kib << 10) }))
	}
	for i, size := range costSizeNames {
		n := costReportSections[i]
		add("html-report-"+size, "index.html", fmt.Sprintf("HTML report, %d sections with tables and lists", n), costBytes(func() []byte { return costReportHTML(n) }))
	}
	for _, kind := range []string{"png", "jpeg"} {
		in = append(in, costInput{
			name: "html-" + kind + "-10", file: "index.html", kind: "HTML page of ten 12-megapixel " + kind + " pictures",
			write: func(dir string) error { return writeStressPictures(dir, kind, 10) },
		})
	}
	for i, size := range costSizeNames {
		n := costReportSections[i]
		add("md-report-"+size, "index.md", fmt.Sprintf("Markdown report, %d sections with tables and lists", n), costBytes(func() []byte { return costReportMarkdown(n) }))
	}
	for i, size := range costSizeNames {
		n := costReportSections[i]
		add("docx-report-"+size, "index.docx", fmt.Sprintf("DOCX report, %d sections with tables and lists", n), func() ([]byte, error) { return costReportDocx(n) })
	}
	add("docx-2000pages", "index.docx", "DOCX of 2,000 one-paragraph pages", func() ([]byte, error) { return stressDocx(stressDocxPages) })
	for i, size := range costSizeNames {
		n := costReportSections[i]
		add("odt-report-"+size, "index.odt", fmt.Sprintf("ODT report, %d sections with tables and lists", n), func() ([]byte, error) { return costReportODT(n) })
	}
	for i, size := range costSizeNames {
		sheets, rows := costSalesSheets[i], costSalesRows[i]
		kind := fmt.Sprintf("workbook, %d sheets of %d rows of 8 cells", sheets, rows)
		add("xlsx-sales-"+size, "index.xlsx", "XLSX "+kind, func() ([]byte, error) { return costSalesXlsx(sheets, rows) })
		add("ods-sales-"+size, "index.ods", "ODS "+kind, func() ([]byte, error) { return costSalesODS(sheets, rows) })
	}
	for _, cells := range []int{10_000, 100_000, 300_000} {
		add(fmt.Sprintf("csv-%dk", cells/1000), "index.csv", fmt.Sprintf("CSV of %d cells, 10 a row", cells), costBytes(func() []byte { return stressCSV(cells) }))
	}
	for _, n := range costSlides {
		add(fmt.Sprintf("pptx-%dslides", n), "index.pptx", fmt.Sprintf("PPTX of %d slides on one master", n), func() ([]byte, error) { return stressPptx(n) })
	}
	for i, size := range costSizeNames {
		n := costSlides[i]
		add("odp-deck-"+size, "index.odp", fmt.Sprintf("ODP of %d slides of a title and bullets", n), func() ([]byte, error) { return costDeckODP(n) })
	}
	return in
}

// costPDFs are the PDF inputs, files of the provisioned corpora named by
// kind and size.
var costPDFs = []costInput{
	{name: "pdf-text-s", pdf: "govdocs/pdf/pdfs/thread_100_100165.pdf", kind: "text-heavy, 17 pages, 123 KB"},
	{name: "pdf-text-m", pdf: "govdocs/pdf/pdfs/thread_400_400760.pdf", kind: "text-heavy, 501 pages, 1.2 MB"},
	{name: "pdf-text-l", pdf: "govdocs/pdf/pdfs/thread_100_100171.pdf", kind: "text-heavy, 1,283 pages, 3.4 MB"},
	{name: "pdf-image-s", pdf: "govdocs/pdf/pdfs/thread_000_000164.pdf", kind: "image-heavy, 13 bilevel scans, 2.6 MB"},
	{name: "pdf-image-m", pdf: "pdfjs/pdfs/issue11230.pdf", kind: "image-heavy, 139 pages of JPEG, 10.9 MB"},
	{name: "pdf-image-l", pdf: "pdfjs/pdfs/issue17061.pdf", kind: "image-heavy, 102 pages, 844 JPEG 2000 pictures, 27 MB"},
	{name: "pdf-objects-m", pdf: "pdfjs/pdfs/intelisa.pdf", kind: "many small objects, 424 pages, 33,000 objects, 2.5 MB"},
	{name: "pdf-tagged-s", pdf: "govdocs/pdf/pdfs/thread_400_400729.pdf", kind: "tagged, 33 pages, 123 KB"},
	{name: "pdf-tagged-m", pdf: "pdfjs/pdfs/ecma262.pdf", kind: "tagged, 258 pages, 18,000 objects, 3.1 MB"},
	{name: "pdf-tagged-l", pdf: "pdfjs/pdfs/pdf.pdf", kind: "tagged, 1,310 pages, 334,000 objects, 32 MB"},
	{name: "pdf-form-s", pdf: "pdfjs/pdfs/issue13132.pdf", kind: "tagged form, 4 pages, 124 fields, 238 KB"},
	{name: "pdf-form-m", pdf: "pdfjs/pdfs/issue14301.pdf", kind: "form, 20 pages, 660 fields, 1 MB"},
	{name: "pdf-form-l", pdf: "pdfjs/pdfs/issue18801.pdf", kind: "tagged form, 7 pages, 991 fields, 6.4 MB"},
}

// costPDFOps maps each PDF operation to the PDF inputs it runs on.
var costPDFOps = []struct {
	op     string
	inputs []string
}{
	{"text", []string{"pdf-text-s", "pdf-text-m", "pdf-text-l", "pdf-image-l", "pdf-objects-m", "pdf-tagged-l"}},
	{"rewrite", []string{"pdf-text-s", "pdf-text-m", "pdf-text-l", "pdf-image-l", "pdf-objects-m", "pdf-tagged-l"}},
	{"merge", []string{"pdf-text-s", "pdf-text-m", "pdf-text-l", "pdf-image-l", "pdf-tagged-m"}},
	{"split", []string{"pdf-text-s", "pdf-text-m", "pdf-text-l", "pdf-image-l", "pdf-tagged-l"}},
	{"render", []string{"pdf-text-l", "pdf-image-s", "pdf-image-m", "pdf-image-l", "pdf-tagged-l"}},
	{"pdfa", []string{"pdf-text-s", "pdf-text-m", "pdf-text-l", "pdf-image-l", "pdf-tagged-l"}},
	{"pdfua", []string{"pdf-tagged-s", "pdf-tagged-m", "pdf-tagged-l"}},
	{"remediate", []string{"pdf-text-s", "pdf-text-m", "pdf-text-l", "pdf-image-l", "pdf-tagged-m"}},
	{"flatten", []string{"pdf-form-s", "pdf-form-m", "pdf-form-l"}},
	{"stamp", []string{"pdf-text-s", "pdf-text-m", "pdf-text-l", "pdf-image-l"}},
	{"colors", []string{"pdf-text-s", "pdf-text-m", "pdf-text-l", "pdf-image-s", "pdf-image-m", "pdf-image-l"}},
}

// costRows returns every row of the set, conversions first, in a fixed
// order.
func costRows() []costRow {
	rows := []costRow{{op: "cold", input: "text-line"}}
	for _, in := range costGenerated() {
		if in.name != "text-line" {
			rows = append(rows, costRow{op: "convert", input: in.name})
		}
	}
	for _, o := range costPDFOps {
		for _, in := range o.inputs {
			rows = append(rows, costRow{op: o.op, input: in})
		}
	}
	return rows
}

// costInputs returns every input by name.
func costInputs() map[string]costInput {
	m := map[string]costInput{}
	for _, in := range costGenerated() {
		m[in.name] = in
	}
	for _, in := range costPDFs {
		m[in.name] = in
	}
	return m
}

// costSource returns the path convert-one reads for in: its directory under
// inputs for a generated input, its file under testdata for a PDF.
func costSource(in costInput, inputs, testdata string) string {
	if in.write == nil {
		return filepath.Join(testdata, filepath.FromSlash(in.pdf))
	}
	return filepath.Join(inputs, in.name)
}

// buildCostInputs writes every generated input of want missing under
// inputs, and reports each it writes to log. An input is present when its
// document is: a build cut short leaves no document, and is built again.
func buildCostInputs(inputs string, want []costInput, log func(string)) error {
	for _, in := range want {
		if in.write == nil {
			continue
		}
		dir := filepath.Join(inputs, in.name)
		if _, err := os.Stat(filepath.Join(dir, in.file)); err == nil {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		// The document is written last, so it stands for a finished build:
		// the pictures of an HTML page are written before the page.
		if err := in.write(dir); err != nil {
			return fmt.Errorf("%s: %w", in.name, err)
		}
		log(fmt.Sprintf("built %s", dir))
	}
	return nil
}
