package compliance

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
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
	pageObject
	resources   ast.PdfDict // the page's own or inherited ones; see [pageObject.effectiveResources]
	content     []byte      // decoded content stream bytes (nil when none decodes)
	ops         []content.Operation
	opsOnce     sync.Once
	parseFailed bool // true when the content could not be read or ParseStreamData errored (not for empty content)

	// contentUnreadable is true when the /Contents could not be resolved or
	// its content is past the page budget. A survey that must stay
	// conservative when it cannot see what a page paints (see
	// pageDrawnResources) keys on this rather than on empty operations, which
	// a blank page, and one whose content does not decode, also produce.
	contentUnreadable bool

	// contentJudged is true when an earlier page's /Contents is the same
	// indirect object, which the content-stream rules judge on that page
	// alone; see [markJudgedContents].
	contentJudged bool
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
// document catalog. Pages are collected once, each with the resources it
// states or inherits from its nearest ancestor in the page tree (see
// [pageObject.effectiveResources]).
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

	vctx.pages = collectPageContexts(ctx, doc, catalog)

	// Freeze the format violations here, on the single goroutine that
	// builds the context: every object has been resolved by now, and the
	// check groups that run next resolve objects of their own.
	vctx.formatViolations = sweptFormatViolations(doc)

	return vctx
}

