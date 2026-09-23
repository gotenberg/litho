package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runAttach(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	name := fs.String("name", "", "Attachment name as stored in the PDF (required)")
	dataPath := fs.String("data", "", "Path to the file to attach (required)")
	mime := fs.String("mime", "", "MIME type (auto-detected from extension if omitted)")
	relationship := fs.String("relationship", "", "AFRelationship for PDF/A-3+ associated files (Source, Data, Alternative, Supplement, EncryptedPayload, FormData, Schema, Unspecified)")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || *name == "" || *dataPath == "" {
		fmt.Fprintln(stderr, "Usage: litho attach <file> --name \"data.csv\" --data data.csv [--mime text/csv] [--relationship Alternative] -o out.pdf")
		return exitError
	}

	data, err := os.ReadFile(*dataPath)
	if err != nil {
		fmt.Fprintf(stderr, "reading attachment file: %v\n", err)
		return exitError
	}

	mimeType := *mime
	if mimeType == "" {
		mimeType = mimeFromExt(*dataPath)
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	// This command has no encryption flag, so it can never be encrypting.
	warnEncryptionDropped(stderr, false, r)

	w := pdf.NewWriter(r)
	if *relationship != "" {
		w.AddAssociatedFile(pdf.AssociatedFile{
			Name:         *name,
			Data:         data,
			MimeType:     mimeType,
			Relationship: *relationship,
		})
	} else {
		w.AddAttachment(*name, data, mimeType)
	}

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
