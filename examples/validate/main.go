// Command validate checks a PDF against PDF/A-2b and prints each violation
// with its clause, test number and severity. It exits 2 when the document
// does not conform.
//
//	go run ./examples/validate document.pdf
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
	conforms, err := run(context.Background(), os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	if !conforms {
		os.Exit(2)
	}
}

func run(ctx context.Context, args []string) (bool, error) {
	if len(args) != 1 {
		return false, errors.New("usage: validate document.pdf")
	}

	r, err := pdf.OpenFile(ctx, args[0])
	if err != nil {
		return false, err
	}
	defer r.Close()

	violations, err := r.ValidatePDFAReport(ctx, models.PDFA2b)
	if err != nil {
		return false, err
	}
	for _, v := range violations {
		fmt.Printf("[%s] clause %s test %d: %s\n", v.Severity, v.Clause, v.TestNumber, v.Description)
	}
	return len(violations) == 0, nil
}
