package pdf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// ExampleWithFontFiles remediates a document that shows Helvetica and a
// custom font without embedding either. The font directory holds a program
// named LiberationSans, which stands for Helvetica, and nothing for the custom
// font, so that one is refused. The directory may as well be
// os.DirFS("/usr/share/fonts") or an embed.FS.
func ExampleWithFontFiles() {
	ctx := context.Background()

	r, err := pdf.Open(ctx, bytes.NewReader(unembeddedFontsPDF), int64(len(unembeddedFontsPDF)))
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	fonts := os.DirFS("testdata/fonts")

	w := pdf.NewWriter(r)
	defer w.Close()
	report, err := w.RemediatePDFA(ctx, models.PDFA2b,
		pdf.WithFontFiles(fonts),
		// Pins the trailer /ID, so two runs write the same bytes.
		pdf.WithDocumentID([]byte("0123456789abcdef")),
	)
	if err != nil {
		log.Fatal(err)
	}
	for _, fix := range report.Fixed {
		if strings.HasPrefix(fix, "embedded LiberationSans (subset") {
			fmt.Println("Helvetica embedded as a LiberationSans subset")
		}
	}
	for _, refusal := range report.FontRefusals {
		fmt.Printf("%s left unembedded, not supplied: %t\n",
			refusal.BaseFont, errors.Is(refusal.Err, pdf.ErrFontNotSupplied))
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		log.Fatal(err)
	}
	// Output:
	// Helvetica embedded as a LiberationSans subset
	// LithoCustom left unembedded, not supplied: true
}

// ExampleWithSystemFonts embeds the unembedded fonts from the operating
// system's fonts, after the programs of a directory the caller controls. It
// has no Output line: what it embeds depends on the fonts of the host.
func ExampleWithSystemFonts() {
	ctx := context.Background()

	r, err := pdf.Open(ctx, bytes.NewReader(unembeddedFontsPDF), int64(len(unembeddedFontsPDF)))
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	w := pdf.NewWriter(r)
	defer w.Close()
	report, err := w.RemediatePDFA(ctx, models.PDFA2b,
		pdf.WithFontFiles(os.DirFS("testdata/fonts")),
		pdf.WithSystemFonts(),
	)
	if err != nil {
		log.Fatal(err)
	}
	for _, refusal := range report.FontRefusals {
		fmt.Println(refusal)
	}
}

// unembeddedFontsPDF is one page showing "AB" in Helvetica and in LithoCustom,
// neither embedded.
var unembeddedFontsPDF = func() []byte {
	const content = "BT /F1 12 Tf 72 700 Td (AB) Tj /F2 12 Tf 0 -20 Td (AB) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /LithoCustom /FirstChar 65 /LastChar 66 /Widths [600 600]" +
			" /FontDescriptor << /Type /FontDescriptor /FontName /LithoCustom /Flags 32 /FontBBox [0 0 1000 800]" +
			" /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >> >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}()
