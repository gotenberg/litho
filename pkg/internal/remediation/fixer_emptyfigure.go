package remediation

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// maxDestSweepDepth bounds how deep the sweep for destinations naming a removed
// Figure descends into the values written directly inside one object. Anti-DoS
// guard, not spec-mandated: each step of a /Next chain of actions written
// inline (ISO 32000-1, 12.6.2, Table 193) nests an array and a dictionary, so
// twice [maxActionChainDepth] reaches every action the action splice reaches,
// while a crafted value nested deeper cannot grow the goroutine stack with it.
const maxDestSweepDepth = 2 * maxActionChainDepth

// destinationViews lists the names that follow the target of an explicit
// destination (ISO 32000-1, 12.3.2.2, Table 151). An array whose second entry
// is one of them is a destination, which no /K, /Ref or /ParentTree array is.
//
//nolint:gochecknoglobals // static spec table.
var destinationViews = map[ast.PdfName]bool{
	"XYZ": true, "Fit": true, "FitH": true, "FitV": true,
	"FitR": true, "FitB": true, "FitBH": true, "FitBV": true,
}

// matchEmptyFigure matches the violations [matchFigureAlt] matches: a Figure
// lacking the alternate text ISO 14289-1, 7.3 and ISO 14289-2, 8.2.5.28.2
// require.
func matchEmptyFigure(v compliance.Violation) bool {
	return matchFigureAlt(v)
}

// fixEmptyFigure takes out of the structure tree each Figure that lacks the
// alternate text ISO 14289-1, 7.3 and ISO 14289-2, 8.2.5.28.2 require and
// encloses nothing, in aggressive mode (opts.PreserveContent false) when the
// caller supplies no alternate text. A Figure written directly in a /K, with no
// object number of its own, goes out of the element or the /K array written as
// an object of its own that holds it ([emptyFigures.dropDirect]), and so does a
// reference to a Figure that goes from the /K of an element written inline. A
// Figure
// encloses nothing when it has no kid, or when each of its kids (ISO 32000-1,
// 14.7.2, Table 323) is:
//
//   - a marked-content identifier, bare or in a marked-content reference
//     (Table 324), that no marked-content sequence of its page carries
//     (14.7.4.2), the page's content, its annotations' appearances and every
//     form XObject their resources name read whole;
//   - an object reference whose /Obj names no object (Table 325), a reference
//     to an undefined object being the null object (7.3.10);
//   - the null object;
//   - a Figure this repair removes in turn.
//
// No alternate text describes such a Figure: it stands for nothing a reader
// meets, and the generic placeholder [fixFigureAlt] writes in aggressive mode
// would describe nothing either. A Figure the caller gave alternate text
// (opts.FigureAlt) keeps its place and takes that text, and the default mode
// leaves every Figure as it is, its finding in the residual.
//
// Every reference the document holds to a removed Figure goes with it: the /K
// of the nodes listing it, the /ParentTree slots and entries naming it
// (14.7.4.4), its /IDTree entry (Table 322), the /Ref of the elements naming it
// (ISO 32000-2, 14.7.2, Table 355) and the /SE of the outline items naming it
// (ISO 32000-1, 12.3.3, Table 153). A structure destination under /SD (ISO
// 32000-2, 12.6.4.2, Table 202) names instead the element its page destination
// opens on, picked as [fixStructDest] picks one. A Figure that is the only
// element such a destination could name stays, with the placeholder
// [fixFigureAlt] gives it: without it the action would keep its page
// destination alone, which ISO 14289-2, 8.8 rejects. An element the removal
// leaves empty goes only when it is such a Figure too; any other stays.
//
// A Figure stays whenever the evidence falls short: a kid naming no page of the
// document, content that cannot be read, a marked-content reference into a
// form XObject (/Stm), a /ParentTree entry pairing it with content a page or an
// annotation holds, an inline element naming it in /Ref, or a destination naming it with no page
// destination to fall back on: a destination array, or an /SD beside no /D. A
// tree, /ParentTree or /IDTree the walks cannot read whole leaves every Figure
// in place. A Figure written directly whose kids lead back to the /K array
// holding it stays too.
//
// It runs before [fixStructDest], so the structure destinations that fixer
// writes never name a removed Figure.
//
// # Corpus evidence
//
//   - All corpora: over the 1129 tagged files, aggressive remediation removes
//     197 Figures from 18 files at PDF/UA-1 and 176 from 15 at PDF/UA-2 and
//     WTPDF-1.0 Accessibility, all but 9 and 2 of them with no kid, the others
//     listing an identifier their page does not mark. No destination, /IDTree
//     or /Ref names one. Only those outputs change, and litho and veraPDF 1.30.2
//     report on each the rules and counts they reported with the placeholder.
//   - litho emptyfig_ua2.pdf: holds the forms and references no other file
//     reaches, and agrees with veraPDF on every output. Its F10 is the only
//     element on page 2, which an outline item's /SD names: removed, it left
//     8.8 test 2 once on both tools at PDF/UA-2 and WTPDF-1.0 Accessibility,
//     and kept, those outputs pass both.
//   - litho sharedk_ua1.pdf, directfig_ua2.pdf: the only files over the 1132
//     tagged ones that write a Figure directly in a /K. Their empty ones stayed
//     under the placeholder-less aggressive run and failed 7.3 or 8.2.5.28.2
//     on both tools; now they go and no other output of the 1132 moves.
//   - All corpora: no file has an element written inline list an element of
//     its own, over the 203 files at PDF/UA-1 and 169 at PDF/UA-2 that hold a
//     Figure lacking alternate text.
//   - litho inlinekids_ua2.pdf: packs the Figures only inline elements list.
//     Its empty ones took the placeholder under the aggressive run, since the
//     removal kept every one; now they go, and the outputs pass litho and
//     veraPDF 1.30.2 at PDF/UA-1 and PDF/UA-2 as before.
func fixEmptyFigure(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, profile string) ([]string, error) {
	if opts.PreserveContent || opts.FigureAlt != "" {
		return nil, nil
	}
	f := newEmptyFigures(ctx, alloc, doc, profile)
	if f == nil {
		return nil, nil
	}
	removed, err := f.plan(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding empty Figures: %w", err)
	}

	snap := alloc.Snapshot()
	dests, err := f.remove(ctx, removed)
	if err == nil && len(dests.stranded) > 0 {
		// plan keeps every Figure whose removal strands a structure
		// destination, so this is a walk reading differently the second
		// time: leave every Figure rather than drop an /SD.
		err = fmt.Errorf("structure destinations naming Figures %v: %w", dests.stranded, errStructTreeUnread)
	}
	if err == nil && len(removed) == 0 && dests.direct == 0 {
		alloc.Restore(snap)
		return nil, nil
	}
	if err != nil {
		// A walk stopping short would leave references to a removed Figure
		// behind, so every Figure stays and figure-alt answers it.
		alloc.Restore(snap)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("removing empty Figures: %w", ctxErr)
		}
		doc.Logger.Debug("empty-figure: leaving the Figures in place", "error", err)
		return nil, nil
	}

	fixes := make([]string, 0, len(removed)+2)
	for _, num := range removed {
		fixes = append(fixes, fmt.Sprintf("removed empty Figure %d 0 R (encloses no content, so no alternate text describes it)", num))
	}
	if dests.direct > 0 {
		fixes = append(fixes, fmt.Sprintf("removed %d empty Figure(s) written directly in a /K (enclosing no content, so no alternate text describes them)", dests.direct))
	}
	if dests.retargeted+dests.outlines > 0 {
		fixes = append(fixes, fmt.Sprintf("repointed the references to removed Figures: %d structure destination(s) retargeted, %d outline /SE entry(ies) dropped",
			dests.retargeted, dests.outlines))
	}
	return fixes, nil
}

