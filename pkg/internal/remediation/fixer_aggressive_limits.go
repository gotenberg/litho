package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

const (
	// pdfAGraphicsDepth is the q/Q nesting cap from ISO 19005-1, 6.1.12
	// test 8 and ISO 19005-2/3, 6.1.13 test 8 (28 levels).
	pdfAGraphicsDepth = 28
	// pdfADictMax is the 4095-entry cap on a single PDF dictionary. ISO
	// 19005-1, 6.1.12 test 6 imposes it from the PDF Reference 1.4
	// architectural limits (Table C.1), which is the reference veraPDF's
	// PDF/A-1 profile cites. ISO 32000-1, Table C.1 lists no dictionary
	// limit, and no ISO 19005-2/3/4 rule in veraPDF's profiles states one,
	// so the cap binds PDF/A-1 alone. The detector is
	// compliance.maxDictCap1.
	pdfADictMax = 4095
	// pdfA1DeviceNComponents is the 8-component cap on DeviceN colour
	// spaces (ISO 19005-1, 6.1.12 test 9, from PDF Reference 1.4, Table
	// C.1).
	pdfA1DeviceNComponents = 8
	// pdfA23DeviceNComponents is the 32-colourant cap of ISO 19005-2/3,
	// 6.1.13 test 9, the ISO 32000-1, Table C.1 value. ISO 19005-4 states no
	// DeviceN limit. Both caps mirror compliance.deviceNColorantLimit, which
	// compliance.checkDeviceNLinks judges.
	pdfA23DeviceNComponents = 32
	// tilingPatternType is the /PatternType of a tiling pattern (ISO 32000-1,
	// 8.7.3.1, Table 75), and uncolouredPaintType the /PaintType of one whose
	// cell specifies no colour of its own (8.7.3.3). Both mirror the
	// constants compliance's pattern walk reads.
	tilingPatternType   = 1
	uncolouredPaintType = 2
)

// matchAggressiveLimits matches implementation-limit violations that
// can only be fixed by dropping content: q/Q stack overflow, oversized
// dictionary and over-component DeviceN.
//
// An out-of-range CID value (ISO 19005-1, 6.1.12 test 10; ISO 19005-2/3,
// 6.1.13 test 10) is not matched: no repair here answers it (see
// [limitRepairsFor]), and file-structure and real-limits claim it already.
func matchAggressiveLimits(v compliance.Violation) bool {
	if !clausePrefix(v, "6.1.12", "6.1.13") {
		return false
	}
	return contextContains(v, "q/Q nesting depth") ||
		contextContains(v, "dict capacity") ||
		contextContains(v, "DeviceN has")
}

// fixAggressiveLimits enforces the PDF/A implementation limits that can only
// be met by discarding material. Gated on [Options.PreserveContent]=false
// because each repair below is a lossy content rewrite: paths flatten,
// dictionaries lose entries, spot colours drop.
//
// Each repair runs only when the profile's part states the limit and a
// violation of that very rule is among those matched, so one violation never
// triggers an unrelated repair. See [limitRepairsFor].
func fixAggressiveLimits(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, violations []compliance.Violation, profile string) ([]string, error) {
	if opts.PreserveContent {
		return nil, nil
	}

	repairs := limitRepairsFor(violations, profile)
	var fixes []string

	if repairs.qNesting {
		if n, err := flattenGraphicsStack(ctx, alloc, doc); err != nil {
			return fixes, err
		} else if n > 0 {
			fixes = append(fixes, fmt.Sprintf("flattened q/Q nesting on %d page(s)", n))
		}
	}

	if repairs.dictCap {
		if n, err := truncateOversizedDicts(ctx, alloc, doc); err != nil {
			return fixes, err
		} else if n > 0 {
			fixes = append(fixes, fmt.Sprintf("truncated %d dictionary(ies) to %d entries", n, pdfADictMax))
		}
	}

	if repairs.deviceNMax > 0 {
		if n, err := trimDeviceNColorants(ctx, alloc, doc, repairs.deviceNMax); err != nil {
			return fixes, err
		} else if n > 0 {
			fixes = append(fixes, fmt.Sprintf("reduced %d DeviceN colour space(s) to %d components", n, repairs.deviceNMax))
		}
	}

	return fixes, nil
}

