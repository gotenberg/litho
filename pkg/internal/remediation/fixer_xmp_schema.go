package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchXMPSchema matches XMP predefined-schema validation violations:
// PDF/A-2/3/4 clause 6.6.2.3.1 and PDF/A-1 clause 6.7.9. Each reports a
// specific XMP property that fails schema validation (wrong cardinality,
// wrong value type, struct with plain text value, unknown property
// name). Other 6.6.2 / 6.7.X violations are handled by the general
// fixXMP pass.
func matchXMPSchema(v compliance.Violation) bool {
	if !strings.HasPrefix(v.Rule.Clause, "6.6.2.3.1") &&
		!strings.HasPrefix(v.Rule.Clause, "6.7.9") {
		return false
	}
	_, _, ok := parseXMPSchemaTarget(v.Context)
	return ok
}

// fixXMPSchema strips XMP properties flagged as schema-noncompliant.
// Dropping the offending property is the only universally safe repair:
// rewriting "observed=Simple expected=Bag" would require synthesizing a
// wrapper array, and rewriting "value X does not match expected type"
// would require fabricating plausible content. Both risk destroying user
// intent. Removal leaves the rest of the XMP intact.
//
// A violation judges the property as the validator read it, in the source
// packet. A property an earlier fixer has rewritten since is no longer the
// value it judged, and is kept: [fixXMP] runs first and writes dc:title,
// dc:creator and the other analogues of the /Info entries as the value types
// the XMP Specification defines for them, which is what ISO 19005-1, 6.7.9 and
// ISO 19005-2/3, 6.6.2.3.1 ask of a property, while ISO 19005-1, 6.7.3
// requires the property to be there, equivalent to its /Info entry. Stripping
// the rewritten value undid that sync, so the output failed 6.7.3 again and
// only a second remediation round restored it.
//
// A property an earlier fixer has only declared since, in an extension schema,
// still holds the value the validator judged. It is kept when the declaration
// answers ISO 19005-1, 6.7.9 and ISO 19005-2/3, 6.6.2.3.1 for it, which it
// does only when the value matches the value type it declares
// ([compliance.XMPExtensionPropertyChecker]), and stripped otherwise. [fixXMP]
// declares a structure, or an array of them, with a type holding their fields,
// where it used to declare an xmpTPg:Fonts of stFnt structures "bag Text",
// which veraPDF fails under 6.7.9 t3.
//
// A property a component metadata stream holds is stripped from that stream
// (see [stripComponentXMPSchema]). Its violation names the stream as
// "object N: ", which was read as part of the namespace URI: the fixer
// claimed the violation, searched the catalog packet for a namespace no
// packet declares, and stripped nothing.
//
// # Corpus evidence
//
//   - govdocs: 17 of 90 sampled files took that second round at PDF/A-1b,
//     default and aggressive alike, and one round now repairs them to the same
//     residual. thread_300_300116 carries its dc:creator as an rdf:Bag, where
//     the XMP Specification defines an ordered array.
//   - pdfcpu pkg_testdata__T4.pdf and litho docsync_1b.pdf: the same at
//     PDF/A-1b, with dc:title, dc:creator and dc:description as simple text.
//   - govdocs thread_000_000365, thread_300_300116 and thread_400_400724: took
//     the second round at PDF/A-2b too. That part has no 6.7.3 rule, but a
//     round run for another residual synced the stripped properties back.
//   - All corpora: over the PDF/A-1b, PDF/A-2b and PDF/UA-1 remediations,
//     default and aggressive, of 472 sampled files, no residual violation
//     moves, and 18 more PDF/A-2b outputs, 14 of them govdocs, now keep the
//     synced properties no later round restored. veraPDF 1.30 reports the
//     same failed rules on every PDF/A output that changes.
//   - All corpora: 65 files carry a component packet with a 6.6.2.3.1
//     violation at PDF/A-2b, 36 govdocs, 24 pdfjs and 5 pdfcpu, and so does
//     litho pagegroup_2b.pdf. Of 517 sampled files, those 66 among them, the
//     PDF/A-2b remediation of 26 left 148 such violations, default and
//     aggressive alike, and now leaves none: 15 of those outputs are now fully
//     fixed.
//   - verapdf PDF_A-1b__veraPDF_test_suite_6-7-2-t03-fail-i.pdf: xmp-metadata
//     declared its xmpTPg:Fonts, a bag of stFnt structures, "bag Text", which
//     veraPDF 1.30.2 fails under 6.7.9 t3, so the strip was the one safe
//     repair. It now declares a bag of a type holding the stFnt fields, and
//     the PDF/A-1b output keeps the fonts and passes veraPDF, as does
//     PDF_A-2b__veraPDF_test_suite_6-6-2-3-1-t03-pass-b.pdf with its xmpDM
//     property.
//   - All corpora: of the 1394 files with a 6.7.9 t2 finding at PDF/A-1b and
//     the 1199 with a 6.6.2.3.1 t1 finding at PDF/A-2b, keeping a property
//     declared with the type it holds changes 786 and 595 outputs, default
//     and aggressive alike, and moves no residual violation. veraPDF 1.30.2
//     reports the same failed rules as on the stripped output on each of 529
//     sampled default PDF/A-1b outputs and 327 PDF/A-2b ones, every
//     non-govdocs one among them, and on the 227 non-govdocs aggressive
//     PDF/A-2b ones.
//   - pdfcpu pkg_testdata__RA_CI.pdf: a component packet declares
//     xmpMM:Manifest and xmpMM:Ingredients arrays of types it defines while
//     the catalog packet extends xmpMM too, which veraPDF fails under
//     6.6.2.3.1 t2, the component's array items resolving through the
//     catalog's entry. Kept, the two failed the output; they are stripped, and
//     xmpTPg:SwatchGroups beside them is kept.
//   - All corpora: over the PDF/A-1b, PDF/A-2b and PDF/UA-1 remediations,
//     default and aggressive, of those 517 files, and the PDF/A-1a and
//     PDF/A-1b remediations of the 2601 verapdf files, no residual violation
//     rises and no fully fixed output is lost. veraPDF 1.30.2 reports the same
//     failed rules on every PDF/A-1a and PDF/A-1b output that changes, and the
//     same or fewer on every PDF/A-2b one: 24 files fail 6.6.2.3.1 fewer
//     times in each mode, and 13 of them now pass.
func fixXMPSchema(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, violations []compliance.Violation, profile string) ([]string, error) {
	targets := xmpSchemaTargets(violations)
	part := compliance.PDFAPart(profile)

	var fixes []string
	if n := stripCatalogXMPSchema(ctx, alloc, doc, targets[0], part); n > 0 {
		fixes = append(fixes, fmt.Sprintf("stripped %d XMP schema-noncompliant propert(ies)", n))
	}
	// The component packets resolve their properties through the catalog
	// packet's extension schemas too, as it stands after the strip above.
	var mainPacket []byte
	for objNum := range targets {
		if objNum != 0 {
			mainPacket = liveCatalogXMP(ctx, alloc, doc)
			break
		}
	}

	stripped, streams := 0, 0
	for _, objNum := range slices.Sorted(maps.Keys(targets)) {
		if objNum == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if n := stripComponentXMPSchema(ctx, alloc, doc, objNum, targets[objNum], part, mainPacket); n > 0 {
			stripped += n
			streams++
		}
	}
	if stripped > 0 {
		fixes = append(fixes, fmt.Sprintf("stripped %d XMP schema-noncompliant propert(ies) from %d component metadata stream(s)", stripped, streams))
	}
	return fixes, nil
}

