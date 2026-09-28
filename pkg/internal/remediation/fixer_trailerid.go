package remediation

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchTrailerID(v compliance.Violation) bool {
	// Clause 6.1.3 covers: t01 missing /ID, t04/t05 PDF/A-4 /Info
	// restrictions, t01 (part 4) empty /ID element. Exclude t02 (Encrypt)
	// which is handled by catalog-cleanup.
	return v.Rule.Clause == "6.1.3" && v.Rule.TestNumber != 2
}

// documentIDBytes is the length of the /ID strings remediation mints: the
// 16 bytes of an MD5 digest ISO 32000-1, 14.4 suggests, and of
// [Options.PinnedID].
const documentIDBytes = 16

// documentIDDomain opens every /ID derivation, so the digest of a document can
// never be mistaken for, or collide by construction with, any other SHA-256
// litho computes over the same bytes. Bump its version to change what the
// derivation covers: the same input then mints a new /ID.
const documentIDDomain = "litho remediation /ID v1"

// documentIDChunk is the read size of the document hash, a checkpoint for
// ctx per chunk.
const documentIDChunk = 64 << 10

// fixTrailerID gives the trailer the two-string /ID that ISO 19005-1 to
// ISO 19005-4, 6.1.3 require, when it has none or a malformed one, and applies
// the PDF/A-4 /Info limits of the same clause.
//
// ISO 32000-1, 14.4 makes the first /ID string the file's permanent
// identifier, so a non-empty first string survives and only the second is
// replaced. Every string it mints is [Options.PinnedID] when that is 16 bytes,
// and otherwise derived from the content by [deriveDocumentID]: the same input
// and options remediate to the same bytes, which a random /ID broke.
func fixTrailerID(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, profile string) ([]string, error) {
	var fixes []string

	// /ID missing or malformed: not a two-element array, or an empty element.
	var permanent ast.PdfString
	needReplace := true
	if arr, ok := doc.Index.Trailer["ID"].(ast.PdfArray); ok {
		if first, ok := firstIDString(arr); ok {
			permanent = first
		}
		needReplace = len(arr) != 2
		for _, e := range arr {
			if s, ok := e.(ast.PdfString); !ok || len(s) == 0 {
				needReplace = true
				break
			}
		}
	}
	if needReplace {
		id := opts.PinnedID
		if len(id) != documentIDBytes {
			derived, err := deriveDocumentID(ctx, doc, opts, profile)
			if err != nil {
				return nil, fmt.Errorf("deriving the trailer /ID: %w", err)
			}
			id = derived
		}
		first := permanent
		if first == "" {
			first = ast.PdfString(string(id))
		}
		doc.Index.Trailer["ID"] = ast.PdfArray{first, ast.PdfString(string(id))}
		fixes = append(fixes, "added /ID to trailer")
	}

	// PDF/A-4 §6.1.3 tests 4-5 constrain /Info to only /ModDate when
	// no /PieceInfo is defined on the catalog.
	if strings.HasPrefix(profile, "PDF/A-4") {
		if infoFixes := enforceInfoDictLimits(ctx, alloc, doc); len(infoFixes) > 0 {
			fixes = append(fixes, infoFixes...)
		}
	}

	if len(fixes) == 0 {
		return nil, nil
	}
	return fixes, nil
}

// firstIDString returns the first element of a trailer /ID array when it is a
// non-empty string.
func firstIDString(arr ast.PdfArray) (ast.PdfString, bool) {
	if len(arr) == 0 {
		return "", false
	}
	s, ok := arr[0].(ast.PdfString)
	return s, ok && len(s) > 0
}

// deriveDocumentID returns the /ID string remediation mints for doc: the
// first 16 bytes of a SHA-256 over the document bytes and every input that
// shapes the remediated output, the profile, the language, the content mode
// and the figure alternate text. ISO 32000-1, 14.4 asks for an identifier
// computed from the file's contents and suggests an MD5 over the time, the
// location, the size and the /Info values; litho hashes what makes the output
// what it is instead, so the same input and options remediate to the same
// bytes, and another input or option mints another /ID.
//
// The bytes hashed are those of the document remediation repairs: the source
// file, or the file the Writer's queued edits produce, so edits that change
// the output change the /ID. [Options.Disabled] is left out, since a
// rolled-back round repairs the same document and keeps the first round's
// /ID. The font programs [Options.Fonts] supplies are left out too, as a
// provider has no identity to hash; whether one is supplied is hashed.
func deriveDocumentID(ctx context.Context, doc *document.Document, opts Options, profile string) ([]byte, error) {
	h := sha256.New()
	fields := []string{
		documentIDDomain,
		profile,
		opts.Lang,
		strconv.FormatBool(opts.PreserveContent),
		opts.FigureAlt,
		strconv.FormatBool(opts.Fonts != nil),
	}
	for _, f := range fields {
		// Length-prefixed, so no two field lists hash the same bytes.
		fmt.Fprintf(h, "%d:%s", len(f), f)
	}
	size := doc.FileSize()
	fmt.Fprintf(h, "%d:", size)
	raw := doc.RawReader()
	if raw == nil {
		return h.Sum(nil)[:documentIDBytes], nil
	}
	src := io.NewSectionReader(raw, 0, size)
	buf := make([]byte, documentIDChunk)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := src.Read(buf)
		h.Write(buf[:n])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("hashing the document bytes: %w", err)
		}
	}
	return h.Sum(nil)[:documentIDBytes], nil
}

// enforceInfoDictLimits applies ISO 19005-4 §6.1.3 tests 4-5: /Info is
// optional for PDF/A-4 and, when present, must only carry /ModDate
// unless the catalog defines /PieceInfo. The trailer reference is
// removed outright when the catalog lacks /PieceInfo; otherwise the
// /Info dict is rewritten through the allocator so the stripped entries
// reach the output.
func enforceInfoDictLimits(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) []string {
	infoObj, ok := doc.Index.Trailer["Info"]
	if !ok {
		return nil
	}
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil
	}
	catalogDict, err := document.ResolveDict(ctx, doc, catalogRef)
	if err != nil || catalogDict == nil {
		return nil
	}
	_, hasPieceInfo := catalogDict["PieceInfo"]

	if !hasPieceInfo {
		delete(doc.Index.Trailer, "Info")
		return []string{"removed /Info from trailer (PDF/A-4 §6.1.3)"}
	}

	infoDict, err := document.ResolveDict(ctx, doc, infoObj)
	if err != nil || infoDict == nil {
		return nil
	}
	changed := false
	newInfo := make(ast.PdfDict, len(infoDict))
	for k, v := range infoDict {
		if k == "ModDate" {
			newInfo[k] = v
			continue
		}
		changed = true
	}
	if !changed {
		return nil
	}
	if ref, isRef := infoObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newInfo)
	} else {
		doc.Index.Trailer["Info"] = newInfo
	}
	return []string{"trimmed /Info to /ModDate only (PDF/A-4 §6.1.3)"}
}
