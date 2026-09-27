package compliance

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// typedValue is one value veraPDF judges once more than the object sweep does,
// through a typed object of its model that reads the value again.
//
// veraPDF judges a value once as a COS object, reached through the
// cross-reference table, and again wherever a typed object wraps it in a new
// COS object of its own: a CosTextString, a CosUnicodeName, a CosBBox. The
// wrapper has no identity of its own (GFCosObject.getID is null), so its
// validator judges it whatever else judged the value, against every rule of
// its type: the hex string tests, ISO 19005-1/2/3, 6.1.6 t1 and t2 and ISO
// 19005-4, 6.1.5 t1 and t2, the string and name length limits, ISO 19005-1,
// 6.1.12 t3 and t4 and ISO 19005-2/3, 6.1.13 t3 and t4, and, for an array it
// walks again, the integer and real limits. See [typedValues] for the
// accessors.
type typedValue struct {
	// value is the value as the accessor reads it: resolved when written as
	// an indirect reference. An array or a dictionary keeps its entries as
	// written, since veraPDF reaches an indirect entry once, through the
	// cross-reference table, however often a wrapper names it.
	value ast.PdfObject
	// site and key locate the bytes of value: the entry key of the dictionary
	// at site, or, when key is empty, the object at site itself. The zero
	// site locates nothing, a dictionary written in an array among them.
	site propertyListSite
	key  ast.PdfName
	// whole reports that the accessor walks the value again, its keys and
	// entries at every depth, rather than judging it alone.
	whole bool
	// unicodeName reports that the accessor reads the value, a name, as a
	// CosUnicodeName, which the UTF-8 rule judges too ([checkUTF8Names]).
	unicodeName bool
	// textString reports that the accessor reads the value, a string, as a
	// CosTextString, a CosLang included, which ISO 14289-2, 8.6 t1 judges too
	// ([checkTextStringPUA]).
	textString bool
	// where names the accessor in the context of a violation.
	where string
}

// entryValue returns the value of entry key of dict, whose site is site, as
// accessor where reads it, and false when the entry is absent or null.
func entryValue(ctx context.Context, doc *document.Document, site propertyListSite, dict ast.PdfDict, key ast.PdfName, where string) (typedValue, bool) {
	raw, ok := dict[key]
	if !ok {
		return typedValue{}, false
	}
	value, err := document.ResolveObject(ctx, doc, raw)
	if err != nil || value == nil {
		return typedValue{}, false
	}
	if _, null := value.(ast.PdfNull); null {
		return typedValue{}, false
	}
	v := typedValue{value: value, where: where}
	if ref, isRef := raw.(ast.PdfIndirectRef); isRef {
		v.site = propertyListSite{objNum: ref.ObjectNum}
	} else if site.objNum != 0 {
		v.site, v.key = site, key
	}
	return v, true
}

// entryString returns what [entryValue] returns when the value is a string,
// the only value a CosTextString, a CosLang, a CosActualText or a CosAlt wraps.
func entryString(ctx context.Context, doc *document.Document, site propertyListSite, dict ast.PdfDict, key ast.PdfName, where string) (typedValue, bool) {
	v, ok := entryValue(ctx, doc, site, dict, key, where)
	if _, isString := v.value.(ast.PdfString); !ok || !isString {
		return typedValue{}, false
	}
	return v, true
}

// holds reports whether violation, a format violation recorded while parsing
// the object at v.site, lies in v: in the value itself for a string or a name,
// and anywhere in it for a value the accessor walks whole.
func (v typedValue) holds(violation error) bool {
	if v.site.objNum == 0 {
		return false
	}
	ev, inEntry := errors.AsType[*parser.EntryViolationError](violation)
	if v.key == "" {
		// The value is the object itself, whose every string it holds.
		return len(v.site.keys) == 0
	}
	if !inEntry {
		return false
	}
	key, inDict := v.site.holds(ev)
	if !inDict {
		return false
	}
	n := len(v.site.keys)
	if key != "" {
		return key == v.key
	}
	// The string lies deeper than an entry of the dictionary at site, in the
	// value of entry v.key when the accessor walks that value whole.
	return v.whole && ev.Path[n].Key == v.key
}