// lookupState tells an object that resolves from one that is the null object,
// or undefined, from one that cannot be read.
type lookupState int

const (
	lookupFound lookupState = iota
	// lookupNull is the null object, or a reference to an object the
	// document does not define, which ISO 32000-1, 7.3.10 reads as null.
	lookupNull
	lookupUnreadable
)

// parentTreeClaim is one /ParentTree entry naming an element: the slot mcid of
// the array under key, or the value under key itself when mcid is -1 (ISO
// 32000-1, 14.7.4.4).
type parentTreeClaim struct{ key, mcid int }

// destCounts tallies what a removal did beyond the Figures it took out by
// object number: the Figures written directly it took out, the destinations it
// repointed, and the Figures named by a structure destination that found no
// element to name instead.
type destCounts struct {
	direct, retargeted, outlines int
	stranded                     []int
}

// emptyFigures finds the Figures [fixEmptyFigure] removes and removes them.
// Every object is read from alloc first.
type emptyFigures struct {
	alloc      *writer.ObjAllocator
	doc        *document.Document
	catalogRef ast.PdfIndirectRef
	rootRef    ast.PdfIndirectRef
	// elems holds each element the walk of /K reached through an object
	// number of its own, as alloc stages it, and order lists their numbers in
	// the order the walk reached them.
	elems map[int]ast.PdfDict
	order []int
	// arrays marks the /K arrays written as objects of their own that the
	// walk read, each read once: the direct elements such an array holds can
	// each name it again as their /K.
	arrays map[int]bool
	// holders maps an element to the object numbers of the nodes whose /K
	// lists it, elements or the root, and inlineHeld marks the elements an
	// element written inline lists, which [emptyFigures.dropDirect] unlists
	// in the object holding that element.
	holders    map[int][]int
	inlineHeld map[int]bool
	// figures marks the elements that stand for Figure and lack the
	// alternate text the profile requires.
	figures map[int]bool
	// kept marks the elements that stay whatever they enclose: named by an
	// inline element's /Ref, or named by a destination with no page
	// destination beside it.
	kept map[int]bool
	// claims maps a Figure to the /ParentTree entries naming it.
	claims map[int][]parentTreeClaim
	// pages maps a page's object number to its page tree entry, and keyPage
	// a /StructParents key to the page carrying it. Both load on first use.
	pages   map[int]annotPageEntry
	keyPage map[int]int
	// mcids holds, per page, the marked-content identifiers its content
	// carries, nil for a page whose content cannot be read.
	mcids map[int]map[int]bool
	// annotKeys holds the /StructParent keys the pages' annotations carry,
	// loaded on first use.
	annotKeys map[int]bool
	verdicts  map[int]bool
	roleMap   map[ast.PdfName]ast.PdfName
	profile   string
	// direct reports that the walk met a Figure written directly in a /K
	// that lacks alternate text.
	direct bool
	// directVerdicts caches whether the kids a Figure written directly lists
	// through a /K array written as an object of its own enclose nothing, by
	// the array's object number and the object number of the Figure's /Pg: a
	// shared array's verdict is reached once per path through it otherwise.
	directVerdicts map[[2]int]bool
	// sdNamed marks the Figures a structure destination beside a page
	// destination names.
	sdNamed map[int]bool
}

// newEmptyFigures walks the structure tree and returns nil when it holds no
// Figure lacking alternate text, or when the tree or its /ParentTree cannot
// be read whole.
func newEmptyFigures(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, profile string) *emptyFigures {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil
	}
	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalog == nil {
		return nil
	}
	rootRef, ok := catalog["StructTreeRoot"].(ast.PdfIndirectRef)
	if !ok || rootRef.ObjectNum <= 0 {
		return nil
	}
	root := resolveFromAllocOrDoc(ctx, alloc, doc, rootRef)
	if root == nil {
		return nil
	}
	f := &emptyFigures{
		alloc:      alloc,
		doc:        doc,
		catalogRef: catalogRef,
		rootRef:    rootRef,
		elems:      make(map[int]ast.PdfDict),
		arrays:     make(map[int]bool),
		holders:    make(map[int][]int),
		inlineHeld: make(map[int]bool),
		figures:    make(map[int]bool),
		kept:       make(map[int]bool),
		claims:     make(map[int][]parentTreeClaim),
		mcids:      make(map[int]map[int]bool),
		verdicts:   make(map[int]bool),
		roleMap:    compliance.LoadRoleMap(ctx, doc, root),
		profile:    profile,

		directVerdicts: make(map[[2]int]bool),
		sdNamed:        make(map[int]bool),
	}
	if !f.walk(ctx, root["K"], rootRef.ObjectNum, 0) {
		doc.Logger.Debug("empty-figure: the structure tree cannot be read whole")
		return nil
	}
	for _, num := range f.order {
		if compliance.FigureNeedsAltText(ctx, doc, f.elems[num], f.roleMap, profile) {
			f.figures[num] = true
		}
	}
	if len(f.figures) == 0 && !f.direct {
		return nil
	}
	if _, _, ok := f.editParentTree(ctx, root["ParentTree"], f.claim, make(map[int]bool), 0); !ok {
		doc.Logger.Debug("empty-figure: the /ParentTree cannot be read whole")
		return nil
	}
	return f
}

// lookup resolves obj, reading an indirect object from alloc first.
func (f *emptyFigures) lookup(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, lookupState) {
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		if pending, queued := f.alloc.Pending()[ref.ObjectNum]; queued {
			obj = pending
		} else {
			resolved, err := f.doc.Resolve(ctx, ref)
			if errors.Is(err, document.ErrObjectNotFound) {
				return nil, lookupNull
			}
			if err != nil {
				return nil, lookupUnreadable
			}
			obj = resolved
		}
	}
	if _, isNull := obj.(ast.PdfNull); isNull || obj == nil {
		return nil, lookupNull
	}
	return obj, lookupFound
}