// xmpSchemaProperty names one XMP property by its namespace URI and local
// name.
type xmpSchemaProperty struct {
	nsURI, localName string
}

// xmpSchemaTargets groups the properties violations flag by the packet that
// holds them: the object number of a component metadata stream, or 0 for the
// catalog packet. Each packet lists a property once, in violation order.
func xmpSchemaTargets(violations []compliance.Violation) map[int][]xmpSchemaProperty {
	targets := make(map[int][]xmpSchemaProperty)
	seen := make(map[int]map[xmpSchemaProperty]bool)
	for _, v := range violations {
		objNum, prop, ok := parseXMPSchemaTarget(v.Context)
		if !ok {
			continue
		}
		if seen[objNum] == nil {
			seen[objNum] = make(map[xmpSchemaProperty]bool)
		}
		if seen[objNum][prop] {
			continue
		}
		seen[objNum][prop] = true
		targets[objNum] = append(targets[objNum], prop)
	}
	return targets
}

// stripCatalogXMPSchema strips props from the catalog packet and returns how
// many it stripped. A property its extension schemas now declare with the value
// type it holds, at the given PDF/A part, is kept.
func stripCatalogXMPSchema(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, props []xmpSchemaProperty, part int) int {
	if len(props) == 0 {
		return 0
	}
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return 0
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return 0
	}
	metaObj, ok := catalogDict["Metadata"]
	if !ok {
		return 0
	}
	var metaRef ast.PdfIndirectRef
	if ref, ok := metaObj.(ast.PdfIndirectRef); ok {
		metaRef = ref
	}
	// Prefer any patched stream already queued by an earlier fixer
	// (e.g. fixXMP's pdfaid updates). Falling back to the source
	// document would undo those edits when this fixer reallocates.
	stream, ok2 := resolveStreamFromAllocOrDoc(ctx, alloc, doc, metaRef, metaObj)
	if !ok2 {
		return 0
	}
	// Decode before searching: ast.PdfStream.Bytes returns the body before
	// filter decoding, so a /Metadata carrying a filter (ISO 32000-1, 14.3.2
	// permits one) was searched as compressed bytes, no property was ever
	// found, and the violation stayed residual. A stream queued by an earlier
	// fixer declares no filter, so decoding is a passthrough for it.
	xmp, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(xmp) == 0 {
		return 0
	}

	declared := compliance.XMPExtensionPropertyChecker(xmp, nil, part)
	valid := func(prop xmpSchemaProperty) bool { return declared(prop.nsURI, prop.localName) }
	out, stripped := stripJudgedXMPProperties(xmp, judgedCatalogXMP(ctx, doc, catalogRef), props, valid)
	if stripped == 0 {
		return 0
	}

	// The packet is written back decoded, so the filter chain that described
	// the original body no longer applies.
	newDict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(newDict, stream.Dict)
	delete(newDict, "Filter")
	delete(newDict, "DecodeParms")
	newStream := ast.NewStream(newDict, out)
	if metaRef.ObjectNum > 0 {
		alloc.AllocAt(metaRef.ObjectNum, newStream)
	} else {
		ref := alloc.Alloc(newStream)
		newCat := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCat, catalogDict)
		newCat["Metadata"] = ref
		alloc.AllocAt(catalogRef.ObjectNum, newCat)
	}
	return stripped
}