// typedValues returns the values veraPDF 1.30 reads again through the typed
// objects of its model that the document structure reaches, each once for
// every time veraPDF reaches it (see [typedValue]):
//
//   - GFPDDocument.getLang: the catalog's /Lang string, as a CosLang.
//   - GFPDOutline.getTitle: the /Title string of every outline item, as a
//     CosTextString. OutlinesHelper reaches the items from the /First of the
//     outline dictionary along /First and /Next, each once, and never an item
//     only /Last or /Prev name.
//   - GFPDStructTreeRoot.getRoleMapNames: every key of the /RoleMap whose value
//     is a name, and that value, as CosUnicodeNames.
//   - GFPDStructElem: the /S name as a CosUnicodeName, and the /Lang,
//     /ActualText and /Alt strings as a CosLang, a CosActualText and a CosAlt,
//     of every structure element TaggedPDFHelper reaches from the /K of the
//     structure tree root: a dictionary with no /Type or /Type /StructElem,
//     reached once when an indirect object.
//   - GFPDAnnot.getLang and GFPDFormField.getLang: the /Lang of the
//     structure element an annotation or a form field names by its
//     /StructParent, as a CosLang; see [structParentLangValues].
//   - GFPDPage.getMediaBox, getCropBox, getBleedBox, getTrimBox and
//     getArtBox: each box of every page, as a CosBBox, a CosArray whose
//     elements it walks again. The media and crop boxes are inherited from
//     the nearest node of the page tree that writes them, PDPage stopping at
//     the first node that does even when it holds no array.
//   - GFCosDocument.getEmbeddedFiles: every dictionary the /EmbeddedFiles name
//     tree holds, as a CosFileSpecification, a CosDict it walks again whole.
//
// [checkNumericLimits] judges them against the limits and
// [checkFormatViolations] restates the hex string findings in their strings.
// More accessors are counted where the object they belong to is reached:
// GFCosStream.getFilters by [streamFilterLimits], the resourcesNames of
// GFPDResources by [resourceNameLimits], GFPDFont.getBaseFont by
// [fontNameLimits], the colorant names of a Separation or a DeviceN colour
// space by [colorantNameReads], and the /RI and /BM of a graphics state and
// the /Intent of an image by [operatorNameLimits] and [checkOperandLimits].
//
// Measured with veraPDF 1.30 at PDF/A-1b, PDF/A-2b and PDF/A-4, a hex string
// with an odd digit count or a non-hex character, a string past the length
// limit and a name past its length limit fail their rule once more at each of
// these positions, written directly or as an indirect object, and an out of
// range real or integer written directly in a box once more for every page
// the box applies to: a real past the PDF/A-1 limit in a /MediaBox of the
// root of the page tree fails ISO 19005-1, 6.1.12 t2 three times for two
// pages. An element of a box written as an indirect object fails once, an
// outline /Title only /Last reaches, the /Title of the outline dictionary, a
// structure element's /E and the /Alt of a marked-content reference once.
//
// Deliberate divergence: a hex string finding (ISO 19005-1/2/3, 6.1.6 t1 and
// t2, ISO 19005-4, 6.1.5 t1 and t2) in a value written directly in an array,
// a structure element in a /K array or a file specification in a /Names
// array, is not restated. The parser records which dictionary entries enclose
// a string, not which array element, so the finding cannot be told from one
// in an element veraPDF does not reach, a marked-content reference beside a
// structure element among them. Measured with veraPDF 1.30 at PDF/A-2b, such
// a string fails the test once more there. The limits are judged on the value
// itself and are restated there.
//
// The walks are bounded: the outline and the structure tree visit each
// indirect object once, the structure tree and the inheritance of a box stop
// at [maxStructTreeWalkDepth] and [maxPageTreeDepth], and the name tree walk
// is bounded as [document.WalkNameTree] states.
//
// # Corpus evidence
//
//   - verapdf PDF_A-1b__veraPDF_test_suite_6-1-12-t03-fail-a.pdf and
//     PDF_A-2b__veraPDF_test_suite_6-1-13-t03-fail-d.pdf: an outline item's
//     /Title is past the string limit, and veraPDF fails ISO 19005-1, 6.1.12
//     t3 and ISO 19005-2, 6.1.13 t3 twice, where litho failed them once.
//   - All corpora: judging the values again moves no other count at PDF/A-1b
//     and PDF/A-2b, veraPDF files at their own profile, and the verapdf and
//     litho files now match their goldens on the hex string tests and on
//     every string, name, integer and real limit.
func typedValues(ctx context.Context, vctx *validationCtx) []typedValue {
	doc, catalog := vctx.doc, vctx.catalog
	catalogSite := trailerCatalogSite(ctx, doc)

	var values []typedValue
	if v, ok := entryString(ctx, doc, catalogSite, catalog, "Lang", "catalog /Lang"); ok {
		v.textString = true
		values = append(values, v)
	}
	values = append(values, outlineTitleValues(ctx, doc, catalog)...)
	values = append(values, structureValues(ctx, vctx, catalogSite)...)
	values = append(values, pageBoxValues(ctx, vctx)...)
	values = append(values, embeddedFileValues(ctx, doc, catalog)...)
	return values
}

// trailerCatalogSite returns the site of the catalog: the object the
// trailer's /Root names when [document.ResolveCatalog] takes it, a dictionary
// whose /Pages resolves to one, and the zero site otherwise. A catalog
// recovered by scanning the file is left unlocated rather than scanned for
// again, since the scan runs the raw-byte augmentation the check groups must
// not run (see [runCheckGroups]).
func trailerCatalogSite(ctx context.Context, doc *document.Document) propertyListSite {
	ref, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return propertyListSite{}
	}
	catalog, err := document.ResolveDict(ctx, doc, ref)
	if err != nil || catalog == nil {
		return propertyListSite{}
	}
	if pages, err := document.ResolveDict(ctx, doc, catalog["Pages"]); err != nil || pages == nil {
		return propertyListSite{}
	}
	return propertyListSite{objNum: ref.ObjectNum}
}

