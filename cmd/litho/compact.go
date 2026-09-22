package main

import (
	"flag"

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
