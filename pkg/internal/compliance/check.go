package compliance

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// Violation records a single rule violation with context about where it occurred.
// It implements the error interface so callers receive typed violations.
//
//nolint:errname // Violation is the domain name; "ViolationError" would be redundant.
type Violation struct {
	// Rule is the VeraPDF validation rule that was violated.
	Rule Rule

	// Context describes where the violation occurred (e.g., page number,
	// object reference, font name).
	Context string
}

// Error implements the error interface so that Violation can be returned directly
// as an error value in ([]error, error) signatures.
func (r Violation) Error() string {
	if r.Context != "" {
		return fmt.Sprintf("clause %s test %d: %s [%s]", r.Rule.Clause, r.Rule.TestNumber, r.Rule.Description, r.Context)
	}
	return fmt.Sprintf("clause %s test %d: %s", r.Rule.Clause, r.Rule.TestNumber, r.Rule.Description)
}

// String returns a human-readable description of the violation.
func (r Violation) String() string {
	return r.Error()
}

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
}

// checkGroup is a function that validates a group of related rules against a
// document. It returns all violations found (empty slice = pass).
type checkGroup func(ctx context.Context, vctx *validationCtx) []Violation

// --- Profile entry points ---

// ValidatePDFA audits the document against a specific PDF/A profile, returning
// all compliance violations found as errors. Each error is a [Violation] value
// that can be type-asserted to access the structured Rule and Context fields.
// An empty slice means the document passes.
func ValidatePDFA(ctx context.Context, doc *document.Document, profile models.PDFAProfile) ([]error, error) {
	slog.Debug("compliance: starting PDF/A validation", "profile", profile)

	groups, ok := pdfaCheckGroups[profile]
	if !ok {
		slog.Error("compliance: unknown PDF/A profile", "profile", profile)
		return nil, fmt.Errorf("compliance: unknown PDF/A profile: %s", profile)
	}

	catalog, err := getCatalog(ctx, doc)
	if err != nil {
		slog.Error("compliance: failed to get catalog", "error", err)
		return nil, fmt.Errorf("compliance: failed to get catalog: %w", err)
	}

	// Enable parser-level format checking and resolve all objects upfront.
	doc.ComplianceMode = true
	resolveAllObjects(ctx, doc)

	violations := runCheckGroups(ctx, doc, catalog, string(profile), groups)

	slog.Debug("compliance: PDF/A validation complete",
		"profile", profile,
		"violations", len(violations),
	)
	return violationsToErrors(violations), nil
}

// ValidatePDFUA audits the document against a specific PDF/UA profile,
// returning all compliance violations found as errors. Each error is a
// [Violation] value that can be type-asserted to access the structured Rule
// and Context fields. An empty slice means the document passes.
func ValidatePDFUA(ctx context.Context, doc *document.Document, profile models.PDFUAProfile) ([]error, error) {
	slog.Debug("compliance: starting PDF/UA validation", "profile", profile)

	groups, ok := pdfuaCheckGroups[profile]
	if !ok {
		slog.Error("compliance: unknown PDF/UA profile", "profile", profile)
		return nil, fmt.Errorf("compliance: unknown PDF/UA profile: %s", profile)
	}

	catalog, err := getCatalog(ctx, doc)
	if err != nil {
		slog.Error("compliance: failed to get catalog", "error", err)
		return nil, fmt.Errorf("compliance: failed to get catalog: %w", err)
	}

	doc.ComplianceMode = true
	resolveAllObjects(ctx, doc)

	violations := runCheckGroups(ctx, doc, catalog, string(profile), groups)

	slog.Debug("compliance: PDF/UA validation complete",
		"profile", profile,
		"violations", len(violations),
	)
	return violationsToErrors(violations), nil
}

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
		vctx.pages[i] = pageCtx{
			dict:      page,
			resources: res,
		}
	}

	return vctx
}

// runCheckGroups executes all check groups against the document, collecting
// violations until context cancellation or all groups complete.
func runCheckGroups(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, groups []checkGroup) []Violation {
	vctx := buildValidationCtx(ctx, doc, catalog, profile)

	var violations []Violation
	for _, group := range groups {
		if ctx.Err() != nil {
			slog.Debug("compliance: context cancelled, stopping validation")
			return violations
		}
		violations = append(violations, group(ctx, vctx)...)
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

// --- Profile → check group mappings ---

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