// intValue returns obj resolved as an integer, and false for anything else.
func (f *emptyFigures) intValue(ctx context.Context, obj ast.PdfObject) (int, bool) {
	v, state := f.lookup(ctx, obj)
	i, isInt := v.(ast.PdfInt)
	return int(i), state == lookupFound && isInt
}

// kids returns the entries of a /K value: the array's, or the one entry it is
// (ISO 32000-1, Table 323).
func (f *emptyFigures) kids(ctx context.Context, kObj ast.PdfObject) ast.PdfArray {
	v, state := f.lookup(ctx, kObj)
	if state != lookupFound {
		return nil
	}
	if arr, isArr := v.(ast.PdfArray); isArr {
		return arr
	}
	return ast.PdfArray{kObj}
}

// isStructElement reports whether d is a structure element rather than a
// marked-content or object reference (ISO 32000-1, Tables 323 to 325).
func isStructElement(d ast.PdfDict) bool {
	if kind, _ := d["Type"].(ast.PdfName); kind == "MCR" || kind == "OBJR" {
		return false
	}
	_, hasType := d["S"]
	return hasType
}

// walk records the elements the /K value kObj lists, holder being the object
// number of the node listing them, 0 for an element written inline, and
// descends into each. A /K array written as an object of its own is read once
// ([emptyFigures.arrays]), as an element is: walked anew wherever it is
// reached, direct elements that name it again fan it out 2^depth. It reports false when the walk stopped at
// [compliance.MaxStructTreeWalkDepth], the depth validation walks to, with part
// of the tree unread.
func (f *emptyFigures) walk(ctx context.Context, kObj ast.PdfObject, holder, depth int) bool {
	if depth > compliance.MaxStructTreeWalkDepth || ctx.Err() != nil {
		return false
	}
	if v, state := f.lookup(ctx, kObj); state == lookupFound {
		if _, isArr := v.(ast.PdfArray); isArr && document.VisitedBefore(kObj, f.arrays) {
			return true
		}
	}
	for _, kid := range f.kids(ctx, kObj) {
		switch v := kid.(type) {
		case ast.PdfIndirectRef:
			obj, _ := f.lookup(ctx, v)
			elem, isDict := obj.(ast.PdfDict)
			if !isDict || !isStructElement(elem) || v.ObjectNum <= 0 {
				continue
			}
			if holder == 0 {
				f.inlineHeld[v.ObjectNum] = true
			} else {
				f.holders[v.ObjectNum] = append(f.holders[v.ObjectNum], holder)
			}
			if _, seen := f.elems[v.ObjectNum]; seen {
				continue
			}
			f.elems[v.ObjectNum] = elem
			f.order = append(f.order, v.ObjectNum)
			if !f.walk(ctx, elem["K"], v.ObjectNum, depth+1) {
				return false
			}
		case ast.PdfDict:
			if !isStructElement(v) {
				continue
			}
			if compliance.FigureNeedsAltText(ctx, f.doc, v, f.roleMap, f.profile) {
				f.direct = true
			}
			// An element written inline has no object number to write it
			// back at, so what it names in /Ref stays named.
			for _, target := range f.kids(ctx, v["Ref"]) {
				if ref, isRef := target.(ast.PdfIndirectRef); isRef {
					f.kept[ref.ObjectNum] = true
				}
			}
			if !f.walk(ctx, v["K"], 0, depth+1) {
				return false
			}
		}
	}
	return true
}

// plan returns the object numbers of the Figures to remove, in ascending
// order.
//
// A structure destination naming a Figure that goes names the element its page
// destination opens on once the Figure is gone ([emptyFigures.retarget]). When
// no element begins on that page but the Figure, the /SD would go and leave
// the action a page destination alone, which ISO 14289-2, 8.8 test 2 rejects,
// so such a Figure stays. plan finds them by running the removal on a snapshot
// of alloc, and runs it again with the Figures kept, until no destination is
// left without an element: each run keeps at least one more Figure, so the
// loop ends within as many runs as Figures go.
func (f *emptyFigures) plan(ctx context.Context) ([]int, error) {
	removed := f.removable(ctx)
	if len(removed) == 0 {
		return nil, nil
	}
	// A destination naming a Figure keeps it when no page destination stands
	// beside it. Reading every object is dear, so the sweep runs only once
	// some Figure is found to go.
	pinned, err := f.pinDestinations(ctx)
	if err != nil {
		return nil, err
	}
	if pinned {
		clear(f.verdicts)
		removed = f.removable(ctx)
	}
	for slices.ContainsFunc(removed, func(num int) bool { return f.sdNamed[num] }) {
		snap := f.alloc.Snapshot()
		dests, err := f.remove(ctx, removed)
		f.alloc.Restore(snap)
		if err != nil {
			return nil, fmt.Errorf("trying the removal: %w", err)
		}
		if len(dests.stranded) == 0 {
			break
		}
		for _, num := range dests.stranded {
			f.kept[num] = true
		}
		clear(f.verdicts)
		removed = f.removable(ctx)
	}
	return removed, nil
}

// removable returns the object numbers of the Figures that go, in ascending
// order.
func (f *emptyFigures) removable(ctx context.Context) []int {
	var out []int
	for _, num := range slices.Sorted(maps.Keys(f.figures)) {
		if f.goes(ctx, num, make(map[int]bool)) {
			out = append(out, num)
		}
	}
	return out
}

// goes reports whether the Figure at num encloses nothing and nothing keeps
// it. onPath holds the Figures whose verdict waits on this one, so a /K cycle
// answers that the Figure stays.
func (f *emptyFigures) goes(ctx context.Context, num int, onPath map[int]bool) bool {
	if verdict, done := f.verdicts[num]; done {
		return verdict
	}
	if onPath[num] || ctx.Err() != nil {
		return false
	}
	if !f.figures[num] || f.kept[num] || (len(f.holders[num]) == 0 && !f.inlineHeld[num]) {
		f.verdicts[num] = false
		return false
	}
	onPath[num] = true
	fig := f.elems[num]
	verdict := true
	for _, kid := range f.kids(ctx, fig["K"]) {
		if f.encloses(ctx, fig, kid, onPath) {
			verdict = false
			break
		}
	}
	if verdict {
		verdict = !f.claimed(ctx, num)
	}
	delete(onPath, num)
	f.verdicts[num] = verdict
	return verdict
}