// limitRepairs selects the repairs one fixAggressiveLimits run performs.
type limitRepairs struct {
	// qNesting flattens q/Q nesting past [pdfAGraphicsDepth].
	qNesting bool
	// dictCap truncates dictionaries past [pdfADictMax].
	dictCap bool
	// deviceNMax is the colourant count DeviceN spaces are trimmed to, or 0
	// when no trim applies.
	deviceNMax int
}

// limitRepairsFor maps the matched violations to the repairs they call for
// under profile. A repair is selected only by the rule that states its limit
// in the profile's own part:
//
//   - q/Q nesting: ISO 19005-1, 6.1.12 test 8 and ISO 19005-2/3, 6.1.13
//     test 8, 28 levels in each. ISO 19005-4 states no nesting limit.
//   - dictionary entries: ISO 19005-1, 6.1.12 test 6 alone, from PDF
//     Reference 1.4, Table C.1. ISO 32000-1, Table C.1 lists no dictionary
//     limit and ISO 19005-2/3 6.1.13 has no such test, so a part 2 or 3
//     dictionary above 4095 entries conforms and keeps every entry.
//   - DeviceN colourants: 8 under ISO 19005-1, 6.1.12 test 9, and 32 under
//     ISO 19005-2/3, 6.1.13 test 9. Trimming a part 2 or 3 space to the
//     part 1 cap would drop up to 24 colourants the profile permits.
//
// Any other rule selects nothing.
func limitRepairsFor(violations []compliance.Violation, profile string) limitRepairs {
	part := compliance.PDFAPart(profile)
	var r limitRepairs
	for _, v := range violations {
		switch {
		case part == 1 && v.Rule == compliance.Rule_19005_1_6_1_12_t08,
			part == 2 && v.Rule == compliance.Rule_19005_2_6_1_13_t08,
			part == 3 && v.Rule == compliance.Rule_19005_3_6_1_13_t08:
			r.qNesting = true
		case part == 1 && v.Rule == compliance.Rule_19005_1_6_1_12_t06:
			r.dictCap = true
		case part == 1 && v.Rule == compliance.Rule_19005_1_6_1_12_t09:
			r.deviceNMax = pdfA1DeviceNComponents
		case part == 2 && v.Rule == compliance.Rule_19005_2_6_1_13_t09,
			part == 3 && v.Rule == compliance.Rule_19005_3_6_1_13_t09:
			r.deviceNMax = pdfA23DeviceNComponents
		}
	}
	return r
}

// flattenGraphicsStack rewrites every page content stream whose q/Q nesting
// exceeds pdfAGraphicsDepth without the q operators that nest deeper and the
// Q operators that close them ([qNestingClip]). Every other operator keeps
// its place, so a state change made inside a dropped pair outlives it.
func flattenGraphicsStack(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (int, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return 0, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return 0, fmt.Errorf("resolving pages: %w", err)
	}

	count := 0
	for pageNum, page := range pages {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		data, contentsObj, err := livePageContentData(ctx, alloc, doc, page)
		if err != nil || contentsObj == nil {
			continue
		}
		// A stream nesting no deeper than the cap drops no q, so it makes no
		// edit and is not written.
		newData, dropped, err := repairContentOps(ctx, data, func() opRepair {
			return (&qNestingClip{limit: pdfAGraphicsDepth}).repair
		})
		if err != nil {
			doc.Logger.Debug("remediation: cannot rewrite a content stream for its q nesting",
				"page", pageNum+1, "error", err)
			continue
		}
		if dropped == 0 {
			continue
		}
		if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum+1); err != nil {
			return count, fmt.Errorf("replacing content stream on page %d: %w", pageNum+1, err)
		}
		count++
	}
	return count, nil
}

