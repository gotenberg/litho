package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// compaction is what the compaction flags select: -compact, -recompress-images
// and -subset-fonts.
type compaction struct {
	compact, recompressImages, subsetFonts bool
}

// compactionFlags holds the values [registerCompactFlags] binds.
type compactionFlags struct {
	compact, recompressImages, subsetFonts *bool
}

// get returns what the parsed flags select.
func (f compactionFlags) get() compaction {
	return compaction{compact: *f.compact, recompressImages: *f.recompressImages, subsetFonts: *f.subsetFonts}
}

// registerCompactFlags adds -compact, -recompress-images and -subset-fonts to
// fs and returns their bound values. The three are registered together so that
// every command offering one offers the others, and are independent: any may
// be given alone or with the others.
//
// -recompress-images stays out of -compact by decision. Recompressing images
// at zlib's best level costs seconds on an image-heavy file that otherwise
// writes in milliseconds, too much for the flag a caller reaches for first.
//
// -subset-fonts stays out of -compact for a different reason, following the
// same decision to keep a pass with a trade of its own behind its own flag.
// Subsetting is lossless for the document as it stands, but a subset font
// holds only the glyphs the document shows, so text a later edit or a form
// field adds in that font finds no glyph to draw. Packing and dedup change
// how the document is stored and nothing a later edit can reach.
func registerCompactFlags(fs *flag.FlagSet) compactionFlags {
	return compactionFlags{
		compact: registerCompactFlag(fs),
		recompressImages: fs.Bool("recompress-images", false,
			"Recompress Flate, LZW, RunLength and uncompressed images losslessly, keeping each "+
				"recompression only when it is smaller, when the command rewrites the whole file. "+
				"Slower than --compact, and independent of it"),
		subsetFonts: fs.Bool("subset-fonts", false,
			"Subset the embedded TrueType and CFF fonts to the glyphs the document shows, "+
				"keeping every glyph ID, when the command rewrites the whole file. Text added "+
				"later in a subset font finds no glyph. Independent of --compact"),
	}
}

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
// deliver. Both warn on stderr when it happens (see [warnCompactIncremental]).
func registerCompactFlag(fs *flag.FlagSet) *bool {
	return fs.Bool("compact", false,
		"Shrink the output by packing objects into object streams and merging "+
			"duplicate streams, when the command rewrites the whole file")
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

// packingForbiddenByProfile reports whether object-stream packing would cost
// the named compliance profile its conformance.
//
// ISO 19005-1, 6.1.4 forbids cross-reference streams in PDF/A-1 outright, and
// packing requires one: a traditional table has no type field and cannot name
// a compressed object. Every other profile litho remediates to permits both
// constructs. Measured on one file carrying an object stream and a
// cross-reference stream against the same file without them, PDF/A-1b gains
// exactly one violation, clause 6.1.4, while PDF/A-2b, PDF/A-3b and PDF/UA-1
// gain none.
//
// Only packing is forbidden. Stream dedup rewrites references to point at one
// of several byte-identical streams and needs neither construct, so a
// traditional cross-reference table still describes the output and the PDF 1.4
// header ISO 19005-1 is written against stays.
//
// Factur-X and the ZUGFeRD variants remediate to a PDF/A-3 base whatever
// generation the variant names, so none of them is affected. An unknown
// profile is not forbidden here: [runRemediate] rejects it before the write,
// and answering "forbidden" would silence packing for a typo.
func packingForbiddenByProfile(profile string) bool {
	p, ok := pdfaProfiles[profile]
	if !ok {
		return false
	}
	return p == models.PDFA1a || p == models.PDFA1b
}

// warnPackingWithheld reports that --compact was asked for and will not pack
// objects, because packing would cost the target profile its conformance.
// Dedup still runs, so the output is compacted as far as the profile permits.
//
// Packing silently would hand the caller a smaller file that no longer
// conforms to the profile they asked the command to produce, which defeats
// remediation entirely. Withholding it silently would leave a caller who sees
// no object stream in the output guessing why.
//
// A warning rather than an error, matching [warnCompactSkipped]: the command
// still produced the conformant document it was asked for, only a larger one.
func warnPackingWithheld(stderr io.Writer, compact bool, profile string) {
	if !compact || !packingForbiddenByProfile(profile) {
		return
	}
	fmt.Fprintf(stderr, "warning: --compact does not pack objects into object streams for %s, "+
		"since ISO 19005-1, 6.1.4 forbids the cross-reference stream they require. "+
		"Duplicate streams are still merged.\n", profile)
}

// warnCompactIncremental reports that --compact, --recompress-images or
// --subset-fonts was asked for and did not happen, because the write appended
// an incremental update, as [pdf.WithCompactionSkipped] reports it after the
// write.
//
// An update appends the edits to the input's bytes and keeps those bytes as
// they are (ISO 32000-1, 7.5.6), and packing, dedup, image recompression and
// font subsetting run on a full rewrite only, so the output is byte-identical
// to the one
// written without the flag. pipe and remediate leave an update when nothing
// they do needs a rewrite, a rotation or a remediation that repaired nothing
// structural, and neither forces one for compaction: a rewrite drops every
// signature the input carries (ISO 32000-1, 12.8.1), a trade the flag does not
// ask for. decrypt always rewrites, so it is the remedy the warning names.
//
// For a profile that forbids packing (see [packingForbiddenByProfile]) the
// remedy leaves --compact out: decrypt knows no profile and would pack the
// output, costing it the conformance the remediation produced.
//
// A warning rather than an error, matching [warnCompactSkipped]: the command
// still produced a correct document, only a larger one.
func warnCompactIncremental(stderr io.Writer, c compaction, skipped bool, profile string) {
	if !skipped {
		return
	}
	var asked []string
	for _, f := range []struct {
		on   bool
		name string
	}{{c.compact, "--compact"}, {c.recompressImages, "--recompress-images"}, {c.subsetFonts, "--subset-fonts"}} {
		if f.on {
			asked = append(asked, f.name)
		}
	}
	var flags string
	switch len(asked) {
	case 0:
		return
	case 1:
		flags = asked[0] + " does"
	default:
		flags = strings.Join(asked[:len(asked)-1], ", ") + " and " + asked[len(asked)-1] + " do"
	}
	var remedy []string
	if c.compact && !packingForbiddenByProfile(profile) {
		remedy = append(remedy, "--compact")
	}
	if c.recompressImages {
		remedy = append(remedy, "--recompress-images")
	}
	if c.subsetFonts {
		remedy = append(remedy, "--subset-fonts")
	}
	fmt.Fprintf(stderr, "warning: %s not apply to an incremental update, so the output is not compacted: "+
		"the edits were appended to the input's bytes, which an update keeps as they are.", flags)
	if len(remedy) > 0 {
		fmt.Fprintf(stderr, " Run the output through `litho decrypt %s`, which rewrites the whole file, to compact it.",
			strings.Join(remedy, " "))
	}
	if c.compact && packingForbiddenByProfile(profile) {
		fmt.Fprintf(stderr, " `litho decrypt --compact` would pack objects into object streams, which %s forbids: "+
			"ISO 19005-1, 6.1.4 forbids the cross-reference stream they require.", profile)
	}
	fmt.Fprintln(stderr)
}

// warnRecompressSkipped reports that --recompress-images was asked for and
// will not happen, because the output is encrypted: encryption has replaced
// every stream's data with ciphertext before the pass would run, so the write
// engine declines it, as it declines packing and dedup.
func warnRecompressSkipped(stderr io.Writer, recompressImages, encrypting bool) {
	if !recompressImages || !encrypting {
		return
	}
	fmt.Fprintln(stderr, "warning: --recompress-images does not apply to an encrypted output, so no image is recompressed. "+
		"Recompress the document first, then encrypt the result.")
}

// compactWriteOptions returns the write options -compact, -recompress-images
// and -subset-fonts select, or none when none was passed.
//
// No PDF/A profile forbids image recompression: it writes FlateDecode, which
// ISO 19005 permits at every part, and needs no PDF 1.5 construct. Nor does
// any forbid font subsetting, which keeps every glyph a document shows and
// every glyph ID, so the /CIDSet and /CharSet ISO 19005 asks of a font stay
// right. A command that targets a profile calls [profileCompactWriteOptions]
// instead.
//
// -compact drives both packing and dedup because they are independently safe and their
// savings add up rather than overlap. Callers wanting one without the other
// use [pdf.WithObjectStreams] and [pdf.WithDedup] directly.
//
// Neither pass can enlarge a file or damage one: packing declines any batch it
// cannot shrink, and both refuse outright on an encrypted output, where
// per-object encryption keys make merging and packing unsafe.
//
// # Corpus evidence
//
//   - All corpora, 37 files: packing saves 6.25% and dedup 0.76% against the
//     same rewrite without them, and 7.00% together.
func compactWriteOptions(c compaction) []pdf.WriteOption {
	var opts []pdf.WriteOption
	if c.compact {
		opts = append(opts, pdf.WithObjectStreams(), pdf.WithDedup())
	}
	if c.recompressImages {
		opts = append(opts, pdf.WithImageRecompression())
	}
	if c.subsetFonts {
		opts = append(opts, pdf.WithFontSubsetting())
	}
	return opts
}

// profileCompactWriteOptions returns the write options -compact,
// -recompress-images and -subset-fonts select for an output that must conform
// to profile.
//
// A profile that forbids packing (see [packingForbiddenByProfile]) keeps
// dedup, image recompression and font subsetting: none writes a construct ISO
// 19005-1 forbids, and refusing all of -compact there threw away a saving the
// profile permits. Callers report the withheld packing with
// [warnPackingWithheld].
func profileCompactWriteOptions(c compaction, profile string) []pdf.WriteOption {
	if !c.compact || !packingForbiddenByProfile(profile) {
		return compactWriteOptions(c)
	}
	rest := c
	rest.compact = false
	return append([]pdf.WriteOption{pdf.WithDedup()}, compactWriteOptions(rest)...)
}