// stripComponentXMPSchema strips props from the packet of the metadata stream
// objNum, and returns how many it stripped. A property the packet's extension
// schemas, or those of mainPacket, the catalog's, now declare with the value
// type it holds, at the given PDF/A part, is kept.
//
// ISO 19005-2 and 19005-3, 6.6.2.3.1 constrain the properties of every
// metadata stream in the file, not only the catalog's, and the validator names
// the stream a property sits in as "object N". The stream is read live:
// [fixXMP] runs first and rewrites component packets too, and the repair
// splices into its copy rather than the source one.
func stripComponentXMPSchema(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, objNum int, props []xmpSchemaProperty, part int, mainPacket []byte) int {
	ref := ast.PdfIndirectRef{ObjectNum: objNum}
	stream, ok := liveStream(ctx, alloc, doc, ref)
	if !ok {
		return 0
	}
	xmp, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(xmp) == 0 {
		doc.Logger.Debug("remediation: xmp-schema: cannot decode the component XMP stream", "object", objNum, "error", err)
		return 0
	}

	declared := compliance.XMPExtensionPropertyChecker(xmp, mainPacket, part)
	valid := func(prop xmpSchemaProperty) bool { return declared(prop.nsURI, prop.localName) }
	out, stripped := stripJudgedXMPProperties(xmp, judgedComponentXMP(ctx, doc, ref), props, valid)
	if stripped == 0 {
		return 0
	}

	// Written back decoded, as fixComponentMetadata writes it.
	newDict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(newDict, stream.Dict)
	delete(newDict, "Filter")
	delete(newDict, "DecodeParms")
	delete(newDict, "Length")
	alloc.AllocAt(objNum, ast.NewStream(newDict, out))
	return stripped
}