// qNestingClip is the repair of [flattenGraphicsStack] for the operations of
// one content stream, handed to it in stream order ([opRepair]): it drops
// every q that would nest deeper than limit, and the Q that closes it. Every
// operator it drops counts as an edit.
type qNestingClip struct {
	limit int
	// depth counts the q operators kept and not yet closed, and skipped the
	// ones dropped and not yet closed.
	depth, skipped int
}

// repair returns op, or none when it is a q or a Q the clip drops.
func (c *qNestingClip) repair(op content.Operation) ([]content.Operation, int) {
	switch op.Operator {
	case "q":
		if c.depth >= c.limit {
			c.skipped++
			return nil, 1
		}
		c.depth++
	case "Q":
		if c.skipped > 0 {
			c.skipped--
			return nil, 1
		}
		if c.depth > 0 {
			c.depth--
		}
	}
	return []content.Operation{op}, 0
}

// truncateOversizedDicts walks every indirect dict, array, or stream
// and deletes keys past the 4095-entry cap. Keys are ordered
// alphabetically so the result is deterministic. PDF/A-spec essentials
// (/Type, /Subtype, /Length, /Size, /Root) are preserved as first-class
// entries.
func truncateOversizedDicts(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (int, error) {
	truncDict := func(d ast.PdfDict) (ast.PdfDict, bool) {
		if len(d) <= pdfADictMax {
			return nil, false
		}
		keep := truncateDict(d)
		return keep, true
	}
	truncArr := func(a ast.PdfArray) (ast.PdfArray, bool) {
		return nil, false
	}
	return walkIndirectObjects(ctx, alloc, doc, truncDict, truncArr)
}

// truncateDict selects up to pdfADictMax entries from d. Priority keys
// (Type, Subtype, Length, Filter, Size, Root, Info, ID, Pages, Metadata)
// come first; the rest fill in alphabetical order.
func truncateDict(d ast.PdfDict) ast.PdfDict {
	priority := map[ast.PdfName]bool{
		"Type": true, "Subtype": true, "Length": true, "Filter": true,
		"Size": true, "Root": true, "Info": true, "ID": true,
		"Pages": true, "Metadata": true, "OutputIntents": true,
		"DecodeParms": true, "FormType": true, "BBox": true,
	}
	out := make(ast.PdfDict, pdfADictMax)
	for k := range priority {
		if v, ok := d[k]; ok {
			out[k] = v
		}
	}
	remaining := make([]ast.PdfName, 0, len(d))
	for k := range d {
		if priority[k] {
			continue
		}
		remaining = append(remaining, k)
	}
	sort.Slice(remaining, func(i, j int) bool {
		return string(remaining[i]) < string(remaining[j])
	})
	for _, k := range remaining {
		if len(out) >= pdfADictMax {
			break
		}
		out[k] = d[k]
	}
	return out
}

// trimDeviceNColorants caps the colourant names of every DeviceN colour space
// the validator's component-count check reaches at limit, keeping the first
// limit. The tint-transform function won't be accurate for the reduced
// component set; aggressive mode accepts that loss.
//
// compliance.checkDeviceNLinks, the only emitter of ISO 19005-1 6.1.12 test 9
// and ISO 19005-2/3 6.1.13 test 9, judges every DeviceN a colour-space link
// reaches. The walk reaches the ones it can trim from each page's effective
// /Resources, inherited from an ancestor when the page states none (ISO
// 32000-1, 7.7.3.4), and the appearance streams of its annotations. Below a
// /Resources it reaches the /ColorSpace entries, the /ColorSpace of each
// shading, the shading of each shading pattern and the /Resources of each
// coloured tiling pattern and form XObject, recursively; see [deviceNTrim].
// It trims every DeviceN space it reaches, where the validator judges a space
// only when a link reaches it: trimming a space nothing paints with changes
// nothing rendered.
//
// Every level is read through the allocator and written back where it lives:
// an indirect object at its own object number, an inline one into its parent,
// up to the page or ancestor holding the /Resources. aggressive-limits is the
// last registry entry, so every colour-space writer precedes it: devicen
// rewrites DeviceN arrays at their object numbers, and output-intent rewrites
// /Resources, form XObjects and patterns with the /ColorSpace dictionary
// inlined, leaving the indirect one unused. A read from the document there
// trims a dictionary the output no longer references. A space counts as
// trimmed only when its write lands.
//
// # Corpus evidence
//
//   - litho limits_2b.pdf and verapdf 6-1-12-t09-fail-a.pdf: the DeviceN
//     array, its /ColorSpace and the /Resources are all inline in the page,
//     the shape the trim used to give up on, so both kept their 33 and 12
//     colourants and their test 9 violation in aggressive mode. Trimmed,
//     neither output draws test 9 from litho or veraPDF, and
//     6-1-12-t09-fail-a.pdf passes veraPDF at PDF/A-1b.
//   - All corpora: these two are the only files declaring a DeviceN space
//     past 8 colourants anywhere, both at page level, so the walk below the
//     page has no corpus reach.
func trimDeviceNColorants(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, limit int) (int, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return 0, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return 0, nil
	}
	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return 0, nil
	}
	var pageEntries []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pageEntries, make(map[int]bool), 0)

	w := &deviceNTrim{alloc: alloc, doc: doc, limit: limit, visited: make(map[int]bool)}
	for _, pe := range pageEntries {
		if err := ctx.Err(); err != nil {
			return w.count, err
		}
		w.pageResources(ctx, pe)
		w.annotationAppearances(ctx, pe)
	}
	return w.count, ctx.Err()
}

