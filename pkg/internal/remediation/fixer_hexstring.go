package remediation

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// oddHexDigits and invalidHexChar are the parser's wordings for the two ways a
// hexadecimal string is written wrongly, both of which the fixer below repairs
// by re-serialising the page. A [compliance.Violation] carries the parser's
// finding only as text in its Context, so [matchHexString] reads these; they
// come from the parser's sentinels so the two cannot drift apart.
var (
	oddHexDigits   = parser.ErrHexStringOddDigits.Error()
	invalidHexChar = parser.ErrHexStringInvalidChar.Error()
)

// matchHexString matches a malformed hex string inside a page content stream:
// an odd number of non-white-space characters (ISO 19005-1/2/3 §6.1.6 test 1,
// ISO 19005-4 §6.1.5 test 1) or a character that is no hexadecimal digit
// (test 2). Re-serialising the page repairs both, so the fixer claims both.
func matchHexString(v compliance.Violation) bool {
	if !clausePrefix(v, "6.1.5", "6.1.6") {
		return false
	}
	return contextContains(v, oddHexDigits) || contextContains(v, invalidHexChar)
}

// fixHexString rewrites the content streams that hold a malformed hex string:
// an odd number of non-white-space characters, or a character that is no
// hexadecimal digit.
//
// ISO 32000-1, 7.3.4.3 lets a reader assume a missing final digit is 0, and
// litho's parser does exactly that, so the value is already understood; the
// defect is in how it is written. Re-serialising the page emits every string
// through the writer, which never produces an odd digit count, and leaves the
// operators and their operands as they were.
//
// The full rewrite the file-structure fixer asks for does not reach these: it
// re-serialises objects, and a content stream's body is carried across as
// bytes. Those violations were reported handled and came back as residuals on
// every pass.
func fixHexString(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	count := 0
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if repairPageHexStrings(ctx, alloc, doc, page, i+1) {
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("rewrote %d content stream(s) holding a malformed hex string", count)}, nil
}

// repairPageHexStrings re-serialises one page when its content carries an
// odd-digit hex string, reporting whether it did.
func repairPageHexStrings(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page, pageNum int) bool {
	pageDict := resolveFromAllocOrDoc(ctx, alloc, doc, page.Ref)
	if pageDict == nil {
		pageDict = page.Dict
	}
	contentsObj, ok := pageDict["Contents"]
	if !ok {
		return false
	}
	data, err := liveContentBytes(ctx, alloc, doc, contentsObj)
	if err != nil || len(data) == 0 {
		return false
	}

	// The detector's parse, so every page litho reports one on is rewritten
	// (compliance.checkContentStreamFormat). The detector judges only the
	// strings an operator reads, so a page is also rewritten for a malformed
	// string it does not report, which re-serialising repairs all the same.
	ops, violations := content.ParseStreamDataFormatChecked(ctx, data)
	if !hasMalformedHexViolation(violations) {
		return false
	}
	if err := replaceContentStream(ctx, alloc, doc, contentsObj, serializeOps(ops), pageNum); err != nil {
		doc.Logger.Debug("remediation: cannot rewrite a content stream for an odd-digit hex string",
			"page", pageNum, "error", err)
		return false
	}
	return true
}

// hasMalformedHexViolation reports whether any parse violation is one of the
// two the fixer repairs: one wrapping [parser.ErrHexStringOddDigits] or
// [parser.ErrHexStringInvalidChar].
//
// A stray non-hexadecimal character used to be repaired only as a side effect:
// the old parity test counted hex digits, so "<41!4>" read as odd and the page
// was re-serialised, dropping the character with it. Counting non-white-space
// characters instead (ISO 19005-2, 6.1.6 t1, which veraPDF measures the same
// way) leaves that string even, so the page was no longer rewritten and its
// invalid character stayed. The parser already drops the character when it
// reads the value (ISO 32000-1, 7.3.4.3 assumes a missing final digit is 0),
// so re-serialising writes what the reader understood.
//
// # Corpus evidence
//
//   - verapdf: once the parity test counted non-white-space characters, the
//     invalid character left unrepaired cost 30 files their remediated
//     verdict.
func hasMalformedHexViolation(violations []error) bool {
	for _, v := range violations {
		if errors.Is(v, parser.ErrHexStringOddDigits) || errors.Is(v, parser.ErrHexStringInvalidChar) {
			return true
		}
	}
	return false
}