// outlineTitleValues returns the /Title of every outline item veraPDF
// reaches; see [typedValues]. ISO 32000-1, Table 153 has /First and /Next
// name an item by an indirect reference, and an item written directly is not
// followed.
func outlineTitleValues(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []typedValue {
	outlines, err := document.ResolveDict(ctx, doc, catalog["Outlines"])
	if err != nil || outlines == nil {
		return nil
	}
	var values []typedValue
	seen := make(map[int]bool)
	pending := []ast.PdfObject{outlines["First"]}
	for len(pending) > 0 {
		if ctx.Err() != nil {
			return values
		}
		ref, ok := pending[len(pending)-1].(ast.PdfIndirectRef)
		pending = pending[:len(pending)-1]
		if !ok || seen[ref.ObjectNum] {
			continue
		}
		item, err := document.ResolveDict(ctx, doc, ref)
		if err != nil || item == nil {
			continue
		}
		seen[ref.ObjectNum] = true
		if v, ok := entryString(ctx, doc, propertyListSite{objNum: ref.ObjectNum}, item, "Title", "outline item /Title"); ok {
			v.textString = true
			values = append(values, v)
		}
		pending = append(pending, item["Next"], item["First"])
	}
	return values
}

// structureValues returns the names of the /RoleMap and the /S, /Lang,
// /ActualText and /Alt of every structure element veraPDF reaches; see
// [typedValues].
func structureValues(ctx context.Context, vctx *validationCtx, catalogSite propertyListSite) []typedValue {
	doc := vctx.doc
	rootRaw := vctx.catalog["StructTreeRoot"]
	root, err := document.ResolveDict(ctx, doc, rootRaw)
	if err != nil || root == nil {
		return nil
	}
	rootSite := catalogSite.child("StructTreeRoot", rootRaw)

	var values []typedValue
	if roleMap, err := document.ResolveDict(ctx, doc, root["RoleMap"]); err == nil {
		for _, key := range slices.Sorted(maps.Keys(roleMap)) {
			target, err := document.ResolveName(ctx, doc, roleMap[key])
			if err != nil || target == "" {
				continue
			}
			values = append(values,
				typedValue{value: key, unicodeName: true, where: "structure tree /RoleMap key"},
				typedValue{value: target, unicodeName: true, where: "structure tree /RoleMap value"})
		}
	}

	w := structValueWalk{doc: doc, seen: make(map[int]bool)}
	w.children(ctx, rootSite, root, 0)
	values = append(values, w.values...)
	return append(values, structParentLangValues(ctx, vctx, root)...)
}

// structParentLangValues returns the /Lang strings veraPDF reads again for
// the annotations and form fields it reaches (GFPDAnnot.getLang and
// GFPDFormField.getLang): the /Lang of the structure element the /ParentTree
// of root maps the /StructParent of each to (ISO 32000-1, 14.7.4.4), as a
// CosLang. veraPDF reaches the dictionaries of every page's /Annots, and the
// form fields of the /Fields of the /AcroForm and, below them, every /Kids
// entry holding a /T (PDFormField.getChildFormFields); each once when written
// as an indirect object. The field descent stops at [maxFieldTreeDepth].
func structParentLangValues(ctx context.Context, vctx *validationCtx, root ast.PdfDict) []typedValue {
	doc := vctx.doc
	parents := make(map[int]ast.PdfObject)
	if tree, ok := root["ParentTree"]; ok {
		err := document.WalkNumberTree(ctx, doc, tree, func(int) {}, func(key int, value ast.PdfObject) {
			if _, seen := parents[key]; !seen {
				parents[key] = value
			}
		})
		if err != nil {
			doc.Logger.Debug("compliance.file_structure: cannot walk /ParentTree", "error", err)
		}
	}
	if len(parents) == 0 {
		return nil
	}

	var values []typedValue
	seen := make(map[int]bool)
	// reach reads the /Lang the holder raw names, unless it was reached
	// already, and returns the holder.
	reach := func(raw ast.PdfObject, where string) (ast.PdfDict, bool) {
		if ref, ok := raw.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				return nil, false
			}
			seen[ref.ObjectNum] = true
		}
		holder, err := document.ResolveDict(ctx, doc, raw)
		if err != nil || holder == nil {
			return nil, false
		}
		structParent, err := document.ResolveObject(ctx, doc, holder["StructParent"])
		index, isInt := structParent.(ast.PdfInt)
		if err != nil || !isInt {
			return holder, true
		}
		elemRaw, ok := parents[int(index)]
		if !ok {
			return holder, true
		}
		elem, err := document.ResolveDict(ctx, doc, elemRaw)
		if err != nil || elem == nil {
			return holder, true
		}
		var site propertyListSite
		if ref, ok := elemRaw.(ast.PdfIndirectRef); ok {
			site = propertyListSite{objNum: ref.ObjectNum}
		}
		if v, ok := entryString(ctx, doc, site, elem, "Lang", where); ok {
			v.textString = true
			values = append(values, v)
		}
		return holder, true
	}

	for i := range vctx.pages {
		if ctx.Err() != nil {
			return values
		}
		annots, _ := document.ResolveArray(ctx, doc, vctx.pages[i].dict["Annots"]) // Best-effort: an unreadable /Annots names no annotation.
		for _, annot := range annots {
			reach(annot, "structure element /Lang of an annotation")
		}
	}

	clear(seen)
	acroForm, _ := document.ResolveDict(ctx, doc, vctx.catalog["AcroForm"]) // Best-effort: an unreadable /AcroForm holds no field.
	fields, _ := document.ResolveArray(ctx, doc, acroForm["Fields"])        // Best-effort: unreadable /Fields hold no field.
	var visit func(raw ast.PdfObject, depth int)
	visit = func(raw ast.PdfObject, depth int) {
		if depth > maxFieldTreeDepth || ctx.Err() != nil {
			return
		}
		field, ok := reach(raw, "structure element /Lang of a form field")
		if !ok {
			return
		}
		kids, _ := document.ResolveArray(ctx, doc, field["Kids"]) // Best-effort: unreadable /Kids hold no field.
		for _, kid := range kids {
			if kidDict, err := document.ResolveDict(ctx, doc, kid); err == nil && knownKey(kidDict, "T") {
				visit(kid, depth+1)
			}
		}
	}
	for _, field := range fields {
		visit(field, 0)
	}
	return values
}

