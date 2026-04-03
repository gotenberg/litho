package compliance

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// validationCtx holds pre-computed document data shared across all check groups.
// It eliminates redundant page tree traversals and resource resolution.
type validationCtx struct {
	doc     *document.Document
	catalog ast.PdfDict
	profile string
	part    int  // pdfaPart(profile), computed once
	level   byte // pdfaLevel(profile), computed once
	ua      bool // isPDFUA(profile), computed once
	pages   []pageCtx
}

// pageCtx holds pre-resolved data for a single page.
type pageCtx struct {
	dict      ast.PdfDict
	resources ast.PdfDict // resolved, with inherited fallback from Pages tree
	content   []byte      // decoded content stream bytes (nil if decode fails)
	ops       []content.Operation
	opsOnce   sync.Once
}

// parsedOps returns the lazily-parsed content stream operations. The result is
// cached so that multiple check groups sharing the same pageCtx avoid redundant
// ParseStreamData calls. Returns nil if content is empty or parsing fails.
func (p *pageCtx) parsedOps() []content.Operation {
	p.opsOnce.Do(func() {
		if len(p.content) > 0 {
			p.ops, _ = content.ParseStreamData(p.content)
		}
	})
	return p.ops
}

// checkGroup is a function that validates a group of related rules against a
// document. It returns all violations found (empty slice = pass).
type checkGroup func(ctx context.Context, vctx *validationCtx) []Violation

// --- Profile to check group mappings ---

//nolint:gochecknoglobals // package-level rule tables are the core design.
var pdfaCheckGroups map[models.PDFAProfile][]checkGroup

//nolint:gochecknoglobals // package-level rule tables are the core design.
var pdfuaCheckGroups map[models.PDFUAProfile][]checkGroup

func init() { //nolint:gochecknoinits // breaks initialization cycle.
	allPDFA := []checkGroup{
		checkFileStructureGroup,
		checkColorGroup,
		checkFontGroup,
		checkGraphicsGroup,
		checkTransparencyGroup,
		checkXObjectGroup,
		checkAnnotationGroup,
		checkActionGroup,
		checkXMPGroup,
		checkStructureGroup,
		checkFormGroup,
		checkEmbeddedGroup,
		checkDocumentGroup,
		checkContentGroup,
	}

	pdfaCheckGroups = map[models.PDFAProfile][]checkGroup{
		models.PDFA1b: allPDFA,
		models.PDFA1a: allPDFA,
		models.PDFA2b: allPDFA,
		models.PDFA2u: allPDFA,
		models.PDFA2a: allPDFA,
		models.PDFA3b: allPDFA,
		models.PDFA3u: allPDFA,
		models.PDFA3a: allPDFA,
		models.PDFA4:  allPDFA,
		models.PDFA4e: allPDFA,
		models.PDFA4f: allPDFA,
	}

	pdfuaCheckGroups = map[models.PDFUAProfile][]checkGroup{
		models.PDFUA1: {
			checkFontGroup,
			checkStructureGroup,
			checkAccessibilityGroup,
			checkFormGroup,
		},
		models.PDFUA2: {
			checkFontGroup,
			checkStructureGroup,
			checkAccessibilityGroup,
		},
	}
}

// --- Internal orchestration ---

// buildValidationCtx pre-computes the shared validation context from the
// document catalog. Pages are collected once and resources resolved with
// inherited fallback from the Pages tree root.
func buildValidationCtx(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) *validationCtx {
	vctx := &validationCtx{
		doc:     doc,
		catalog: catalog,
		profile: profile,
		part:    pdfaPart(profile),
		level:   pdfaLevel(profile),
		ua:      isPDFUA(profile),
	}

	// Resolve inherited resources from Pages tree root.
	pagesDict, _ := document.ResolveDict(ctx, doc, catalog["Pages"])
	var inheritedRes ast.PdfDict
	if pagesDict != nil {
		inheritedRes = getResources(ctx, doc, pagesDict)
	}

	// Collect pages once and resolve resources per page.
	rawPages := collectPages(ctx, doc, catalog)
	vctx.pages = make([]pageCtx, len(rawPages))
	for i, page := range rawPages {
		res := getResources(ctx, doc, page)
		if res == nil {
			res = inheritedRes
		}
		contentBytes, _ := getPageContentBytes(ctx, doc, page)
		vctx.pages[i] = pageCtx{
			dict:      page,
			resources: res,
			content:   contentBytes,
		}
	}

	return vctx
}

// runCheckGroups executes all check groups concurrently against the document,
// collecting violations until context cancellation or all groups complete.
// The document resolver is safe for concurrent use, and the validation context
// is read-only during check execution (pageCtx.parsedOps uses sync.Once).
func runCheckGroups(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, groups []checkGroup) []Violation {
	vctx := buildValidationCtx(ctx, doc, catalog, profile)

	type result struct {
		index      int
		violations []Violation
	}

	ch := make(chan result, len(groups))
	for i, g := range groups {
		go func() {
			ch <- result{index: i, violations: g(ctx, vctx)}
		}()
	}

	// Collect results in submission order for deterministic output.
	collected := make([][]Violation, len(groups))
	for range groups {
		r := <-ch
		collected[r.index] = r.violations
	}

	var violations []Violation
	for _, vs := range collected {
		violations = append(violations, vs...)
	}
	return violations
}

