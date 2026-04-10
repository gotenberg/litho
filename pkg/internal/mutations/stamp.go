package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// StampText overlays text on the specified pages using a standard font.
// Pages is the full set of resolved pages; pageNums contains 1-based page
// numbers to stamp. If pageNums is empty, all pages are stamped.
func StampText(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page, text string, x, y, fontSize, opacity float64, pageNums []int) error {
	// If no pages specified, stamp all.
	if len(pageNums) == 0 {
		pageNums = make([]int, len(pages))
		for i := range pages {
			pageNums[i] = i + 1
		}
	}

	// Create a Helvetica font resource (standard Type1, always available).
	fontDict := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName("Helvetica"),
	}
	fontRef := alloc.Alloc(fontDict)

	// Escape parentheses in the text for PDF string literal.
	escaped := strings.ReplaceAll(text, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "(", "\\(")
	escaped = strings.ReplaceAll(escaped, ")", "\\)")

	if fontSize == 0 {
		fontSize = 12
	}

	// Build the content stream for the stamp.
	var ops strings.Builder
	ops.WriteString("\nq ")
	fmt.Fprintf(&ops, "BT /StampF1 %s Tf %s %s Td (%s) Tj ET ",
		FormatFloat(fontSize), FormatFloat(x), FormatFloat(y), escaped)
	ops.WriteString("Q")

	stampStream := ast.NewStream(ast.PdfDict{}, []byte(ops.String()))
	stampRef := alloc.Alloc(stampStream)

	for _, pageNum := range pageNums {
		if pageNum < 1 || pageNum > len(pages) {
			doc.Logger.Debug("skipping out-of-range stamp page",
				slog.Int("page", pageNum),
				slog.Int("total", len(pages)),
			)
			continue
		}
		page := pages[pageNum-1]

		// Add font to page resources.
		resources := make(ast.PdfDict, len(page.Resources)+1)
		maps.Copy(resources, page.Resources)
		fontResDict := ast.PdfDict{}
		if existingFonts, ok := page.Resources["Font"]; ok {
			if fd, err := document.ResolveDict(ctx, doc, existingFonts); err == nil && fd != nil {
				maps.Copy(fontResDict, fd)
			}
		}
		fontResDict["StampF1"] = fontRef
		resources["Font"] = fontResDict

		// Append stamp stream to page contents.
		newDict := make(ast.PdfDict, len(page.Dict)+1)
		maps.Copy(newDict, page.Dict)
		newDict["Resources"] = resources
		if existing, ok := page.Dict["Contents"]; ok {
			newDict["Contents"] = ast.PdfArray{existing, stampRef}
		} else {
			newDict["Contents"] = stampRef
		}
		alloc.AllocAt(page.Ref.ObjectNum, newDict)
	}

	return nil
}
