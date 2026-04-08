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
// Catalog-level fixers run first, then resource-level.
var registry = []fixer{
	{name: "trailer-id", matches: matchTrailerID, apply: fixTrailerID},
	{name: "xmp-metadata", matches: matchXMP, apply: fixXMP},
	{name: "output-intent", matches: matchOutputIntent, apply: fixOutputIntent},
	{name: "mark-info", matches: matchMarkInfo, apply: fixMarkInfo},
	{name: "struct-tree", matches: matchStructTree, apply: fixStructTree},
	{name: "viewer-prefs", matches: matchViewerPrefs, apply: fixViewerPrefs},
	{name: "catalog-lang", matches: matchCatalogLang, apply: fixCatalogLang},
	{name: "need-appearances", matches: matchNeedAppearances, apply: fixNeedAppearances},
	{name: "javascript", matches: matchJavaScript, apply: fixJavaScript},
	{name: "xfa", matches: matchXFA, apply: fixXFA},
}

// descriptionContains checks if a violation's description contains a substring (case-insensitive).
func descriptionContains(v compliance.Violation, sub string) bool {
	return strings.Contains(strings.ToLower(v.Rule.Description), strings.ToLower(sub))
}