// deviceNTrim carries the state of one [trimDeviceNColorants] walk. visited
// holds the object number of every resource dictionary, form, pattern and
// shading already walked, so a shared object is trimmed once and a cyclic
// /Resources chain cannot loop. The recursion depth through forms and tiling
// patterns is bounded by the document's form-nesting budget, the bound
// compliance's formTooDeep applies to the walk this one mirrors.
type deviceNTrim struct {
	alloc   *writer.ObjAllocator
	doc     *document.Document
	limit   int
	visited map[int]bool
	count   int
}

// trimmed is what one level of a [deviceNTrim] walk hands its parent: the
// value the parent holds, whether that value changed (an inline level the
// parent must write into itself), and how many trimmed spaces the change
// carries that no write has landed yet.
type trimmed struct {
	value   ast.PdfObject
	changed bool
	pending int
}

// unchanged is the [trimmed] of a level that needs no write.
func unchanged(obj ast.PdfObject) trimmed {
	return trimmed{value: obj}
}

// land writes value at obj's object number when obj is an indirect reference,
// counting the pending spaces it carries, and otherwise hands value back for
// the parent to hold inline.
func (w *deviceNTrim) land(obj, value ast.PdfObject, pending int) trimmed {
	if ref, ok := obj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		w.alloc.AllocAt(ref.ObjectNum, value)
		w.count += pending
		return unchanged(obj)
	}
	return trimmed{value: value, changed: true, pending: pending}
}

// firstVisit reports whether obj is an inline value or an indirect object the
// walk has not reached yet, marking it.
func (w *deviceNTrim) firstVisit(obj ast.PdfObject) bool {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok {
		return true
	}
	if w.visited[ref.ObjectNum] {
		return false
	}
	w.visited[ref.ObjectNum] = true
	return true
}

