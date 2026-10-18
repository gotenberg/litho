// Command litho is the command-line front end to litho's public API in
// pkg/pdf. Its commands fall into four groups:
//
//   - Inspection reports a document's summary, text, metadata, pages,
//     outline, form fields, annotations, images, signatures, and
//     attachments, and validates PDF/A, PDF/UA, WTPDF and Factur-X
//     conformance.
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
// Global flags precede the command: --timeout bounds the run,
// --log-level sends library diagnostics to stderr, and --max-stream-bytes,
// --max-image-pixels, --max-embedded-file-bytes, --max-form-depth and
// --max-cached-objects set the resource limits every document the command
// reads is held to (pdf.Limits). A byte count takes a KiB, MiB or GiB
// suffix, and 0 keeps a limit's default. Run litho -h for the command list,
// the limits and their defaults, and litho <command> -h for a command's
// flags.
//
// The exit status is 0 on success, 1 on error, 2 when the document does
// not conform (validate, or remediate and pipe under --strict), and 124
// when a command fails because --timeout expired.
package main