// collectPageContexts collects the pages of the document catalog once, each
// with its resources, its own or those its nearest ancestor in the page tree
// lends it, and its decoded content, and marks the pages whose content an
// earlier page shares (see [markJudgedContents]).
func collectPageContexts(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []pageCtx {
	rawPages := collectPageObjects(ctx, doc, catalog)
	pages := make([]pageCtx, len(rawPages))
	for i, pageObj := range rawPages {
		contentBytes, contentErr := resolvePageContentBytes(ctx, doc, pageObj.dict)
		pages[i] = pageCtx{
			pageObject: pageObj,
			resources:  pageObj.effectiveResources(ctx, doc),
			content:    contentBytes,
			// A /Contents that does not resolve, or content past the page
			// budget, is content litho cannot read, which the per-font and
			// per-colour skips call "present but unparseable" and answer
			// conservatively. A content stream that does not decode is not:
			// veraPDF reads no operator from it, so it reads as no content
			// (see [resolvePageContentBytes]).
			parseFailed:       contentErr != nil,
			contentUnreadable: contentErr != nil,
		}
	}
	markJudgedContents(pages)
	return pages
}

// markJudgedContents sets [pageCtx.contentJudged] on every page whose
// /Contents is an indirect object an earlier page's /Contents already is.
//
// ISO 32000-1, 7.7.3.3 lets pages share a content stream, and veraPDF judges it
// once for the whole document, on the first page that draws it: the
// content-stream rules state nothing for the later pages, and the streams the
// content draws are reached from the first page alone, through its resources.
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on two-page fixtures, for
// the hex string tests, the undefined-operator rule, the real limit, the q/Q
// nesting limit, the inline image filter, interpolation and intent tests, the
// ri test and the inherited-resource test:
//
//   - Two pages whose /Contents is the same stream, or the same indirect array
//     of streams, fail each test as often as one of them alone does.
//   - Two pages whose /Contents are direct arrays fail each test twice as
//     often, the arrays naming the same streams or not, and so do a page whose
//     /Contents is a stream and one whose array holds it.
//   - When the shared content paints /Fm0 and each page maps /Fm0 to its own
//     form, only the first page's form is judged.
//
// # Corpus evidence
//
//   - pdfjs hello_world_rotated.pdf, issue14847.pdf, issue18042.pdf and
//     pypdf2332.pdf: are the only files whose pages share a /Contents, and none
//     of the content-stream rules fires in the shared streams, so no count
//     moves at PDF/A-1b or PDF/A-2b.
func markJudgedContents(pages []pageCtx) {
	seen := make(map[int]bool)
	for i := range pages {
		ref, ok := pages[i].dict["Contents"].(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		pages[i].contentJudged = seen[ref.ObjectNum]
		seen[ref.ObjectNum] = true
	}
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
// The groups resolve under [document.OpenedObjectsOnly], which
// [runValidationViolations] sets for the whole run, so a lookup that misses
// the cross-reference index reads the object as undefined instead of running
// the one-shot raw-byte augmentation that rewrites the index. Run from a
// group, the augmentation wrote the index while the other groups were reading
// it, and the lookups made before that moment missed the objects it added
// while the ones after found them, so the same bytes reported a different
// violation count from run to run (CONTRIBUTING "Deterministic output"). A
// concurrent caller that does not validate, text extraction on the same
// Document for one, can still run it during the fan-out, but the objects it
// adds are out of the groups' reach, and it moves no entry a group reads: a
// listed entry whose offset misses its object's header runs the augmentation
// the first time the object is resolved, and [resolveAllObjects] has resolved
// every listed object before the groups start. A rebuilt index, whose objects
// the view leaves in reach, the augmentation's included, has run the
// augmentation before the run began.
func runCheckGroups(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, groups []checkGroup) ([]Violation, error) {
	vctx := buildValidationCtx(ctx, doc, catalog, profile)
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
// table, and format-checks each one, uncompressed or held in an object
// stream, so the parser-level format checks cover all of them.
//
// Resolving alone checks only the objects that miss the cache. Those opening
// the file already resolved, the catalog and the root of the page tree among
// them, are cache hits, and a bad hex string in them (ISO 19005-1/2/3, 6.1.6
// t1 and t2) was never reported. [document.Document.CheckObjectFormat] parses
// each object no parse has recorded yet, which is only those.
//
// # Corpus evidence
//
//   - All corpora: opening leaves cached 9,202 of the 513,524 uncompressed
//     objects outside the verapdf corpus, 4,090 of them in the 46 files whose
//     index is rebuilt from a raw scan, and 5,240 of the 57,723 in the verapdf
//     corpus, about two per file. Those are the only objects the check parses
//     a second time, and validating every file outside the verapdf corpus at
//     PDF/A-1b takes the same CPU time as before, 178 to 183 s either way.
//   - pdfjs issue9105_other.pdf, poppler-742-0-fuzzed.pdf and
//     poppler-85140-0.pdf: are the only files whose counts move at PDF/A-1b
//     and PDF/A-2b, and the verapdf files under their own profile, the litho
//     files under theirs and the facturx files under PDF/A-3b move none. All
//     three are rebuilt, which parses their objects while opening, and
//     veraPDF parses none of the three. poppler-742-0-fuzzed.pdf gains 60
//     odd-digit and 129 non-hex strings.
func resolveAllObjects(ctx context.Context, doc *document.Document) {
	doc.Logger.Debug("compliance: resolving all objects for format checks")
	count := 0
	// The iterator yields from a copy: this warming pass resolves each
	// object, and one whose offset misses its header runs the lazy xref
	// augmentation write on a damaged file, as a concurrent reader (e.g.
	// ExtractText on the same Document) can. Resolving every listed object
	// here settles those offsets before the check groups fan out, see
	// runCheckGroups.
	for objNum, entry := range sweptXRefEntries(doc) {
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
		doc.CheckObjectFormat(ctx, objNum)
		count++
	}
	doc.Logger.Debug("compliance: resolved all objects", "count", count)
}

// sweptXRefEntries returns the cross-reference entries a check that sweeps
// every object judges, in ascending object-number order: the ones the
// document was opened with, which its cross-reference sections list or its
// rebuild found (see [document.Document.OpenedXRefEntries]).
//
// An object the file's table leaves out is not one of them, even once the
// raw-byte augmentation that repairs a damaged index has added it. ISO
// 32000-1, 7.3.10 reads a reference to an undefined object as the null
// object, and veraPDF, which reads the file's own table, judges no such
// object. Measured with veraPDF 1.30.2 at PDF/A-2b: a stream with an
// LZWDecode filter and an /F key fails 6.1.7.1 t3 and 6.1.7.2 t1 when the
// table lists it, and fails nothing when the table leaves it out, whether the
// catalog references it or not and whether its number is below the trailer's
// /Size or not.
//
// The augmentation runs at the first lookup that misses the index, for
// whichever caller makes it, so sweeping the live index judged the objects it
// adds only when something run before the validation had missed: text
// extraction that follows a font's /Widths to an object the table leaves out
// runs it, and a validation run that never follows that reference does not.
// The same bytes then drew a different verdict depending on what the caller
// did first (CONTRIBUTING "Deterministic output").
//
// # Corpus evidence
//
//   - All corpora: each of the 5171 files draws the same PDF/A-2b violations
//     on a fresh reader and on one that extracted the text of every page
//     first, sweeping the live index or this one, so no file shows the
//     dependence.
//   - pdfjs GHOSTSCRIPT-698804-1-fuzzed.pdf: is the only file whose PDF/A-2b
//     verdict moves. Its table lists objects 0 and 1 alone, and it loses a
//     6.1.7.1 t1 violation on object 5, which the augmentation adds when the
//     validation resolves the page's /Contents. veraPDF does not open the
//     file ("Pages not found").
func sweptXRefEntries(doc *document.Document) iter.Seq2[int, xref.XRefEntry] {
	return doc.OpenedXRefEntries()
}

// sweptFormatViolations returns the parser-level format violations recorded
// on the objects [sweptXRefEntries] yields, ordered by object number as
// [document.Document.FormatViolations] orders them. An object the
// augmentation added is parsed in compliance mode when a check reaches it
// through a reference, and a caller that resolved it before the validation
// cached it with nothing recorded, so its findings came and went with what
// ran first.
func sweptFormatViolations(doc *document.Document) []error {
	var out []error
	for objNum := range sweptXRefEntries(doc) {
		out = append(out, doc.ObjectFormatViolations(objNum)...)
	}
	return out
}

// collectPages traverses the page tree and returns all leaf page dicts.
func collectPages(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []ast.PdfDict {
	objs := collectPageObjects(ctx, doc, catalog)
	pages := make([]ast.PdfDict, len(objs))
	for i, p := range objs {
		pages[i] = p.dict
	}
	return pages
}

// pageObject is one leaf of the page tree: its dictionary, its object number,
// 0 for a page written directly in its parent's /Kids, and the node whose
// /Kids listed it.
type pageObject struct {
	dict   ast.PdfDict
	objNum int
	// parent is the node the walk reached the page through, nil for a page no
	// page tree node lists.
	parent *pageTreeNode
}

// pageTreeNode is a /Pages node on the path the walk took from the catalog to
// a page. It is veraPDF's tree parent (PDPageTreeNode.getParent), which
// PDPageTreeBranch sets from /Kids and writes over the child's /Parent entry,
// so a page whose /Parent names another node still inherits from the node that
// lists it.
type pageTreeNode struct {
	dict   ast.PdfDict
	objNum int
	// key names the node in the identities the checks key on: its object, or
	// its place under the nearest node written as one.
	key    string
	parent *pageTreeNode
}

// inheritedHolder returns the node of the page tree whose entry key the page
// takes, its site, and that node as the walk reached it, nil when it is the
// page itself: the page or its nearest ancestor holding the entry, whatever
// its value. It returns a nil dictionary when no node holds the entry.
//
// ISO 32000-1, 7.7.3.4 and Table 30 make /Resources, /MediaBox, /CropBox and
// /Rotate inheritable, and veraPDF looks them up this way
// (PDPageTreeNode.getInheritableResources, PDPage.getInheritedCOSBBox), testing
// whether the node holds the entry as [knownKey] does: a null entry is absent
// (ISO 32000-1, 7.3.7), and any other value, one that is not a dictionary
// included, ends the search. So does an indirect reference whatever it names,
// although ISO 32000-1, 7.3.10 reads a reference to an undefined object as the
// null object, which would make it absent: the verdicts follow the entry veraPDF
// reads. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b: a page whose
// /Resources is null inherits its ancestor's fonts and colour spaces, and one
// whose /Resources is 99 0 R, with no object 99, or 7 inherits none.
//
// # Corpus evidence
//
//   - All corpora: 35 of 39188 pages hold no /Resources that resolve to a
//     dictionary. 14 inherit them from the root of the page tree, 21 from no
//     node, and none from an intermediate node. No /Parent chain leads to
//     another holder than the walk does, so resolving through the tree moves
//     no count at PDF/A-1b or PDF/A-2b.
func (p *pageObject) inheritedHolder(key ast.PdfName) (ast.PdfDict, propertyListSite, *pageTreeNode) {
	if knownKey(p.dict, key) {
		return p.dict, propertyListSite{objNum: p.objNum}, nil
	}
	for n := p.parent; n != nil; n = n.parent {
		if knownKey(n.dict, key) {
			return n.dict, propertyListSite{objNum: n.objNum}, n
		}
	}
	return nil, propertyListSite{}, nil
}

// effectiveResources returns the resource dictionary the page's content resolves
// its names against: the page's own or, when it states none, the one its
// nearest ancestor in the page tree states, whole, never merged with a farther
// ancestor's (see [pageObject.inheritedHolder]). It is nil when that entry is
// not a dictionary or no node states one.
func (p *pageObject) effectiveResources(ctx context.Context, doc *document.Document) ast.PdfDict {
	node, _, _ := p.inheritedHolder("Resources")
	if node == nil {
		return nil
	}
	return resolveResources(ctx, doc, node)
}

// collectPageObjects traverses the page tree and returns all leaf pages, in
// the order [collectPages] returns their dictionaries.
func collectPageObjects(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []pageObject {
	pagesObj, ok := catalog["Pages"]
	if !ok {
		doc.Logger.Debug("compliance: catalog missing /Pages")
		return nil
	}
	var pages []pageObject
	collectPagesRecursive(ctx, doc, pagesObj, nil, "pages", &pages, make(map[int]bool), 0)
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

// collectPagesRecursive walks the page tree, appending leaf pages. parent is
// the node whose /Kids listed obj, nil for the root, and key the name obj
// takes in [pageTreeNode.key] when it is written directly. seen records each
// node visited so a shared or cyclic /Kids tree is traversed once: a page tree
// is a strict tree (ISO 32000-1 §7.7.3.2), so a node reached again is
// malformed, and without the guard a crafted tree whose every node lists the
// same child twice fans out 2^depth (a denial of service). seen holds each
// /Kids array written as an object of its own too, as [document.ResolvePages]
// does: direct nodes that each name their own array again would fan it out the
// same way.
func collectPagesRecursive(ctx context.Context, doc *document.Document, obj ast.PdfObject, parent *pageTreeNode, key string, pages *[]pageObject, seen map[int]bool, depth int) {
	if ctx.Err() != nil || depth > maxPageTreeDepth {
		return
	}
	objNum := 0
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if seen[ref.ObjectNum] {
			return
		}
		seen[ref.ObjectNum] = true
		objNum = ref.ObjectNum
		key = "obj " + strconv.Itoa(objNum)
	}
	dict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || dict == nil {
		return
	}
	typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
	_, hasKids := dict["Kids"]
	// Some producers omit /Type; /Kids tells a node from a page.
	if typeName == "Page" || (typeName != "Pages" && !hasKids) {
		*pages = append(*pages, pageObject{dict: dict, objNum: objNum, parent: parent})
		return
	}
	kids, err := document.ResolveArray(ctx, doc, dict["Kids"])
	if err != nil || document.VisitedBefore(dict["Kids"], seen) {
		return
	}
	node := &pageTreeNode{dict: dict, objNum: objNum, key: key, parent: parent}
	for i, kid := range kids {
		collectPagesRecursive(ctx, doc, kid, node, key+"/Kids/"+strconv.Itoa(i), pages, seen, depth+1)
	}
}

// resolveResources returns the /Resources dict of a page or page tree node:
// its own entry alone, with no inheritance (see [pageObject.effectiveResources]).
func resolveResources(ctx context.Context, doc *document.Document, page ast.PdfDict) ast.PdfDict {
	res, err := document.ResolveDict(ctx, doc, page["Resources"])
	if err != nil {
		doc.Logger.Debug("compliance: cannot resolve page resources", "error", err)
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

// safeReadHeader reads the first n bytes from the document file.
// Returns an error if the underlying reader is nil.
func safeReadHeader(ctx context.Context, doc *document.Document, n int64) ([]byte, error) {
	if doc.RawReader() == nil {
		return nil, fmt.Errorf("cannot read header: nil reader")
	}
	return doc.ReadFileBytes(ctx, 0, n)
}