// pageResources trims the page's effective /Resources and writes an inline
// one back into the page or ancestor that holds it. The inheritance walk
// follows /Parent to the nearest node, the page first, whose /Resources is a
// dictionary. Compliance (pageObject.inheritedHolder) stops at the nearest node
// that states /Resources at all and follows the node the page tree walk came
// through, so the two differ only on a /Resources that is not a dictionary or
// a /Parent that names another node.
func (w *deviceNTrim) pageResources(ctx context.Context, pe annotPageEntry) {
	owner, ownerRef := pe.dict, pe.ref
	seen := make(map[int]bool)
	for depth := 0; liveDictValue(ctx, w.alloc, w.doc, owner["Resources"]) == nil; depth++ {
		parentRef, ok := owner["Parent"].(ast.PdfIndirectRef)
		if !ok || parentRef.ObjectNum <= 0 || seen[parentRef.ObjectNum] || depth >= maxPageTreeDepth {
			return
		}
		seen[parentRef.ObjectNum] = true
		owner = resolveFromAllocOrDoc(ctx, w.alloc, w.doc, parentRef)
		if owner == nil {
			return
		}
		ownerRef = parentRef
	}

	t := w.resources(ctx, owner["Resources"], 0)
	if !t.changed || ownerRef.ObjectNum <= 0 {
		return
	}
	newOwner := maps.Clone(owner)
	newOwner["Resources"] = t.value
	w.land(ownerRef, newOwner, t.pending)
}

// annotationAppearances trims the /Resources of every appearance stream the
// page's annotations carry under /N, /R and /D, as compliance's colour-space
// link walk reaches them. An appearance subdictionary is walked
// state by state: its streams are indirect, so the annotation itself is never
// rewritten.
func (w *deviceNTrim) annotationAppearances(ctx context.Context, pe annotPageEntry) {
	for _, annotObj := range liveArray(ctx, w.alloc, w.doc, pe.dict["Annots"]) {
		if ctx.Err() != nil {
			return
		}
		annot := liveDictValue(ctx, w.alloc, w.doc, annotObj)
		ap := liveDictValue(ctx, w.alloc, w.doc, annot["AP"])
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			entry, ok := ap[key]
			if !ok {
				continue
			}
			if _, isStream := w.dictOrStream(ctx, entry); isStream {
				w.formStream(ctx, entry, 0, false)
				continue
			}
			states := liveDictValue(ctx, w.alloc, w.doc, entry)
			for _, state := range slices.Sorted(maps.Keys(states)) {
				w.formStream(ctx, states[state], 0, false)
			}
		}
	}
}

// resources trims one /Resources dictionary: its /ColorSpace entries, and the
// shadings, patterns and form XObjects it names.
func (w *deviceNTrim) resources(ctx context.Context, obj ast.PdfObject, depth int) trimmed {
	if ctx.Err() != nil || depth > w.doc.Limits().MaxFormDepth || !w.firstVisit(obj) {
		return unchanged(obj)
	}
	res := liveDictValue(ctx, w.alloc, w.doc, obj)
	if res == nil {
		return unchanged(obj)
	}

	walks := []struct {
		key  ast.PdfName
		trim func(ast.PdfObject) trimmed
	}{
		{"ColorSpace", func(e ast.PdfObject) trimmed { return w.colorSpace(ctx, e) }},
		{"Shading", func(e ast.PdfObject) trimmed { return w.shading(ctx, e) }},
		{"Pattern", func(e ast.PdfObject) trimmed { return w.pattern(ctx, e, depth) }},
		{"XObject", func(e ast.PdfObject) trimmed { return w.formStream(ctx, e, depth+1, true) }},
	}
	var updates ast.PdfDict
	pending := 0
	for _, walk := range walks {
		sub, ok := res[walk.key]
		if !ok {
			continue
		}
		t := w.entries(ctx, sub, walk.trim)
		if !t.changed {
			continue
		}
		if updates == nil {
			updates = ast.PdfDict{}
		}
		updates[walk.key] = t.value
		pending += t.pending
	}
	if updates == nil {
		return unchanged(obj)
	}
	newRes := maps.Clone(res)
	maps.Copy(newRes, updates)
	return w.land(obj, newRes, pending)
}

