package bookmarks

import (
	"context"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// headingTypes are the structure element types recognized as headings.
// Keys include standard ISO 32000 types and common remappings.
var headingTypes = map[string]int{
	"Title": 1,
	"H":     1, // generic heading (PDF 2.0)
	"H1":    1,
	"H2":    2,
	"H3":    3,
	"H4":    4,
	"H5":    5,
	"H6":    6,
}

// GenerateFromStructTree walks the document's structure tree and extracts
// headings as a hierarchical bookmark list. Returns nil if the document has
// no structure tree, or if no headings are found.
//
// pageRefToNum maps a page indirect reference to its 1-based page number.
// mcidTextByPage maps (pageNum, MCID) to the text on that page.
func GenerateFromStructTree(
	ctx context.Context,
	doc *document.Document,
	pageRefToNum map[ast.PdfIndirectRef]int,
	mcidTextByPage map[int]map[int]string,
	opts Options,
) ([]models.Bookmark, error) {
	opts.applyDefaults()

	// Recovered ref first (ISO 32000-1 §7.5.5 Table 15: /Root shall be the
	// catalog), falling back to the raw trailer ref when no catalog object is
	// found at all, so a file this used to read is never read less well.
	catalogRef, ok := doc.CatalogRef(ctx)
	if !ok {
		catalogRef, ok = doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	}
	if !ok {
		return nil, nil
	}
	catalog, err := document.ResolveDict(ctx, doc, catalogRef)
	if err != nil || catalog == nil {
		return nil, nil
	}
	strTreeObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil, nil
	}
	strTree, err := document.ResolveDict(ctx, doc, strTreeObj)
	if err != nil || strTree == nil {
		return nil, nil
	}

	// Resolve RoleMap if present: non-standard type → standard type.
	roleMap := resolveRoleMap(ctx, doc, strTree["RoleMap"])

	// Walk the tree and collect heading candidates.
	var candidates []headingCandidate
	walkStructElem(ctx, doc, strTree, 0, roleMap, pageRefToNum, mcidTextByPage, opts, &candidates)

	if len(candidates) == 0 {
		return nil, nil
	}

	// Build a level map from the candidates' "fontSize" field (which we
	// repurpose as the heading level × -1 so larger = earlier).
	levels := make(map[float64]int)
	for _, c := range candidates {
		lvl := max(int(-c.fontSize), 1)
		levels[c.fontSize] = lvl
	}

	return buildBookmarkTree(candidates, levels), nil
}

// walkStructElem recursively walks a struct element, collecting heading
// candidates with their inferred page numbers.
func walkStructElem(
	ctx context.Context,
	doc *document.Document,
	elem ast.PdfDict,
	currentPage int,
	roleMap map[string]string,
	pageRefToNum map[ast.PdfIndirectRef]int,
	mcidTextByPage map[int]map[int]string,
	opts Options,
	out *[]headingCandidate,
) {
	if err := ctx.Err(); err != nil {
		return
	}

	// Inherit page reference from /Pg if present.
	if pgObj, ok := elem["Pg"]; ok {
		if ref, ok := pgObj.(ast.PdfIndirectRef); ok {
			if num, ok := pageRefToNum[ref]; ok {
				currentPage = num
			}
		}
	}

	// Determine structure type, remapping non-standard via RoleMap.
	sType := ""
	if sObj, ok := elem["S"]; ok {
		if name, ok := sObj.(ast.PdfName); ok {
			sType = string(name)
		}
	}
	if mapped, ok := roleMap[sType]; ok && mapped != "" {
		sType = mapped
	}

	// Check if this is a heading.
	if level, isHeading := headingTypes[sType]; isHeading && level <= opts.MaxDepth {
		text := collectStructText(ctx, doc, elem, currentPage, pageRefToNum, mcidTextByPage)
		text = normalizeTitle(text, opts.MaxTitleLength)
		if text != "" && currentPage > 0 {
			*out = append(*out, headingCandidate{
				text:     text,
				fontSize: float64(-level), // repurpose to carry level
				pageNum:  currentPage,
			})
		}
	}

	// Recurse into children.
	if kObj, ok := elem["K"]; ok {
		walkStructK(ctx, doc, kObj, currentPage, roleMap, pageRefToNum, mcidTextByPage, opts, out)
	}
}

