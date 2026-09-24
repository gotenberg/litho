// Command litho is the command-line front end to litho's public API in
// pkg/pdf. Its commands fall into four groups:
//
//   - Inspection reports a document's summary, text, metadata, pages,
//     outline, form fields, annotations, images, signatures, and
//     attachments, and validates PDF/A and PDF/UA conformance.
//   - Extraction pulls tables, geospatial data, measurement scales, 3D
//     models, MathML formulas, pronunciation lexicons, and the text under
//     redaction marks.
//   - Mutation writes a new document: page edits, merge and split,
//     redaction, encryption, metadata, form fields, bookmarks, stamps,
//     attachments, auto-tagging, and PDF/A or PDF/UA remediation.
//   - pipe chains several operations on one document.
//
// Global flags precede the command: --timeout bounds the run and
// --log-level sends library diagnostics to stderr. Run litho -h for the
// command list and litho <command> -h for a command's flags.
//
// The exit status is 0 on success, 1 on error, 2 when the document does
// not conform (validate, or remediate and pipe under --strict), and 124
// when a command fails because --timeout expired.
package main
