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

// warnEncryptionDropped reports on stderr that an encrypted input is about to
// become an unencrypted output. encrypting says whether the command was asked
// to encrypt, in which case there is nothing to warn about.
//
// The reader decrypts every object as it resolves it, so a page operation
// copies a plaintext object graph and the output carries no /Encrypt dictionary
// unless one is requested. That is the documented behaviour, not a defect, but
// it is a confidentiality downgrade the user cannot see: they supplied a
// password to READ the file and got an unprotected one back. Only the silence
// is fixed here; the default is deliberate.
//
// Variadic because a merge has several inputs and an insert has two, and any
// encrypted input means the output loses protection the input had.
//
// This is the form for a command that has no encryption flag of its own, which
// is fourteen of the seventeen emitters. One message used to advise "Pass
// --user-password to encrypt the output" for every one of them. That was true
// of the five page operations it was written for, and became wrong when
// `bbed371f` widened the warning to every output-producing command: those
// fourteen reject the flag with "flag provided but not defined", so the warning
// answered a confidentiality downgrade with a command that fails. Advice a
// command refuses is worse than no advice.
//
// README.md already documents three remedies, one per family. The two variants
// below carry the other two, so the message a caller sees and the manual agree.
func warnEncryptionDropped(stderr io.Writer, encrypting bool, readers ...*pdf.Reader) {
	warnEncryptionDroppedWithRemedy(stderr, encrypting,
		"Run litho encrypt on the result to protect it again.", readers...)
}

// warnEncryptionDroppedOfferingPassword is the form for split and merge, the
// only commands that accept --user-password directly.
//
// Their encrypting argument is true exactly when a password was supplied, in
// which case the warning never fires, so naming the flag reaches only the
// caller who has it available and did not use it.
func warnEncryptionDroppedOfferingPassword(stderr io.Writer, encrypting bool, readers ...*pdf.Reader) {
	warnEncryptionDroppedWithRemedy(stderr, encrypting,
		"Pass --user-password to encrypt the output, or run litho encrypt on the result.", readers...)
}

// warnEncryptionDroppedAddingEncryptStage is the form for pipe, whose
// --user-password belongs to its encrypt STAGE rather than to pipe itself:
// `litho pipe --user-password X` is rejected, `litho pipe ... encrypt
// --user-password X` is not.
//
// Symmetric to the split and merge variant. Pipe sets encryptsOutput when an
// encrypt stage is present and the warning then returns early, so naming the
// stage reaches only the caller who omitted it.
func warnEncryptionDroppedAddingEncryptStage(stderr io.Writer, encrypting bool, readers ...*pdf.Reader) {
	warnEncryptionDroppedWithRemedy(stderr, encrypting,
		"Add an encrypt stage, or run litho encrypt on the result.", readers...)
}

// warnEncryptionDroppedWithRemedy carries the shared body. remedy completes the
// sentence and must name only what the calling command actually accepts.
func warnEncryptionDroppedWithRemedy(stderr io.Writer, encrypting bool, remedy string, readers ...*pdf.Reader) {
	if encrypting {
		return
	}
	for _, r := range readers {
		if r == nil || !r.IsEncrypted() {
			continue
		}
		fmt.Fprintln(stderr, "warning: the input is encrypted but the output will not be. "+remedy)
		return
	}
}

func runEncrypt(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("encrypt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	userPwd := fs.String("user-password", "", "User password")
	ownerPwd := fs.String("owner-password", "", "Owner password")
	algorithm := fs.String("algorithm", "aes256", "Encryption algorithm: rc4-128, aes128, aes256")
	compact := registerCompactFlag(fs)

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" {
		fmt.Fprintln(stderr, "Usage: litho encrypt <file> --user-password X [--owner-password Y] [--algorithm aes256] [--compact] -o out.pdf")
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

	// This command always encrypts its output, so asking to compact it is
	// always a request the write engine will refuse.
	warnCompactSkipped(stderr, *compact, true)

	w := pdf.NewWriter(r)
	defer releaseWriter(w, stderr)
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

	if err := w.WriteTo(ctx, dst, compactWriteOptions(*compact)...); err != nil {
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