// structValueWalk collects the values [structureValues] returns from the
// structure elements below one node.
type structValueWalk struct {
	doc    *document.Document
	seen   map[int]bool
	values []typedValue
}

// children visits the structure elements the /K of node, whose site is site,
// names at depth levels below the root: the dictionary /K holds, or each
// dictionary of the array it holds, that has no /Type or /Type /StructElem
// (TaggedPDFHelper.getStructChildren).
func (w *structValueWalk) children(ctx context.Context, site propertyListSite, node ast.PdfDict, depth int) {
	if depth > maxStructTreeWalkDepth || ctx.Err() != nil {
		return
	}
	raw := node["K"]
	k, err := document.ResolveObject(ctx, w.doc, raw)
	if err != nil {
		return
	}
	switch kids := k.(type) {
	case ast.PdfDict:
		w.element(ctx, site.child("K", raw), raw, kids, depth)
	case ast.PdfArray:
		for _, kid := range kids {
			elem, err := document.ResolveDict(ctx, w.doc, kid)
			if err != nil || elem == nil {
				continue
			}
			// A dictionary written in the array has no site the parser
			// records; see [typedValues].
			var elemSite propertyListSite
			if ref, ok := kid.(ast.PdfIndirectRef); ok {
				elemSite = propertyListSite{objNum: ref.ObjectNum}
			}
			w.element(ctx, elemSite, kid, elem, depth)
		}
	}
}

// element collects the values of the structure element elem, written as raw
// at site, and visits its children, unless elem is no structure element or an
// indirect object already visited.
func (w *structValueWalk) element(ctx context.Context, site propertyListSite, raw ast.PdfObject, elem ast.PdfDict, depth int) {
	if typ, _ := document.ResolveName(ctx, w.doc, elem["Type"]); typ != "" && typ != "StructElem" {
		return
	}
	if ref, ok := raw.(ast.PdfIndirectRef); ok {
		if w.seen[ref.ObjectNum] {
			return
		}
		w.seen[ref.ObjectNum] = true
	}
	if v, ok := entryValue(ctx, w.doc, site, elem, "S", "structure element /S"); ok {
		if _, isName := v.value.(ast.PdfName); isName {
			v.unicodeName = true
			w.values = append(w.values, v)
		}
	}
	for _, key := range []ast.PdfName{"Lang", "ActualText", "Alt"} {
		if v, ok := entryString(ctx, w.doc, site, elem, key, "structure element /"+string(key)); ok {
			// Only the /Lang is a CosTextString: veraPDF's CosActualText and
			// CosAlt derive from CosString directly.
			v.textString = key == "Lang"
			w.values = append(w.values, v)
		}
	}
	w.children(ctx, site, elem, depth+1)
}

// pageBoxes lists the page boundaries of ISO 32000-1, 14.11.2, which veraPDF
// reads as CosBBoxes, and whether a page inherits the box from the page tree
// (ISO 32000-1, Table 30).
//
//nolint:gochecknoglobals // static lookup table.
var pageBoxes = []struct {
	key       ast.PdfName
	inherited bool
}{
	{"MediaBox", true}, {"CropBox", true}, {"BleedBox", false}, {"TrimBox", false}, {"ArtBox", false},
}

// pageBoxValues returns the boxes of every page veraPDF reaches; see
// [typedValues].
func pageBoxValues(ctx context.Context, vctx *validationCtx) []typedValue {
	var values []typedValue
	for i := range vctx.pages {
		if ctx.Err() != nil {
			return values
		}
		p := &vctx.pages[i]
		for _, box := range pageBoxes {
			node, site := p.dict, propertyListSite{objNum: p.objNum}
			if box.inherited {
				node, site, _ = p.inheritedHolder(box.key)
			}
			if node == nil {
				continue
			}
			v, ok := entryValue(ctx, vctx.doc, site, node, box.key, "page "+strconv.Itoa(i+1)+" /"+string(box.key))
			if _, isArray := v.value.(ast.PdfArray); !ok || !isArray {
				continue
			}
			v.whole = true
			values = append(values, v)
		}
	}
	return values
}

// pageResources returns the resource dictionary veraPDF reads for page p, the
// page's own or else its nearest ancestor's (PDPage.getResources, ISO
// 32000-1, Table 30; see [pageObject.inheritedHolder]), and its object number
// when written as an indirect reference, 0 otherwise.
func pageResources(p *pageCtx) (ast.PdfDict, int) {
	node, _, _ := p.inheritedHolder("Resources")
	if node == nil || p.resources == nil {
		return nil, 0
	}
	if ref, ok := node["Resources"].(ast.PdfIndirectRef); ok {
		return p.resources, ref.ObjectNum
	}
	return p.resources, 0
}

