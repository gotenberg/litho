package remediation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

// matchTextControl matches ISO 19005-2 §6.2.11.8 and ISO 19005-4
// §6.2.10.9 violations: text-showing operators render the .notdef
// glyph because the character code resolves to a missing or undefined
// entry in the font's charset.
func matchTextControl(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.10.9", "6.2.11.8") {
		return false
	}
	return descriptionContains(v, ".notdef") || contextContains(v, ".notdef")
}

// fixTextControl strips ASCII control bytes (0x00-0x1F excluding TAB,
// LF, CR) from every text-showing operator in every page's content
// stream. These bytes typically map to .notdef in standard font
// encodings and are almost never intended glyph references; removing
// them eliminates the violation without affecting legitimate text.
func fixTextControl(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, _ string) ([]string, error) {
	// Dropping bytes from Tj/TJ operands removes whatever glyph those
	// codes mapped to. That is the standard .notdef-reference fix, but
	// it is still content loss; gate behind PreserveContent.
	if opts.PreserveContent {
		return nil, nil
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	// Pages share font objects, and describing a font parses its program.
	// Resolve each font object once per document.
	fontCache := make(map[int]textFontInfo)

	total := 0
	for pageNum, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ops, contentsObj, err := livePageContentOps(ctx, alloc, doc, page)
		if err != nil || contentsObj == nil {
			continue
		}
		removed, modified := stripControlBytesFromOps(ops, pageTextFonts(ctx, doc, page, fontCache))
		if !modified {
			continue
		}
		total += removed
		filtered := filterOps(ops)
		newData := serializeOps(filtered)
		if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum+1); err != nil {
			return nil, fmt.Errorf("replacing content stream on page %d: %w", pageNum+1, err)
		}
	}
	if total == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("stripped %d control-byte(s) from text operators", total)}, nil
}

// stripControlBytesFromOps mutates the operand slices of every
// text-showing operator (Tj, TJ, ', ") in place, removing bytes that
// fall in the ASCII control range 0x00-0x1F except TAB/LF/CR.
// Operators whose entire operand becomes empty are cleared so the
// serialiser drops them. Returns the count of removed bytes plus a
// flag indicating whether any op changed.
func stripControlBytesFromOps(ops []content.Operation, fonts map[string]textFontInfo) (int, bool) {
	removed := 0
	modified := false
	var currentFont string
	for i := range ops {
		op := ops[i].Operator
		if op == "Tf" && len(ops[i].Operands) > 0 {
			if name, ok := ops[i].Operands[0].(ast.PdfName); ok {
				currentFont = string(name)
			}
		}
		if op != "Tj" && op != "TJ" && op != "'" && op != "\"" {
			continue
		}
		// A composite font addresses glyphs with multi-byte codes
		// (ISO 32000-1, 9.7.6), so a byte below 0x20 is usually the high
		// half of an ordinary code, not a glyph reference: under
		// Identity-H every CID under U+2000 carries a 0x00 high byte.
		// Dropping those bytes deletes half of each code and slides the
		// rest of the string, which turned "A different method" into
		// "㼼8E8AG@8" and cost issue2856 a third of its text.
		//
		// TODO: a composite font could still be repaired by dropping whole
		// codes rather than bytes, which needs the /Encoding CMap's
		// codespace ranges to know each code's width (ISO 32000-1, 9.7.6.2);
		// Identity-H is two bytes but a predefined CMap may mix widths.
		// Skipping leaves ~56 .notdef references unfixed across the veraPDF
		// corpus, which is the safe side of the trade.
		info := fonts[currentFont]
		if info.composite {
			continue
		}
		for j, operand := range ops[i].Operands {
			switch v := operand.(type) {
			case ast.PdfString:
				cleaned, n := stripControlString(v, info.defined)
				if n == 0 {
					continue
				}
				removed += n
				modified = true
				ops[i].Operands[j] = cleaned
				if len(cleaned) == 0 {
					ops[i].Operator = ""
				}
			case ast.PdfArray:
				cleanedArr, n := stripControlTJArray(v, info.defined)
				if n == 0 {
					continue
				}
				removed += n
				modified = true
				ops[i].Operands[j] = cleanedArr
				if isEmptyTJArray(cleanedArr) {
					ops[i].Operator = ""
				}
			}
		}
	}
	return removed, modified
}