// encloses reports whether kid, a kid of the Figure fig, may enclose content:
// anything the kinds [fixEmptyFigure] lists as enclosing nothing does not
// cover.
func (f *emptyFigures) encloses(ctx context.Context, fig ast.PdfDict, kid ast.PdfObject, onPath map[int]bool) bool {
	if mcid, isInt := kid.(ast.PdfInt); isInt {
		return f.marked(ctx, fig["Pg"], int(mcid))
	}
	obj, state := f.lookup(ctx, kid)
	switch state {
	case lookupNull:
		return false
	case lookupUnreadable:
		return true
	case lookupFound:
	}
	dict, isDict := obj.(ast.PdfDict)
	if !isDict {
		return true
	}
	switch kind, _ := dict["Type"].(ast.PdfName); kind {
	case "MCR":
		// ISO 32000-1, Table 324: /Stm names a form XObject whose content
		// holds the sequence, not the page's.
		if present(dict, "Stm") {
			return true
		}
		mcid, ok := f.intValue(ctx, dict["MCID"])
		if !ok {
			return true
		}
		page := dict["Pg"]
		if !present(dict, "Pg") {
			page = fig["Pg"]
		}
		return f.marked(ctx, page, mcid)
	case "OBJR":
		_, target := f.lookup(ctx, dict["Obj"])
		return target != lookupNull
	}
	if !isStructElement(dict) {
		return true
	}
	ref, isRef := kid.(ast.PdfIndirectRef)
	if !isRef {
		return !f.directGoes(ctx, dict, onPath)
	}
	return !f.goes(ctx, ref.ObjectNum, onPath)
}

// directGoes reports whether fig, a structure element written directly in a
// /K, is a Figure lacking alternate text that encloses nothing. Nothing but
// the value holding it names such an element, so no reference keeps it. A
// /K array written as an object of its own is judged once per /Pg
// ([emptyFigures.directVerdicts]), and one the verdict already waits on
// answers that the Figure stays, as a /K cycle does in [emptyFigures.goes].
func (f *emptyFigures) directGoes(ctx context.Context, fig ast.PdfDict, onPath map[int]bool) bool {
	if ctx.Err() != nil || len(onPath) > compliance.MaxStructTreeWalkDepth ||
		!compliance.FigureNeedsAltText(ctx, f.doc, fig, f.roleMap, f.profile) {
		return false
	}
	ref, isRef := fig["K"].(ast.PdfIndirectRef)
	if !isRef {
		return f.enclosesNothing(ctx, fig, onPath)
	}
	if onPath[ref.ObjectNum] {
		return false
	}
	page, _ := fig["Pg"].(ast.PdfIndirectRef)
	key := [2]int{ref.ObjectNum, page.ObjectNum}
	if verdict, done := f.directVerdicts[key]; done {
		return verdict
	}
	onPath[ref.ObjectNum] = true
	verdict := f.enclosesNothing(ctx, fig, onPath)
	delete(onPath, ref.ObjectNum)
	f.directVerdicts[key] = verdict
	return verdict
}

// enclosesNothing reports whether no kid of the Figure fig may enclose content
// ([emptyFigures.encloses]).
func (f *emptyFigures) enclosesNothing(ctx context.Context, fig ast.PdfDict, onPath map[int]bool) bool {
	for _, kid := range f.kids(ctx, fig["K"]) {
		if f.encloses(ctx, fig, kid, onPath) {
			return false
		}
	}
	return true
}

// marked reports whether the page pgObj names may carry a marked-content
// sequence with identifier mcid: it does, or the page is no page of the
// document, or its content cannot be read.
func (f *emptyFigures) marked(ctx context.Context, pgObj ast.PdfObject, mcid int) bool {
	pg, isRef := pgObj.(ast.PdfIndirectRef)
	if !isRef {
		return true
	}
	set := f.pageMCIDs(ctx, pg.ObjectNum)
	return set == nil || set[mcid]
}

// claimed reports whether a /ParentTree entry naming the Figure at num pairs it
// with content: a slot of a page's array whose identifier the page's content
// carries, the array of a key no page carries, which a form XObject's content
// may use (ISO 32000-1, 14.7.4.4), or an entry an annotation's /StructParent
// names.
func (f *emptyFigures) claimed(ctx context.Context, num int) bool {
	for _, c := range f.claims[num] {
		if c.mcid < 0 {
			if f.annotationKeys(ctx)[c.key] {
				return true
			}
			continue
		}
		f.loadPages(ctx)
		page, ok := f.keyPage[c.key]
		if !ok {
			return true
		}
		if set := f.pageMCIDs(ctx, page); set == nil || set[c.mcid] {
			return true
		}
	}
	return false
}

// claim records the /ParentTree entries naming a Figure. It edits nothing.
func (f *emptyFigures) claim(ctx context.Context, key int, value ast.PdfObject) (ast.PdfObject, bool) {
	if ref, isRef := value.(ast.PdfIndirectRef); isRef && f.figures[ref.ObjectNum] {
		f.claims[ref.ObjectNum] = append(f.claims[ref.ObjectNum], parentTreeClaim{key: key, mcid: -1})
		return value, false
	}
	obj, _ := f.lookup(ctx, value)
	slots, isArr := obj.(ast.PdfArray)
	if !isArr {
		return value, false
	}
	for i, entry := range slots {
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef && f.figures[ref.ObjectNum] {
			f.claims[ref.ObjectNum] = append(f.claims[ref.ObjectNum], parentTreeClaim{key: key, mcid: i})
		}
	}
	return value, false
}

// loadPages reads the page tree once: each page's entry by object number, and
// the page each /StructParents key names (ISO 32000-1, Table 30).
func (f *emptyFigures) loadPages(ctx context.Context) {
	if f.pages != nil {
		return
	}
	f.pages = make(map[int]annotPageEntry)
	f.keyPage = make(map[int]int)
	catalog, err := document.ResolveCatalog(ctx, f.doc)
	if err != nil {
		f.doc.Logger.Debug("empty-figure: cannot resolve the catalog", "error", err)
		return
	}
	pages, err := document.ResolvePages(ctx, f.doc, catalog)
	if err != nil {
		f.doc.Logger.Debug("empty-figure: cannot resolve the page tree", "error", err)
		return
	}
	for _, pe := range pageTreeEntries(ctx, f.alloc, f.doc, pages) {
		if pe.ref.ObjectNum <= 0 {
			continue
		}
		pe = pageTreeEntry(ctx, f.alloc, f.doc, pe)
		f.pages[pe.ref.ObjectNum] = pe
		if key, ok := f.intValue(ctx, pe.dict["StructParents"]); ok {
			f.keyPage[key] = pe.ref.ObjectNum
		}
	}
}

