package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/pdf"
)

// signFlags holds the flags the sign command and the pipe's sign stage share.
type signFlags struct {
	cert, key, chain                *string
	reason, location, contact, name *string
	field, signingTime              *string

	p12, p12PasswordFile, p12PasswordEnv *string

	pades padesFlags
}

// registerSignFlags declares the signing flags on fs.
func registerSignFlags(fs *flag.FlagSet) signFlags {
	return signFlags{
		cert:        fs.String("cert", "", "PEM file holding the signing certificate, and optionally its chain (required without --p12)"),
		key:         fs.String("key", "", "PEM file holding the unencrypted private key: PKCS #8, PKCS #1 or SEC 1 (required without --p12)"),
		chain:       fs.String("chain", "", "PEM file holding the intermediate certificates"),
		reason:      fs.String("reason", "", "Reason for signing"),
		location:    fs.String("location", "", "Location of the signing"),
		contact:     fs.String("contact", "", "Contact information of the signer"),
		name:        fs.String("name", "", "Name of the signer, when the certificate's differs or is missing"),
		field:       fs.String("field", "", "Signature field: an unsigned one of that name is signed in place, else a new field takes the name (default: Signature1, ...)"),
		signingTime: fs.String("signing-time", "", "Signing time, RFC 3339 (default: now); fixes the output for reproducible builds"),

		// The password is never a flag value: a command line shows in the
		// process list and the shell history.
		p12:             fs.String("p12", "", "PKCS #12 (.p12, .pfx) key store holding the private key, its certificate and chain; replaces --cert, --key and --chain"),
		p12PasswordFile: fs.String("p12-password-file", "", "File whose first line is the --p12 password (default: the empty password)"),
		p12PasswordEnv:  fs.String("p12-password-env", "", "Environment variable holding the --p12 password"),

		pades: registerPAdESFlags(fs),
	}
}

// hasKeyMaterial reports whether the flags name a key and a certificate:
// --cert and --key, or --p12.
func (f signFlags) hasKeyMaterial() bool {
	return *f.p12 != "" || (*f.cert != "" && *f.key != "")
}

// signer loads the signer the flags name.
func (f signFlags) signer() (pdf.Signer, error) {
	if *f.p12 != "" {
		return f.pkcs12Signer()
	}
	if *f.p12PasswordFile != "" || *f.p12PasswordEnv != "" {
		return nil, errors.New("--p12-password-file and --p12-password-env need --p12")
	}
	if *f.cert == "" || *f.key == "" {
		return nil, errors.New("--cert and --key, or --p12, are required")
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

// pkcs12Signer loads the signer of the --p12 key store.
func (f signFlags) pkcs12Signer() (pdf.Signer, error) {
	if *f.cert != "" || *f.key != "" || *f.chain != "" {
		return nil, errors.New("--p12 replaces --cert, --key and --chain: the key store holds the key, its certificate and chain")
	}
	password, err := f.pkcs12Password()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(*f.p12)
	if err != nil {
		return nil, fmt.Errorf("reading the key store: %w", err)
	}
	signer, err := pdf.ParseSignerPKCS12(data, password)
	if errors.Is(err, pdf.ErrSignerPKCS12Password) {
		return nil, fmt.Errorf("%w: pass the key store's password with --p12-password-file or --p12-password-env", err)
	}
	if err != nil {
		return nil, fmt.Errorf("loading the signer from %s: %w", *f.p12, err)
	}
	return signer, nil
}

// pkcs12Password returns the --p12 password: the first line of
// --p12-password-file, the value of the --p12-password-env variable, or the
// empty password without either.
func (f signFlags) pkcs12Password() (string, error) {
	switch {
	case *f.p12PasswordFile != "" && *f.p12PasswordEnv != "":
		return "", errors.New("--p12-password-file and --p12-password-env are exclusive")
	case *f.p12PasswordFile != "":
		data, err := os.ReadFile(*f.p12PasswordFile)
		if err != nil {
			return "", fmt.Errorf("reading the key store password: %w", err)
		}
		line, _, _ := strings.Cut(string(data), "\n")
		return strings.TrimSuffix(line, "\r"), nil
	case *f.p12PasswordEnv != "":
		password, ok := os.LookupEnv(*f.p12PasswordEnv)
		if !ok {
			return "", fmt.Errorf("--p12-password-env: %s is not set", *f.p12PasswordEnv)
		}
		return password, nil
	}
	return "", nil
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
	return append(opts, f.pades.options()...), nil
}

// encryptedSignRefusal explains why an encrypted document is not signed.
// The signature of an encrypted document is written in that document, its
// signature dictionary's strings encrypted under the document's key (ISO
// 32000-1, 7.6.1), an update litho cannot write (see [pdf.Writer.Sign]).
const encryptedSignRefusal = "litho cannot sign an encrypted document: its signature dictionary would have to be encrypted under the document's key (ISO 32000-1, 7.6.1). Decrypt it first (litho decrypt), then sign the output"

func runSign(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultPageSize := registerDefaultPageSizeFlag(fs)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	flags := registerSignFlags(fs)

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || !flags.hasKeyMaterial() {
		fmt.Fprintln(stderr, "Usage: litho sign <file> (--cert cert.pem --key key.pem [--chain chain.pem] | --p12 id.p12 [--p12-password-file F | --p12-password-env VAR]) [--reason R] [--location L] [--contact C] [--name N] [--field F] [--signing-time T] [--pades] [--tsa-url URL] -o out.pdf")
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

	w := pdf.NewWriter(r, defaultPageSize.writeOptions()...)
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
