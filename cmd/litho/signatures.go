package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/models"
)

type signaturesResult struct {
	File        string                       `json:"file"`
	Signatures  []models.Signature           `json:"signatures,omitempty"`
	Validations []models.SignatureValidation `json:"validations,omitempty"`
}

func runSignatures(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("signatures", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	verify := fs.Bool("verify", false, "Verify signatures against system root certificates")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho signatures [-f json] [-p password] [--verify] <file> [file...]")
		return exitError
	}

	var results []signaturesResult
	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		res := signaturesResult{File: path}

		if *verify {
			validations, err := r.VerifySignatures(ctx, nil)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
			res.Validations = validations
		} else {
			sigs, err := r.ExtractSignatures(ctx)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
			res.Signatures = sigs
		}

		results = append(results, res)
		closer()
	}

	switch *format {
	case "json":
		if len(results) == 1 {
			return jsonOut(stdout, stderr, results[0])
		}
		return jsonOut(stdout, stderr, results)
	default:
		for _, res := range results {
			if len(results) > 1 {
				fmt.Fprintf(stdout, "--- %s ---\n", res.File)
			}
			if res.Validations != nil {
				if len(res.Validations) == 0 {
					fmt.Fprintln(stdout, "(no signatures)")
				}
				for _, v := range res.Validations {
					fmt.Fprintf(stdout, "Field:       %s\n", v.FieldName)
					fmt.Fprintf(stdout, "Signer:      %s\n", v.SignerCN)
					fmt.Fprintf(stdout, "Issuer:      %s\n", v.Issuer)
					fmt.Fprintf(stdout, "Hash valid:  %t\n", v.HashValid)
					fmt.Fprintf(stdout, "Chain valid: %t\n", v.CertChainValid)
					fmt.Fprintf(stdout, "Not before:  %s\n", v.NotBefore)
					fmt.Fprintf(stdout, "Not after:   %s\n", v.NotAfter)
					if v.SigningTime != "" {
						fmt.Fprintf(stdout, "Signed at:   %s\n", v.SigningTime)
					}
					if v.ErrorMessage != "" {
						fmt.Fprintf(stdout, "Error:       %s\n", v.ErrorMessage)
					}
					fmt.Fprintln(stdout)
				}
			} else {
				if len(res.Signatures) == 0 {
					fmt.Fprintln(stdout, "(no signatures)")
				}
				for _, s := range res.Signatures {
					fmt.Fprintf(stdout, "Field:       %s\n", s.FieldName)
					fmt.Fprintf(stdout, "Byte range:  [%d, %d, %d, %d]\n", s.ByteRange[0], s.ByteRange[1], s.ByteRange[2], s.ByteRange[3])
					if s.SigningTime != "" {
						fmt.Fprintf(stdout, "Signed at:   %s\n", s.SigningTime)
					}
					fmt.Fprintln(stdout)
				}
			}
		}
	}
	return exitOK
}