// annotationKeys returns the /StructParent keys the annotations of the
// document's pages carry (ISO 32000-1, Table 164).
func (f *emptyFigures) annotationKeys(ctx context.Context) map[int]bool {
	if f.annotKeys != nil {
		return f.annotKeys
	}
	f.loadPages(ctx)
	f.annotKeys = make(map[int]bool)
	for _, num := range slices.Sorted(maps.Keys(f.pages)) {
		for _, entry := range liveArray(ctx, f.alloc, f.doc, f.pages[num].dict["Annots"]) {
			if annot := liveAnnotation(ctx, f.alloc, f.doc, entry); annot != nil {
				if key, ok := f.intValue(ctx, annot["StructParent"]); ok {
					f.annotKeys[key] = true
				}
			}
		}
	}
	return f.annotKeys
}

// pageMCIDs returns the marked-content identifiers the page at pageNum carries,
// nil when it is no page of the document or its content cannot be read.
func (f *emptyFigures) pageMCIDs(ctx context.Context, pageNum int) map[int]bool {
	if set, done := f.mcids[pageNum]; done {
		return set
	}
	f.loadPages(ctx)
	var set map[int]bool
	if pe, ok := f.pages[pageNum]; ok {
		set = f.collectMCIDs(ctx, pe)
	}
	f.mcids[pageNum] = set
	return set
}

// collectMCIDs returns the marked-content identifiers the page of pe carries,
// in its own content, in the appearance streams of its annotations and in every
// form XObject their resources name, drawn or not, or nil when some of it
// cannot be read. A form's sequences belong to the page when the form has no
// /StructParents (ISO 32000-1, 14.7.4.3), and an appearance is a form (12.5.5);
// reading every one keeps a Figure that one of them may enclose.
func (f *emptyFigures) collectMCIDs(ctx context.Context, pe annotPageEntry) map[int]bool {
	var data []byte
	if contents, has := pe.dict["Contents"]; has {
		read, err := liveContentBytes(ctx, f.alloc, f.doc, contents)
		if err != nil {
			f.doc.Logger.Debug("empty-figure: cannot read a page's content", "page", pe.ref.ObjectNum, "error", err)
			return nil
		}
		data = read
	}
	set := make(map[int]bool)
	visited := make(map[int]bool)
	res := pageResourcesFor(ctx, f.alloc, f.doc, pe)
	if !f.streamMCIDs(ctx, data, res, set, visited, 0) {
		return nil
	}
	for _, entry := range liveArray(ctx, f.alloc, f.doc, pe.dict["Annots"]) {
		ap := liveDictValue(ctx, f.alloc, f.doc, liveAnnotation(ctx, f.alloc, f.doc, entry)["AP"])
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			appearances := ast.PdfArray{ap[key]}
			// An appearance is a stream, or a dictionary of them by state.
			if states := liveDictValue(ctx, f.alloc, f.doc, ap[key]); states != nil {
				appearances = appearances[:0]
				for _, state := range slices.Sorted(maps.Keys(states)) {
					appearances = append(appearances, states[state])
				}
			}
			for _, appearance := range appearances {
				if !f.formMCIDs(ctx, appearance, res, set, visited, 1) {
					return nil
				}
			}
		}
	}
	return set
}

// streamMCIDs adds to set the identifiers the marked-content sequences of one
// content stream carry (ISO 32000-1, 14.6.2: a BDC property list written inline
// or named in the /Properties of res), then those of each form XObject res
// names, below the document's form-nesting budget. It reports false when
// something cannot be read.
func (f *emptyFigures) streamMCIDs(ctx context.Context, data []byte, res ast.PdfDict, set, visited map[int]bool, depth int) bool {
	if depth > f.doc.Limits().MaxFormDepth || ctx.Err() != nil {
		return false
	}
	ops, err := content.ParseStreamData(ctx, data, content.WholeStream())
	if err != nil {
		return false
	}
	var properties ast.PdfDict
	for _, op := range ops {
		if op.Operator != "BDC" || len(op.Operands) < 2 {
			continue
		}
		props, isDict := op.Operands[1].(ast.PdfDict)
		if name, isName := op.Operands[1].(ast.PdfName); isName {
			if properties == nil {
				properties = liveDictValue(ctx, f.alloc, f.doc, res["Properties"])
			}
			props, isDict = liveDictValue(ctx, f.alloc, f.doc, properties[name]), true
		}
		if !isDict {
			continue
		}
		if mcid, ok := f.intValue(ctx, props["MCID"]); ok {
			set[mcid] = true
		}
	}
	xobjects := liveDictValue(ctx, f.alloc, f.doc, res["XObject"])
	for _, name := range slices.Sorted(maps.Keys(xobjects)) {
		if !f.formMCIDs(ctx, xobjects[name], res, set, visited, depth+1) {
			return false
		}
	}
	return true
}

// formMCIDs adds to set the identifiers the form XObject obj names carries, in
// its content and the forms its resources name, [streamMCIDs] reading them. An
// entry that names no form, or one already read, adds nothing. It reports
// false when something cannot be read.
func (f *emptyFigures) formMCIDs(ctx context.Context, obj ast.PdfObject, res ast.PdfDict, set, visited map[int]bool, depth int) bool {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if !isRef || ref.ObjectNum <= 0 || visited[ref.ObjectNum] {
		return true
	}
	stream, ok := liveStream(ctx, f.alloc, f.doc, ref)
	if !ok {
		return true
	}
	// An appearance stream is a form XObject whether or not it states its
	// /Subtype (ISO 32000-1, 12.5.5), and an image names no marked content.
	if subtype, _ := document.ResolveName(ctx, f.doc, stream.Dict["Subtype"]); subtype != "" && !isFormXObject(ctx, f.doc, stream.Dict) {
		return true
	}
	visited[ref.ObjectNum] = true
	body, err := f.doc.DecodeStream(ctx, stream)
	if err != nil {
		return false
	}
	// A form stating no /Resources takes the ones of the stream drawing it
	// (ISO 32000-1, 7.8.3).
	formRes := liveDictValue(ctx, f.alloc, f.doc, stream.Dict["Resources"])
	if formRes == nil {
		formRes = res
	}
	return f.streamMCIDs(ctx, body, formRes, set, visited, depth)
}

// pinDestinations keeps each Figure a destination names that has no page
// destination to fall back on, and reports whether it kept one. An explicit
// destination whose target is a structure element (ISO 32000-2, 12.3.2.3) has
// no page to stand in for it, and neither has an /SD beside no /D.
func (f *emptyFigures) pinDestinations(ctx context.Context) (bool, error) {
	pinned := false
	pin := func(v ast.PdfObject) {
		if f.pinValue(ctx, v, 0) {
			pinned = true
		}
	}
	_, err := walkNumberedObjects(ctx, f.alloc, f.doc,
		func(_ int, d ast.PdfDict) (ast.PdfDict, bool) { pin(d); return nil, false },
		func(_ int, a ast.PdfArray) (ast.PdfArray, bool) { pin(a); return nil, false })
	if err != nil {
		return false, fmt.Errorf("reading the destinations naming Figures: %w", err)
	}
	return pinned, nil
}

