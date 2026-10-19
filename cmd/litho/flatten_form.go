package main

import (
	"context"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runFlattenForm(ctx context.Context, args []string, _ io.Writer, stderr io.Writer) int {
	return runSimpleMutation(ctx, args, stderr, "flatten-form",
		"Usage: litho flatten-form <file> [--compact] [--recompress-images] [--subset-fonts] -o out.pdf",
		func(w *pdf.Writer) { w.FlattenForm() },
		withCompactFlag())
}
