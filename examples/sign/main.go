// Command sign signs a PDF with a certificate and a private key read from PEM
// files, as a PAdES baseline signature appended in an incremental update.
//
//	go run ./examples/sign cert.pem key.pem document.pdf signed.pdf
package main

import (
	"context"
	"errors"
	"log"
	"os"

	"github.com/gotenberg/litho/pkg/pdf"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 4 {
		return errors.New("usage: sign cert.pem key.pem document.pdf signed.pdf")
	}

	certPEM, err := os.ReadFile(args[0]) //nolint:gosec // The certificate path is the example's own argument.
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(args[1]) //nolint:gosec // The key path is the example's own argument.
	if err != nil {
		return err
	}
	signer, err := pdf.ParseSignerPEM(certPEM, keyPEM)
	if err != nil {
		return err
	}

	r, err := pdf.OpenFile(ctx, args[2])
	if err != nil {
		return err
	}
	defer r.Close()

	w := pdf.NewWriter(r)
	defer w.Close()
	w.Sign(signer, pdf.WithPAdES(), pdf.WithSignatureReason("Approved"))

	out, err := os.Create(args[3]) //nolint:gosec // The output path is the example's own argument.
	if err != nil {
		return err
	}
	err = w.WriteTo(ctx, out)
	return errors.Join(err, out.Close())
}
