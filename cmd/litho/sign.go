package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gotenberg/litho/pkg/pdf"
)

// signFlags holds the flags the sign command and the pipe's sign stage share.
type signFlags struct {
	cert, key, chain                *string
	reason, location, contact, name *string
	field, signingTime              *string
}

// registerSignFlags declares the signing flags on fs.
func registerSignFlags(fs *flag.FlagSet) signFlags {
	return signFlags{
		cert:        fs.String("cert", "", "PEM file holding the signing certificate, and optionally its chain (required)"),
		key:         fs.String("key", "", "PEM file holding the unencrypted private key: PKCS #8, PKCS #1 or SEC 1 (required)"),
		chain:       fs.String("chain", "", "PEM file holding the intermediate certificates"),
		reason:      fs.String("reason", "", "Reason for signing"),
		location:    fs.String("location", "", "Location of the signing"),
		contact:     fs.String("contact", "", "Contact information of the signer"),
		name:        fs.String("name", "", "Name of the signer, when the certificate's differs or is missing"),
		field:       fs.String("field", "", "Signature field: an unsigned one of that name is signed in place, else a new field takes the name (default: Signature1, ...)"),
		signingTime: fs.String("signing-time", "", "Signing time, RFC 3339 (default: now); fixes the output for reproducible builds"),
	}
}

// signer loads the signer the flags name.
func (f signFlags) signer() (pdf.Signer, error) {
	if *f.cert == "" || *f.key == "" {
		return nil, errors.New("--cert and --key are required")
	}
	certPEM, err := os.ReadFile(*f.cert)
	if err != nil {
		return nil, fmt.Errorf("reading the certificate: %w", err)
	}
	if *f.chain != "" {
		chainPEM, err := os.ReadFile(*f.chain)
		if err != nil {
			return nil, fmt.Errorf("reading the chain: %w", err)
		}
		certPEM = append(append(certPEM, '\n'), chainPEM...)
	}
	keyPEM, err := os.ReadFile(*f.key)
	if err != nil {
		return nil, fmt.Errorf("reading the private key: %w", err)
	}
	signer, err := pdf.ParseSignerPEM(certPEM, keyPEM)
	if errors.Is(err, pdf.ErrSignerKeyEncrypted) {
		return nil, fmt.Errorf("%w: litho reads unencrypted keys only; decrypt it first, for instance with `openssl pkey -in %s -out key.pem`", err, *f.key)
	}
	if err != nil {
		return nil, fmt.Errorf("loading the signer: %w", err)
	}
	return signer, nil
}

// options returns the [pdf.SignOption] values the flags set.
func (f signFlags) options() ([]pdf.SignOption, error) {
	opts := []pdf.SignOption{
		pdf.WithSignatureReason(*f.reason),
		pdf.WithSignatureLocation(*f.location),
		pdf.WithSignatureContactInfo(*f.contact),
		pdf.WithSignerName(*f.name),
		pdf.WithSignatureField(*f.field),
	}
	if *f.signingTime != "" {
		t, err := time.Parse(time.RFC3339, *f.signingTime)
		if err != nil {
			return nil, fmt.Errorf("--signing-time: %w", err)
		}
		opts = append(opts, pdf.WithSigningTime(t))
	}
	return opts, nil
}

// encryptedSignRefusal explains why an encrypted document is not signed.
// The signature of an encrypted document is written in that document, its
// signature dictionary's strings encrypted under the document's key (ISO
// 32000-1, 7.6.1), an update litho cannot write (see [pdf.Writer.Sign]).
const encryptedSignRefusal = "litho cannot sign an encrypted document: its signature dictionary would have to be encrypted under the document's key (ISO 32000-1, 7.6.1). Decrypt it first (litho decrypt), then sign the output"

func runSign(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	flags := registerSignFlags(fs)

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || *flags.cert == "" || *flags.key == "" {
		fmt.Fprintln(stderr, "Usage: litho sign <file> --cert cert.pem --key key.pem [--chain chain.pem] [--reason R] [--location L] [--contact C] [--name N] [--field F] [--signing-time T] -o out.pdf")
		return exitError
	}

	signer, err := flags.signer()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	opts, err := flags.options()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	// Refused before the output file exists. WriteTo refuses it too, with
	// ErrSignFullRewriteIncompatible, which names the encryption and not the
	// remedy.
	if r.IsEncrypted() {
		fmt.Fprintln(stderr, encryptedSignRefusal)
		return exitError
	}

	w := pdf.NewWriter(r)
	defer releaseWriter(w, stderr)
	w.Sign(signer, opts...)

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := w.WriteTo(ctx, dst); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "signing: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	return exitOK
}
