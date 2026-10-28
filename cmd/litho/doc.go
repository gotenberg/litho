// Command litho is the command-line front end to litho's public API in
// pkg/pdf. Its commands fall into four groups:
//
//   - Inspection reports a document's summary, text, metadata, pages,
//     outline, form fields, annotations, images, signatures, and
//     attachments, draws pages to PNG images, and validates PDF/A, PDF/UA,
//     WTPDF and Factur-X conformance.
//   - Extraction pulls tables, geospatial data, measurement scales, 3D
//     models, MathML formulas, pronunciation lexicons, and the text under
//     redaction marks.
//   - Mutation writes a new document: page edits, merge and split,
//     redaction, encryption, metadata, form fields, bookmarks, stamps,
//     attachments, auto-tagging, and PDF/A, PDF/UA, WTPDF or Factur-X
//     remediation.
//   - pipe chains several operations on one document. A remediate stage runs
//     at most once, as the last stage, since a remediation's report
//     describes the output only when nothing edits it afterwards; pipe
//     refuses any other order before it runs a stage.
//
// Run litho with no arguments for the command list, litho -h for the global
// flags and the limits with their defaults, and litho <command> -h for a
// command's flags.
//
// # Global flags
//
// Global flags precede the command: --timeout bounds the run, --log-level
// sends library diagnostics to stderr, and --max-stream-bytes,
// --max-image-pixels, --max-embedded-file-bytes, --max-form-depth,
// --max-cached-objects and --max-render-pixels set the resource limits
// (pdf.Limits). A limit holds every document the command reads, each input of
// a multi-input command and each pipe stage included.
//
// A byte count takes a plain integer or a KiB, MiB or GiB suffix, the other
// limits a plain integer, and 0 keeps a limit's default. A value that does not
// parse, or one past its ceiling, exits 1 before any document opens.
// --max-stream-bytes also bounds the content of one page, summed across its
// content streams, at a quarter of its value.
//
// # Pipe stages
//
// A pipe stage prints no report. pipe --strict is how a pipeline learns that
// its remediate stage left a violation, or a font unembedded: it exits 2.
//
// # Size
//
// Every command that rewrites the whole file, pipe included, takes three size
// passes, alone or together:
//
//   - --compact packs objects into object streams and merges duplicate
//     streams (pdf.WithObjectStreams, pdf.WithDedup). For a PDF/A-1 target it
//     only merges, since ISO 19005-1 forbids the cross-reference stream
//     packing needs.
//   - --recompress-images recompresses images losslessly
//     (pdf.WithImageRecompression). It can take seconds on an image-heavy file
//     that --compact alone writes in milliseconds.
//   - --subset-fonts cuts the embedded fonts down to the glyphs the document
//     can show (pdf.WithFontSubsetting). A subset font has no glyph for text
//     added later.
//
// The first two do not apply to an encrypted output; --subset-fonts does.
//
// optimize rewrites the file for those passes alone, all three on.
// --no-compact, --no-recompress-images and --no-subset-fonts turn one off, and
// turning off all three exits 1. An input that declares PDF/A-1 is
// deduplicated and not packed, and optimize says so. As a pipe stage, optimize
// sets the final write rather than editing the document, so it may stand
// anywhere in the chain, once. A --no- flag against the global flag of the same
// pass exits 1.
//
// # Remediation
//
// remediate lists every font it leaves unembedded, with the reason, under
// "Fonts left unembedded", or refused_fonts with -f json. --font-dir naming a
// directory that does not exist, or a --document-id that is not 32 hex
// digits, exits 1 and writes nothing.
//
// # Encrypted documents
//
// Editing an encrypted document produces an unprotected one: no command keeps
// the source's encryption. Every command that writes a document warns on
// stderr when the input carried protection the output will not, except
// decrypt, whose job that is. The warning names the remedy the command
// accepts:
//
//   - split and merge take --user-password;
//   - pipe takes an encrypt stage, which applies to the document it writes;
//   - any other command is followed by litho encrypt.
//
// A PDF/A output is never encrypted, since ISO 19005 forbids it: pipe refuses
// an encrypt stage beside a PDF/A remediate stage, and encrypting the result
// costs its conformance.
//
// # Exit status
//
//   - 0: success.
//   - 1: the command failed, such as a missing file, an invalid flag or an
//     unreadable document.
//   - 2: the document does not conform: validate, or remediate and pipe under
//     --strict. Those two still write their output, since the status is a
//     verdict, not a refusal.
//   - 124: the --timeout deadline expired, the status timeout(1) reports. A
//     command that reached a verdict before the deadline keeps it: only a
//     failure becomes 124.
package main