// embeddedFileValues returns the dictionaries of the /EmbeddedFiles name tree
// of the catalog's /Names; see [typedValues].
func embeddedFileValues(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []typedValue {
	names, err := document.ResolveDict(ctx, doc, catalog["Names"])
	if err != nil || names == nil {
		return nil
	}
	tree, ok := names["EmbeddedFiles"]
	if !ok {
		return nil
	}
	var values []typedValue
	err = document.WalkNameTree(ctx, doc, tree, func(_ string, raw ast.PdfObject) {
		spec, err := document.ResolveDict(ctx, doc, raw)
		if err != nil || spec == nil {
			return
		}
		v := typedValue{value: spec, whole: true, where: "embedded file specification"}
		if ref, isRef := raw.(ast.PdfIndirectRef); isRef {
			v.site = propertyListSite{objNum: ref.ObjectNum}
		}
		values = append(values, v)
	})
	if err != nil {
		doc.Logger.Debug("compliance.file_structure: cannot walk /EmbeddedFiles", "error", err)
	}
	return values
}

// nameTooLong returns the violation of the name length limit, ISO 19005-1,
// 6.1.12 t4 and ISO 19005-2/3, 6.1.13 t4, for a name veraPDF gives length
// bytes, or nil when it keeps within the limit or the profile states no such
// limit (ISO 19005-4 does not).
func nameTooLong(length int, where, profile string) []Violation {
	rule := nameLengthRule(profile)
	if length <= maxNameBytes || rule.Specification == "" {
		return nil
	}
	return []Violation{{
		Rule:    rule,
		Context: fmt.Sprintf("%s: name length %d exceeds %d", where, length, maxNameBytes),
	}}
}

// streamFilterLimits judges the /Filter names of obj, when it is a stream,
// against the name length limit once more: veraPDF reads each as a CosFilter
// of the stream (GFCosStream.getFilters), a name of its own. Measured with
// veraPDF 1.30 at PDF/A-1b and PDF/A-2b, a stream whose /Filter is a name of
// 130 bytes fails ISO 19005-1, 6.1.12 t4 and ISO 19005-2, 6.1.13 t4 twice.
func streamFilterLimits(ctx context.Context, doc *document.Document, obj ast.PdfObject, where, profile string) []Violation {
	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return nil
	}
	filter, err := document.ResolveObject(ctx, doc, stream.Dict["Filter"])
	if err != nil {
		return nil
	}
	var results []Violation
	judge := func(obj ast.PdfObject) {
		if name, err := document.ResolveName(ctx, doc, obj); err == nil && name != "" {
			results = append(results, nameTooLong(len(name), where+" /Filter, read again as a filter", profile)...)
		}
	}
	switch f := filter.(type) {
	case ast.PdfName:
		judge(f)
	case ast.PdfArray:
		for _, elem := range f {
			judge(elem)
		}
	}
	return results
}

// nameNotUTF8 returns the violation of the rule that a font name, a colorant
// name or a structure type name be valid UTF-8, ISO 19005-2/3, 6.1.8 t1 and
// ISO 19005-4, 6.1.7 t1, for a name veraPDF reads as a CosUnicodeName, or nil
// when its bytes are valid UTF-8 or the profile states no such rule (ISO
// 19005-1 does not).
//
// name holds the bytes the name stands for: the lexer already expanded its #XX
// escapes (ISO 32000-1, 7.3.5), and the rule judges those bytes. Expanding them
// a second time misread a name whose bytes spell an escape: /Foo#23C3 stands
// for the valid "Foo#C3", and read as the invalid "Foo\xC3". veraPDF 1.30
// passes ISO 19005-2, 6.1.8 t1 and ISO 19005-4, 6.1.7 t1 on it and fails
// /Foo#C3.
func nameNotUTF8(name ast.PdfName, where, profile string) []Violation {
	rule := utf8NameRule(profile)
	if utf8.ValidString(string(name)) || rule.Specification == "" {
		return nil
	}
	return []Violation{{
		Rule:    rule,
		Context: fmt.Sprintf("%s: name %q is not valid UTF-8", where, string(name)),
	}}
}

// resourceNameLimits judges the names veraPDF reads again from the resource
// dictionary res, once for each time it reaches it
// (GFPDResources.getResourcesNames), against the name length limit and, each
// being a CosUnicodeName, against the UTF-8 rule ([nameNotUTF8]): the
// /BaseFont of every font of /Font that PDFontFactory builds, a dictionary
// whose /Subtype is Type1, MMType1, TrueType, Type3 or Type0, whatever the
// content draws with it, and the colorant names of every Separation and
// DeviceN colour space of /ColorSpace (ISO 32000-1, 8.6.6.4 and 8.6.6.5), each
// read with the bytes it is written with. where names the resources in the
// context of a violation.
//
// veraPDF reaches the resources of every page, inherited from the page tree
// (ISO 32000-1, Table 30), and the own resources of every content stream it
// draws, and reaches a dictionary written as an indirect object once for the
// whole document. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, a
// /BaseFont of 130 bytes fails ISO 19005-1, 6.1.12 t4 and ISO 19005-2, 6.1.13
// t4 once more for resources two pages share as an indirect object, a form
// draws with the page's, or a page with no content holds, and once more for
// each page when two pages write the resources directly or inherit them
// from a node of the page tree, and for each glyph description of a Type3
// font whose own /Resources name the font.
//
// Measured with veraPDF 1.30 at PDF/A-2b and PDF/A-4 on a colorant name that
// is not valid UTF-8, /Bad#E9: a Separation of the page's /ColorSpace that no
// operator selects fails ISO 19005-2, 6.1.8 t1 and ISO 19005-4, 6.1.7 t1 once,
// twice when two entries hold it, when two pages write their resources
// directly or inherit them from the page tree, and once more for the resources
// of a form the page draws, of a painted tiling pattern, of an annotation's
// appearance stream and of a Type 3 glyph description, each once for a form
// drawn twice or two forms sharing one indirect /Resources; a /BaseFont that
// is not valid UTF-8 fails them once for each such dictionary whose /Font
// holds the font, whether or not the content shows it. A DeviceN's /Colorants
// dictionary is not read here.
func resourceNameLimits(ctx context.Context, doc *document.Document, res ast.PdfDict, where, profile string) []Violation {
	var results []Violation
	judge := func(name ast.PdfName, at string) {
		results = append(results, nameTooLong(len(name), at, profile)...)
		results = append(results, nameNotUTF8(name, at, profile)...)
	}
	fonts, _ := document.ResolveDict(ctx, doc, res["Font"]) // Best-effort: an unreadable /Font names no font.
	for _, key := range slices.Sorted(maps.Keys(fonts)) {
		font, err := document.ResolveDict(ctx, doc, fonts[key])
		if err != nil || font == nil {
			continue
		}
		if subtype, _ := document.ResolveName(ctx, doc, font["Subtype"]); !pdFontSubtypes[string(subtype)] {
			continue
		}
		if name, err := document.ResolveName(ctx, doc, font["BaseFont"]); err == nil {
			judge(name, where+" /Font /"+string(key)+" /BaseFont, read again")
		}
	}
	spaces, _ := document.ResolveDict(ctx, doc, res["ColorSpace"]) // Best-effort: an unreadable /ColorSpace names no space.
	for _, key := range slices.Sorted(maps.Keys(spaces)) {
		space, err := document.ResolveArray(ctx, doc, spaces[key])
		if err != nil || len(space) < 2 {
			continue
		}
		at := where + " /ColorSpace /" + string(key) + " colorant, read again"
		switch family, _ := document.ResolveName(ctx, doc, space[0]); family {
		case "Separation":
			if name, err := document.ResolveName(ctx, doc, space[1]); err == nil {
				judge(name, at)
			}
		case "DeviceN":
			names, _ := document.ResolveArray(ctx, doc, space[1]) // Best-effort: an unreadable array names no colorant.
			for _, obj := range names {
				if name, err := document.ResolveName(ctx, doc, obj); err == nil {
					judge(name, at)
				}
			}
		}
	}
	return results
}

