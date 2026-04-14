package main

import (
	"context"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runDecrypt(ctx context.Context, args []string, _ io.Writer, stderr io.Writer) int {
	return runSimpleMutation(ctx, args, stderr, "decrypt",
		"Usage: litho decrypt <file> -p PASSWORD -o out.pdf",
		func(w *pdf.Writer) { w.RemoveEncryption() })
}