// entries applies trim to every value of one resource category dictionary, in
// sorted name order. Inline results accumulate before the dictionary is
// rebuilt once: rebuilding it per entry would keep only the last, the trap
// fixExtGStateDict records.
func (w *deviceNTrim) entries(ctx context.Context, obj ast.PdfObject, trim func(ast.PdfObject) trimmed) trimmed {
	d := liveDictValue(ctx, w.alloc, w.doc, obj)
	var updates ast.PdfDict
	pending := 0
	for _, name := range slices.Sorted(maps.Keys(d)) {
		if ctx.Err() != nil {
			break
		}
		t := trim(d[name])
		if !t.changed {
			continue
		}
		if updates == nil {
			updates = ast.PdfDict{}
		}
		updates[name] = t.value
		pending += t.pending
	}
	if updates == nil {
		return unchanged(obj)
	}
	nd := maps.Clone(d)
	maps.Copy(nd, updates)
	return w.land(obj, nd, pending)
}

// colorSpace trims one colour space value when it is a DeviceN space past the
// limit. An /Indexed space over a DeviceN base is left alone, though
// compliance judges the base against the limit: its lookup table holds one
// byte per base component (ISO 32000-1, 8.6.6.3), which a trim would misread,
// so the violation stays.
func (w *deviceNTrim) colorSpace(ctx context.Context, entry ast.PdfObject) trimmed {
	newArr, ok := trimDeviceNArray(ctx, w.alloc, w.doc, entry, w.limit)
	if !ok {
		return unchanged(entry)
	}
	return w.land(entry, newArr, 1)
}

// shading trims the /ColorSpace of one shading, a dictionary or, for types 4
// to 7, a stream (ISO 32000-1, 8.7.4.5).
func (w *deviceNTrim) shading(ctx context.Context, entry ast.PdfObject) trimmed {
	if !w.firstVisit(entry) {
		return unchanged(entry)
	}
	obj, isStream := w.dictOrStream(ctx, entry)
	dict := dictOf(obj)
	cs, ok := dict["ColorSpace"]
	if !ok {
		return unchanged(entry)
	}
	t := w.colorSpace(ctx, cs)
	if !t.changed {
		return unchanged(entry)
	}
	return w.rewrite(ctx, entry, obj, isStream, "ColorSpace", t)
}

// pattern trims one pattern. A shading pattern names its colour space on its
// /Shading; a tiling pattern (ISO 32000-1, 8.7.3.3) paints its cell from its
// own /Resources, walked one form level deeper. An uncoloured tiling pattern
// (PaintType 2) is skipped: its cell specifies no colour of its own and
// processes none of its colour operators (ISO 32000-1, 8.7.3.3), so compliance
// judges no space they select there, only the images the cell paints, which
// [deviceNTrim.formStream] leaves alone.
func (w *deviceNTrim) pattern(ctx context.Context, entry ast.PdfObject, depth int) trimmed {
	if !w.firstVisit(entry) {
		return unchanged(entry)
	}
	obj, isStream := w.dictOrStream(ctx, entry)
	dict := dictOf(obj)
	if dict == nil {
		return unchanged(entry)
	}
	if !isStream {
		sh, ok := dict["Shading"]
		if !ok {
			return unchanged(entry)
		}
		t := w.shading(ctx, sh)
		if !t.changed {
			return unchanged(entry)
		}
		return w.rewrite(ctx, entry, obj, false, "Shading", t)
	}
	patternType, _ := document.ResolveInt(ctx, w.doc, dict["PatternType"])
	paintType, _ := document.ResolveInt(ctx, w.doc, dict["PaintType"])
	if patternType != tilingPatternType || paintType == uncolouredPaintType {
		return unchanged(entry)
	}
	t := w.resources(ctx, dict["Resources"], depth+1)
	if !t.changed {
		return unchanged(entry)
	}
	return w.rewrite(ctx, entry, obj, true, "Resources", t)
}

