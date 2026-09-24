package mutations

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxScanDepth bounds nesting while walking one object. Anti-DoS guard, not
// spec-mandated: arrays and dictionaries nest a handful of levels in real
// files, and a crafted object must not drive the walk into unbounded
// recursion. It matches maxPageTreeDepth in the compliance walk for the same
// reason.
const maxScanDepth = 100

// maxScanRefDepth bounds how long a chain of indirect references an output scan
// follows. Anti-DoS guard, not spec-mandated: the visited set already stops a
// cycle, so what this bounds is Go stack depth on a crafted file that chains
// references linearly rather than circularly. It is counted separately from
// maxScanDepth on purpose. Sharing one budget would let an ordinary page tree
// spend what a deeply tagged document's structure tree needs, and a structure
// the walk gave up on is reported unreadable, which fails a caller closed over
// a document that was never malformed.
const maxScanRefDepth = 100

// TokenHit records one token still present in the document, and where.
type TokenHit struct {
	ObjectNum int
	Token     string
	// Where is "string" for a text string, or "stream" for decoded stream data.
	Where string
}

// ScanResult is what a scan found and what it could not read.
type ScanResult struct {
	// Hits are the surviving tokens, ordered by object number, then token, then
	// location, so a caller and a test see the same sequence every run.
	Hits []TokenHit
	// Unreadable names the objects whose stream could not be decoded or was
	// longer than the scan reads of one stream (see [scanWalk.scanStreamData]),
	// in ascending order. A scan that
	// skipped a stream has not proved that stream clean, so a caller that must
	// fail closed refuses when this is non-empty rather than reading the empty
	// Hits as success.
	Unreadable []int
}

// Clean reports whether the scan found nothing and read everything.
func (r ScanResult) Clean() bool {
	return len(r.Hits) == 0 && len(r.Unreadable) == 0
}

// ScanForTokens reports every place one of tokens still appears in doc.
//
// Removing content from a page's content stream does not remove the copies of
// it held elsewhere: the structure tree's /ActualText and /Alt, annotation
// values and their appearance streams, outline titles, named destinations,
// /PieceInfo and document metadata all survive a page edit, and several survive
// a full rewrite as well. This walks every object in the file instead of the
// places a redaction happens to know about, which is the point: it catches the
// vector nobody enumerated.
//
// It reads the document as parsed, so it answers for the bytes on disk and for
// an incremental update, which appends and leaves every superseded object in
// the file. It does not see edits queued in an allocator, and it reports
// objects a full rewrite would drop as unreferenced. Ask
// [ScanOutputForTokens] what a full rewrite would write.
//
// Strings are compared after [document.DecodePdfString], so a token written as
// UTF-16 matches the same as one written in PDFDocEncoding. Comparison is exact
// and case-sensitive: a scan is a check on litho's own removal, not a search
// for what a human would consider the same word.
//
// It is a check, not a proof. A token absent from every object may still be
// recoverable from glyph advances left behind by a split text run
// (arXiv 2206.02285), and a token this finds may be an innocent coincidence in
// unrelated data.
func ScanForTokens(ctx context.Context, doc *document.Document, tokens []string) (ScanResult, error) {
	if err := validateScanTokens(tokens); err != nil {
		return ScanResult{}, err
	}
	if len(tokens) == 0 {
		return ScanResult{}, nil
	}

	// No allocator and no reference following: this sweep reaches every object
	// in the file on its own.
	w := newScanWalk(doc, nil, tokens, false)

	count := 0
	for objNum, entry := range doc.XRefEntriesSnapshot() {
		if count%100 == 0 { //nolint:mnd // amortized cancellation check, matching the compliance sweep
			if err := ctx.Err(); err != nil {
				return ScanResult{}, err
			}
		}
		count++
		if entry.Free {
			continue
		}

		ref := ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation}
		obj, err := document.ResolveObject(ctx, doc, ref)
		if err != nil {
			// An object litho cannot parse is one it cannot clear either, so it
			// counts as unread rather than as clean.
			w.unreadable[objNum] = true
			doc.Logger.Debug("token scan: object could not be resolved",
				slog.Int("object", objNum), slog.Any("err", err))
			continue
		}
		w.walk(ctx, objNum, obj, 0)
	}

	return w.result(), nil
}

