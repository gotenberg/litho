package remediation

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// oddHexDigits is the parser's wording for a hex string whose digit count is
// odd, which is what the fixer below repairs.
const oddHexDigits = "hex string has odd number of digits"

// matchHexString matches an odd-digit hex string inside a page content stream
// (ISO 19005-1/2/3 §6.1.6 test 1, ISO 19005-4 §6.1.5 test 1).
func matchHexString(v compliance.Violation) bool {
	if !clausePrefix(v, "6.1.5", "6.1.6") {
		return false
	}
	return contextContains(v, oddHexDigits)
}

// fixHexString rewrites the content streams that hold a hex string with an odd
// number of digits.
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
	return []string{fmt.Sprintf("rewrote %d content stream(s) holding an odd-digit hex string", count)}, nil
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

	// The detector's own parse, so a page is rewritten exactly when litho
	// reports one (compliance.checkContentStreamHexStrings).
	ops, violations := content.ParseStreamDataFormatChecked(ctx, data)
	if !hasOddHexViolation(violations) {
		return false
	}
	if err := replaceContentStream(ctx, alloc, doc, contentsObj, serializeOps(ops), pageNum); err != nil {
		doc.Logger.Debug("remediation: cannot rewrite a content stream for an odd-digit hex string",
			"page", pageNum, "error", err)
		return false
	}
	return true
}

// hasOddHexViolation reports whether any parse violation is the odd-digit one.
// A page whose only complaint is an invalid character is left alone: dropping
// that character is a different repair, and re-serialising would silently make
// the string mean something else.
func hasOddHexViolation(violations []error) bool {
	for _, v := range violations {
		if v != nil && strings.Contains(v.Error(), oddHexDigits) {
			return true
		}
	}
	return false
}