// pinValue keeps the Figures the destinations written in v name with no page
// destination beside them, and reports whether it kept one.
func (f *emptyFigures) pinValue(ctx context.Context, v ast.PdfObject, depth int) bool {
	if depth > maxDestSweepDepth {
		return false
	}
	pinned := false
	switch x := v.(type) {
	case ast.PdfArray:
		if len(x) >= 2 {
			view, _ := x[1].(ast.PdfName)
			if ref, isRef := x[0].(ast.PdfIndirectRef); isRef && f.figures[ref.ObjectNum] && destinationViews[view] {
				f.kept[ref.ObjectNum] = true
				pinned = true
			}
		}
		for _, item := range x {
			pinned = f.pinValue(ctx, item, depth+1) || pinned
		}
	case ast.PdfDict:
		if num, names := f.structDestTarget(ctx, x["SD"], f.figures); names && !remoteAction(x) {
			if present(x, "D") {
				f.sdNamed[num] = true
			} else {
				f.kept[num] = true
				pinned = true
			}
		}
		for _, key := range slices.Sorted(maps.Keys(x)) {
			// An /SD beside a /D is repointed, not kept: its array is judged
			// here, with the dictionary holding it.
			if key == "SD" {
				continue
			}
			pinned = f.pinValue(ctx, x[key], depth+1) || pinned
		}
	}
	return pinned
}

// structDestTarget returns the object number of the element the structure
// destination sd names, and whether set holds it.
func (f *emptyFigures) structDestTarget(ctx context.Context, sd ast.PdfObject, set map[int]bool) (int, bool) {
	if sd == nil {
		return 0, false
	}
	obj, _ := f.lookup(ctx, sd)
	arr, isArr := obj.(ast.PdfArray)
	if !isArr || len(arr) == 0 {
		return 0, false
	}
	ref, isRef := arr[0].(ast.PdfIndirectRef)
	if !isRef || !set[ref.ObjectNum] {
		return 0, false
	}
	return ref.ObjectNum, true
}

// remoteAction reports whether d is a remote or embedded go-to action, whose
// /SD names an element of another document (ISO 32000-2, Tables 203 and 204).
func remoteAction(d ast.PdfDict) bool {
	s, _ := d["S"].(ast.PdfName)
	return s == "GoToR" || s == "GoToE"
}

// remove takes the Figures removed lists out of the tree and every reference
// to them out of the document, and returns what it did to destinations.
func (f *emptyFigures) remove(ctx context.Context, removed []int) (destCounts, error) {
	gone := make(map[int]bool, len(removed))
	for _, num := range removed {
		gone[num] = true
	}
	f.unlist(ctx, gone)
	f.unref(ctx, gone)
	direct := f.dropDirect(ctx, gone)

	root := resolveFromAllocOrDoc(ctx, f.alloc, f.doc, f.rootRef)
	if root == nil {
		return destCounts{}, fmt.Errorf("rereading the structure tree root: %w", errStructTreeUnread)
	}
	newRoot := maps.Clone(root)
	rootChanged := false

	unclaim := func(ctx context.Context, _ int, value ast.PdfObject) (ast.PdfObject, bool) {
		return f.unclaim(ctx, value, gone)
	}
	tree, replace, ok := f.editParentTree(ctx, root["ParentTree"], unclaim, make(map[int]bool), 0)
	if !ok {
		return destCounts{}, fmt.Errorf("editing the /ParentTree: %w", errStructTreeUnread)
	}
	if replace {
		newRoot["ParentTree"] = tree
		rootChanged = true
	}

	if ids, has := root["IDTree"]; has {
		pruned, changed, truncated := mutations.PruneNameTree(ctx, f.alloc, f.doc, ids, func(value ast.PdfObject) bool {
			ref, isRef := value.(ast.PdfIndirectRef)
			return isRef && gone[ref.ObjectNum]
		})
		if truncated {
			return destCounts{}, fmt.Errorf("pruning the /IDTree: %w", errStructTreeUnread)
		}
		if _, isRef := ids.(ast.PdfIndirectRef); changed && !isRef {
			newRoot["IDTree"] = pruned
			rootChanged = true
		}
	}
	if rootChanged {
		f.alloc.AllocAt(f.rootRef.ObjectNum, newRoot)
	}
	var counts destCounts
	if len(gone) > 0 {
		var err error
		if counts, err = f.retarget(ctx, gone); err != nil {
			return destCounts{}, err
		}
	}
	counts.direct = direct
	return counts, nil
}

// dropDirect takes out each Figure written directly in a /K that encloses
// nothing ([emptyFigures.directGoes]), and each reference to a Figure gone
// holds that the /K of an element written inline lists, and returns how many
// Figures written directly it took out. It rewrites the object holding one at
// its number: the structure tree root or an element whose /K holds it, through
// inline elements however deep, or a /K array written as an object of its own.
// A node left with no kid loses /K, and such an array is left empty.
func (f *emptyFigures) dropDirect(ctx context.Context, gone map[int]bool) int {
	dropped := 0
	for _, num := range append([]int{f.rootRef.ObjectNum}, f.order...) {
		if gone[num] || ctx.Err() != nil {
			continue
		}
		node := resolveFromAllocOrDoc(ctx, f.alloc, f.doc, ast.PdfIndirectRef{ObjectNum: num})
		updated, n, changed := f.pruneElement(ctx, node, gone)
		if changed {
			dropped += n
			f.alloc.AllocAt(num, updated)
		}
	}
	for _, num := range slices.Sorted(maps.Keys(f.arrays)) {
		obj, _ := f.lookup(ctx, ast.PdfIndirectRef{ObjectNum: num})
		arr, isArr := obj.(ast.PdfArray)
		if !isArr {
			continue
		}
		kept, n, changed := f.pruneKids(ctx, arr, gone)
		if !changed {
			continue
		}
		dropped += n
		if kept == nil {
			kept = ast.PdfArray{}
		}
		f.alloc.AllocAt(num, kept)
	}
	return dropped
}

