package main

import (
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

// signatureDropReason completes each line [warnSignaturesDropped] prints.
const signatureDropReason = "it covers bytes of the input that the output rewrites (ISO 32000-1, 12.8.1)"

// warnSignaturesDropped reports on stderr, one line each, the signatures of
// the input that the output does not carry.
//
// A full rewrite, a split and a merge build a new file, so a signature of the
// input covers bytes the output does not hold and could never verify there.
// litho draws each signed widget's appearance into its page and leaves the
// signature out (see [pdf.DroppedSignature]), and the command succeeds. Without
// this line the caller got an unsigned document back from a signed one with no
// word of it. An incremental update keeps every signature, so a command that
// writes one prints nothing.
//
// Call it only once the output is written: the library reports what the write
// left out.
func warnSignaturesDropped(stderr io.Writer, dropped []pdf.DroppedSignature) {
	for _, sig := range dropped {
		fmt.Fprintf(stderr, "warning: the output does not carry %s: %s\n", describeSignature(sig), signatureDropReason)
	}
}

// describeSignature names a dropped signature for [warnSignaturesDropped].
func describeSignature(sig pdf.DroppedSignature) string {
	kind := "signature"
	switch sig.Perms {
	case "DocMDP":
		kind = "certification signature"
	case "UR3":
		kind = "usage rights signature"
	}
	if sig.Field == "" {
		if sig.Perms == "" {
			return "the " + kind + " of an unnamed field"
		}
		return "the " + kind
	}
	return fmt.Sprintf("%s %q", kind, sig.Field)
}
