package remediation

import (
	"context"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// fixer is a targeted repair that handles one or more related violations.
type fixer struct {
	name    string
	matches func(v compliance.Violation) bool
	apply   func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, violations []compliance.Violation, profile string) ([]string, error)
}

// registry lists all fixers in priority order.
// Tier 1 (content-preserving) fixers run first, then Tier 2 (aggressive).
var registry = []fixer{
	// Tier 1: catalog and metadata.
	{name: "trailer-id", matches: matchTrailerID, apply: fixTrailerID},
	{name: "xmp-metadata", matches: matchXMP, apply: fixXMP},
	{name: "output-intent", matches: matchOutputIntent, apply: fixOutputIntent},
	{name: "corrupt-icc", matches: matchCorruptICC, apply: fixCorruptICC},
	{name: "mark-info", matches: matchMarkInfo, apply: fixMarkInfo},
	{name: "struct-tree", matches: matchStructTree, apply: fixStructTree},
	{name: "viewer-prefs", matches: matchViewerPrefs, apply: fixViewerPrefs},
	{name: "catalog-lang", matches: matchCatalogLang, apply: fixCatalogLang},
	{name: "need-appearances", matches: matchNeedAppearances, apply: fixNeedAppearances},
	// Tier 1: resource-level.
	{name: "graphics", matches: matchGraphics, apply: fixGraphics},
	{name: "transparency", matches: matchTransparency, apply: fixTransparency},
	{name: "file-structure", matches: matchFileStructure, apply: fixFileStructure},
	{name: "catalog-cleanup", matches: matchCatalogCleanup, apply: fixCatalogCleanup},
	{name: "annotations", matches: matchAnnotations, apply: fixAnnotations},
	{name: "javascript", matches: matchJavaScript, apply: fixJavaScript},
	{name: "xfa", matches: matchXFA, apply: fixXFA},
	{name: "fonts", matches: matchFontFixes, apply: fixFontIssues},
	{name: "lzw", matches: matchLZW, apply: fixLZW},
	{name: "af-relationship", matches: matchAFRelationship, apply: fixAFRelationship},
	// Tier 2: aggressive (content-modifying, gated on PreserveContent=false).
	{name: "aggressive", matches: matchAggressive, apply: fixAggressive},
}

// clausePrefix checks if a violation's rule clause starts with any of the given prefixes.
func clausePrefix(v compliance.Violation, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(v.Rule.Clause, p) {
			return true
		}
	}
	return false
}

// descriptionContains checks if a violation's description contains a substring (case-insensitive).
func descriptionContains(v compliance.Violation, sub string) bool {
	return strings.Contains(strings.ToLower(v.Rule.Description), strings.ToLower(sub))
}
