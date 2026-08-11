package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

var encryptAlgorithms = map[string]pdf.EncryptAlgorithm{
	"rc4-128": pdf.EncryptRC4_128,
	"aes128":  pdf.EncryptAES128,
	"aes256":  pdf.EncryptAES256,
}

func runEncrypt(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("encrypt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	userPwd := fs.String("user-password", "", "User password")
	ownerPwd := fs.String("owner-password", "", "Owner password")
	algorithm := fs.String("algorithm", "aes256", "Encryption algorithm: rc4-128, aes128, aes256")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	if fs.NArg() < 1 || *output == "" {
		fmt.Fprintln(stderr, "Usage: litho encrypt <file> --user-password X [--owner-password Y] [--algorithm aes256] -o out.pdf")
		return exitError
	}

	algo, ok := encryptAlgorithms[*algorithm]
	if !ok {
		fmt.Fprintf(stderr, "unknown algorithm %q. Valid: rc4-128, aes128, aes256\n", *algorithm)
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	w := pdf.NewWriter(r)
	w.Encrypt(pdf.EncryptConfig{
		UserPassword:  *userPwd,
		OwnerPassword: *ownerPwd,
		Algorithm:     algo,
		Permissions:   -1,
	})

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := w.WriteTo(ctx, dst); err != nil {
		closeDst() //nolint:errcheck // already returning an error; a close failure would mask its cause
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	return exitOK
}
