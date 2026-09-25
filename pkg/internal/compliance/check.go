package compliance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"

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
	part    int  // PDFAPart(profile), computed once
	level   byte // PDFALevel(profile), computed once
	ua      bool // IsPDFUA(profile), computed once
	pages   []pageCtx

	// formatViolations is the document's parser-level format violations,
	// snapshotted after every object has been resolved and before the check
	// groups fan out. Reading the live document from a check group returned
	// whatever prefix the goroutines happened to have recorded by then, so
	// the reported count moved from run to run.
	formatViolations []error
}

// pageCtx holds pre-resolved data for a single page.
type pageCtx struct {
	dict        ast.PdfDict
	resources   ast.PdfDict // resolved, with inherited fallback from Pages tree
	content     []byte      // decoded content stream bytes (nil if decode fails)
	ops         []content.Operation
	opsOnce     sync.Once
	parseFailed bool // true only when ParseStreamData errored (not for empty content)

	// contentUnreadable is true when the content stream could not be resolved
	// or decoded at all, including the undecodable-filter case that
	// parseFailed deliberately excludes. A survey that must stay conservative
	// when it cannot see what a page paints (see pageDrawnResources) keys on
	// this rather than on empty operations, which a blank page also produces.
	contentUnreadable bool
}

// parsedOps returns the lazily-parsed content stream operations. The result is
// cached so that multiple check groups sharing the same pageCtx avoid redundant
// ParseStreamData calls. Returns nil if content is empty or parsing fails;
// [pageCtx.parseFailed] distinguishes the two (true only on a parse error, so
// callers can tell "no operators" from "could not parse").
func (p *pageCtx) parsedOps(ctx context.Context) []content.Operation {
	p.opsOnce.Do(func() {
		if len(p.content) > 0 {
			ops, _, err := content.ParseStreamDataWithOverflows(ctx, p.content)
			p.ops = ops // Parse failure yields nil ops; checks run on whatever is available.
			if err != nil {
				// Assigning the comparison would clear the flag a failed decode
				// already set on a page whose bytes never reached the parser.
				p.parseFailed = true
			}
		}
	})
	return p.ops
}

// checkGroup is a function that validates a group of related rules against a
// document. It returns all violations found (empty slice = pass).
type checkGroup func(ctx context.Context, vctx *validationCtx) []Violation

//nolint:gochecknoglobals // package-level rule tables are the core design.
var pdfaCheckGroups map[models.PDFAProfile][]checkGroup

// pdfuaCheckGroups maps each PDF/UA profile to its check groups.
//
// Neither list carries [checkDocumentGroup]. Its checks are PDF/A clauses, and
// veraPDF's PDF/UA-2 profile (PDF_UA/PDFUA-2.xml) states no PDF version test:
// the version check that group once ran for PDF/UA-2 reported under ISO
// 14289-2, 6.2 t1, which is the MarkInfo rule, a rule no oracle states.
// [checkUAHeaderGroup] is PDF/UA-1 only for the same reason: ISO 14289-1, 6.1
// constrains the file header, and the PDF/UA-2 profile has no counterpart.
//
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
			checkUAHeaderGroup,
		},
		models.PDFUA2: {
			checkFontGroup,
			checkStructureGroup,
			checkAccessibilityGroup,
			checkFormGroup,
		},
	}
}

// buildValidationCtx pre-computes the shared validation context from the
// document catalog. Pages are collected once and resources resolved with
// inherited fallback from the Pages tree root.
//
// # Corpus evidence
//
//   - pdfjs issue17190_1.pdf: holds one unembedded TrueType font behind a
//     truncated Flate stream, and litho reported none of the embedding
//     violation veraPDF raises while an undecodable /Contents read as a page
//     with no content.
func buildValidationCtx(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) *validationCtx {
	vctx := &validationCtx{
		doc:     doc,
		catalog: catalog,
		profile: profile,
		part:    PDFAPart(profile),
		level:   PDFALevel(profile),
		ua:      IsPDFUA(profile),
	}

	// Resolve inherited resources from Pages tree root.
	pagesDict, _ := document.ResolveDict(ctx, doc, catalog["Pages"])
	var inheritedRes ast.PdfDict
	if pagesDict != nil {
		inheritedRes = resolveResources(ctx, doc, pagesDict)
	}

	// Collect pages once and resolve resources per page.
	rawPages := collectPages(ctx, doc, catalog)
	vctx.pages = make([]pageCtx, len(rawPages))
	for i, page := range rawPages {
		res := resolveResources(ctx, doc, page)
		if res == nil {
			res = inheritedRes
		}
		contentBytes, contentErr := resolvePageContentBytes(ctx, doc, page)
		vctx.pages[i] = pageCtx{
			dict:      page,
			resources: res,
			content:   contentBytes,
			// A page whose /Contents is present but will not decode has content
			// litho cannot read, which is the case the per-font and per-colour
			// skips call "present but unparseable" and answer conservatively.
			// Discarding the error left it looking like a page with no content
			// at all, so every font declared on it was skipped as unused and
			// an unembedded font behind a truncated stream drew none of the
			// embedding violation veraPDF raises.
			//
			// An undecodable FILTER is excluded. veraPDF does not parse that
			// stream either, so checking every declared font against it would
			// report defects veraPDF never raises; the filter itself is already
			// reported by its own rule.
			parseFailed:       contentErr != nil && !errors.Is(contentErr, errUndecodableContent),
			contentUnreadable: contentErr != nil,
		}
	}

	// Freeze the format violations here, on the single goroutine that
	// builds the context: every object has been resolved by now, and the
	// check groups that run next resolve objects of their own.
	vctx.formatViolations = doc.FormatViolations()

	return vctx
}

