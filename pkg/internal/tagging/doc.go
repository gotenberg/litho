// Package tagging synthesizes a PDF logical structure tree (a "tagged PDF")
// and the marked content that links it to page content streams.
//
// litho already has a flat content-tag fixer that wraps each untagged page in
// a single Document marked-content sequence, which clears the structural
// PDF/UA bar but produces no real logical structure. This package emits a
// populated tree: Document with H1..Hn / P / Figure / Table children, each
// linked to its own marked-content sequence by a distinct MCID.
//
// Scope. This package is the emitter: it takes an in-memory logical tree
// ([Element]) whose leaves carry byte-range [ContentSpan] values into a page's
// decoded content, and writes the structure objects, marked content, and
// catalog wiring into a [writer.ObjAllocator]. Deriving the tree from layout
// analysis (heading detection, reading order, tables) is a separate concern;
// so is mapping content-stream operations to byte ranges. Callers supply spans
// that fall on content-stream operation boundaries.
//
// Concurrency. [Emit] mutates the allocator it is given and is not safe for
// concurrent use. Remediation runs are serialized by the caller.
package tagging