// formStream trims the /Resources of one form: a form XObject when
// requireForm is set, where an image XObject is left alone, or an annotation
// appearance stream. An image's DeviceN space is left alone, though
// compliance judges it against the limit: each sample carries one component
// per colourant (ISO 32000-1, 8.9.5.2), so a trim would scramble the image,
// and the violation stays.
func (w *deviceNTrim) formStream(ctx context.Context, entry ast.PdfObject, depth int, requireForm bool) trimmed {
	if !w.firstVisit(entry) {
		return unchanged(entry)
	}
	obj, isStream := w.dictOrStream(ctx, entry)
	if !isStream {
		return unchanged(entry)
	}
	dict := dictOf(obj)
	if requireForm {
		if subtype, _ := document.ResolveName(ctx, w.doc, dict["Subtype"]); subtype != "Form" {
			return unchanged(entry)
		}
	}
	t := w.resources(ctx, dict["Resources"], depth)
	if !t.changed {
		return unchanged(entry)
	}
	return w.rewrite(ctx, entry, obj, true, "Resources", t)
}

// rewrite replaces key in the dictionary or stream obj that entry holds with
// the changed child t and lands the copy. A stream keeps its raw body; one
// whose body cannot be read is left as it is rather than written empty.
func (w *deviceNTrim) rewrite(ctx context.Context, entry, obj ast.PdfObject, isStream bool, key ast.PdfName, t trimmed) trimmed {
	nd := maps.Clone(dictOf(obj))
	nd[key] = t.value
	if !isStream {
		return w.land(entry, nd, t.pending)
	}
	ref, isRef := entry.(ast.PdfIndirectRef)
	if !isRef || ref.ObjectNum <= 0 {
		return unchanged(entry)
	}
	stream, _ := obj.(ast.PdfStream)
	raw, err := stream.Bytes(ctx)
	if err != nil {
		w.doc.Logger.Debug("remediation: skipping DeviceN trim, stream body read failed",
			slog.Int("obj", ref.ObjectNum), slog.Any("err", err))
		return unchanged(entry)
	}
	return w.land(entry, ast.NewStream(nd, raw), t.pending)
}

// dictOrStream returns the dictionary or stream entry names, read through the
// allocator, and whether it is a stream. It returns nil for anything else.
func (w *deviceNTrim) dictOrStream(ctx context.Context, entry ast.PdfObject) (ast.PdfObject, bool) {
	obj := entry
	if ref, ok := entry.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		pending, isPending := w.alloc.Pending()[ref.ObjectNum]
		if !isPending {
			resolved, err := w.doc.Resolve(ctx, ref)
			if err != nil {
				return nil, false
			}
			pending = resolved
		}
		obj = pending
	}
	switch v := obj.(type) {
	case ast.PdfStream:
		return v, v.Dict != nil
	case ast.PdfDict:
		return v, false
	}
	return nil, false
}

// dictOf returns the dictionary of a dictionary or stream, or nil.
func dictOf(obj ast.PdfObject) ast.PdfDict {
	switch v := obj.(type) {
	case ast.PdfStream:
		return v.Dict
	case ast.PdfDict:
		return v
	}
	return nil
}

// trimDeviceNArray returns the DeviceN colour space entry, read through the
// allocator, with its colourant names cut to the first limit, and false when
// entry is no DeviceN space or already within limit. The array is
// read alloc-first because devicen, which precedes this fixer, rewrites
// DeviceN arrays at their own object numbers, and the copy replaces the array.
func trimDeviceNArray(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, entry ast.PdfObject, limit int) (ast.PdfArray, bool) {
	arr := liveArray(ctx, alloc, doc, entry)
	if len(arr) < 2 {
		return nil, false
	}
	name, ok := arr[0].(ast.PdfName)
	if !ok || (name != "DeviceN" && name != "NChannel") {
		return nil, false
	}
	names := liveArray(ctx, alloc, doc, arr[1])
	if len(names) <= limit {
		return nil, false
	}
	newNames := make(ast.PdfArray, limit)
	copy(newNames, names[:limit])
	newArr := make(ast.PdfArray, len(arr))
	copy(newArr, arr)
	newArr[1] = newNames
	return newArr, true
}