// maxColorantNamesRead caps how many colorant names past the length limit one
// colour space adds to the failures of each link to it (see
// [colorantNameReads]). Anti-DoS guard, not spec-mandated: each failure is
// one link times one long name, so a crafted stream repeating one operator
// millions of times over a DeviceN naming thousands of long colorants would
// multiply the two, where the cap keeps the failures a constant multiple of
// the links [colorLinkWalk] follows. It equals the most colorants ISO
// 32000-1, Annex C, Table C.1 lets a DeviceN name, [maxDeviceN2], so every
// Separation, and every DeviceN within that limit, is judged whole.
const maxColorantNamesRead = maxDeviceN2

// colorantNameReads judges the colorant names veraPDF reads again at every
// link [colorLinkWalk] follows to a Separation or a DeviceN colour space, as
// the colorant name of its PDSeparation and the colorant names of its
// PDDeviceN (veraPDF-validation 1.30, GFPDSeparation.getColorantName and
// GFPDDeviceN.getColorantNames), each a CosUnicodeName of its own, against
// the name length limit and the UTF-8 rule ([nameNotUTF8]): a colour space
// written directly wherever a link reaches it, an indirect one once for the
// whole document, and the Separations of a DeviceN /Colorants dictionary as
// the links reach them. Each name counts the bytes it is written with, as the
// object sweep counts them, whether written directly or as an indirect object.
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, a colorant name of 130
// bytes fails ISO 19005-1, 6.1.12 t4 and ISO 19005-2, 6.1.13 t4 once more for
// each of cs, scn and f reaching a direct Separation or DeviceN, or a
// Separation of a direct DeviceN's /Colorants, once more for all of them
// reaching an indirect Separation, and, reaching an /Indexed space over a
// direct Separation, once more for cs and for f; once more for an image
// painted twice and for each sh painting a direct shading; and not for a cs
// past the d1 of a Type 3 glyph.
//
// Measured with veraPDF 1.30 at PDF/A-2b and PDF/A-4 on a colorant name that
// is not valid UTF-8, /Bad#E9, beside the one read of the page's resources
// ([resourceNameLimits]): a direct Separation that cs selects fails ISO
// 19005-2, 6.1.8 t1 and ISO 19005-4, 6.1.7 t1 once more, then once more for
// each scn, f, S after SCN, B, and text shown in rendering mode 0 reaching it,
// an indirect one once more for all of them, and a direct /Indexed space over
// it once more for each of cs, scn and f; an image in it painted twice once
// more, and a direct shading in it once more for each sh; a direct DeviceN
// naming two such colorants twice more for each link, and a /Colorants
// Separation naming one of them once more.
//
// failures remembers, by the colour space array, the failures one link to it
// adds, at most [maxColorantNamesRead] for each rule, so a link costs no more
// than the failures it adds.
type colorantNameReads struct {
	profile  string
	failures map[*ast.PdfObject][]Violation
}

// newColorantNameReads returns the reads of a validation run under profile.
func newColorantNameReads(profile string) *colorantNameReads {
	return &colorantNameReads{profile: profile, failures: make(map[*ast.PdfObject][]Violation)}
}

// judge returns the failures of the colorant names of n, a Separation or a
// DeviceN node, read again at one link.
func (r *colorantNameReads) judge(ctx context.Context, doc *document.Document, n colorNode) []Violation {
	if len(n.arr) < 2 || (nameLengthRule(r.profile).Specification == "" && utf8NameRule(r.profile).Specification == "") {
		return nil
	}
	key := &n.arr[0]
	failures, ok := r.failures[key]
	if !ok {
		where := n.kind + " colorant name, read again at each link to its colour space"
		long, invalid := colorantNameFaults(ctx, doc, n)
		for _, length := range long {
			failures = append(failures, nameTooLong(length, where, r.profile)...)
		}
		for _, name := range invalid {
			failures = append(failures, nameNotUTF8(name, where, r.profile)...)
		}
		r.failures[key] = failures
	}
	return failures
}

