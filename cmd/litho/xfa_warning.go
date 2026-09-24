package main

import (
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

// warnXFADropped reports on stderr that XFA form data in an input will not
// reach the output.
//
// The page operations copy widgets and their field tree; they carry no /XFA,
// the XML form description of ISO 32000-1, 12.7.8. A merge or a split leaves
// a dynamic XFA document with no form at all, because it keeps its fields
// only in the XFA packets. A hybrid, carrying both /XFA and real widgets,
// keeps the widgets and loses the XFA layer.
//
// Dropping it is the defensible part: two XFA templates cannot be reconciled
// into one, so there is no merge semantics to implement. Only the silence is
// fixed here, exactly as [warnEncryptionDropped] fixes the silence of the
// confidentiality downgrade without changing that default either.
//
// Unlike that warning this one names no remedy, because none exists: no flag
// preserves XFA through a rebuild. What it buys the caller is the chance to
// stop, since the output is not the equivalent document they asked for. A
// warning with no remedy is worth printing when the alternative is losing a
// whole form in silence.
//
// Variadic over readers because a merge has several inputs and any one of them
// carrying XFA means the output is missing form data the input had.
//
// # Corpus evidence
//
//   - All corpora, every XFA-bearing file litho can open: a merge or a split
//     leaves 19 of 23 with no form at all, and one of them loses 1455 fields.
func warnXFADropped(ctx context.Context, stderr io.Writer, readers ...*pdf.Reader) {
	for _, r := range readers {
		if r == nil || !r.HasXFAForm(ctx) {
			continue
		}
		fmt.Fprintln(stderr, "warning: the input carries XFA form data, which the output will not. "+
			"Widget fields survive the rebuild; the XFA layer does not, so a dynamic XFA form loses every field.")
		return
	}
}
