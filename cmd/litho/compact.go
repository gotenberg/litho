package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

// registerCompactFlag adds -compact to fs and returns the bound value.
//
// Only commands that always rewrite the whole file register it. Object-stream
// packing and stream dedup both run on the full-rewrite path, so offering the
// flag on a command that appends an incremental update would advertise an
// option that silently does nothing. That rules out rotate, set-metadata and
// autotag, none of which forces a rewrite.
func registerCompactFlag(fs *flag.FlagSet) *bool {
	return fs.Bool("compact", false,
		"Shrink the output by packing objects into object streams and merging duplicate streams")
}

// warnCompactSkipped reports that --compact was asked for and will not happen,
// because the output is encrypted.
//
// The write engine refuses to compact an encrypted document: encryption runs
// over every object before serialization, so packing afterwards would place
// ciphertext strings inside a stream that is encrypted again, which
// ISO 32000-1, 7.5.7 forbids, and dedup would compare ciphertext keyed per
// object number and find nothing. That refusal is correct, but silent, and a
// caller who passed the flag would otherwise receive an uncompacted file with
// no indication why.
//
// A warning rather than an error, matching [warnEncryptionDropped]: the
// command still produces a correct document, only a larger one.
func warnCompactSkipped(stderr io.Writer, compact, encrypting bool) {
	if !compact || !encrypting {
		return
	}
	fmt.Fprintln(stderr, "warning: --compact does not apply to an encrypted output, so the output is not compacted. "+
		"Compact the document first, then encrypt the result.")
}

// compactWriteOptions returns the write options -compact selects, or none when
// it was not passed.
//
// One flag drives both passes because they are independently safe and their
// savings add up rather than overlap: measured across 37 corpus files, packing
// saves 6.25% and dedup 0.76% against the same rewrite without them, 7.00%
// together. Callers wanting one without the other use [pdf.WithObjectStreams]
// and [pdf.WithDedup] directly.
//
// Neither pass can enlarge a file or damage one: packing declines any batch it
// cannot shrink, and both refuse outright on an encrypted output, where
// per-object encryption keys make merging and packing unsafe.
func compactWriteOptions(compact bool) []pdf.WriteOption {
	if !compact {
		return nil
	}
	return []pdf.WriteOption{pdf.WithObjectStreams(), pdf.WithDedup()}
}