// stripControlString returns a copy of s with ASCII control bytes
// removed. The returned count reports how many bytes were stripped.
func stripControlString(s ast.PdfString, defined map[byte]bool) (ast.PdfString, int) {
	src := []byte(s)
	dst := make([]byte, 0, len(src))
	removed := 0
	for _, b := range src {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' && !defined[b] {
			removed++
			continue
		}
		dst = append(dst, b)
	}
	if removed == 0 {
		return s, 0
	}
	return ast.PdfString(dst), removed
}

// stripControlTJArray cleans every PdfString inside a TJ operand array.
func stripControlTJArray(arr ast.PdfArray, defined map[byte]bool) (ast.PdfArray, int) {
	out := make(ast.PdfArray, 0, len(arr))
	removed := 0
	for _, elem := range arr {
		if s, ok := elem.(ast.PdfString); ok {
			cleaned, n := stripControlString(s, defined)
			removed += n
			if len(cleaned) == 0 {
				continue
			}
			out = append(out, cleaned)
			continue
		}
		out = append(out, elem)
	}
	return out, removed
}

// isEmptyTJArray reports whether arr holds no text content after
// cleaning: only spacing adjustments (numbers) remain.
func isEmptyTJArray(arr ast.PdfArray) bool {
	for _, elem := range arr {
		if _, ok := elem.(ast.PdfString); ok {
			return false
		}
	}
	return true
}

// textFontInfo records what a page's font says about the codes drawn with it.
type textFontInfo struct {
	// composite marks a font addressing glyphs with multi-byte codes, whose
	// strings must not be edited a byte at a time.
	composite bool
	// defined holds the single-byte codes the font's /Encoding gives a glyph
	// for, which are therefore not .notdef references.
	defined map[byte]bool
}

// pageTextFonts describes every font in a page's resources, reusing entries
// already described for another page through cache, keyed by object number.
func pageTextFonts(ctx context.Context, doc *document.Document, page *document.Page, cache map[int]textFontInfo) map[string]textFontInfo {
	if page == nil || page.Resources == nil {
		return nil
	}
	fonts, err := document.ResolveDict(ctx, doc, page.Resources["Font"])
	if err != nil || fonts == nil {
		doc.Logger.Debug("resolving page fonts for control-byte strip", slog.Any("err", err))
		return nil
	}
	infos := make(map[string]textFontInfo, len(fonts))
	for name, entry := range fonts {
		var objNum int
		if ref, ok := entry.(ast.PdfIndirectRef); ok {
			objNum = ref.ObjectNum
			if cached, ok := cache[objNum]; ok {
				infos[string(name)] = cached
				continue
			}
		}
		fd, err := document.ResolveDict(ctx, doc, entry)
		if err != nil || fd == nil {
			// An unresolvable font could be composite; treat it as such
			// so a byte-level edit is never applied blind.
			infos[string(name)] = textFontInfo{composite: true}
			if objNum > 0 {
				cache[objNum] = textFontInfo{composite: true}
			}
			continue
		}
		if subtype, _ := document.ResolveName(ctx, doc, fd["Subtype"]); subtype == "Type0" {
			infos[string(name)] = textFontInfo{composite: true}
			if objNum > 0 {
				cache[objNum] = textFontInfo{composite: true}
			}
			continue
		}
		info := textFontInfo{}
		if font, err := typography.ResolveFont(ctx, doc, fd); err == nil && font != nil {
			for code := range font.Encoding {
				if code < 0x20 {
					if info.defined == nil {
						info.defined = make(map[byte]bool)
					}
					info.defined[code] = true
				}
			}
		} else {
			doc.Logger.Debug("resolving font encoding for control-byte strip", slog.Any("err", err))
		}
		infos[string(name)] = info
		if objNum > 0 {
			cache[objNum] = info
		}
	}
	return infos
}
