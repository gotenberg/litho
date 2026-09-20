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

// encryptWriteOptions builds the write options that encrypt a command's
// output, or none when no password was supplied. The bool reports whether the
// flags were usable: an unknown algorithm is a usage error, not a silent
// fallback to writing the output in the clear.
//
// Either password alone is enough to ask for encryption. An empty user
// password with an owner password set is the standard "opens without a
// password, but permissions apply" document of ISO 32000-1, 7.6.3.
func encryptWriteOptions(userPwd, ownerPwd, algorithm string, stderr io.Writer) ([]pdf.WriteOption, bool) {
	if userPwd == "" && ownerPwd == "" {
		return nil, true
	}

	algo, ok := encryptAlgorithms[algorithm]
	if !ok {
		fmt.Fprintf(stderr, "unknown algorithm %q. Valid: rc4-128, aes128, aes256\n", algorithm)
		return nil, false
	}

	return []pdf.WriteOption{pdf.WithEncryption(pdf.EncryptConfig{
		UserPassword:  userPwd,
		OwnerPassword: ownerPwd,
		Algorithm:     algo,
		Permissions:   -1,
	})}, true
}

func runEncrypt(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
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
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	return exitOK
}
