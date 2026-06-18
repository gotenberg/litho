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
func fixXMPSchema(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, violations []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
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
	xmp, err := stream.Bytes(ctx)
	if err != nil || len(xmp) == 0 {
		return nil, nil
	}

	dropped := make(map[string]bool)
	out := xmp
	for _, v := range violations {
		nsURI, localName, ok := parseXMPSchemaContext(v.Context)
		if !ok {
			continue
		}
		key := nsURI + ":" + localName
		if dropped[key] {
			continue
		}
		// Loop to drop every occurrence; a single property may appear in
		// more than one rdf:Description in pathological XMP.
		for {
			loc := findXMPProperty(out, nsURI, localName)
			if loc.Form == xmpPropertyNotFound {
				break
			}
			out = removeXMPProperty(out, loc)
		}
		dropped[key] = true
	}

	if len(dropped) == 0 || len(out) == len(xmp) {
		return nil, nil
	}

	newDict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(newDict, stream.Dict)
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

	names := make([]string, 0, len(dropped))
	for k := range dropped {
		names = append(names, k)
	}
	return []string{fmt.Sprintf("stripped %d XMP schema-noncompliant propert(ies)", len(names))}, nil
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
