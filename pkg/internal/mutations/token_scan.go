package mutations

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// maxScannedStreamSize bounds the decoded bytes read from any one stream while
// scanning. Anti-DoS guard, not spec-mandated: a scan decodes every stream in
// the document, so a decompression bomb would otherwise be read in full. It
// mirrors [content.MaxPageContentSize], which is measured against the corpora
// rather than picked round, and a single stream larger than the heaviest
// legitimate page is past anything a scan needs to read.
const maxScannedStreamSize = content.MaxPageContentSize

// maxScanDepth bounds nesting while walking one object. Anti-DoS guard, not
// spec-mandated: arrays and dictionaries nest a handful of levels in real
// files, and a crafted object must not drive the walk into unbounded
// recursion. It matches maxPageTreeDepth in the compliance walk for the same
// reason.
const maxScanDepth = 100

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
	// larger than [maxScannedStreamSize], in ascending order. A scan that
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
	for _, tok := range tokens {
		if tok == "" {
			return ScanResult{}, fmt.Errorf("%w: an empty token matches every object", ErrScanTokenEmpty)
		}
	}
	if len(tokens) == 0 {
		return ScanResult{}, nil
	}

	hits := make(map[TokenHit]bool)
	unreadable := make(map[int]bool)

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
			unreadable[objNum] = true
			doc.Logger.Debug("token scan: object could not be resolved",
				slog.Int("object", objNum), slog.Any("err", err))
			continue
		}
		scanObject(ctx, doc, objNum, obj, tokens, 0, hits, unreadable)
	}

	return buildScanResult(hits, unreadable), nil
}

// scanObject walks one object's strings and stream data. Indirect references
// are deliberately not followed: the caller's sweep visits every object in the
// file exactly once, so following them would rescan objects and could loop.
func scanObject(
	ctx context.Context,
	doc *document.Document,
	objNum int,
	obj ast.PdfObject,
	tokens []string,
	depth int,
	hits map[TokenHit]bool,
	unreadable map[int]bool,
) {
	if depth > maxScanDepth {
		unreadable[objNum] = true
		return
	}

	switch v := obj.(type) {
	case ast.PdfString:
		text := document.DecodePdfString(v)
		for _, tok := range tokens {
			if strings.Contains(text, tok) {
				hits[TokenHit{ObjectNum: objNum, Token: tok, Where: "string"}] = true
			}
		}
	case ast.PdfArray:
		for _, item := range v {
			scanObject(ctx, doc, objNum, item, tokens, depth+1, hits, unreadable)
		}
	case ast.PdfDict:
		// Keys are names, not text: a /Title key never carries a redacted word,
		// its value does.
		for _, item := range v {
			scanObject(ctx, doc, objNum, item, tokens, depth+1, hits, unreadable)
		}
	case ast.PdfStream:
		scanObject(ctx, doc, objNum, v.Dict, tokens, depth+1, hits, unreadable)
		scanStreamData(ctx, doc, objNum, v, tokens, hits, unreadable)
	}
}

// scanStreamData decodes one stream and looks for the tokens in its bytes.
//
// Decoded rather than raw, because the copy that matters is the one a reader
// sees: a redacted word inside a Flate-compressed structure tree or appearance
// stream is invisible to a scan of the stored bytes.
func scanStreamData(
	ctx context.Context,
	doc *document.Document,
	objNum int,
	stream ast.PdfStream,
	tokens []string,
	hits map[TokenHit]bool,
	unreadable map[int]bool,
) {
	rc, err := doc.NewStreamReader(ctx, stream)
	if err != nil {
		unreadable[objNum] = true
		doc.Logger.Debug("token scan: stream could not be opened",
			slog.Int("object", objNum), slog.Any("err", err))
		return
	}
	data, err := io.ReadAll(io.LimitReader(rc, int64(maxScannedStreamSize)+1))
	if closeErr := rc.Close(); closeErr != nil {
		doc.Logger.Debug("token scan: closing stream reader",
			slog.Int("object", objNum), slog.Any("err", closeErr))
	}
	if err != nil {
		unreadable[objNum] = true
		doc.Logger.Debug("token scan: stream could not be decoded",
			slog.Int("object", objNum), slog.Any("err", err))
		return
	}
	if len(data) > maxScannedStreamSize {
		// Read short of the end, so the rest of it is unexamined.
		unreadable[objNum] = true
		doc.Logger.Debug("token scan: stream exceeds the scan cap",
			slog.Int("object", objNum), slog.Int("cap", maxScannedStreamSize))
		return
	}

	text := string(data)
	for _, tok := range tokens {
		if strings.Contains(text, tok) {
			hits[TokenHit{ObjectNum: objNum, Token: tok, Where: "stream"}] = true
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
