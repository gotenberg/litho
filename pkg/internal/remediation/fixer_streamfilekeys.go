package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// streamFileKeys are the three keys every part of ISO 19005 forbids on a stream
// dictionary. They declare that the stream's data lives in an external file,
// which no conforming file may rely on.
var streamFileKeys = map[string]ast.PdfName{
	"F":            "F",
	"FFilter":      "FFilter",
	"FDecodeParms": "FDecodeParms",
}

// matchStreamFileKeys matches the one requirement that forbids /F, /FFilter and
// /FDecodeParms on a stream dictionary, in each of the four spellings the parts
// give it.
//
// Matched on the whole specification/clause/test triple, never on the clause
// number: ISO 19005-4 states this rule as 6.1.6.1 t2 rather than t3 only
// because part 4 dropped the stream-keyword EOL requirement that parts 1 to 3
// carry as t2. Its clause 6.1.7 is a different requirement entirely
// (CosUnicodeName: font and colourant names), so a prefix or clause-only match
// would claim a rule this fixer cannot repair.
func matchStreamFileKeys(v compliance.Violation) bool {
	s, c, t := v.Rule.Specification, v.Rule.Clause, v.Rule.TestNumber
	return (s == "ISO_19005_1" && c == "6.1.7" && t == 3) ||
		(s == "ISO_19005_2" && c == "6.1.7.1" && t == 3) ||
		(s == "ISO_19005_3" && c == "6.1.7.1" && t == 3) ||
		(s == "ISO_19005_4" && c == "6.1.6.1" && t == 2)
}

// streamKeysContextRe matches the context checkStreamExternalKeys emits, which
// is a %v of a []string: "object 13: stream dict contains [F FFilter]".
var streamKeysContextRe = regexp.MustCompile(`^object (\d+): stream dict contains \[([A-Za-z ]+)\]$`)

// parseStreamKeysContext reads the offending object number and the keys the
// detector actually found. Repairing only the named keys keeps the fixer honest
// about what it changed, and a context this fixer did not shape repairs nothing
// rather than guessing an object.
func parseStreamKeysContext(s string) (int, []ast.PdfName, bool) {
	m := streamKeysContextRe.FindStringSubmatch(s)
	if m == nil {
		return 0, nil, false
	}
	objNum, err := strconv.Atoi(m[1])
	if err != nil || objNum <= 0 {
		return 0, nil, false
	}

	var keys []ast.PdfName
	for field := range strings.FieldsSeq(m[2]) {
		if k, ok := streamFileKeys[field]; ok {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return 0, nil, false
	}
	return objNum, keys, true
}

// fixStreamFileKeys removes /F, /FFilter and /FDecodeParms from the stream
// dictionaries the violations name.
//
// The keys say the stream's bytes live in an external file. A conforming file
// may not depend on one, and the external bytes are not in the document to
// inline, so deleting the keys is the repair the clause asks for: it leaves the
// stream's own data authoritative. Real carriers declare /Length 0 on those
// streams, so nothing retrievable is lost. It is still content the author
// described, so PreserveContent refuses.
//
// Only the dictionary changes. The raw bytes are carried across with
// [ast.PdfStream.Bytes], which returns them after decryption but BEFORE filter
// decoding, so the surviving /Filter entry keeps describing them correctly.
//
// # Corpus evidence
//
//   - All corpora: the fixtures carrying these keys declare /Length 0 on those
//     streams, so nothing retrievable is lost.
//   - All corpora: no stream this fixer rewrites is queued by an earlier fixer
//     first, at PDF/A-1b, 2b or 4, so the allocator read changes no output.
func fixStreamFileKeys(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, violations []compliance.Violation, _ string) ([]string, error) {
	if opts.PreserveContent {
		return nil, nil
	}

	targets := make(map[int][]ast.PdfName, len(violations))
	var objNums []int
	for _, v := range violations {
		objNum, keys, ok := parseStreamKeysContext(v.Context)
		if !ok {
			continue
		}
		if _, seen := targets[objNum]; !seen {
			objNums = append(objNums, objNum)
		}
		targets[objNum] = append(targets[objNum], keys...)
	}
	// A violation slice is ordered by the check that produced it, and the writer
	// must emit the same bytes for the same input.
	slices.Sort(objNums)

	var fixes []string
	for _, objNum := range objNums {
		// The loop is the fixer's last statement, so breaking out of it
		// reported a cancelled run as a completed repair.
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("removing external-file keys: %w", err)
		}

		// Alloc-first: both rules bind on every stream and lzw runs first, so
		// the named stream may already be queued re-encoded. Rewriting the
		// document's copy would put the LZWDecode body back.
		stream, ok := liveStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if !ok {
			continue
		}

		newDict := make(ast.PdfDict, len(stream.Dict))
		maps.Copy(newDict, stream.Dict)
		var removed []string
		for _, k := range targets[objNum] {
			if _, present := newDict[k]; present {
				delete(newDict, k)
				removed = append(removed, string(k))
			}
		}
		if len(removed) == 0 {
			continue
		}

		// An empty body is the NORMAL case here: these streams declare /Length 0
		// because the data was external. Only a read ERROR may skip the rewrite,
		// never an empty slice, or the fixer would decline every cell it exists
		// to repair.
		raw, err := stream.Bytes(ctx)
		if err != nil {
			doc.Logger.Debug("remediation: skipping stream rewrite, body read failed",
				slog.Int("obj", objNum), slog.Any("err", err))
			continue
		}

		alloc.AllocAt(objNum, ast.NewStream(newDict, raw))
		slices.Sort(removed)
		fixes = append(fixes, fmt.Sprintf(
			"removed %s from stream dict (object %d)", strings.Join(removed, ", "), objNum))
	}

	return fixes, nil
}