// pruneElement returns node with the Figures written directly in its /K that
// go, and the references to a Figure gone holds, taken out
// ([emptyFigures.pruneKids]), how many Figures written directly went, and
// whether node changed. A /K naming any other object is left to that object's
// own turn.
func (f *emptyFigures) pruneElement(ctx context.Context, node ast.PdfDict, gone map[int]bool) (ast.PdfDict, int, bool) {
	kObj, has := node["K"]
	if !has {
		return node, 0, false
	}
	if ref, isRef := kObj.(ast.PdfIndirectRef); isRef {
		if !gone[ref.ObjectNum] {
			return node, 0, false
		}
		updated := maps.Clone(node)
		delete(updated, "K")
		return updated, 0, true
	}
	kept, n, changed := f.pruneKids(ctx, kObj, gone)
	if !changed {
		return node, 0, false
	}
	updated := maps.Clone(node)
	if kept == nil {
		delete(updated, "K")
	} else {
		updated["K"] = kept
	}
	return updated, n, true
}

// pruneKids returns the /K value kObj without the Figures written directly in
// it that go and the references to a Figure gone holds, and with the inline
// elements it holds pruned in turn, how many Figures written directly went, and
// whether kObj changed. It returns nil when no kid is left.
func (f *emptyFigures) pruneKids(ctx context.Context, kObj ast.PdfObject, gone map[int]bool) (ast.PdfObject, int, bool) {
	switch v := kObj.(type) {
	case ast.PdfArray:
		out := make(ast.PdfArray, 0, len(v))
		n := 0
		changed := false
		for _, item := range v {
			if ref, isRef := item.(ast.PdfIndirectRef); isRef && gone[ref.ObjectNum] {
				changed = true
				continue
			}
			elem, isDict := item.(ast.PdfDict)
			if !isDict || !isStructElement(elem) {
				out = append(out, item)
				continue
			}
			if f.directGoes(ctx, elem, make(map[int]bool)) {
				n++
				changed = true
				continue
			}
			updated, m, elemChanged := f.pruneElement(ctx, elem, gone)
			n += m
			changed = changed || elemChanged
			out = append(out, updated)
		}
		if !changed {
			return kObj, 0, false
		}
		if len(out) == 0 {
			return nil, n, true
		}
		return out, n, true
	case ast.PdfDict:
		if !isStructElement(v) {
			return kObj, 0, false
		}
		if f.directGoes(ctx, v, make(map[int]bool)) {
			return nil, 1, true
		}
		return f.pruneElement(ctx, v, gone)
	}
	return kObj, 0, false
}

// unlist takes the removed Figures out of the /K of each node listing them,
// one that goes too aside. A /K array written as an object of its own is
// rewritten at its number, and a node left with no kid loses /K.
func (f *emptyFigures) unlist(ctx context.Context, gone map[int]bool) {
	holders := make(map[int]bool)
	for num := range gone {
		for _, holder := range f.holders[num] {
			if !gone[holder] {
				holders[holder] = true
			}
		}
	}
	for _, num := range slices.Sorted(maps.Keys(holders)) {
		node := resolveFromAllocOrDoc(ctx, f.alloc, f.doc, ast.PdfIndirectRef{ObjectNum: num})
		if node == nil {
			continue
		}
		if ref, isRef := node["K"].(ast.PdfIndirectRef); isRef {
			if obj, _ := f.lookup(ctx, ref); obj != nil {
				if arr, isArr := obj.(ast.PdfArray); isArr {
					f.alloc.AllocAt(ref.ObjectNum, withoutRefs(arr, gone))
					continue
				}
			}
		}
		kept := withoutRefs(f.kids(ctx, node["K"]), gone)
		updated := maps.Clone(node)
		if len(kept) == 0 {
			delete(updated, "K")
		} else {
			updated["K"] = kept
		}
		f.alloc.AllocAt(num, updated)
	}
}

// unref takes the removed Figures out of the /Ref of every element that stays
// (ISO 32000-2, 14.7.2, Table 355). A /Ref array written as an object of its
// own is rewritten at its number, and an element left naming none loses /Ref.
func (f *emptyFigures) unref(ctx context.Context, gone map[int]bool) {
	for _, num := range f.order {
		if gone[num] || ctx.Err() != nil {
			continue
		}
		elem := resolveFromAllocOrDoc(ctx, f.alloc, f.doc, ast.PdfIndirectRef{ObjectNum: num})
		refObj, has := elem["Ref"]
		if elem == nil || !has {
			continue
		}
		targets := f.kids(ctx, refObj)
		kept := withoutRefs(targets, gone)
		if len(kept) == len(targets) {
			continue
		}
		if ref, isRef := refObj.(ast.PdfIndirectRef); isRef && len(kept) > 0 {
			if obj, _ := f.lookup(ctx, ref); obj != nil {
				if _, isArr := obj.(ast.PdfArray); isArr {
					f.alloc.AllocAt(ref.ObjectNum, kept)
					continue
				}
			}
		}
		updated := maps.Clone(elem)
		if len(kept) == 0 {
			delete(updated, "Ref")
		} else {
			updated["Ref"] = kept
		}
		f.alloc.AllocAt(num, updated)
	}
}