// ScanOutputForTokens reports every place one of tokens would still appear in
// the document [FullRewrite] writes from doc and the edits queued in alloc.
//
// A redaction queues its replacements in an allocator and leaves the document
// it read untouched, so [ScanForTokens] run straight afterwards answers about
// the wrong bytes twice over: it reports the original content stream, which the
// output no longer references, and it never sees the replacement, which exists
// only in the allocator. This resolves every reference through alloc first and
// reaches outward from the roots [FullRewrite] copies, so what it reports is
// what the rewritten file would hold.
//
// Reaching outward from the roots is also what drops an unreferenced object
// from the answer. An excision leaves the page's original content stream in the
// document, referenced by nothing; a full rewrite never copies it, so reporting
// it would name a leak the output does not have. That reasoning holds only for
// a full rewrite: an incremental update keeps every superseded object in the
// file, and a caller writing one has to ask [ScanForTokens] instead.
//
// A nil alloc scans the document as a full rewrite of it would be written, with
// no edits applied.
//
// The caveats on [ScanForTokens] hold here too: it is a check, not a proof.
func ScanOutputForTokens(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	tokens []string,
) (ScanResult, error) {
	if err := validateScanTokens(tokens); err != nil {
		return ScanResult{}, err
	}
	if len(tokens) == 0 {
		return ScanResult{}, nil
	}
	if err := ctx.Err(); err != nil {
		return ScanResult{}, err
	}

	w := newScanWalk(doc, alloc, tokens, true)

	// Mirrors FullRewrite: the trailer /Root when it resolves to a real
	// catalog, and the catalog the reader recovers when it does not. Reaching
	// from a root the rewrite would not use answers about a file litho never
	// writes.
	catalogRef, hasRef := CatalogRef(ctx, doc)
	if hasRef && catalogRefResolves(ctx, doc, catalogRef) {
		w.follow(ctx, catalogRef)
	} else {
		catalog, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			return ScanResult{}, fmt.Errorf("resolving catalog for the scan: %w", err)
		}
		if catalog == nil || catalog.Dict == nil {
			return ScanResult{}, ErrCatalogUnreadable
		}
		// Reported against the object number the trailer named, or against zero
		// when it named none. Zero is not a real object number: ISO 32000-1,
		// 7.5.4 reserves cross-reference entry zero for the head of the free
		// list, which is why writer.ObjAllocator.AllocAt refuses it. It reads
		// here as "no object number", never as object zero.
		w.walk(ctx, catalogRef.ObjectNum, catalog.Dict, 0)
	}

	// The rewrite copies the trailer's /Info, and /Info is a documented home for
	// a copy of page text, so it is a root in its own right: nothing in the
	// catalog graph reaches it.
	if infoRef, ok := doc.Index.Trailer["Info"].(ast.PdfIndirectRef); ok {
		w.follow(ctx, infoRef)
	}

	if w.err != nil {
		return ScanResult{}, w.err
	}
	return w.result(), nil
}

// validateScanTokens refuses a token that would make the answer meaningless.
func validateScanTokens(tokens []string) error {
	if slices.Contains(tokens, "") {
		return fmt.Errorf("%w: an empty token matches every object", ErrScanTokenEmpty)
	}
	return nil
}

// scanWalk carries the state of one scan.
type scanWalk struct {
	doc    *document.Document
	alloc  *writer.ObjAllocator
	tokens []string

	hits       map[TokenHit]bool
	unreadable map[int]bool

	// visited is non-nil only on a walk that follows indirect references, and
	// holds every object number already entered. The xref sweep leaves it nil:
	// it reaches every object itself, so following a reference there would
	// rescan objects and could loop.
	visited  map[int]bool
	refDepth int

	// err holds a cancellation seen mid-walk. The walk cannot return one, and
	// abandoning it silently would hand back a short result that reads as a
	// cleaner document than was examined.
	err error
}

func newScanWalk(doc *document.Document, alloc *writer.ObjAllocator, tokens []string, follow bool) *scanWalk {
	w := &scanWalk{
		doc:        doc,
		alloc:      alloc,
		tokens:     tokens,
		hits:       make(map[TokenHit]bool),
		unreadable: make(map[int]bool),
	}
	if follow {
		w.visited = make(map[int]bool)
	}
	return w
}

func (w *scanWalk) result() ScanResult {
	return buildScanResult(w.hits, w.unreadable)
}

// walk scans one object's strings and stream data. depth bounds nesting inside
// this object; a reference leaves it, and the object it names starts afresh
// bounded by refDepth instead.
func (w *scanWalk) walk(ctx context.Context, objNum int, obj ast.PdfObject, depth int) {
	if w.err != nil {
		return
	}
	if depth > maxScanDepth {
		w.unreadable[objNum] = true
		return
	}

	switch v := obj.(type) {
	case ast.PdfString:
		text := document.DecodePdfString(v)
		for _, tok := range w.tokens {
			if strings.Contains(text, tok) {
				w.hits[TokenHit{ObjectNum: objNum, Token: tok, Where: "string"}] = true
			}
		}
	case ast.PdfArray:
		for _, item := range v {
			w.walk(ctx, objNum, item, depth+1)
		}
	case ast.PdfDict:
		// Keys are names, not text: a /Title key never carries a redacted word,
		// its value does.
		for _, item := range v {
			w.walk(ctx, objNum, item, depth+1)
		}
	case ast.PdfStream:
		w.walk(ctx, objNum, v.Dict, depth+1)
		w.scanStreamData(ctx, objNum, v)
	case ast.PdfIndirectRef:
		w.follow(ctx, v)
	}
}