// colorantNameFaults returns, of the colorant names of n, the lengths of those
// past the name length limit and the names that are not valid UTF-8, the
// first [maxColorantNamesRead] of each: the name a Separation array gives, or
// each name of the array a DeviceN array gives (ISO 32000-1, 8.6.6.4 and
// 8.6.6.5).
func colorantNameFaults(ctx context.Context, doc *document.Document, n colorNode) (long []int, invalid []ast.PdfName) {
	names := []ast.PdfObject{n.arr[1]}
	if n.kind == "DeviceN" {
		arr, err := document.ResolveArray(ctx, doc, n.arr[1])
		if err != nil {
			return nil, nil
		}
		names = arr
	}
	for _, obj := range names {
		if len(long) == maxColorantNamesRead && len(invalid) == maxColorantNamesRead {
			break
		}
		name, ok := resolveNameObject(ctx, doc, obj)
		if !ok {
			continue
		}
		if len(name) > maxNameBytes && len(long) < maxColorantNamesRead {
			long = append(long, len(name))
		}
		if !utf8.ValidString(string(name)) && len(invalid) < maxColorantNamesRead {
			invalid = append(invalid, name)
		}
	}
	return long, invalid
}

// operatorNameLimits judges against the name length limit the names veraPDF
// reads again through the objects the gs and Do operators of one content
// stream reach, ops, whose names resolve against the resources scopeOf
// returns, read only when ops hold such an operator:
//
//   - The /RI and /BM of the graphics state every gs builds, as the
//     CosRenderingIntent and the CosBM of its PDExtGState (veraPDF-validation
//     1.30, GFPDExtGState.getRI and getlinkBM; see [cosBlendMode]): a state
//     written as an indirect object once for the whole document, and one
//     written directly once for every gs, as [blendModeTimes] counts. A state
//     no gs sets is never built.
//   - The /Intent of every image a Do paints, as the CosRenderingIntent of its
//     PDXImage (GFPDXImage.getIntent), once for the whole document.
//
// judged records the indirect graphics states and the images already read,
// and where names the stream in the context of a violation. The work is one
// pass over ops and one read per name they give, whatever the count of gs
// operators naming it.
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, an /RI or a /BM of 130
// bytes in a state written directly that two gs set fails ISO 19005-1, 6.1.12
// t4 and ISO 19005-2, 6.1.13 t4 twice more, once more in an indirect state
// two gs set or a direct state one gs sets in a form painted twice, and not
// in a state no gs sets; an /Intent of 130 bytes in an image painted twice
// fails it once more.
func operatorNameLimits(ctx context.Context, doc *document.Document, ops []content.Operation, scopeOf func() ast.PdfDict, judged map[int]bool, where, profile string) []Violation {
	if len(ops) == 0 || nameLengthRule(profile).Specification == "" {
		return nil
	}
	drawn := collectDrawnResources(ops)
	if len(drawn.extGStates) == 0 && len(drawn.xobjects) == 0 {
		return nil
	}
	scope := scopeOf()
	var results []Violation
	if len(drawn.extGStates) > 0 {
		states, _ := document.ResolveDict(ctx, doc, scope["ExtGState"]) // Best-effort: an unreadable /ExtGState defines no state.
		for _, name := range slices.Sorted(maps.Keys(drawn.extGStates)) {
			gsObj, ok := states[ast.PdfName(name)]
			if !ok {
				continue
			}
			gs, err := document.ResolveDict(ctx, doc, gsObj)
			if err != nil || gs == nil {
				continue
			}
			var failures []Violation
			at := where + " ExtGState /" + name
			if ri, ok := resolveNameObject(ctx, doc, gs["RI"]); ok {
				failures = append(failures, nameTooLong(len(ri), at+" /RI, read again as its rendering intent", profile)...)
			}
			if bm, ok := cosBlendMode(ctx, doc, gs["BM"], PDFAPart(profile)); ok {
				failures = append(failures, nameTooLong(len(bm), at+" /BM, read again as its blend mode", profile)...)
			}
			times := blendModeTimes(gsObj, drawn.extGStates[name], judged)
			if len(failures) == 0 {
				continue
			}
			for range times {
				results = append(results, failures...)
			}
		}
	}
	if len(drawn.xobjects) > 0 {
		xobjects, _ := document.ResolveDict(ctx, doc, scope["XObject"]) // Best-effort: an unreadable /XObject defines no image.
		for _, name := range slices.Sorted(maps.Keys(drawn.xobjects)) {
			ref, ok := xobjects[ast.PdfName(name)].(ast.PdfIndirectRef)
			if !ok || judged[ref.ObjectNum] {
				continue
			}
			stream, err := document.ResolveStream(ctx, doc, ref)
			if err != nil {
				continue
			}
			if subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]); subtype != "Image" { // Best-effort: an unreadable /Subtype is no image.
				continue
			}
			results = append(results, imageIntentLimits(ctx, doc, ref, judged, where+" image /"+name, profile)...)
		}
	}
	return results
}