// walkStructK handles the /K array which may contain dicts, refs, integers
// (MCIDs), or nested arrays.
func walkStructK(
	ctx context.Context,
	doc *document.Document,
	kObj ast.PdfObject,
	currentPage int,
	roleMap map[string]string,
	pageRefToNum map[ast.PdfIndirectRef]int,
	mcidTextByPage map[int]map[int]string,
	opts Options,
	out *[]headingCandidate,
) {
	resolved, err := document.ResolveObject(ctx, doc, kObj)
	if err != nil {
		return
	}

	switch v := resolved.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			walkStructK(ctx, doc, kid, currentPage, roleMap, pageRefToNum, mcidTextByPage, opts, out)
		}
	case ast.PdfDict:
		// If it has /S, it's a struct element. Recurse.
		if _, hasS := v["S"]; hasS {
			walkStructElem(ctx, doc, v, currentPage, roleMap, pageRefToNum, mcidTextByPage, opts, out)
		}
		// MCR / OBJR dicts don't have /S and aren't struct elements.
	}
}

// collectStructText walks the subtree rooted at elem and concatenates all
// text from referenced MCIDs.
func collectStructText(
	ctx context.Context,
	doc *document.Document,
	elem ast.PdfDict,
	currentPage int,
	pageRefToNum map[ast.PdfIndirectRef]int,
	mcidTextByPage map[int]map[int]string,
) string {
	var sb strings.Builder
	collectTextFromK(ctx, doc, elem["K"], currentPage, pageRefToNum, mcidTextByPage, &sb)
	return sb.String()
}

func collectTextFromK(
	ctx context.Context,
	doc *document.Document,
	kObj ast.PdfObject,
	currentPage int,
	pageRefToNum map[ast.PdfIndirectRef]int,
	mcidTextByPage map[int]map[int]string,
	sb *strings.Builder,
) {
	if kObj == nil {
		return
	}
	resolved, err := document.ResolveObject(ctx, doc, kObj)
	if err != nil {
		return
	}

	switch v := resolved.(type) {
	case ast.PdfInt:
		// Direct MCID on the current page.
		if text, ok := mcidTextByPage[currentPage][int(v)]; ok {
			if sb.Len() > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(text)
		}
	case ast.PdfDict:
		// MCR, OBJR, or nested struct elem.
		pg := currentPage
		if pgObj, ok := v["Pg"]; ok {
			if ref, ok := pgObj.(ast.PdfIndirectRef); ok {
				if num, ok := pageRefToNum[ref]; ok {
					pg = num
				}
			}
		}

		if mcidObj, ok := v["MCID"]; ok {
			if mcid, ok := mcidObj.(ast.PdfInt); ok {
				if text, ok := mcidTextByPage[pg][int(mcid)]; ok {
					if sb.Len() > 0 {
						sb.WriteByte(' ')
					}
					sb.WriteString(text)
				}
			}
		}

		// Recurse into nested /K.
		if subK, ok := v["K"]; ok {
			collectTextFromK(ctx, doc, subK, pg, pageRefToNum, mcidTextByPage, sb)
		}
	case ast.PdfArray:
		for _, item := range v {
			collectTextFromK(ctx, doc, item, currentPage, pageRefToNum, mcidTextByPage, sb)
		}
	}
}

// resolveRoleMap extracts the /RoleMap dictionary from a StructTreeRoot.
// Returns a map of non-standard type → standard type.
func resolveRoleMap(ctx context.Context, doc *document.Document, obj ast.PdfObject) map[string]string {
	if obj == nil {
		return nil
	}
	d, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || d == nil {
		return nil
	}
	result := make(map[string]string, len(d))
	for k, v := range d {
		if name, ok := v.(ast.PdfName); ok {
			result[string(k)] = string(name)
		}
	}
	return result
}
