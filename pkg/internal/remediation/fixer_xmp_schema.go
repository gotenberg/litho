package remediation

import (
	"context"
	"fmt"
	"maps"
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
	_, _, ok := parseXMPSchemaContext(v.Context)
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
func fixXMPSchema(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, violations []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	metaObj, ok := catalogDict["Metadata"]
	if !ok {
		return nil, nil
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
		return nil, nil
	}
	// Decode before searching: ast.PdfStream.Bytes returns the body before
	// filter decoding, so a /Metadata carrying a filter (ISO 32000-1, 14.3.2
	// permits one) was searched as compressed bytes, no property was ever
	// found, and the violation stayed residual. A stream queued by an earlier
	// fixer declares no filter, so decoding is a passthrough for it.
	xmp, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(xmp) == 0 {
		return nil, nil
	}

	judged := judgedCatalogXMP(ctx, doc, catalogRef)
	tried := make(map[string]bool)
	stripped := 0
	out := xmp
	for _, v := range violations {
		nsURI, localName, ok := parseXMPSchemaContext(v.Context)
		if !ok {
			continue
		}
		key := nsURI + ":" + localName
		if tried[key] {
			continue
		}
		tried[key] = true
		read := xmpPropertyValues(judged, nsURI, localName)
		// Loop to drop every occurrence; a single property may appear in
		// more than one rdf:Description in pathological XMP. The first one
		// an earlier fixer rewrote ends the loop: findXMPProperty only ever
		// returns the first occurrence, and that one is kept.
		removed := false
		for {
			loc := findXMPProperty(out, nsURI, localName)
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

	if stripped == 0 {
		return nil, nil
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

	return []string{fmt.Sprintf("stripped %d XMP schema-noncompliant propert(ies)", stripped)}, nil
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

// parseXMPSchemaContext extracts the namespace URI and local name from a
// clause 6.6.2.3.1 violation context string. The compliance engine
// encodes the offending XMP property as "<URI>:<localName> <detail>",
// where the namespace URI always ends with "/". Returns ok=false if the
// context does not match this shape.
func parseXMPSchemaContext(s string) (nsURI, localName string, ok bool) {
	idx := strings.Index(s, "/:")
	if idx < 0 {
		return "", "", false
	}
	nsURI = s[:idx+1]
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
