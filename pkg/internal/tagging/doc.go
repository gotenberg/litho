// Package tagging synthesizes a PDF logical structure tree (a "tagged PDF")
// and the marked content that links it to page content streams.
//
// litho already has a flat content-tag fixer that wraps each untagged page in
// a single Document marked-content sequence, which clears the structural
// PDF/UA bar but produces no real logical structure. This package emits a
// populated tree: Document with H1..Hn / P / L / Figure / Table children, each
// linked to its own marked-content sequence by a distinct MCID. Running
// headers, footers and page numbers are marked as pagination artifacts
// instead (see [Element.Pagination]).
//
// Scope. [Classify] derives the logical tree ([Element]) from the page
// layout: headings, paragraphs, lists, tables and figures in reading order,
// with running headers, footers and page numbers set apart. Its leaves carry
// byte-range [ContentSpan] values into a page's decoded content, on
// content-stream operation boundaries. [Emit] takes such a tree and writes the
// structure objects, marked content, and catalog wiring into a
// [writer.ObjAllocator]. [AutoTag] runs both.
//
// Concurrency. [Emit] mutates the allocator it is given and is not safe for
// concurrent use. Remediation runs are serialized by the caller.
package tagging