// violationsToErrors converts a slice of Violation into a slice of error.
func violationsToErrors(violations []Violation) []error {
	if len(violations) == 0 {
		return nil
	}
	errs := make([]error, len(violations))
	for i, r := range violations {
		errs[i] = r
	}
	return errs
}

// --- Shared helpers ---

// getCatalog retrieves the document catalog dictionary.
func getCatalog(ctx context.Context, doc *document.Document) (ast.PdfDict, error) {
	catalog, err := document.GetCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	return catalog.Dict, nil
}

// resolveAllObjects forces resolution of every indirect object in the xref
// table. This ensures parser-level format checks run on ALL objects.
func resolveAllObjects(ctx context.Context, doc *document.Document) {
	slog.Debug("compliance: resolving all objects for format checks")
	count := 0
	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			return
		}
		if entry.Free {
			continue
		}
		_, _ = document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		count++
	}
	slog.Debug("compliance: resolved all objects", "count", count)
}

// collectPages traverses the page tree and returns all leaf page dicts.
func collectPages(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []ast.PdfDict {
	pagesObj, ok := catalog["Pages"]
	if !ok {
		slog.Debug("compliance: catalog missing /Pages")
		return nil
	}
	var pages []ast.PdfDict
	collectPagesRecursive(ctx, doc, pagesObj, &pages, 0)
	slog.Debug("compliance: collected pages", "count", len(pages))
	return pages
}

// maxPageTreeDepth limits recursion when walking the page tree. Page trees are
// typically shallow (a few levels deep), so 100 is conservative.
const maxPageTreeDepth = 100

func collectPagesRecursive(ctx context.Context, doc *document.Document, obj ast.PdfObject, pages *[]ast.PdfDict, depth int) {
	if ctx.Err() != nil || depth > maxPageTreeDepth {
		return
	}
	dict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || dict == nil {
		return
	}
	typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
	switch typeName {
	case "Pages":
		kids, err := document.ResolveArray(ctx, doc, dict["Kids"])
		if err != nil {
			return
		}
		for _, kid := range kids {
			collectPagesRecursive(ctx, doc, kid, pages, depth+1)
		}
	case "Page":
		*pages = append(*pages, dict)
	default:
		// Some producers omit /Type; try /Kids to distinguish.
		if _, hasKids := dict["Kids"]; hasKids {
			kids, err := document.ResolveArray(ctx, doc, dict["Kids"])
			if err != nil {
				return
			}
			for _, kid := range kids {
				collectPagesRecursive(ctx, doc, kid, pages, depth+1)
			}
		} else {
			*pages = append(*pages, dict)
		}
	}
}

// getResources returns the /Resources dict from a page.
func getResources(ctx context.Context, doc *document.Document, page ast.PdfDict) ast.PdfDict {
	res, err := document.ResolveDict(ctx, doc, page["Resources"])
	if err != nil {
		slog.Debug("compliance: cannot resolve page resources", "error", err)
		return nil
	}
	return res
}

// fail creates a single-element Violation slice for a rule violation.
func fail(rule Rule, context string) []Violation {
	return []Violation{{Rule: rule, Context: context}}
}

// failf creates a single-element Violation slice with a formatted context string.
func failf(rule Rule, format string, args ...any) []Violation {
	return []Violation{{Rule: rule, Context: fmt.Sprintf(format, args...)}}
}

// ruleByPart selects a Rule from up to four part-specific variants.
// part must be 1..4; any other value returns a zero Rule.
func ruleByPart(part int, r1, r2, r3, r4 Rule) Rule {
	switch part {
	case 1:
		return r1
	case 2:
		return r2
	case 3:
		return r3
	case 4:
		return r4
	}
	return Rule{}
}

// pdfaPart returns the numeric part (1, 2, 3, 4) from a profile string.
func pdfaPart(profile string) int {
	switch profile {
	case "PDF/A-1a", "PDF/A-1b":
		return 1
	case "PDF/A-2a", "PDF/A-2b", "PDF/A-2u":
		return 2
	case "PDF/A-3a", "PDF/A-3b", "PDF/A-3u":
		return 3
	case "PDF/A-4", "PDF/A-4e", "PDF/A-4f":
		return 4
	}
	return 0
}

// pdfaLevel returns the conformance level character from a profile string.
func pdfaLevel(profile string) byte {
	if len(profile) == 0 {
		return 0
	}
	return profile[len(profile)-1]
}

// isPDFUA returns true if the profile string starts with "PDF/UA".
func isPDFUA(profile string) bool {
	return len(profile) >= 6 && profile[:6] == "PDF/UA"
}

// safeReadHeader reads the first n bytes from the document file.
// Returns an error if the underlying reader is nil.
func safeReadHeader(doc *document.Document, n int64) ([]byte, error) {
	if doc.RawReader() == nil {
		return nil, fmt.Errorf("cannot read header: nil reader")
	}
	return doc.ReadFileBytes(0, n)
}

// resolveNumber resolves a PdfObject to a float64, handling both PdfInt and
// PdfReal types. Returns (0, error) if the object is neither type.
func resolveNumber(obj ast.PdfObject) (float64, error) {
	switch v := obj.(type) {
	case ast.PdfInt:
		return float64(v), nil
	case ast.PdfReal:
		return float64(v), nil
	default:
		return 0, fmt.Errorf("expected number, got %T", obj)
	}
}
