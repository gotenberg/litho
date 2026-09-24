// Package compliance validates PDFs against ISO 19005 (PDF/A),
// ISO 14289 (PDF/UA) and Well-Tagged PDF (WTPDF). Each rule encodes a
// clause and test from those standards, identified by
// (Specification, Clause, TestNumber).
//
// # Entry Points
//
// [ValidatePDFA], [ValidatePDFUA] and [ValidateWTPDF] audit a parsed document
// against a specific profile. All return violations as []error where each
// element is a [Violation] value. Type-assert to access the structured [Rule]
// and Context fields:
//
//	violations, err := compliance.ValidatePDFA(ctx, doc, models.PDFA2b)
//	for _, v := range violations {
//	    if viol, ok := v.(compliance.Violation); ok {
//	        fmt.Printf("clause %s test %d: %s\n",
//	            viol.Rule.Clause, viol.Rule.TestNumber, viol.Context)
//	    }
//	}
//
// An empty violations slice means the document passes.
//
// # Architecture
//
// Validation is organized as check groups, each responsible for a domain of the
// specification (colour spaces, fonts, structure tree, annotations, etc.).
// Check groups run concurrently and share a pre-computed [validationCtx] that
// holds resolved pages, resources, and decoded content streams. This eliminates
// redundant page tree traversals across groups.
//
// # Silent-skip pattern
//
// Many checks begin by resolving an object (a font dict, an XObject, an OCG
// entry) through [document.ResolveDict] or a typed variant. When the resolve
// fails, typically because of a dangling indirect reference, a corrupt
// object stream, or a decryption failure, the check *skips* the affected
// subtree rather than emitting a [Violation]. The rationale: no PDF/A or
// PDF/UA rule directly covers "a referenced object could not be parsed",
// so a synthetic "structural corruption" violation has no governing clause
// to cite and would report a verdict the standards do not define.
//
// These silent skips always log the underlying error at Debug level so an
// operator running with pdf.WithWriteLogger (or equivalent) can see the
// context. Structural corruption that makes the entire document unparseable
// is surfaced earlier, at pdf.Open time, via pdf.ErrDamagedXRef and
// related sentinels.
//
// When adding a new check, follow this policy:
//
//   - Legitimate "not applicable" (e.g. the page has no /Font dict at all):
//     silently continue, no log.
//   - Partial corruption (resolve of a referenced object fails): log at
//     Debug via the document logger and continue, do not emit a violation.
//   - Infrastructure failure that prevents the whole check from running
//     (e.g. ctx canceled): propagate the error to the orchestrator.
//
// # Function signature convention
//
// Check functions that inspect pre-resolved dicts without doing I/O or
// recursion may not need ctx or doc today, but they accept
// (_ context.Context, _ *document.Document, ...) anyway. This keeps a
// uniform call pattern across all checks: the orchestrator passes ctx and
// doc to every check function without special-casing, and individual
// checks can start chasing indirect refs or logging without a signature
// change.
//
// Rules are generated via go generate (see cmd/gen-rules) so the constant set
// stays in sync with the standards. Each [Rule] maps 1:1 to a clause-and-test
// pair, identified by (Specification, Clause, TestNumber). 751 rules cover 6
// ISO standards: ISO 19005-1 through 19005-4 (PDF/A) and ISO 14289-1 through
// 14289-2 (PDF/UA).
//
// # File Organization
//
//   - compliance.go: public API ([ValidatePDFA], [ValidatePDFUA], [Violation])
//   - check.go: internal orchestration (context building, parallel dispatch, helpers)
//   - rules.go: generated rule constants (do not edit)
//   - rules_extra.go: hand-maintained rules outside the generated set
//   - color*.go: OutputIntent, device CS, ICCBased, Separation/DeviceN
//   - fonts*.go: embedding, encoding, widths, ToUnicode, CharSet/CIDSet, CMap
//   - structure.go: StructTreeRoot, RoleMap, heading/table/list hierarchy
//   - accessibility*.go: PDF/UA lang, alt text, annotations, namespaces, destinations
//   - xmp*.go: XMP metadata, extension schemas, DocInfo sync
//   - annotations.go: annotation types, flags, appearances
//   - actions.go: forbidden action types
//   - graphics.go: ExtGState, halftone, rendering intent
//   - transparency.go: blend modes, blending CS, soft masks
//   - content*.go: content stream operators, graphics state
//   - file_structure.go, file_format.go, file_limits.go: header, xref, streams, limits
//   - embedded.go: embedded file specs, recursive compliance
//   - document.go: PDF version, OCG, permissions
//   - forms.go: XFA, form field appearances
//   - jpx.go: JPEG2000 codestream validation (JP2/J2K)
package compliance
