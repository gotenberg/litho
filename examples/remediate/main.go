// Command remediate repairs a PDF for PDF/A-2b and writes the result.
//
// litho ships no font program, so it embeds a font the document leaves
// unembedded only from the programs given to it: here, every font file under
// fontdir. The trailer /ID is pinned so two runs write identical bytes.
//
//	go run ./examples/remediate document.pdf fontdir out.pdf
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 3 {
		return errors.New("usage: remediate document.pdf fontdir out.pdf")
	}

	r, err := pdf.OpenFile(ctx, args[0])
	if err != nil {
		return err
	}
	defer r.Close()

	w := pdf.NewWriter(r)
	defer w.Close()

	report, err := w.RemediatePDFA(ctx, models.PDFA2b,
		pdf.WithFontFiles(os.DirFS(args[1])),
		pdf.WithDocumentID([]byte("0123456789abcdef")),
	)
	if err != nil {
		return err
	}
	for _, fix := range report.Fixed {
		fmt.Println("fixed:", fix)
	}
	for _, refusal := range report.FontRefusals {
		fmt.Println("font left unembedded:", refusal)
	}
	for _, v := range report.ResidualViolations() {
		fmt.Println("residual:", v)
	}

	out, err := os.Create(args[2]) //nolint:gosec // The output path is the example's own argument.
	if err != nil {
		return err
	}
	err = w.WriteTo(ctx, out)
	return errors.Join(err, out.Close())
}