// stripJudgedXMPProperties removes every occurrence of each of props from xmp
// that still holds a value the validator judged in judged, unless valid
// reports the property declared since with the value type it holds, and
// returns the new packet with the number of properties it removed.
func stripJudgedXMPProperties(xmp, judged []byte, props []xmpSchemaProperty, valid func(xmpSchemaProperty) bool) ([]byte, int) {
	stripped := 0
	out := xmp
	for _, prop := range props {
		if valid(prop) {
			continue
		}
		read := xmpPropertyValues(judged, prop.nsURI, prop.localName)
		// Loop to drop every occurrence; a single property may appear in
		// more than one rdf:Description in pathological XMP. The first one
		// an earlier fixer rewrote ends the loop: findXMPProperty only ever
		// returns the first occurrence, and that one is kept.
		removed := false
		for {
			loc := findXMPProperty(out, prop.nsURI, prop.localName)
			if loc.Form == xmpPropertyNotFound || !read[xmpPropertyValueAt(out, loc)] {
				break
			}
			next := removeXMPProperty(out, loc)
			if len(next) >= len(out) {
				break
			}
			out = next
			removed = true
		}
		if removed {
			stripped++
		}
	}
	return out, stripped
}

// liveCatalogXMP returns the catalog packet as the fixers have left it so
// far, decoded, or nil when there is none it can read.
func liveCatalogXMP(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) []byte {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil
	}
	metaObj, ok := catalogDict["Metadata"]
	if !ok {
		return nil
	}
	var metaRef ast.PdfIndirectRef
	if ref, ok := metaObj.(ast.PdfIndirectRef); ok {
		metaRef = ref
	}
	stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, metaRef, metaObj)
	if !ok {
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("remediation: xmp-schema: cannot decode the live catalog XMP stream", "error", err)
		return nil
	}
	return data
}

// judgedCatalogXMP returns the catalog XMP packet the validator read: the
// source document's, decoded and transcoded to UTF-8 as [fixXMP] transcodes
// it, so a property in it compares with the same property in the packet an
// earlier fixer queued. It returns nil when the source catalog carries no
// packet it can read, and every property of the live packet is then an
// earlier fixer's.
func judgedCatalogXMP(ctx context.Context, doc *document.Document, catalogRef ast.PdfIndirectRef) []byte {
	catalog, err := document.ResolveDict(ctx, doc, catalogRef)
	if err != nil || catalog == nil {
		doc.Logger.Debug("remediation: xmp-schema: cannot resolve the source catalog", "error", err)
		return nil
	}
	metaObj, ok := catalog["Metadata"]
	if !ok {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, metaObj)
	if err != nil {
		doc.Logger.Debug("remediation: xmp-schema: cannot resolve the source XMP stream", "error", err)
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("remediation: xmp-schema: cannot decode the source XMP stream", "error", err)
		return nil
	}
	data, _ = xmpToUTF8(data)
	return data
}

// judgedComponentXMP returns the packet of the metadata stream at ref that the
// validator read: the source document's, decoded. It returns nil when the
// source stream cannot be read, and every property of the live packet is then
// an earlier fixer's.
func judgedComponentXMP(ctx context.Context, doc *document.Document, ref ast.PdfIndirectRef) []byte {
	stream, err := document.ResolveStream(ctx, doc, ref)
	if err != nil {
		doc.Logger.Debug("remediation: xmp-schema: cannot resolve the source component XMP stream", "object", ref.ObjectNum, "error", err)
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("remediation: xmp-schema: cannot decode the source component XMP stream", "object", ref.ObjectNum, "error", err)
		return nil
	}
	return data
}

// xmpPropertyValue is one serialization of an XMP property: its form and its
// value, the element content or the attribute value.
type xmpPropertyValue struct {
	form  xmpPropertyForm
	value string
}

// xmpPropertyValueAt returns the serialization of the property located at loc.
func xmpPropertyValueAt(xmp []byte, loc xmpPropertyLocation) xmpPropertyValue {
	return xmpPropertyValue{form: loc.Form, value: xmpPropertyRawValue(xmp, loc)}
}

