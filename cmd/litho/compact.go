package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// registerCompactFlag adds -compact to fs and returns the bound value.
//
// Object-stream packing, stream dedup and image recompression all run on the
// full-rewrite path.
// [pdf.WithObjectStreams] states the rule: an incremental update appends to
// the original bytes and leaves their packing untouched, and the incremental
// write config carries no compaction field to honor in the first place. A
// command that never rewrites must therefore not offer the flag at all, which
// is why rotate, set-metadata and autotag do not register it.
//
// Two commands sit between those cases. pipe and remediate rewrite the whole
// file for most inputs and append an incremental update for the rest, so the
// flag is real for them but conditional. Remediating a document that needs no
// structural repair leaves the write incremental, and the output is then
// byte-identical with the flag and without it. Refusing the flag there would
// deny compaction to every input that does rewrite, so the help text states
// the condition rather than promising a result the command cannot always
// deliver. [pdf.Writer.WriteTo] logs the ignored option at Debug when it
// happens.
func registerCompactFlag(fs *flag.FlagSet) *bool {
	return fs.Bool("compact", false,
		"Shrink the output losslessly by packing objects into object streams, merging "+
			"duplicate streams and recompressing Flate, LZW, RunLength and uncompressed images, "+
			"when the command rewrites the whole file")
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

// compactForbiddenByProfile reports whether packing would cost the named
// compliance profile its conformance.
//
// ISO 19005-1, 6.1.4 forbids cross-reference streams in PDF/A-1 outright, and
// packing requires one: a traditional table has no type field and cannot name
// a compressed object. Every other profile litho remediates to permits both
// constructs. Measured on one file carrying an object stream and a
// cross-reference stream against the same file without them, PDF/A-1b gains
// exactly one violation, clause 6.1.4, while PDF/A-2b, PDF/A-3b and PDF/UA-1
// gain none.
//
// Factur-X and the ZUGFeRD variants remediate to a PDF/A-3 base whatever
// generation the variant names, so none of them is affected. An unknown
// profile is not forbidden here: [runRemediate] rejects it before the write,
// and answering "forbidden" would silence compaction for a typo.
func compactForbiddenByProfile(profile string) bool {
	p, ok := pdfaProfiles[profile]
	if !ok {
		return false
	}
	return p == models.PDFA1a || p == models.PDFA1b
}

// warnCompactProfileRefused reports that --compact was asked for and will not
// happen, because packing would cost the target profile its conformance.
//
// Ignoring the flag silently would hand back a larger file with no reason
// given. Honoring it silently would be worse: the caller would receive a
// smaller file that no longer conforms to the profile they asked the command
// to produce, which defeats remediation entirely.
//
// A warning rather than an error, matching [warnCompactSkipped]: the command
// still produced the conformant document it was asked for, only a larger one.
func warnCompactProfileRefused(stderr io.Writer, compact bool, profile string) {
	if !compact || !compactForbiddenByProfile(profile) {
		return
	}
	fmt.Fprintf(stderr, "warning: --compact does not apply to %s, so the output is not compacted. "+
		"ISO 19005-1, 6.1.4 forbids the cross-reference stream that object streams require.\n", profile)
}

// compactWriteOptions returns the write options -compact selects, or none when
// it was not passed.
//
// One flag drives the three passes because they are independently safe and
// their savings add up rather than overlap. Callers wanting one without the
// others use [pdf.WithObjectStreams], [pdf.WithDedup] and
// [pdf.WithImageRecompression] directly.
//
// No pass can enlarge a file or damage one: packing declines any batch it
// cannot shrink, recompression keeps a re-encoding only when it is smaller
// and decodes to the same samples, and all three refuse outright on an
// encrypted output, where per-object encryption keys make them unsafe.
//
// # Corpus evidence
//
//   - All corpora, 37 files: packing saves 6.25% and dedup 0.76% against the
//     same rewrite without them, and 7.00% together.
func compactWriteOptions(compact bool) []pdf.WriteOption {
	if !compact {
		return nil
	}
	return []pdf.WriteOption{pdf.WithObjectStreams(), pdf.WithDedup(), pdf.WithImageRecompression()}
}