// withoutRefs returns list without the references to an object gone holds.
func withoutRefs(list ast.PdfArray, gone map[int]bool) ast.PdfArray {
	kept := make(ast.PdfArray, 0, len(list))
	for _, item := range list {
		if ref, isRef := item.(ast.PdfIndirectRef); isRef && gone[ref.ObjectNum] {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

// unclaim takes the removed Figures out of one /ParentTree value: an entry
// naming one goes, and a page's slot naming one becomes the null object, so the
// identifiers of the slots after it keep their place (ISO 32000-1, 14.7.4.4).
// An array written as an object of its own is rewritten at its number.
func (f *emptyFigures) unclaim(ctx context.Context, value ast.PdfObject, gone map[int]bool) (ast.PdfObject, bool) {
	if ref, isRef := value.(ast.PdfIndirectRef); isRef && gone[ref.ObjectNum] {
		return nil, true
	}
	obj, _ := f.lookup(ctx, value)
	slots, isArr := obj.(ast.PdfArray)
	if !isArr {
		return value, false
	}
	var out ast.PdfArray
	for i, entry := range slots {
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef && gone[ref.ObjectNum] {
			if out == nil {
				out = slices.Clone(slots)
			}
			out[i] = ast.PdfNull{}
		}
	}
	if out == nil {
		return value, false
	}
	if ref, isRef := value.(ast.PdfIndirectRef); isRef {
		f.alloc.AllocAt(ref.ObjectNum, out)
		return value, false
	}
	return out, true
}

// parentTreeEdit decides what becomes of one /ParentTree entry: the value to
// keep, nil to drop the entry, and whether that differs from value.
type parentTreeEdit func(ctx context.Context, key int, value ast.PdfObject) (ast.PdfObject, bool)

// editParentTree walks the number tree rooted at nodeObj (ISO 32000-1, 7.9.7),
// reading every node from alloc first, and hands edit each entry. A node, or a
// /Nums or /Kids array, written as an object of its own is rewritten at its
// number, so the tree keeps its shape; replace reports that the returned value
// must take the place of nodeObj in its parent, which happens only for a node
// written inline. ok is false when a node does not resolve, a /Nums array is
// malformed, or the walk stopped at [maxNameTreeWalkDepth]: ISO 32000-1, 7.9.7
// gives a number tree the shape of a name tree. An absent tree is read whole.
func (f *emptyFigures) editParentTree(ctx context.Context, nodeObj ast.PdfObject, edit parentTreeEdit, visited map[int]bool, depth int) (value ast.PdfObject, replace, ok bool) {
	if nodeObj == nil {
		return nil, false, true
	}
	if depth > maxNameTreeWalkDepth || ctx.Err() != nil {
		return nodeObj, false, false
	}
	nodeRef, isRef := nodeObj.(ast.PdfIndirectRef)
	if isRef {
		if visited[nodeRef.ObjectNum] {
			return nodeObj, false, true
		}
		visited[nodeRef.ObjectNum] = true
	}
	obj, _ := f.lookup(ctx, nodeObj)
	node, isDict := obj.(ast.PdfDict)
	if !isDict {
		return nodeObj, false, false
	}
	updated := maps.Clone(node)
	changed := false

	if numsObj, has := node["Nums"]; has {
		numsVal, _ := f.lookup(ctx, numsObj)
		nums, isArr := numsVal.(ast.PdfArray)
		if !isArr || len(nums)%2 != 0 {
			return nodeObj, false, false
		}
		kept := make(ast.PdfArray, 0, len(nums))
		numsChanged := false
		for i := 0; i < len(nums); i += 2 {
			key, isInt := f.intValue(ctx, nums[i])
			if !isInt {
				// Table 37 keys a number tree by integer, so no lookup
				// reaches this value.
				kept = append(kept, nums[i], nums[i+1])
				continue
			}
			value, valueChanged := edit(ctx, key, nums[i+1])
			numsChanged = numsChanged || valueChanged
			if value != nil {
				kept = append(kept, nums[i], value)
			}
		}
		if numsChanged {
			changed = true
			if ref, isNumsRef := numsObj.(ast.PdfIndirectRef); isNumsRef {
				f.alloc.AllocAt(ref.ObjectNum, kept)
			} else {
				updated["Nums"] = kept
			}
		}
	}

	if kidsObj, has := node["Kids"]; has {
		kidsVal, _ := f.lookup(ctx, kidsObj)
		kids, isArr := kidsVal.(ast.PdfArray)
		if !isArr {
			return nodeObj, false, false
		}
		var newKids ast.PdfArray
		for i, kid := range kids {
			value, replaceKid, kidOK := f.editParentTree(ctx, kid, edit, visited, depth+1)
			if !kidOK {
				return nodeObj, false, false
			}
			if replaceKid {
				if newKids == nil {
					newKids = slices.Clone(kids)
				}
				newKids[i] = value
			}
		}
		if newKids != nil {
			changed = true
			if ref, isKidsRef := kidsObj.(ast.PdfIndirectRef); isKidsRef {
				f.alloc.AllocAt(ref.ObjectNum, newKids)
			} else {
				updated["Kids"] = newKids
			}
		}
	}

	if !changed {
		return nodeObj, false, true
	}
	if isRef {
		f.alloc.AllocAt(nodeRef.ObjectNum, updated)
		return nodeObj, false, true
	}
	return updated, true, true
}

// retarget repoints the destinations and outline items naming a removed Figure
// throughout the document, reading the tree the removal left: an /SD names the
// element its page destination opens on ([structElemPicker.structDest]), and an
// /SE goes. An /SD with no element to name goes, and the Figure it named is
// reported stranded, for [emptyFigures.plan] to keep.
func (f *emptyFigures) retarget(ctx context.Context, gone map[int]bool) (destCounts, error) {
	var counts destCounts
	catalog := resolveFromAllocOrDoc(ctx, f.alloc, f.doc, f.catalogRef)
	picker := newStructElemPicker(ctx, f.alloc, f.doc, catalog)
	_, err := walkNumberedObjects(ctx, f.alloc, f.doc,
		func(_ int, d ast.PdfDict) (ast.PdfDict, bool) {
			out, changed := f.retargetValue(ctx, d, gone, picker, &counts, 0)
			if !changed {
				return nil, false
			}
			updated, isDict := out.(ast.PdfDict)
			return updated, isDict
		},
		func(_ int, a ast.PdfArray) (ast.PdfArray, bool) {
			out, changed := f.retargetValue(ctx, a, gone, picker, &counts, 0)
			if !changed {
				return nil, false
			}
			updated, isArr := out.(ast.PdfArray)
			return updated, isArr
		})
	if err != nil {
		return destCounts{}, fmt.Errorf("repointing destinations: %w", err)
	}
	return counts, nil
}

// retargetValue returns v with the destinations written in it repointed, and
// whether anything changed. Only values written inline are rewritten here; an
// object v names is rewritten on its own turn of the sweep.
func (f *emptyFigures) retargetValue(ctx context.Context, v ast.PdfObject, gone map[int]bool, picker *structElemPicker, counts *destCounts, depth int) (ast.PdfObject, bool) {
	if depth > maxDestSweepDepth {
		return v, false
	}
	switch x := v.(type) {
	case ast.PdfArray:
		var out ast.PdfArray
		for i, item := range x {
			if updated, changed := f.retargetValue(ctx, item, gone, picker, counts, depth+1); changed {
				if out == nil {
					out = slices.Clone(x)
				}
				out[i] = updated
			}
		}
		if out == nil {
			return v, false
		}
		return out, true
	case ast.PdfDict:
		var out ast.PdfDict
		set := func(key ast.PdfName, value ast.PdfObject) {
			if out == nil {
				out = maps.Clone(x)
			}
			if value == nil {
				delete(out, key)
				return
			}
			out[key] = value
		}
		for _, key := range slices.Sorted(maps.Keys(x)) {
			if updated, changed := f.retargetValue(ctx, x[key], gone, picker, counts, depth+1); changed {
				set(key, updated)
			}
		}
		if num, names := f.structDestTarget(ctx, x["SD"], gone); names && !remoteAction(x) {
			var sd ast.PdfArray
			ok := false
			if picker != nil {
				_, sd, ok = picker.structDest(ctx, x["D"])
			}
			if ok {
				set("SD", sd)
				counts.retargeted++
			} else {
				set("SD", nil)
				if !slices.Contains(counts.stranded, num) {
					counts.stranded = append(counts.stranded, num)
				}
			}
		}
		if ref, isRef := x["SE"].(ast.PdfIndirectRef); isRef && gone[ref.ObjectNum] {
			set("SE", nil)
			counts.outlines++
		}
		if out == nil {
			return v, false
		}
		return out, true
	}
	return v, false
}