// xmpPropertyValues returns every serialization of nsURI:localName in xmp. The
// namespace prefix is not part of it: canonicalizing a prefix, as [fixXMP]
// does for pdfaid, leaves the value the validator judged in place.
func xmpPropertyValues(xmp []byte, nsURI, localName string) map[xmpPropertyValue]bool {
	values := make(map[xmpPropertyValue]bool)
	for {
		loc := findXMPProperty(xmp, nsURI, localName)
		if loc.Form == xmpPropertyNotFound {
			return values
		}
		values[xmpPropertyValueAt(xmp, loc)] = true
		next := removeXMPProperty(xmp, loc)
		if len(next) >= len(xmp) {
			return values
		}
		xmp = next
	}
}

// parseXMPSchemaTarget extracts the packet and the property a clause
// 6.6.2.3.1 or 6.7.9 violation context names. The compliance engine prefixes
// the context of a component metadata stream's violation with "object N: ",
// and objNum is then N; it is 0 for the catalog packet, whose context carries
// no prefix. The rest of the context is parsed by [parseXMPSchemaContext].
func parseXMPSchemaTarget(s string) (objNum int, prop xmpSchemaProperty, ok bool) {
	if rest, found := strings.CutPrefix(s, "object "); found {
		num, tail, cut := strings.Cut(rest, ": ")
		n, err := strconv.Atoi(num)
		if !cut || err != nil || n <= 0 {
			return 0, xmpSchemaProperty{}, false
		}
		objNum, s = n, tail
	}
	nsURI, localName, ok := parseXMPSchemaContext(s)
	if !ok {
		return 0, xmpSchemaProperty{}, false
	}
	return objNum, xmpSchemaProperty{nsURI: nsURI, localName: localName}, true
}

// namesComponentPacket reports whether v was found in a component metadata
// stream, one other than the catalog's packet: the compliance engine prefixes
// the context of such a finding with "object N: ".
//
// xmp-metadata runs its component repair on such a finding even when the
// catalog packet needs no patch, and only then. A round of remediation leaves
// the catalog's former packet behind, unreferenced, and a repair of every
// metadata stream counted rewriting it as a fix: at PDF/UA-1, where a residual
// 7.1 finding runs xmp-metadata again, that kept an extra round which changed
// nothing any validator reads.
//
// # Corpus evidence
//
//   - litho imgxmp_4.pdf: at PDF/A-4 the catalog packet needs no patch, so
//     the bytes and encoding attributes of two page packets stayed, and the
//     output failed ISO 19005-4, 6.7.2.1 t2 and t3 on litho and veraPDF 1.30.
//   - govdocs thread_300_300779.pdf, pdfcpu pkg_testdata__VectorApple.pdf,
//     pdfjs S2.pdf, bug1720411.pdf, bug1722036.pdf, bug1766987.pdf,
//     freeculture.pdf, issue13520.pdf and issue18801.pdf: with the component
//     repair run whatever the findings, a PDF/UA-1 remediation, default and
//     aggressive, took a second round to rewrite the unreferenced packet, and
//     left the same residual.
func namesComponentPacket(v compliance.Violation) bool {
	rest, found := strings.CutPrefix(v.Context, "object ")
	if !found {
		return false
	}
	num, _, cut := strings.Cut(rest, ": ")
	n, err := strconv.Atoi(num)
	return cut && err == nil && n > 0
}

// parseXMPSchemaContext extracts the namespace URI and local name from a
// clause 6.6.2.3.1 violation context string. The compliance engine
// encodes the offending XMP property as "<URI>:<localName> <detail>",
// where the namespace URI always ends with "/". Returns ok=false if the
// context does not match this shape, including when what precedes the local
// name holds whitespace, which no namespace URI does: a component packet's
// "object N: " prefix is not part of the namespace.
func parseXMPSchemaContext(s string) (nsURI, localName string, ok bool) {
	idx := strings.Index(s, "/:")
	if idx < 0 {
		return "", "", false
	}
	nsURI = s[:idx+1]
	if strings.ContainsAny(nsURI, " \t\r\n") {
		return "", "", false
	}
	rest := s[idx+2:]
	end := strings.IndexAny(rest, " \t")
	if end < 0 {
		localName = rest
	} else {
		localName = rest[:end]
	}
	if localName == "" {
		return "", "", false
	}
	return nsURI, localName, true
}
