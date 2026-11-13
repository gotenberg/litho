package pdf_test

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// ExampleWithHeader draws a running title and the page number on every page
// but the first, and a footer on the last page alone.
func ExampleWithHeader() {
	src := `<html><head><title>Quarterly report</title></head><body>` +
		`<h1>Revenue</h1><p>Up.</p><h1 style="break-before: page">Costs</h1><p>Down.</p></body></html>`
	var out bytes.Buffer
	report, err := pdf.FromHTML(strings.NewReader(src),
		pdf.WithConvertFontDirs("testdata/fonts"),
		pdf.WithMargins(72, 54, 72, 54),
		// A header on every page, then none on the first.
		pdf.WithHeader(pdf.PageBand{
			HTML: `<div class="row"><span class="title"></span><span class="page"></span></div>`,
			CSS: `.row { display: flex; justify-content: space-between; font-size: 9pt }
			      .title::after { content: string(litho-title) }
			      .page::after { content: "Page " counter(page) " of " counter(pages) }`,
		}),
		pdf.WithHeader(pdf.PageBand{Pages: pdf.FirstPage()}),
		pdf.WithFooter(pdf.PageBand{HTML: `<p style="font-size: 8pt">Confidential</p>`, Pages: pdf.LastPage()}),
	).WriteTo(context.Background(), &out)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(report.Pages, "pages")
	// Output:
	// 2 pages
}