// imageIntentLimits judges against the name length limit the /Intent of the
// image, and of the images it holds, each once for the whole document:
// its /SMask and its /Mask when they are streams, and the /Image of every
// alternate its /Alternates lists (ISO 32000-1, 8.9.5, Table 89), which
// veraPDF reads as images of its model too (veraPDF-validation 1.30,
// GFPDXImage.getSMask, getMask and getAlternates). judged records the images
// already read, and where names the image in the context of a violation.
// Measured with veraPDF 1.30 at PDF/A-1b, an /Intent of 130 bytes in the
// /SMask of a painted image fails ISO 19005-1, 6.1.12 t4 once more.
func imageIntentLimits(ctx context.Context, doc *document.Document, image ast.PdfIndirectRef, judged map[int]bool, where, profile string) []Violation {
	var results []Violation
	pending := []ast.PdfObject{image}
	for len(pending) > 0 && ctx.Err() == nil {
		obj := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		ref, ok := obj.(ast.PdfIndirectRef)
		if !ok || judged[ref.ObjectNum] {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, ref)
		if err != nil {
			continue
		}
		judged[ref.ObjectNum] = true
		if intent, ok := resolveNameObject(ctx, doc, stream.Dict["Intent"]); ok {
			results = append(results, nameTooLong(len(intent), where+" /Intent, read again as its rendering intent", profile)...)
		}
		pending = append(pending, stream.Dict["SMask"], stream.Dict["Mask"])
		alternates, _ := document.ResolveArray(ctx, doc, stream.Dict["Alternates"]) // Best-effort: unreadable /Alternates hold no image.
		for _, alt := range alternates {
			if altDict, err := document.ResolveDict(ctx, doc, alt); err == nil && altDict != nil {
				pending = append(pending, altDict["Image"])
			}
		}
	}
	return results
}

// inlineIntentLimit judges against the name length limit the /Intent of the
// inline image whose dictionary is dict, which veraPDF reads again as the
// CosRenderingIntent of its PDInlineImage (veraPDF-validation 1.30,
// GFPDXImage.getIntent and PDInlineImage.getIntent) beside the dictionary the
// operand walk judges, for every inline image it reads (see
// [ColourState.ReadsInlineImage]). where names the content stream in the
// context of a violation. Measured with veraPDF 1.30 at PDF/A-1b and
// PDF/A-2b, an inline image of a page whose /Intent is a name of 130 bytes
// fails ISO 19005-1, 6.1.12 t4 and ISO 19005-2, 6.1.13 t4 twice.
func inlineIntentLimit(dict ast.PdfObject, where, profile string) []Violation {
	d, ok := dict.(ast.PdfDict)
	if !ok {
		return nil
	}
	intent, ok := d["Intent"].(ast.PdfName)
	if !ok {
		return nil
	}
	return nameTooLong(len(intent), where+" inline image /Intent, read again as its rendering intent", profile)
}

// pdFontSubtypes lists the /Subtype values of the font dictionaries veraPDF
// builds a font of its model for (PDFontFactory.getPDFont): the font types of
// ISO 32000-1, Table 110, but the CIDFonts, which only a Type0 font's
// /DescendantFonts names.
//
//nolint:gochecknoglobals // static lookup table.
var pdFontSubtypes = map[string]bool{
	"Type1": true, "MMType1": true, "TrueType": true, "Type3": true, "Type0": true,
}

// fontNameLimits judges against the name length limit the name veraPDF reads
// again from a font it reaches, as the BaseFont of its PDFont
// (GFPDFont.getBaseFont): the /Name of a Type3 font as written, and the
// /BaseFont of any other font decoded as UTF-8, whose length veraPDF counts
// in UTF-16 code units, as a Java string does. The font group calls it once
// for every font object veraPDF reaches through the text it shows, and once
// for the descendant CIDFont of a composite font. where names the font in
// the context of a violation.
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b: a /BaseFont of 130
// bytes fails ISO 19005-1, 6.1.12 t4 and ISO 19005-2, 6.1.13 t4 once more for
// a font a page, a form, an annotation appearance or a Type3 glyph shows text
// with, for a Type0 font and for its descendant, and not for a font no text
// is shown with; one of 140 bytes spelling 70 two-byte characters fails it
// only as a name of the file. A Type3 font's /Name of 130 bytes fails it once
// more.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-1-13-t04-fail-b.pdf: shows text
//     with a Type3 font whose /Name has 130 bytes, and veraPDF fails ISO
//     19005-2, 6.1.13 t4 twice, where litho failed it once.
func fontNameLimits(ctx context.Context, doc *document.Document, font ast.PdfDict, where, profile string) []Violation {
	subtype, _ := document.ResolveName(ctx, doc, font["Subtype"]) // Best-effort: an unreadable /Subtype reads as no Type3.
	if subtype == "Type3" {
		name, err := document.ResolveName(ctx, doc, font["Name"])
		if err != nil {
			return nil
		}
		return nameTooLong(len(name), where+" /Name, read again as its font name", profile)
	}
	name, err := document.ResolveName(ctx, doc, font["BaseFont"])
	if err != nil {
		return nil
	}
	return nameTooLong(unicodeNameLength(name), where+" /BaseFont, read again as its font name", profile)
}

// unicodeNameLength returns the length veraPDF gives the name it builds from
// a font's /BaseFont (PDFont.getName): its bytes decoded as UTF-8, counted in
// UTF-16 code units, as a Java string counts them. A byte that is no UTF-8
// counts one unit, as Go decodes it; Java replaces a malformed sequence of
// up to three bytes with one, so the lengths part only on such a sequence.
func unicodeNameLength(name ast.PdfName) int {
	return len(utf16.Encode([]rune(string(name))))
}