// follow visits the object a reference names, once, on a walk that reaches
// outward from a root. It does nothing on the xref sweep, which visits every
// object itself.
func (w *scanWalk) follow(ctx context.Context, ref ast.PdfIndirectRef) {
	if w.visited == nil || w.visited[ref.ObjectNum] || w.err != nil {
		return
	}
	if w.refDepth >= maxScanRefDepth {
		w.unreadable[ref.ObjectNum] = true
		return
	}
	if len(w.visited)%100 == 0 { //nolint:mnd // amortized cancellation check, matching the sweep above
		if err := ctx.Err(); err != nil {
			w.err = err
			return
		}
	}
	w.visited[ref.ObjectNum] = true

	obj, ok := w.resolve(ctx, ref)
	if !ok {
		w.unreadable[ref.ObjectNum] = true
		return
	}

	w.refDepth++
	w.walk(ctx, ref.ObjectNum, obj, 0)
	w.refDepth--
}

// resolve returns the object the output would hold at this reference: the
// version queued in the allocator when there is one, the document's otherwise.
func (w *scanWalk) resolve(ctx context.Context, ref ast.PdfIndirectRef) (ast.PdfObject, bool) {
	if w.alloc != nil {
		if obj, ok := w.alloc.Pending()[ref.ObjectNum]; ok {
			return obj, true
		}
	}
	obj, err := document.ResolveObject(ctx, w.doc, ref)
	if err != nil {
		w.doc.Logger.Debug("token scan: object could not be resolved",
			slog.Int("object", ref.ObjectNum), slog.Any("err", err))
		return nil, false
	}
	return obj, true
}

// scanStreamData decodes one stream and looks for the tokens in its bytes.
//
// Decoded rather than raw, because the copy that matters is the one a reader
// sees: a redacted word inside a Flate-compressed structure tree or appearance
// stream is invisible to a scan of the stored bytes.
//
// It reads at most [limits.Limits.MaxPageContentBytes] of the document's
// budgets from one stream, and reports a longer one unreadable. That bound is an
// anti-DoS guard, not spec-mandated: a scan decodes every stream in the
// document, so a decompression bomb would otherwise be read in full. It is the
// page content bound because a single stream larger than the heaviest page any
// reader accepts is past anything a scan needs to read, and a caller that
// raises the stream budget to edit a heavy page can then scan that page too.
func (w *scanWalk) scanStreamData(ctx context.Context, objNum int, stream ast.PdfStream) {
	limit := w.doc.Limits().MaxPageContentBytes()
	rc, err := w.doc.NewStreamReader(ctx, stream)
	if err != nil {
		w.unreadable[objNum] = true
		w.doc.Logger.Debug("token scan: stream could not be opened",
			slog.Int("object", objNum), slog.Any("err", err))
		return
	}
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if closeErr := rc.Close(); closeErr != nil {
		w.doc.Logger.Debug("token scan: closing stream reader",
			slog.Int("object", objNum), slog.Any("err", closeErr))
	}
	if err != nil {
		w.unreadable[objNum] = true
		w.doc.Logger.Debug("token scan: stream could not be decoded",
			slog.Int("object", objNum), slog.Any("err", err))
		return
	}
	if int64(len(data)) > limit {
		// Read short of the end, so the rest of it is unexamined.
		w.unreadable[objNum] = true
		w.doc.Logger.Debug("token scan: stream exceeds the scan cap",
			slog.Int("object", objNum), slog.Int64("cap", limit))
		return
	}

	text := string(data)
	for _, tok := range w.tokens {
		if strings.Contains(text, tok) {
			w.hits[TokenHit{ObjectNum: objNum, Token: tok, Where: "stream"}] = true
		}
	}
}

// buildScanResult turns the collected sets into the ordered result. The sweep
// ranges a map, so nothing here may depend on the order objects arrived in.
func buildScanResult(hits map[TokenHit]bool, unreadable map[int]bool) ScanResult {
	out := ScanResult{
		Hits:       make([]TokenHit, 0, len(hits)),
		Unreadable: make([]int, 0, len(unreadable)),
	}
	for hit := range hits {
		out.Hits = append(out.Hits, hit)
	}
	for objNum := range unreadable {
		out.Unreadable = append(out.Unreadable, objNum)
	}
	sort.Slice(out.Hits, func(i, j int) bool {
		a, b := out.Hits[i], out.Hits[j]
		if a.ObjectNum != b.ObjectNum {
			return a.ObjectNum < b.ObjectNum
		}
		if a.Token != b.Token {
			return a.Token < b.Token
		}
		return a.Where < b.Where
	})
	sort.Ints(out.Unreadable)
	return out
}