// runCheckGroups executes the check groups concurrently and returns their
// violations in group order, so the output does not depend on scheduling. The
// document resolver is safe for concurrent use, and the validation context is
// read-only during check execution (pageCtx.parsedOps uses sync.Once).
//
// Fan-out is bounded by the profile's fixed group list. A group that panics is
// recovered on its own goroutine, logged at Error with its stack, and reported
// as an error wrapping [document.ErrInternalPanic]: the public API boundary's
// recover runs on the calling goroutine and cannot see a panic raised on
// another, which would otherwise end the process. Collection selects on
// ctx.Done(). A cancelled run still waits for every group to return, so no
// goroutine outlives the call, and it returns the context error instead of the
// partial violations the groups stopped at, which would read as a complete
// verdict.
//
// A group can miss a cross-reference lookup, and the first miss runs the
// one-shot raw-byte augmentation that rewrites the index. Run from a group, it
// wrote the index while the other groups were ranging it, and the groups that
// swept it before that moment missed the objects it added while the ones after
// saw them, so the same bytes reported a different violation count from run to
// run (CONTRIBUTING "Deterministic output"). The groups therefore resolve
// under [document.DeferXRefAugmentation]: a miss only sets a flag. When one
// did, the augmentation runs once every group has returned, and when it
// changed the index the groups run again over the same validation context,
// this time over the final index. Only a file whose table misses an object a
// check reaches, and whose bytes hold objects the table lacks, pays for the
// second run.
//
// # Corpus evidence
//
//   - All corpora: no file changes the index during the fan-out at PDF/A-1b,
//     PDF/A-2b, PDF/A-4 or PDF/UA-1, so none pays the second run. pdfjs
//     poppler-742-0-fuzzed.pdf, pdfjs pypdf2332.pdf and verapdf
//     PDF_A-2b__veraPDF_test_suite_6-2-11-4-1-t01-fail-a.pdf miss a lookup
//     during it, and the scan finds nothing to add. pdfjs
//     poppler-85140-0.pdf changes the index while the validation context is
//     built, which is still before the fan-out.
func runCheckGroups(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, groups []checkGroup) ([]Violation, error) {
	vctx := buildValidationCtx(ctx, doc, catalog, profile)

	var missed atomic.Bool
	violations, err := fanOutCheckGroups(document.DeferXRefAugmentation(ctx, &missed), doc, vctx, profile, groups)
	if err != nil || !missed.Load() || !doc.AugmentXRef(ctx) {
		return violations, err
	}
	doc.Logger.Debug("compliance: a check group missed the xref and the augmentation changed it, running the groups again", "profile", profile)
	return fanOutCheckGroups(ctx, doc, vctx, profile, groups)
}

// fanOutCheckGroups runs each check group on its own goroutine over vctx and
// collects the violations in group order. See [runCheckGroups] for the
// panic, cancellation and lifetime contract.
func fanOutCheckGroups(ctx context.Context, doc *document.Document, vctx *validationCtx, profile string, groups []checkGroup) ([]Violation, error) {
	type result struct {
		index      int
		violations []Violation
		err        error
	}

	ch := make(chan result, len(groups))
	for i, g := range groups {
		go func() {
			res := result{index: i}
			defer func() {
				if rec := recover(); rec != nil {
					name := runtime.FuncForPC(reflect.ValueOf(g).Pointer()).Name()
					doc.Logger.Error("compliance: recovered panic in check group",
						"group", name,
						"profile", profile,
						"panic", rec,
						"stack", string(debug.Stack()),
					)
					res.violations = nil
					res.err = fmt.Errorf("%w in compliance check group %s: %v", document.ErrInternalPanic, name, rec)
				}
				ch <- res
			}()
			res.violations = g(ctx, vctx)
		}()
	}

	collected := make([][]Violation, len(groups))
	errs := make([]error, len(groups))
	for pending := len(groups); pending > 0; pending-- {
		select {
		case r := <-ch:
			collected[r.index] = r.violations
			errs[r.index] = r.err
		case <-ctx.Done():
			// The channel is buffered, so the remaining groups never block on
			// their send. Waiting for them anyway keeps them from outliving
			// the call; each one checks ctx and returns promptly.
			for ; pending > 0; pending-- {
				<-ch
			}
			return nil, fmt.Errorf("compliance: check groups canceled: %w", ctx.Err())
		}
	}

	// A group that saw the cancellation returns early with what it had, and
	// its result can win the select above over ctx.Done(), so check again.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compliance: check groups canceled: %w", err)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	var violations []Violation
	for _, vs := range collected {
		violations = append(violations, vs...)
	}
	return violations, nil
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

// resolveCatalog retrieves the document catalog dictionary.
func resolveCatalog(ctx context.Context, doc *document.Document) (ast.PdfDict, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	return catalog.Dict, nil
}

// resolveAllObjects forces resolution of every indirect object in the xref
// table. This ensures parser-level format checks run on ALL objects.
func resolveAllObjects(ctx context.Context, doc *document.Document) {
	doc.Logger.Debug("compliance: resolving all objects for format checks")
	count := 0
	// Iterate a snapshot: this warming pass resolves each object, which
	// can trigger the lazy xref augmentation write on a damaged file. A
	// concurrent reader (e.g. ExtractText on the same Document) could win
	// the augmentation race, so ranging Index.Entries directly here would
	// race that write. A lookup the check groups miss later is deferred,
	// see runCheckGroups.
	for objNum, entry := range doc.XRefEntriesSnapshot() {
		if count%100 == 0 {
			if ctx.Err() != nil {
				return
			}
		}
		if entry.Free {
			continue
		}
		// Prefetch: the object is warmed into the resolver cache here.
		// Individual parse failures are tolerated. The per-group
		// checks below will surface them as structural violations if
		// they matter. Aggregate failure would stop compliance mode
		// from seeing a broken doc at all, which is worse.
		_, _ = document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum}) // Warming the cache; resolve errors are tolerated.
		count++
	}
	doc.Logger.Debug("compliance: resolved all objects", "count", count)
}

