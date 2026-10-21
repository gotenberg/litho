package page

import (
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// resourceCategories are the entries of a resource dictionary that map names
// a content stream uses to resources (ISO 32000-1, 7.8.3, Table 33). /ProcSet
// names nothing.
var resourceCategories = []ast.PdfName{"ExtGState", "ColorSpace", "Pattern", "Shading", "XObject", "Font", "Properties"}

// defaultSpaces are the /ColorSpace entries that stand in for a device space
// (ISO 32000-1, 8.6.5.6) rather than name one a content stream selects.
var defaultSpaces = []ast.PdfName{"DefaultGray", "DefaultRGB", "DefaultCMYK"}

// withPageNames returns own, the /Resources of the form XObject ref, with
// the names it does not define looked up in the page's resources: each
// category of own holds its own entries over the page's. The page's default
// colour spaces stay out, since no content stream names them and a form's own
// resources alone cover its colours (ISO 32000-2, 8.6.5.7). The result of a
// form that is an indirect object is kept for the render, so a form painted
// many times is merged once and keeps one identity.
//
// ISO 32000-1, 7.8.3 has a form XObject or a Type 3 font whose stream carries
// no /Resources inherit the resources of the page it is used on, a usage of
// PDF 1.1. Deliberate leniency: a form whose /Resources does not define a name
// it uses resolves that name in the page's resources too, as poppler (Gfx
// chains a form's resources to those in force) and PDFium
// (CPDF_StreamContentParser::FindResourceObj falls back on the page's
// resources) draw it, and as veraPDF reads it (compliance resolves a form's
// names that way, see ISO 19005-2, 6.2.2). An empty /Resources then reads as
// an absent one. A tiling pattern's cell keeps its own resources alone: both
// renderers draw it with a reader of its own, which resolves no name the cell
// does not define.
//
// # Corpus evidence
//
//   - litho sharednested_2b.pdf: two forms under /Resources << >> set a /GS0
//     or /GS1 only the page defines, and poppler 26.09.0 and PDFium (pypdfium2
//     4.30.0) stroke them at the page's line width, 2 on page 1 and 4 on page
//     2, where litho stroked them at the default of 1. Its tiling pattern sets
//     /GS0 alike, and both stroke that cell at the same width on both pages.
//   - litho colouroff_2b.pdf: a coloured tiling cell selects a /CS0 only the
//     page defines, and both renderers fill it in black, as with no space.
func (r *renderer) withPageNames(ref ast.PdfIndirectRef, own ast.PdfDict) ast.PdfDict {
	page := r.page.Resources
	if own == nil || page == nil {
		return own
	}
	if ref.ObjectNum != 0 {
		if merged, ok := r.mergedResources[ref]; ok {
			return merged
		}
	}
	merged := maps.Clone(own)
	for _, category := range resourceCategories {
		inherited, err := document.ResolveDict(r.ctx, r.doc, page[category])
		if err != nil {
			r.doc.Logger.Debug("render: resolving the page's resources for a form", slog.String("category", string(category)), slog.Any("err", err))
			continue
		}
		if len(inherited) == 0 {
			continue
		}
		mine, err := document.ResolveDict(r.ctx, r.doc, own[category])
		if err != nil {
			r.doc.Logger.Debug("render: resolving a form's resources", slog.String("category", string(category)), slog.Any("err", err))
			continue
		}
		entries := maps.Clone(inherited)
		if category == "ColorSpace" {
			for _, name := range defaultSpaces {
				delete(entries, name)
			}
		}
		maps.Copy(entries, mine)
		if len(entries) > 0 {
			merged[category] = entries
		}
	}
	if ref.ObjectNum != 0 {
		if r.mergedResources == nil {
			r.mergedResources = make(map[ast.PdfIndirectRef]ast.PdfDict)
		}
		r.mergedResources[ref] = merged
	}
	return merged
}
