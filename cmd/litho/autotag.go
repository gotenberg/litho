package main

import (
	"context"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runAutotag(ctx context.Context, args []string, _ io.Writer, stderr io.Writer) int {
	return runSimpleMutation(ctx, args, stderr, "autotag",
		"Usage: litho autotag <file> -o out.pdf",
		func(w *pdf.Writer) { w.AutoTag() })
}