// collectPages traverses the page tree and returns all leaf page dicts.
func collectPages(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []ast.PdfDict {
	pagesObj, ok := catalog["Pages"]
	if !ok {
		doc.Logger.Debug("compliance: catalog missing /Pages")
		return nil
	}
	var pages []ast.PdfDict
	collectPagesRecursive(ctx, doc, pagesObj, &pages, make(map[int]bool), 0)
	doc.Logger.Debug("compliance: collected pages", "count", len(pages))
	return pages
}

// maxPageTreeDepth limits recursion when walking the page tree. Anti-DoS guard,
// not spec-mandated: page trees are typically shallow (a few levels deep), so
// 100 is conservative.
//
// core/document/catalog.go bounds the same /Kids descent at 256 and carries the
// home note for this cap, including why the two are documented rather than
// realigned. remediation/fixer_annotations.go uses 100 as well.
const maxPageTreeDepth = 100

// collectPagesRecursive walks the page tree, appending leaf page dicts. seen
// records each node visited so a shared or cyclic /Kids tree is traversed once:
// a page tree is a strict tree (ISO 32000-1 §7.7.3.2), so a node reached again
// is malformed, and without the guard a crafted tree whose every node lists the
// same child twice fans out 2^depth (a denial of service).
func collectPagesRecursive(ctx context.Context, doc *document.Document, obj ast.PdfObject, pages *[]ast.PdfDict, seen map[int]bool, depth int) {
	if ctx.Err() != nil || depth > maxPageTreeDepth {
		return
	}
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if seen[ref.ObjectNum] {
			return
		}
		seen[ref.ObjectNum] = true
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
			collectPagesRecursive(ctx, doc, kid, pages, seen, depth+1)
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
				collectPagesRecursive(ctx, doc, kid, pages, seen, depth+1)
			}
		} else {
			*pages = append(*pages, dict)
		}
	}
}

// resolveResources returns the /Resources dict from a page.
func resolveResources(ctx context.Context, doc *document.Document, page ast.PdfDict) ast.PdfDict {
	res, err := document.ResolveDict(ctx, doc, page["Resources"])
	if err != nil {
		doc.Logger.Debug("compliance: cannot resolve page resources", "error", err)
		return nil
	}
	return res
}

// resolveInheritedResources returns a page's effective /Resources, honouring the
// ISO 32000-1 §7.7.3.4 inheritance rule: a page that declares no /Resources of
// its own uses the nearest ancestor Pages node that does. The upward /Parent
// walk is bounded by [maxPageTreeDepth] so a malformed or cyclic /Parent chain
// cannot loop.
func resolveInheritedResources(ctx context.Context, doc *document.Document, page ast.PdfDict) ast.PdfDict {
	node := page
	for depth := 0; depth <= maxPageTreeDepth && node != nil; depth++ {
		if res := resolveResources(ctx, doc, node); res != nil {
			return res
		}
		parent, err := document.ResolveDict(ctx, doc, node["Parent"])
		if err != nil || parent == nil {
			return nil
		}
		node = parent
	}
	return nil
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

// safeReadHeader reads the first n bytes from the document file.
// Returns an error if the underlying reader is nil.
func safeReadHeader(ctx context.Context, doc *document.Document, n int64) ([]byte, error) {
	if doc.RawReader() == nil {
		return nil, fmt.Errorf("cannot read header: nil reader")
	}
	return doc.ReadFileBytes(ctx, 0, n)
}
