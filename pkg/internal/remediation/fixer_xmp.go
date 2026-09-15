package remediation

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchXMP(v compliance.Violation) bool {
	return clausePrefix(v,
		"6.6.2", "6.6.4", // XMP metadata streams, version ID (parts 2-4)
		"6.7.2", "6.7.3", // XMP metadata, version/conformance (part 1)
		"6.7.8", "6.7.9", // extension schemas, namespace validation (part 1)
		"6.7.11",      // version identification (part 1)
		"5",           // PDF/UA identification
		"7.1", "8.11", // PDF/UA metadata
	)
}

func fixXMP(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	// A trailer with no /Root indirect reference cannot be patched in place
	// (there is no catalog object number to write back to). Skip rather than
	// error, matching every other catalog-dependent fixer (fixCatalogCleanup,
	// fixXMPSchema): aborting here discarded the whole document's remediation.
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	// Gather metadata from /Info dict for syncing.
	info := gatherInfoMetadata(ctx, alloc, doc)

	// Determine PDF/A part and conformance from profile string.
	part := compliance.PDFAPart(profile)
	conformance := compliance.PDFAConformance(profile)
	uaPart := compliance.PDFUAPart(profile)

	// Check if XMP metadata stream already exists.
	var existingXMP string
	var reencoded bool
	if metaObj, ok := catalogDict["Metadata"]; ok {
		if stream, err := document.ResolveStream(ctx, doc, metaObj); err == nil {
			// Decode the packet: a non-PDF/A input may store the catalog XMP
			// FlateDecode-compressed, and the raw bytes would parse as garbage,
			// so patchExistingXMP would inject no pdfaid and leave the file
			// non-conforming. The XMP stream fixXMP writes back is uncompressed.
			if data, err := doc.DecodeStream(ctx, stream); err == nil {
				// The XMP package must be UTF-8 (ISO 19005-2/3 §6.6.2.1,
				// ISO 19005-4 §6.7.2.1). A UTF-16 or UTF-32 packet also
				// defeats the byte-range splicer, silently leaving stale
				// pdfaid behind, so transcode before any patching.
				if utf8Data, changed := xmpToUTF8(data); changed {
					data = utf8Data
					reencoded = true
				}
				existingXMP = string(data)
			}
		}
	}

	var xmpBytes []byte
	var fixes []string

	if existingXMP != "" {
		// Patch existing XMP.
		patched, patchFixes := patchExistingXMP(existingXMP, part, conformance, uaPart, info)
		if len(patchFixes) == 0 && !reencoded {
			return nil, nil // nothing to patch
		}
		xmpBytes = []byte(patched)
		fixes = patchFixes
		if reencoded {
			fixes = append(fixes, "re-encoded XMP packet to UTF-8")
		}
	} else {
		// Generate new XMP.
		xmpBytes = []byte(generateXMP(part, conformance, uaPart, info))
		fixes = []string{"generated XMP metadata stream"}
	}

	// Create XMP stream.
	xmpDict := ast.PdfDict{
		"Type":    ast.PdfName("Metadata"),
		"Subtype": ast.PdfName("XML"),
	}
	xmpStream := ast.NewStream(xmpDict, xmpBytes)
	xmpRef := alloc.Alloc(xmpStream)

	// Update catalog.
	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Metadata"] = xmpRef
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	// Repair XMP on component /Metadata streams (image XObjects, embedded
	// files, ...). veraPDF validates their custom properties against the same
	// ISO 19005-2 §6.6.2.3 extension-schema rules, but the catalog-only pass
	// above leaves them undeclared, so litho over-reports these files as fixed.
	catalogMetaNum := 0
	if ref, ok := catalogDict["Metadata"].(ast.PdfIndirectRef); ok {
		catalogMetaNum = ref.ObjectNum
	}
	fixes = append(fixes, fixComponentMetadata(ctx, alloc, doc, catalogMetaNum, part)...)

	return fixes, nil
}

// fixComponentMetadata applies the extension-schema declaration and untypeable
// strip repairs to every /Type /Metadata stream other than the catalog's, which
// [fixXMP] handles separately. veraPDF validates XMP on image XObjects and other
// components, so their custom properties need the same treatment. Streams are
// rewritten uncompressed (the fresh dict drops /Filter) to keep the edited XMP
// readable, matching the catalog XMP stream fixXMP writes.
func fixComponentMetadata(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogMetaNum, part int) []string {
	// Iterate object numbers in sorted order so the emitted fix notes and the
	// destination object numbering stay deterministic across runs.
	objNums := make([]int, 0, len(doc.Index.Entries))
	for objNum := range doc.Index.Entries {
		objNums = append(objNums, objNum)
	}
	sort.Ints(objNums)

	var fixes []string
	for _, objNum := range objNums {
		if objNum == 0 || objNum == catalogMetaNum {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fixes
		}
		ref := ast.PdfIndirectRef{ObjectNum: objNum}
		stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, ref, ref)
		if !ok {
			continue
		}
		if t, _ := stream.Dict["Type"].(ast.PdfName); t != "Metadata" {
			continue
		}
		// Decode the stream: component metadata (unlike the catalog packet,
		// which PDF/A requires uncompressed) may be FlateDecode-compressed, and
		// the raw bytes would parse as garbage. The write-back below drops
		// /Filter so the edited XMP is stored decoded.
		xmp, err := doc.DecodeStream(ctx, stream)
		if err != nil || len(xmp) == 0 {
			continue
		}
		// ISO 19005-2, 6.6.2.1 t2 and t3 apply to every XMPPackage, not only
		// the catalog's, so a page or image packet keeping bytes= or encoding=
		// leaves litho's own output failing litho's own check: remediation then
		// never converged, appending a fresh revision on every pass.
		out, streamFixes := stripForbiddenXPacketAttrs(xmp)
		schemaOut, schemaFixes := applyExtensionSchemas(out, part)
		out = schemaOut
		streamFixes = append(streamFixes, schemaFixes...)
		if len(streamFixes) == 0 {
			continue
		}
		newDict := make(ast.PdfDict, len(stream.Dict))
		maps.Copy(newDict, stream.Dict)
		delete(newDict, "Filter")
		delete(newDict, "DecodeParms")
		delete(newDict, "Length")
		alloc.AllocAt(objNum, ast.NewStream(newDict, out))
		fixes = append(fixes, streamFixes...)
	}
	return fixes
}

type infoMetadata struct {
	Title        string
	Author       string
	Subject      string
	Creator      string
	Producer     string
	Keywords     string
	CreationDate string
	ModDate      string
	// FallbackTitle holds a best-effort title (first outline entry) for
	// PDF/UA dc:title synthesis when the document has no /Title at all.
	FallbackTitle string
	// Present records which Info dict keys exist at all. ISO 19005
	// §6.7.3 keys its sync rules on key presence, so an absent /Keywords
	// must not be confused with an empty one: only present keys drive
	// the XMP sync.
	Present map[string]bool
}

func gatherInfoMetadata(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) infoMetadata {
	var info infoMetadata
	info.Present = make(map[string]bool)
	if infoDict := liveInfoDict(ctx, alloc, doc); infoDict != nil {
		readField := func(key ast.PdfName) string {
			v, ok := infoDict[key]
			if !ok {
				return ""
			}
			info.Present[string(key)] = true
			if _, isRef := v.(ast.PdfIndirectRef); isRef {
				resolved, err := document.ResolveObject(ctx, doc, v)
				if err != nil {
					return ""
				}
				v = resolved
			}
			if s, ok := v.(ast.PdfString); ok {
				return document.DecodePdfString(s)
			}
			return ""
		}

		info.Title = readField("Title")
		info.Author = readField("Author")
		info.Subject = readField("Subject")
		info.Creator = readField("Creator")
		info.Producer = readField("Producer")
		info.Keywords = readField("Keywords")
		info.CreationDate = readField("CreationDate")
		info.ModDate = readField("ModDate")
	}
	if info.Title == "" {
		info.FallbackTitle = fallbackDocTitle(ctx, alloc, doc)
	}
	return info
}

// liveInfoDict resolves the trailer /Info dict, preferring an in-flight
// patched version from alloc so earlier fixers (e.g. info-date) see their
// changes flow into the XMP sync. Returns nil when absent or unreadable.
func liveInfoDict(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) ast.PdfDict {
	infoObj, ok := doc.Index.Trailer["Info"]
	if !ok {
		return nil
	}
	if ref, isRef := infoObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
			if d, ok := pending.(ast.PdfDict); ok {
				return d
			}
		}
	}
	resolved, err := document.ResolveDict(ctx, doc, infoObj)
	if err != nil {
		return nil
	}
	return resolved
}

// maxFallbackTitleRunes caps a synthesized dc:title sourced from an outline
// entry. Anti-bloat guard, not spec-mandated: outline titles are
// file-supplied strings, and a multi-kilobyte document title helps no
// reader while inflating every rewrite.
const maxFallbackTitleRunes = 256

// fallbackDocTitle returns a best-effort document title for PDF/UA
// dc:title synthesis when DocInfo carries none: the first outline item's
// /Title, or "" when the document has no usable outline.
func fallbackDocTitle(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) string {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return ""
	}
	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalog == nil {
		return ""
	}
	outlines, err := document.ResolveDict(ctx, doc, catalog["Outlines"])
	if err != nil || outlines == nil {
		return ""
	}
	first, err := document.ResolveDict(ctx, doc, outlines["First"])
	if err != nil || first == nil {
		return ""
	}
	titleObj, err := document.ResolveObject(ctx, doc, first["Title"])
	if err != nil {
		return ""
	}
	s, ok := titleObj.(ast.PdfString)
	if !ok {
		return ""
	}
	title := strings.TrimSpace(document.DecodePdfString(s))
	if runes := []rune(title); len(runes) > maxFallbackTitleRunes {
		title = string(runes[:maxFallbackTitleRunes])
	}
	return title
}

func generateXMP(part int, conformance string, uaPart int, info infoMetadata) string {
	title := xmlEscape(info.Title)
	if title == "" {
		title = "Untitled"
	}
	creator := xmlEscape(info.Creator)
	if creator == "" {
		creator = "litho"
	}
	producer := xmlEscape(info.Producer)
	if producer == "" {
		producer = "litho"
	}

	var sb strings.Builder
	sb.WriteString("<?xpacket begin=\"\xEF\xBB\xBF\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n")
	sb.WriteString("<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n")
	sb.WriteString("<rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n")
	sb.WriteString("<rdf:Description rdf:about=\"\"\n")
	sb.WriteString("  xmlns:dc=\"http://purl.org/dc/elements/1.1/\"\n")
	sb.WriteString("  xmlns:xmp=\"http://ns.adobe.com/xap/1.0/\"\n")
	sb.WriteString("  xmlns:pdf=\"http://ns.adobe.com/pdf/1.3/\"\n")
	if part > 0 {
		sb.WriteString("  xmlns:pdfaid=\"http://www.aiim.org/pdfa/ns/id/\"\n")
	}
	if uaPart > 0 {
		sb.WriteString("  xmlns:pdfuaid=\"http://www.aiim.org/pdfua/ns/id/\"\n")
	}
	sb.WriteString(">\n")

	if part > 0 {
		fmt.Fprintf(&sb, "  <pdfaid:part>%d</pdfaid:part>\n", part)
		// PDF/A-4 (plain) must NOT have pdfaid:conformance; only 4e/4f do.
		// Parts 1-3 always have conformance.
		if part < 4 || (conformance != "" && conformance != "B" && conformance != "A") {
			fmt.Fprintf(&sb, "  <pdfaid:conformance>%s</pdfaid:conformance>\n", conformance)
		} else if part == 4 && (conformance == "E" || conformance == "F") {
			fmt.Fprintf(&sb, "  <pdfaid:conformance>%s</pdfaid:conformance>\n", conformance)
		}
		// PDF/A-4 requires pdfaid:rev=2020.
		if part == 4 {
			sb.WriteString("  <pdfaid:rev>2020</pdfaid:rev>\n")
		}
	}
	if uaPart > 0 {
		fmt.Fprintf(&sb, "  <pdfuaid:part>%d</pdfuaid:part>\n", uaPart)
		// PDF/UA-2 (ISO 14289-2 §5) requires pdfuaid:rev=2024 alongside part.
		if uaPart == 2 {
			fmt.Fprintf(&sb, "  <pdfuaid:rev>%s</pdfuaid:rev>\n", compliance.PDFUA2ExpectedRev)
		}
	}

	fmt.Fprintf(&sb, "  <dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">%s</rdf:li></rdf:Alt></dc:title>\n", title)
	if info.Author != "" {
		fmt.Fprintf(&sb, "  <dc:creator><rdf:Seq><rdf:li>%s</rdf:li></rdf:Seq></dc:creator>\n", xmlEscape(info.Author))
	}
	if info.Subject != "" {
		fmt.Fprintf(&sb, "  <dc:description><rdf:Alt><rdf:li xml:lang=\"x-default\">%s</rdf:li></rdf:Alt></dc:description>\n", xmlEscape(info.Subject))
	}
	fmt.Fprintf(&sb, "  <xmp:CreatorTool>%s</xmp:CreatorTool>\n", creator)
	if info.CreationDate != "" {
		if xmpDate := pdfDateToXMP(info.CreationDate); xmpDate != "" {
			fmt.Fprintf(&sb, "  <xmp:CreateDate>%s</xmp:CreateDate>\n", xmpDate)
		}
	}
	if info.ModDate != "" {
		if xmpDate := pdfDateToXMP(info.ModDate); xmpDate != "" {
			fmt.Fprintf(&sb, "  <xmp:ModifyDate>%s</xmp:ModifyDate>\n", xmpDate)
		}
	}
	fmt.Fprintf(&sb, "  <pdf:Producer>%s</pdf:Producer>\n", producer)
	if info.Keywords != "" {
		fmt.Fprintf(&sb, "  <pdf:Keywords>%s</pdf:Keywords>\n", xmlEscape(info.Keywords))
	}

	sb.WriteString("</rdf:Description>\n")
	sb.WriteString("</rdf:RDF>\n")
	sb.WriteString("</x:xmpmeta>\n")
	sb.WriteString("<?xpacket end=\"w\"?>")
	return sb.String()
}

// XMP namespace URIs used by the remediation fixer. Kept as package-level
// constants so the patching paths and tests refer to the same values.
const (
	pdfaidNS        = "http://www.aiim.org/pdfa/ns/id/"
	pdfuaidNS       = "http://www.aiim.org/pdfua/ns/id/"
	dcNS            = "http://purl.org/dc/elements/1.1/"
	pdfNS           = "http://ns.adobe.com/pdf/1.3/"
	xmpNS           = "http://ns.adobe.com/xap/1.0/"
	pdfaExtensionNS = "http://www.aiim.org/pdfa/ns/extension/"
	pdfaSchemaNS    = "http://www.aiim.org/pdfa/ns/schema#"
	pdfaPropertyNS  = "http://www.aiim.org/pdfa/ns/property#"
	pdfaTypeNS      = "http://www.aiim.org/pdfa/ns/type#"
	pdfaFieldNS     = "http://www.aiim.org/pdfa/ns/field#"
)

// setStructuredXMPProperty writes a property whose value is structured XMP
// markup (an rdf:Alt or rdf:Seq container, as dc:title and dc:creator require).
// An attribute-form property, e.g. the Distiller shorthand dc:title='text', can
// only hold a simple string: splicing markup into the attribute value corrupts
// the packet because the inner quotes close the attribute early. Such a property
// is therefore removed and re-inserted in element form. element is the full
// <ns:name>value</ns:name> to inject; value is the inner markup used when an
// element already exists. Returns the updated bytes and whether it changed them.
func setStructuredXMPProperty(buf []byte, nsURI, prefix, localName, value, element string) ([]byte, bool) {
	loc := findXMPProperty(buf, nsURI, localName)
	switch loc.Form {
	case xmpPropertyAsElement:
		if out, err := replacePropertyValue(buf, nsURI, localName, value); err == nil {
			// Rewriting a value already equal to itself is not a change:
			// reporting one made remediation write a fresh revision, so
			// remediating its own output grew the file on every pass.
			return out, !bytes.Equal(out, buf)
		}
		return buf, false
	case xmpPropertyAsAttribute:
		buf = removeXMPProperty(buf, loc)
	case xmpPropertyNotFound:
		// Fall through to inject the element form below.
	}
	if out, err := ensureNamespaceDecl(buf, prefix, nsURI); err == nil {
		buf = out
	}
	if out, err := insertChildElement(buf, element); err == nil {
		return out, true
	}
	return buf, false
}

func patchExistingXMP(xmp string, part int, conformance string, uaPart int, info infoMetadata) (string, []string) {
	// Byte-level property surgery assumes a parseable packet. On a packet
	// that is not well-formed XML (itself an unfixable §6.7.2.1/§6.6.2.1 t04
	// violation), injecting pdfaid:conformance shifts how a lenient reader
	// parses pdfaid:part and pdfaid:rev, so the output newly fails §6.7.3 t02
	// and t05. Leave a malformed packet untouched: the caller then writes no
	// new stream and remediation reports the malformed-XMP defect as an
	// honest residual instead of making the file worse.
	if !compliance.XMPWellFormed([]byte(xmp)) {
		return xmp, nil
	}

	var fixes []string
	buf := []byte(xmp)

	// Canonicalize required XMP prefixes. Some producers bind the
	// PDF/A identification namespace to a non-canonical prefix, e.g.
	// `xmlns:pdfa="http://www.aiim.org/pdfa/ns/id/"`. ISO 19005 requires
	// the prefix `pdfaid`.
	for _, canon := range []struct{ prefix, uri string }{
		{"pdfaid", pdfaidNS},
		{"pdfuaid", pdfuaidNS},
	} {
		before := buf
		buf = canonicalizePrefix(buf, canon.prefix, canon.uri)
		// Compare content, not length: an alias→canonical prefix rewrite
		// (e.g. pdfuaia→pdfuaid) is length-neutral yet still a fix.
		if !bytes.Equal(buf, before) {
			fixes = append(fixes, fmt.Sprintf("canonicalized %s namespace prefix", canon.prefix))
		}
	}

	// Strip the attributes ISO 19005-2, 6.6.2.1 t2 and t3 forbid on the
	// <?xpacket?> header.
	var hdrFixes []string
	buf, hdrFixes = stripForbiddenXPacketAttrs(buf)
	fixes = append(fixes, hdrFixes...)

	// PDF/A-4 (plain): remove pdfaid:conformance if present (forbidden).
	// Uses the byte-range splicer so the removal works regardless of
	// whether the producer serialized the property as a child element
	// or as an attribute on the rdf:Description start tag. A missing
	// property is silently skipped: other namespaces may coincidentally
	// use the "conformance" local name, and aborting the whole patch
	// function on that basis would forfeit every later fix.
	if part == 4 && conformance != "E" && conformance != "F" {
		if loc := findXMPProperty(buf, pdfaidNS, "conformance"); loc.Form != xmpPropertyNotFound {
			buf = removeXMPProperty(buf, loc)
			fixes = append(fixes, "removed pdfaid:conformance (forbidden for plain PDF/A-4)")
		}
	}

	// Parts 1-3: remove pdfaid:rev. The property exists only in the
	// PDF/A-4 identification schema; the XMP 2004/2005 catalogs that
	// ISO 19005-1 §6.7.9 and 19005-2/3 §6.6.2.3.1 bind to predefine
	// pdfaid part/conformance/amd/corr only, so a leftover rev (typical
	// when re-targeting a PDF/A-4 document) is a non-predefined property
	// in those parts and must be declared in an extension schema or
	// dropped. Dropping it is the conformant repair here.
	if part >= 1 && part <= 3 {
		if loc := findXMPProperty(buf, pdfaidNS, "rev"); loc.Form != xmpPropertyNotFound {
			buf = removeXMPProperty(buf, loc)
			fixes = append(fixes, "removed pdfaid:rev (PDF/A-4-only property)")
		}
	}

	// Inject or correct pdfaid:part + pdfaid:conformance.
	if part > 0 {
		partStr := fmt.Sprintf("%d", part)
		if findXMPProperty(buf, pdfaidNS, "part").Form == xmpPropertyNotFound {
			// Repair any wrong-URI xmlns:pdfaid binding first. The rebind
			// can make an existing pdfaid block (part, conformance, rev)
			// valid in one stroke, so the property must be re-checked
			// before injecting: a second pdfaid:part would make the
			// packet's XMP serialization invalid (ISO 16684-1 forbids
			// duplicate properties on the same XMP node).
			if out, err := ensureNamespaceDecl(buf, "pdfaid", pdfaidNS); err == nil {
				buf = out
			}
		}
		// Whether the packet ends up carrying a part number at all. The
		// identification schema is the pair: writing a conformance level
		// without one leaves pdfaid:part empty, which is a 6.6.4 test 2
		// failure the file did not have before.
		partPresent := true
		if findXMPProperty(buf, pdfaidNS, "part").Form == xmpPropertyNotFound {
			if out, injected := injectPropertyOnce(buf, "pdfaid:part", fmt.Sprintf("<pdfaid:part>%s</pdfaid:part>", partStr)); injected {
				buf = out
				fixes = append(fixes, "injected pdfaid:part into XMP")
			} else {
				partPresent = false
			}
		} else if out, err := replacePropertyValue(buf, pdfaidNS, "part", partStr); err == nil {
			if !bytes.Equal(out, buf) {
				fixes = append(fixes, fmt.Sprintf("set pdfaid:part to %d", part))
			}
			buf = out
		}
		// For conformance levels other than plain PDF/A-4 (where
		// conformance is forbidden), also inject or correct the level.
		needConformance := conformance != "" && (part != 4 || conformance == "E" || conformance == "F")
		// Injecting a conformance level next to a part number that could not
		// be written builds half an identification schema. issue15833.pdf
		// binds the rdf prefix to a proxy-rewritten URI, so the element
		// insertion finds no rdf:Description while the attribute insertion,
		// which matches on the prefix alone, still succeeds. The file came
		// out of remediation reporting pdfaid:part= with nothing in it.
		if needConformance && !partPresent &&
			findXMPProperty(buf, pdfaidNS, "conformance").Form == xmpPropertyNotFound {
			needConformance = false
		}
		if needConformance {
			if findXMPProperty(buf, pdfaidNS, "conformance").Form == xmpPropertyNotFound {
				// Repair any wrong-URI xmlns:pdfaid binding first, then
				// add the attribute on the rdf:Description that binds the
				// pdfaid namespace URI; child-element insertion lands in
				// the first description, which may lack the binding.
				if out, err := ensureNamespaceDecl(buf, "pdfaid", pdfaidNS); err == nil {
					buf = out
				}
				// Guarded like the element form: findXMPProperty cannot always
				// see an attribute it just wrote, and a second injection makes
				// the packet invalid XML with a duplicate attribute rather than
				// merely redundant (issue15833.pdf).
				if !bytes.Contains(buf, []byte("pdfaid:conformance=")) {
					if out, err := insertAttributeAtDescription(buf, "pdfaid", pdfaidNS, fmt.Sprintf(`pdfaid:conformance="%s"`, conformance)); err == nil {
						buf = out
						fixes = append(fixes, "injected pdfaid:conformance into XMP")
					}
				}
			} else if out, err := replacePropertyValue(buf, pdfaidNS, "conformance", conformance); err == nil {
				if !bytes.Equal(out, buf) {
					fixes = append(fixes, fmt.Sprintf("set pdfaid:conformance to %s", conformance))
				}
				buf = out
			}
		}
	}

	// Normalize PDF-format date values in XMP date properties. Some
	// producers copy the Info dict D:YYYY... string verbatim into
	// xmp:CreateDate and friends; the XMP Date value type is ISO 8601
	// (ISO 16684-1), so the D: form is an invalid Date value. Convert
	// in place.
	for _, dateProp := range []string{"CreateDate", "ModifyDate", "MetadataDate"} {
		loc := findXMPProperty(buf, xmpNS, dateProp)
		if loc.Form == xmpPropertyNotFound {
			continue
		}
		raw := xmpPropertyRawValue(buf, loc)
		if !strings.HasPrefix(raw, "D:") {
			continue
		}
		if iso := pdfDateToXMP(raw); iso != "" {
			if out, err := replacePropertyValue(buf, xmpNS, dateProp, iso); err == nil {
				if !bytes.Equal(out, buf) {
					fixes = append(fixes, fmt.Sprintf("normalized xmp:%s to ISO 8601", dateProp))
				}
				buf = out
			}
		}
	}

	// Inject pdfuaid namespace + pdfuaid:part if missing, or correct an
	// existing wrong value (ISO 14289-2 §5 t2 requires part == 2; a file
	// claiming part 1 under a PDF/UA-2 run keeps failing until the claim
	// is updated).
	if uaPart > 0 && findXMPProperty(buf, pdfuaidNS, "part").Form != xmpPropertyNotFound {
		before := buf
		if out, err := replacePropertyValue(buf, pdfuaidNS, "part", fmt.Sprintf("%d", uaPart)); err == nil {
			buf = out
			// Compare content, not length: only report a fix when the
			// value actually changed, so an already-correct part stays
			// idempotent.
			if !bytes.Equal(buf, before) {
				fixes = append(fixes, fmt.Sprintf("set pdfuaid:part to %d", uaPart))
			}
		}
	} else if uaPart > 0 {
		if out, err := ensureNamespaceDecl(buf, "pdfuaid", pdfuaidNS); err == nil {
			buf = out
		}
		if out, injected := injectPropertyOnce(buf, "pdfuaid:part", fmt.Sprintf("<pdfuaid:part>%d</pdfuaid:part>", uaPart)); injected {
			buf = out
			fixes = append(fixes, "injected pdfuaid:part into XMP")
		}
	}

	// PDF/UA-2 (ISO 14289-2 §5): inject pdfuaid:rev=2024 if missing, or
	// correct it when present but not the required value.
	if uaPart == 2 {
		if findXMPProperty(buf, pdfuaidNS, "rev").Form == xmpPropertyNotFound {
			if out, err := ensureNamespaceDecl(buf, "pdfuaid", pdfuaidNS); err == nil {
				buf = out
			}
			if out, injected := injectPropertyOnce(buf, "pdfuaid:rev", fmt.Sprintf("<pdfuaid:rev>%s</pdfuaid:rev>", compliance.PDFUA2ExpectedRev)); injected {
				buf = out
				fixes = append(fixes, "injected pdfuaid:rev=2024 into XMP")
			}
		} else {
			before := buf
			if out, err := replacePropertyValue(buf, pdfuaidNS, "rev", compliance.PDFUA2ExpectedRev); err == nil {
				buf = out
				// Compare content, not length: only report a fix when the
				// value actually changed, so an already-correct rev stays
				// idempotent.
				if !bytes.Equal(buf, before) {
					fixes = append(fixes, "set pdfuaid:rev to 2024")
				}
			}
		}
	}

	// PDF/A-4: inject pdfaid:rev=2020 if missing.
	if part == 4 && findXMPProperty(buf, pdfaidNS, "rev").Form == xmpPropertyNotFound {
		if out, err := ensureNamespaceDecl(buf, "pdfaid", pdfaidNS); err == nil {
			buf = out
		}
		if out, err := insertChildElement(buf, "<pdfaid:rev>2020</pdfaid:rev>"); err == nil {
			buf = out
			fixes = append(fixes, "injected pdfaid:rev=2020 into XMP")
		}
	}

	// Sync dc:title with Info /Title. PDF/UA additionally requires a
	// dc:title outright (ISO 14289-1 §7.1, ISO 14289-2 §8.11.1): when
	// neither DocInfo nor the packet carries one, synthesize it from the
	// first outline title, then from a placeholder, so the requirement is
	// met and /DisplayDocTitle has something to show.
	title := info.Title
	titleSource := "Info /Title"
	if uaPart > 0 && title == "" && findXMPProperty(buf, dcNS, "title").Form == xmpPropertyNotFound {
		if info.FallbackTitle != "" {
			title = info.FallbackTitle
			titleSource = "outline title"
		} else {
			title = "Untitled document"
			titleSource = "placeholder"
		}
	}
	if title != "" {
		escaped := xmlEscape(title)
		value := fmt.Sprintf(`<rdf:Alt><rdf:li xml:lang="x-default">%s</rdf:li></rdf:Alt>`, escaped)
		element := fmt.Sprintf(`<dc:title>%s</dc:title>`, value)
		existed := findXMPProperty(buf, dcNS, "title").Form != xmpPropertyNotFound
		if out, changed := setStructuredXMPProperty(buf, dcNS, "dc", "title", value, element); changed {
			buf = out
			if existed {
				fixes = append(fixes, "synced dc:title with Info /Title")
			} else {
				fixes = append(fixes, fmt.Sprintf("injected dc:title from %s", titleSource))
			}
		}
	}

	// Sync dc:creator with Info /Author.
	if info.Author != "" {
		escaped := xmlEscape(info.Author)
		value := fmt.Sprintf(`<rdf:Seq><rdf:li>%s</rdf:li></rdf:Seq>`, escaped)
		element := fmt.Sprintf(`<dc:creator>%s</dc:creator>`, value)
		existed := findXMPProperty(buf, dcNS, "creator").Form != xmpPropertyNotFound
		if out, changed := setStructuredXMPProperty(buf, dcNS, "dc", "creator", value, element); changed {
			buf = out
			if existed {
				fixes = append(fixes, "synced dc:creator with Info /Author")
			} else {
				fixes = append(fixes, "injected dc:creator from Info /Author")
			}
		}
	}

	// Sync pdf:Producer with Info /Producer.
	if info.Producer != "" {
		escaped := xmlEscape(info.Producer)
		if findXMPProperty(buf, pdfNS, "Producer").Form != xmpPropertyNotFound {
			if out, err := replacePropertyValue(buf, pdfNS, "Producer", escaped); err == nil {
				if !bytes.Equal(out, buf) {
					fixes = append(fixes, "synced pdf:Producer with Info /Producer")
				}
				buf = out
			}
		} else {
			if out, err := ensureNamespaceDecl(buf, "pdf", pdfNS); err == nil {
				buf = out
			}
			if out, err := insertChildElement(buf, fmt.Sprintf(
				"<pdf:Producer>%s</pdf:Producer>", escaped)); err == nil {
				buf = out
				fixes = append(fixes, "injected pdf:Producer from Info dict")
			}
		}
	}

	// Sync xmp:CreatorTool with Info /Creator.
	if info.Creator != "" {
		escaped := xmlEscape(info.Creator)
		if findXMPProperty(buf, xmpNS, "CreatorTool").Form != xmpPropertyNotFound {
			if out, err := replacePropertyValue(buf, xmpNS, "CreatorTool", escaped); err == nil {
				if !bytes.Equal(out, buf) {
					fixes = append(fixes, "synced xmp:CreatorTool with Info /Creator")
				}
				buf = out
			}
		} else {
			if out, err := ensureNamespaceDecl(buf, "xmp", xmpNS); err == nil {
				buf = out
			}
			if out, err := insertChildElement(buf, fmt.Sprintf(
				"<xmp:CreatorTool>%s</xmp:CreatorTool>", escaped)); err == nil {
				buf = out
				fixes = append(fixes, "injected xmp:CreatorTool from Info dict")
			}
		}
	}

	// Sync xmp:CreateDate with Info /CreationDate.
	if info.CreationDate != "" {
		xmpDate := pdfDateToXMP(info.CreationDate)
		if xmpDate != "" {
			if findXMPProperty(buf, xmpNS, "CreateDate").Form != xmpPropertyNotFound {
				if out, err := replacePropertyValue(buf, xmpNS, "CreateDate", xmpDate); err == nil {
					if !bytes.Equal(out, buf) {
						fixes = append(fixes, "synced xmp:CreateDate with Info /CreationDate")
					}
					buf = out
				}
			} else {
				if out, err := ensureNamespaceDecl(buf, "xmp", xmpNS); err == nil {
					buf = out
				}
				if out, err := insertChildElement(buf, fmt.Sprintf(
					"<xmp:CreateDate>%s</xmp:CreateDate>", xmpDate)); err == nil {
					buf = out
					fixes = append(fixes, "injected xmp:CreateDate from Info /CreationDate")
				}
			}
		}
	}

	// Sync xmp:ModifyDate with Info /ModDate.
	if info.ModDate != "" {
		xmpDate := pdfDateToXMP(info.ModDate)
		if xmpDate != "" {
			if findXMPProperty(buf, xmpNS, "ModifyDate").Form != xmpPropertyNotFound {
				if out, err := replacePropertyValue(buf, xmpNS, "ModifyDate", xmpDate); err == nil {
					if !bytes.Equal(out, buf) {
						fixes = append(fixes, "synced xmp:ModifyDate with Info /ModDate")
					}
					buf = out
				}
			} else {
				if out, err := ensureNamespaceDecl(buf, "xmp", xmpNS); err == nil {
					buf = out
				}
				if out, err := insertChildElement(buf, fmt.Sprintf(
					"<xmp:ModifyDate>%s</xmp:ModifyDate>", xmpDate)); err == nil {
					buf = out
					fixes = append(fixes, "injected xmp:ModifyDate from Info /ModDate")
				}
			}
		}
	}

	// Sync dc:description with Info /Subject. dc:description is a language
	// alternative, so it goes through setStructuredXMPProperty like dc:title:
	// writing the rdf:Alt into a property a producer wrote in attribute form
	// puts a literal '<' inside an attribute value, which XML 1.0, 2.3
	// forbids. Seven govdocs files came out of remediation newly failing
	// ISO 19005-2, 6.6.2.1 tests 4 and 5 that way.
	if info.Subject != "" {
		escaped := xmlEscape(info.Subject)
		value := fmt.Sprintf(`<rdf:Alt><rdf:li xml:lang="x-default">%s</rdf:li></rdf:Alt>`, escaped)
		element := fmt.Sprintf(`<dc:description>%s</dc:description>`, value)
		existed := findXMPProperty(buf, dcNS, "description").Form != xmpPropertyNotFound
		if out, changed := setStructuredXMPProperty(buf, dcNS, "dc", "description", value, element); changed {
			buf = out
			if existed {
				fixes = append(fixes, "synced dc:description with Info /Subject")
			} else {
				fixes = append(fixes, "injected dc:description from Info /Subject")
			}
		}
	}

	// Sync pdf:Keywords with Info /Keywords. A present-but-empty
	// /Keywords also drives the sync: ISO 19005 §6.7.3 requires the XMP
	// property and the Info entry to match verbatim, so a whitespace-only
	// pdf:Keywords is not equivalent to an empty Info entry.
	if info.Keywords != "" || info.Present["Keywords"] {
		escaped := xmlEscape(info.Keywords)
		if findXMPProperty(buf, pdfNS, "Keywords").Form != xmpPropertyNotFound {
			if out, err := replacePropertyValue(buf, pdfNS, "Keywords", escaped); err == nil && !bytes.Equal(out, buf) {
				buf = out
				fixes = append(fixes, "synced pdf:Keywords with Info /Keywords")
			}
		} else {
			if out, err := ensureNamespaceDecl(buf, "pdf", pdfNS); err == nil {
				buf = out
			}
			if out, err := insertChildElement(buf, fmt.Sprintf(
				"<pdf:Keywords>%s</pdf:Keywords>", escaped)); err == nil {
				buf = out
				fixes = append(fixes, "injected pdf:Keywords from Info dict")
			}
		}
	}

	buf, extFixes := applyExtensionSchemas(buf, part)
	fixes = append(fixes, extFixes...)

	return string(buf), fixes
}

// applyExtensionSchemas ensures buf declares an XMP extension schema for every
// custom or non-predefined property it uses (ISO 19005-2 §6.6.2.3), and strips
// properties whose value is an untypeable nested structure. An incomplete
// existing block (§6.6.2.3.3) is regenerated. Returns the updated bytes and
// human-readable fix notes. The schema block is a large multi-element XML
// fragment placed with insertChildElement so its indentation matches the
// surrounding rdf:Description.
func applyExtensionSchemas(buf []byte, part int) ([]byte, []string) {
	var fixes []string
	if strings.Contains(string(buf), "pdfaExtension:schemas") {
		if extensionSchemaIncomplete(buf) {
			if stripped, ok := stripExtensionSchemasBlock(buf); ok {
				buf = stripped
				fixes = append(fixes, "stripped incomplete extension schema block")
			}
		}
	}
	if !strings.Contains(string(buf), "pdfaExtension:schemas") {
		schemas, stripKeys := extractUnknownNamespaceProperties(buf, part)
		if stripped, n := stripXMPPropertyKeys(buf, stripKeys); n > 0 {
			buf = stripped
			fixes = append(fixes, fmt.Sprintf("stripped %d XMP propert(ies) with untypeable structured values", n))
		}
		if len(schemas) > 0 {
			decls := map[string]string{
				"pdfaExtension": pdfaExtensionNS,
				"pdfaSchema":    pdfaSchemaNS,
				"pdfaProperty":  pdfaPropertyNS,
			}
			for _, sc := range schemas {
				for _, prop := range sc.properties {
					if len(prop.structFields) > 0 {
						decls["pdfaType"] = pdfaTypeNS
						decls["pdfaField"] = pdfaFieldNS
					}
				}
			}
			// Each declaration is written into the packet, so ranging the
			// map would emit them in a different order on every run and the
			// same input would produce different output bytes.
			for _, prefix := range slices.Sorted(maps.Keys(decls)) {
				if out, err := ensureNamespaceDecl(buf, prefix, decls[prefix]); err == nil {
					buf = out
				}
			}
			block := generateExtensionSchemaBlock(schemas)
			if out, err := insertChildElement(buf, strings.TrimSpace(block)); err == nil {
				buf = out
				fixes = append(fixes, fmt.Sprintf("generated extension schema declarations for %d custom namespaces", len(schemas)))
			}
		}
	}
	return buf, fixes
}

// extensionSchemaIncomplete reports whether the pdfaExtension:schemas
// block is missing a field required by ISO 19005-1 §6.7.8 or
// ISO 19005-2 §6.6.2.3.3. When any nested valueType declares fields but
// omits pdfaField:name/valueType/description, or the outer schema lacks
// the three pdfaSchema fields, the block cannot be repaired in place and
// is regenerated from scratch from the live namespace/property bindings.
func extensionSchemaIncomplete(xmp []byte) bool {
	start := bytes.Index(xmp, []byte("<pdfaExtension:schemas"))
	if start < 0 {
		return false
	}
	end := bytes.Index(xmp[start:], []byte("</pdfaExtension:schemas>"))
	if end < 0 {
		return false
	}
	block := xmp[start : start+end]

	// Outer schema record: PDF/A-1 §6.7.8 tests 1-3 /
	// PDF/A-2 §6.6.2.3.3 tests 1-3 require these three fields.
	schemaRequired := [][]byte{
		[]byte("<pdfaSchema:namespaceURI"),
		[]byte("<pdfaSchema:prefix"),
		[]byte("<pdfaSchema:schema"),
	}
	for _, f := range schemaRequired {
		if !bytes.Contains(block, f) {
			return true
		}
	}

	// Property list: when the block declares pdfaSchema:property it
	// must expose pdfaProperty:name, pdfaProperty:valueType,
	// pdfaProperty:category and pdfaProperty:description for each entry.
	if bytes.Contains(block, []byte("pdfaSchema:property")) {
		propRequired := [][]byte{
			[]byte("<pdfaProperty:name"),
			[]byte("<pdfaProperty:valueType"),
			[]byte("<pdfaProperty:category"),
			[]byte("<pdfaProperty:description"),
		}
		for _, f := range propRequired {
			if !bytes.Contains(block, f) {
				return true
			}
		}
	}

	// ValueType declarations: when pdfaSchema:valueType appears each
	// entry needs pdfaType:type, pdfaType:namespaceURI, pdfaType:prefix
	// and pdfaType:description.
	if bytes.Contains(block, []byte("pdfaSchema:valueType")) {
		typeRequired := [][]byte{
			[]byte("<pdfaType:type"),
			[]byte("<pdfaType:namespaceURI"),
			[]byte("<pdfaType:prefix"),
			[]byte("<pdfaType:description"),
		}
		for _, f := range typeRequired {
			if !bytes.Contains(block, f) {
				return true
			}
		}
	}

	// Field declarations inside pdfaType:field lists: each field needs
	// pdfaField:name, pdfaField:valueType, pdfaField:description.
	if bytes.Contains(block, []byte("pdfaType:field")) {
		fieldRequired := [][]byte{
			[]byte("<pdfaField:name"),
			[]byte("<pdfaField:valueType"),
			[]byte("<pdfaField:description"),
		}
		for _, f := range fieldRequired {
			if !bytes.Contains(block, f) {
				return true
			}
		}
	}
	return false
}

// stripExtensionSchemasBlock removes the pdfaExtension:schemas element and its
// subtree so the caller can regenerate a complete one. It removes only that
// element, never the enclosing rdf:Description: a compact XMP packet hosts
// dc:title, the PDF/UA identifier, dc:creator and other required metadata in the
// same rdf:Description, and taking the whole Description out destroys them
// (producing a packet a strict validator rejects). Any specialised pdfaType /
// pdfaField xmlns bindings left behind are unused-but-valid declarations; the
// regeneration pass reuses them. Returns ok=false if the element's closing tag
// cannot be located.
func stripExtensionSchemasBlock(xmp []byte) ([]byte, bool) {
	start := bytes.Index(xmp, []byte("<pdfaExtension:schemas"))
	if start < 0 {
		return xmp, false
	}
	closeMark := []byte("</pdfaExtension:schemas>")
	closeIdx := bytes.Index(xmp[start:], closeMark)
	if closeIdx < 0 {
		return xmp, false
	}
	end := start + closeIdx + len(closeMark)

	// Trim surrounding whitespace so the removal leaves no blank line.
	for end < len(xmp) && (xmp[end] == '\n' || xmp[end] == '\r' || xmp[end] == ' ' || xmp[end] == '\t') {
		end++
	}
	for start > 0 && (xmp[start-1] == '\n' || xmp[start-1] == '\r' || xmp[start-1] == ' ' || xmp[start-1] == '\t') {
		start--
	}

	out := make([]byte, 0, len(xmp)-(end-start))
	out = append(out, xmp[:start]...)
	out = append(out, xmp[end:]...)
	return out, true
}

// knownXMPNamespaces mirrors the predefined namespace list from the compliance
// validator. Properties in these namespaces do not need extension schema
// declarations.
var knownXMPNamespaces = map[string]bool{
	"http://purl.org/dc/elements/1.1/":                 true,
	"http://ns.adobe.com/xap/1.0/":                     true,
	"http://ns.adobe.com/xap/1.0/mm/":                  true,
	"http://ns.adobe.com/xap/1.0/rights/":              true,
	"http://ns.adobe.com/pdf/1.3/":                     true,
	"http://www.aiim.org/pdfa/ns/id/":                  true,
	"http://ns.adobe.com/photoshop/1.0/":               true,
	"http://ns.adobe.com/tiff/1.0/":                    true,
	"http://ns.adobe.com/exif/1.0/":                    true,
	"http://ns.adobe.com/exif/1.0/aux/":                true,
	"http://ns.adobe.com/camera-raw-settings/1.0/":     true,
	"http://ns.adobe.com/xmp/1.0/DynamicMedia/":        true,
	"http://ns.adobe.com/xmp/Identifier/qual/1.0/":     true,
	"http://iptc.org/std/Iptc4xmpCore/1.0/xmlns/":      true,
	"http://iptc.org/std/Iptc4xmpExt/2008-02-29/":      true,
	"http://ns.adobe.com/xap/1.0/sType/ResourceRef#":   true,
	"http://ns.adobe.com/xap/1.0/sType/ResourceEvent#": true,
	"http://ns.adobe.com/xap/1.0/sType/Version#":       true,
	"http://ns.adobe.com/xap/1.0/sType/Rendition#":     true,
	"http://ns.adobe.com/xap/1.0/sType/Job#":           true,
	"http://ns.adobe.com/xap/1.0/g/":                   true,
	"http://ns.adobe.com/xap/1.0/g/img/":               true,
	"http://ns.adobe.com/xap/1.0/sType/Dimensions#":    true,
	"http://ns.adobe.com/xap/1.0/sType/Font#":          true,
	"http://ns.adobe.com/xap/1.0/t/pg/":                true,
	"http://ns.adobe.com/xap/1.0/sType/ManifestItem#":  true,
	"http://ns.adobe.com/xap/1.0/bj/":                  true,
	"http://ns.adobe.com/pdfx/1.3/":                    true,
	"http://www.w3.org/1999/02/22-rdf-syntax-ns#":      true,
	"http://www.w3.org/XML/1998/namespace":             true,
	"adobe:ns:meta/":                                   true,
	"http://www.w3.org/2001/XMLSchema-instance":        true,
	"http://www.w3.org/2001/XMLSchema":                 true,
	"http://www.w3.org/2000/xmlns/":                    true,
	"http://www.aiim.org/pdfa/ns/extension/":           true,
	"http://www.aiim.org/pdfa/ns/schema#":              true,
	"http://www.aiim.org/pdfa/ns/property#":            true,
	"http://www.aiim.org/pdfa/ns/field#":               true,
	"http://www.aiim.org/pdfa/ns/type#":                true,
	"http://www.aiim.org/pdfua/ns/id/":                 true,
}

type extSchemaInfo struct {
	nsURI      string
	prefix     string
	properties []extPropInfo
}

// extPropInfo names one custom property and the XMP valueType matching its
// serialized shape. A declared property's valueType must match its actual
// container (ISO 19005-2 §6.6.2.3.1 t2), so declaring plain Text for an
// rdf:Bag/Seq/Alt property trades the missing-schema violation for a
// type-mismatch one.
type extPropInfo struct {
	name      string
	valueType string
	// structNS, structPrefix, and structFields describe an inline
	// structure property (rdf:parseType="Resource" or nested element
	// fields). When structFields is non-empty the generator emits a
	// matching pdfaSchema:valueType definition: a declared property's
	// valueType must match its serialized shape, so a structure declared
	// as a simple type fails ISO 19005-2 §6.6.2.3.1 t2.
	structNS     string
	structPrefix string
	structFields []string
}

// needsExtensionDeclaration reports whether the top-level property ns:name
// must be declared in a PDF/A extension schema for the given PDF/A part.
// Custom namespaces always qualify. Predefined XMP schema namespaces
// qualify only for property names the XMP Specification variant for the
// part does not define (e.g. exif:MakerNote under PDF/A-2): the PDF/A
// extension schema mechanism keys declarations per namespace URI and
// property name and does not forbid declaring a property that lives in a
// predefined namespace, so extending a predefined schema is a conformant
// repair. The pdfaid identification namespace keeps its dedicated
// handling and is never collected here.
func needsExtensionDeclaration(part int, ns, name string) bool {
	if ns == pdfaidNS {
		return false
	}
	if compliance.XMPPredefinedSchemaNamespace(part, ns) {
		return !compliance.XMPPredefinedProperty(part, ns, name)
	}
	return !knownXMPNamespaces[ns]
}

// xmpPropKey identifies one XMP property by its namespace URI and local name.
type xmpPropKey struct {
	nsURI string
	name  string
}

// extractUnknownNamespaceProperties parses XMP XML and returns the
// namespaces holding properties that need an extension schema declaration
// for the given PDF/A part, with their prefixes and property names: custom
// namespaces in full, predefined namespaces restricted to the property
// names absent from the part's predefined catalog (see
// [needsExtensionDeclaration]).
//
// The second return lists properties whose value is a seq or bag of inline
// structures (rdf:li rdf:parseType="Resource"). litho cannot declare an
// arbitrarily nested structure type, and declaring such a property with a flat
// "seq Text" type fails veraPDF's ISO 19005-2 §6.6.2.3.1 test 2. Callers strip
// these from the XMP, the same safe repair [fixXMPSchema] applies.
func extractUnknownNamespaceProperties(data []byte, part int) ([]extSchemaInfo, []xmpPropKey) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	// Track namespace URI → prefix mapping from xmlns declarations.
	nsPrefixes := make(map[string]string)
	// Track namespace URI → property name → property info.
	nsProps := make(map[string]map[string]extPropInfo)

	ignoredNS := map[string]bool{
		"": true, "xmlns": true,
		"http://www.w3.org/1999/02/22-rdf-syntax-ns#": true,
		"http://www.w3.org/XML/1998/namespace":        true,
		"http://www.w3.org/2000/xmlns/":               true,
		"http://www.w3.org/2001/XMLSchema-instance":   true,
		"http://www.w3.org/2001/XMLSchema":            true,
		"adobe:ns:meta/":                              true,
	}
	record := func(ns string, info extPropInfo) {
		if nsProps[ns] == nil {
			nsProps[ns] = make(map[string]extPropInfo)
		}
		if _, seen := nsProps[ns][info.name]; !seen {
			nsProps[ns][info.name] = info
		}
	}

	// descDepth counts nested rdf:Description elements; elemDepth counts
	// elements below the innermost Description so that only DIRECT children
	// are recorded as properties. Nested elements are property content
	// (array items, structure fields), not properties of their own.
	descDepth := 0
	elemDepth := 0
	var curNS, curName string // top-level custom property being classified
	curType := ""
	var curStructNS string
	var curStructFields []string
	curUntypeable := false
	var stripKeys []xmpPropKey

	finishProp := func() {
		if curName == "" {
			curStructNS, curStructFields, curUntypeable = "", nil, false
			return
		}
		switch {
		case curUntypeable:
			stripKeys = append(stripKeys, xmpPropKey{nsURI: curNS, name: curName})
		case len(curStructFields) > 0:
			record(curNS, extPropInfo{
				name:         curName,
				valueType:    curName + "Struct",
				structNS:     curStructNS,
				structPrefix: nsPrefixes[curStructNS],
				structFields: curStructFields,
			})
		default:
			if curType == "" || curType == "structPending" {
				curType = "Text"
			}
			record(curNS, extPropInfo{name: curName, valueType: curType})
		}
		curNS, curName, curType = "", "", ""
		curStructNS, curStructFields = "", nil
		curUntypeable = false
	}

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			for _, attr := range t.Attr {
				if attr.Name.Space == "xmlns" && attr.Value != "" {
					nsPrefixes[attr.Value] = attr.Name.Local
				}
			}
			if t.Name.Space == "http://www.w3.org/1999/02/22-rdf-syntax-ns#" && t.Name.Local == "Description" && elemDepth == 0 {
				descDepth++
				// Attribute-form custom properties are always simple values.
				for _, attr := range t.Attr {
					ns := attr.Name.Space
					if ns == "" || ignoredNS[ns] || ns == "xmlns" ||
						!needsExtensionDeclaration(part, ns, attr.Name.Local) {
						continue
					}
					record(ns, extPropInfo{name: attr.Name.Local, valueType: "Text"})
				}
				continue
			}
			if descDepth == 0 {
				continue
			}
			elemDepth++
			switch {
			case elemDepth == 1:
				if !ignoredNS[t.Name.Space] && needsExtensionDeclaration(part, t.Name.Space, t.Name.Local) {
					curNS, curName = t.Name.Space, t.Name.Local
					curType = ""
					// rdf:parseType="Resource" marks an inline structure:
					// capture its field names so the generator can emit a
					// matching pdfaSchema:valueType definition. A declared
					// property's valueType must match its serialized shape,
					// so a structure declared as a simple type fails
					// §6.6.2.3.1 t2, and an undeclared one fails t1.
					for _, attr := range t.Attr {
						if attr.Name.Local == "parseType" {
							curType = "structPending"
						}
					}
				}
			case elemDepth == 2 && curName != "":
				if curType == "" {
					// First child decides the container type. The accepted
					// names mirror the predefined XMP value types the
					// validator recognizes; a non-container child means a
					// structure, handled like the parseType form below.
					if t.Name.Space == "http://www.w3.org/1999/02/22-rdf-syntax-ns#" {
						switch t.Name.Local {
						case "Bag":
							curType = "bag Text"
						case "Seq":
							curType = "seq Text"
						case "Alt":
							curType = "Lang Alt"
						default:
							curType = "structPending"
						}
					} else {
						curType = "structPending"
					}
				}
				if curType == "structPending" || curType == "Text" && len(curStructFields) > 0 {
					// Structure field. Fields spanning more than one
					// namespace cannot be described by a single pdfaType
					// definition; drop the property rather than declare a
					// type whose fields do not all share its namespace.
					if curStructNS == "" {
						curStructNS = t.Name.Space
					}
					if t.Name.Space == curStructNS {
						curStructFields = append(curStructFields, t.Name.Local)
					} else {
						curName, curStructFields = "", nil
					}
				}
			case elemDepth == 3 && curName != "":
				// Items of a seq/bag container. When the container was typed as
				// a flat "seq Text"/"bag Text" but its items are inline
				// structures (rdf:li rdf:parseType="Resource", e.g. Adobe
				// xapTPg:SwatchGroups), that type is wrong: §6.6.2.3.1 t2 rejects
				// a structured value declared as text. Mark the property for
				// stripping rather than declaring an untypeable nested structure.
				if curType == "seq Text" || curType == "bag Text" {
					for _, attr := range t.Attr {
						if attr.Name.Local == "parseType" && attr.Value == "Resource" {
							curUntypeable = true
						}
					}
				}
			}

		case xml.EndElement:
			if t.Name.Space == "http://www.w3.org/1999/02/22-rdf-syntax-ns#" && t.Name.Local == "Description" && elemDepth == 0 {
				descDepth--
				continue
			}
			if descDepth > 0 && elemDepth > 0 {
				if elemDepth == 1 {
					finishProp()
				}
				elemDepth--
			}
		}
	}

	// Build result. Iterate namespaces in sorted URI order so the
	// synthesized fallback prefixes (nsN) - and therefore the emitted XMP
	// bytes - are deterministic across runs.
	schemas := make([]extSchemaInfo, 0, len(nsProps))
	nsURIs := make([]string, 0, len(nsProps))
	for nsURI := range nsProps {
		nsURIs = append(nsURIs, nsURI)
	}
	sort.Strings(nsURIs)
	for _, nsURI := range nsURIs {
		props := nsProps[nsURI]
		prefix := nsPrefixes[nsURI]
		if prefix == "" {
			prefix = "ns" + fmt.Sprintf("%d", len(schemas)+1)
		}
		names := make([]string, 0, len(props))
		for p := range props {
			names = append(names, p)
		}
		sort.Strings(names)
		infos := make([]extPropInfo, 0, len(names))
		for _, n := range names {
			infos = append(infos, props[n])
		}
		schemas = append(schemas, extSchemaInfo{nsURI: nsURI, prefix: prefix, properties: infos})
	}
	sort.Slice(schemas, func(i, j int) bool { return schemas[i].nsURI < schemas[j].nsURI })
	return schemas, stripKeys
}

// stripXMPPropertyKeys removes every occurrence of each property key from the
// XMP bytes, returning the result and the number of properties removed.
func stripXMPPropertyKeys(xmp []byte, keys []xmpPropKey) ([]byte, int) {
	removed := 0
	for _, k := range keys {
		for {
			loc := findXMPProperty(xmp, k.nsURI, k.name)
			if loc.Form == xmpPropertyNotFound {
				break
			}
			xmp = removeXMPProperty(xmp, loc)
			removed++
		}
	}
	return xmp, removed
}

// generateExtensionSchemaBlock builds the pdfaExtension:schemas XML block.
func generateExtensionSchemaBlock(schemas []extSchemaInfo) string {
	var sb strings.Builder
	sb.WriteString("  <pdfaExtension:schemas>\n    <rdf:Bag>\n")

	for _, s := range schemas {
		sb.WriteString("      <rdf:li rdf:parseType=\"Resource\">\n")
		fmt.Fprintf(&sb, "        <pdfaSchema:schema>%s</pdfaSchema:schema>\n", xmlEscape(s.prefix))
		fmt.Fprintf(&sb, "        <pdfaSchema:namespaceURI>%s</pdfaSchema:namespaceURI>\n", xmlEscape(s.nsURI))
		fmt.Fprintf(&sb, "        <pdfaSchema:prefix>%s</pdfaSchema:prefix>\n", xmlEscape(s.prefix))

		if len(s.properties) > 0 {
			sb.WriteString("        <pdfaSchema:property>\n          <rdf:Seq>\n")
			for _, prop := range s.properties {
				sb.WriteString("            <rdf:li rdf:parseType=\"Resource\">\n")
				fmt.Fprintf(&sb, "              <pdfaProperty:name>%s</pdfaProperty:name>\n", xmlEscape(prop.name))
				fmt.Fprintf(&sb, "              <pdfaProperty:valueType>%s</pdfaProperty:valueType>\n", xmlEscape(prop.valueType))
				sb.WriteString("              <pdfaProperty:category>internal</pdfaProperty:category>\n")
				fmt.Fprintf(&sb, "              <pdfaProperty:description>%s</pdfaProperty:description>\n", xmlEscape(prop.name))
				sb.WriteString("            </rdf:li>\n")
			}
			sb.WriteString("          </rdf:Seq>\n        </pdfaSchema:property>\n")
		}

		// Structure-valued properties need a matching valueType
		// definition (ISO 19005-2 §6.6.2.3.3): name, namespace, prefix,
		// description, and one pdfaField entry per observed field. The
		// fields are declared Text, matching their serialized leaves.
		var structs []extPropInfo
		for _, prop := range s.properties {
			if len(prop.structFields) > 0 {
				structs = append(structs, prop)
			}
		}
		if len(structs) > 0 {
			sb.WriteString("        <pdfaSchema:valueType>\n          <rdf:Seq>\n")
			for _, prop := range structs {
				sb.WriteString("            <rdf:li rdf:parseType=\"Resource\">\n")
				fmt.Fprintf(&sb, "              <pdfaType:type>%s</pdfaType:type>\n", xmlEscape(prop.valueType))
				fmt.Fprintf(&sb, "              <pdfaType:namespaceURI>%s</pdfaType:namespaceURI>\n", xmlEscape(prop.structNS))
				fmt.Fprintf(&sb, "              <pdfaType:prefix>%s</pdfaType:prefix>\n", xmlEscape(prop.structPrefix))
				fmt.Fprintf(&sb, "              <pdfaType:description>%s</pdfaType:description>\n", xmlEscape(prop.name))
				sb.WriteString("              <pdfaType:field>\n                <rdf:Seq>\n")
				for _, f := range prop.structFields {
					sb.WriteString("                  <rdf:li rdf:parseType=\"Resource\">\n")
					fmt.Fprintf(&sb, "                    <pdfaField:name>%s</pdfaField:name>\n", xmlEscape(f))
					sb.WriteString("                    <pdfaField:valueType>Text</pdfaField:valueType>\n")
					fmt.Fprintf(&sb, "                    <pdfaField:description>%s</pdfaField:description>\n", xmlEscape(f))
					sb.WriteString("                  </rdf:li>\n")
				}
				sb.WriteString("                </rdf:Seq>\n              </pdfaType:field>\n")
				sb.WriteString("            </rdf:li>\n")
			}
			sb.WriteString("          </rdf:Seq>\n        </pdfaSchema:valueType>\n")
		}

		sb.WriteString("      </rdf:li>\n")
	}

	sb.WriteString("    </rdf:Bag>\n  </pdfaExtension:schemas>\n")
	return sb.String()
}

// injectPropertyOnce inserts element into the packet unless an element with the
// same qualified name is already there.
//
// [findXMPProperty] resolves a property through the rdf:Description that binds
// its namespace, and on packets where it cannot (a schema description mentioning
// the same name, several Descriptions with different bindings) it reports the
// property missing even after one was injected. Every fixpoint iteration then
// injected another: xfa_bug1722030_1.pdf came out with three <pdfaid:part>
// elements and gained three more per remediation. ISO 16684-1 forbids duplicate
// properties on one XMP node, so a literal match is reason enough to stop.
func injectPropertyOnce(buf []byte, qname, element string) ([]byte, bool) {
	if bytes.Contains(buf, []byte("<"+qname+">")) || bytes.Contains(buf, []byte("<"+qname+" ")) {
		return buf, false
	}
	out, err := insertChildElement(buf, element)
	if err != nil {
		return buf, false
	}
	return out, true
}

// stripXPacketAttr removes attr="..." from the <?xpacket ...?> header.
// Handles both single and double quoted values.
//
// The edit is confined to the header: an XMP packet also carries an XML
// declaration, and <?xml version="1.0" encoding="UTF-8"?> contains the very
// attribute name ISO 19005-2, 6.6.2.1 t3 forbids on the xpacket header.
// Editing the first match anywhere in the packet would corrupt that
// declaration instead.
func stripXPacketAttr(data []byte, attr string) []byte {
	s := string(data)
	hdrStart := strings.Index(s, "<?xpacket")
	if hdrStart == -1 {
		return data
	}
	hdrEnd := strings.Index(s[hdrStart:], "?>")
	if hdrEnd == -1 {
		return data
	}
	hdrEnd += hdrStart

	idx := strings.Index(s[hdrStart:hdrEnd], attr+"=")
	if idx == -1 {
		return data
	}
	idx += hdrStart

	rest := s[idx+len(attr)+1:]
	if len(rest) == 0 {
		return data
	}
	quote := rest[0]
	if quote != '"' && quote != '\'' {
		return data
	}
	endIdx := strings.IndexByte(rest[1:], quote)
	if endIdx == -1 {
		return data
	}
	// Remove attr="value" including any leading whitespace.
	start := idx
	for start > 0 && s[start-1] == ' ' {
		start--
	}
	end := idx + len(attr) + 1 + 1 + endIdx + 1
	return []byte(s[:start] + s[end:])
}

// stripForbiddenXPacketAttrs removes the xpacket header attributes ISO 19005-2,
// 6.6.2.1 t2 and t3 forbid on EVERY XMP package, not only the catalog's.
// Returns the packet and the fix notes for whatever it removed.
func stripForbiddenXPacketAttrs(buf []byte) ([]byte, []string) {
	var fixes []string
	for _, attr := range []string{"bytes", "encoding"} {
		out := stripXPacketAttr(buf, attr)
		if !bytes.Equal(out, buf) {
			buf = out
			fixes = append(fixes, fmt.Sprintf("removed %s attribute from xpacket header", attr))
		}
	}
	return buf, fixes
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	// Both quote characters are escaped because a value can land in an
	// attribute, and producers write XMP attributes with either delimiter.
	// A /Keywords of "victims' rights" spliced into a single-quoted
	// pdf:Keywords closed the attribute early and cost the packet its
	// well-formedness (ISO 19005-2, 6.6.2.1 tests 4 and 5).
	s = strings.ReplaceAll(s, "'", "&apos;")
	// A literal carriage return cannot survive a round trip: XML 1.0, 2.11
	// requires a parser to normalise CR and CRLF to a single LF before the
	// application ever sees the value, so an /Info string containing CR can
	// never equal the XMP property parsed back out, and the docinfo-XMP
	// equality rules (ISO 19005-1, 6.7.3) keep failing however faithfully the
	// bytes were written. A numeric character reference is exempt from that
	// normalisation and parses back as CR. govdocs thread_000_000187.pdf has a
	// CRLF inside /Keywords: litho reported the sync as done and both litho and
	// veraPDF still failed 6.7.3 test 5 on the output.
	s = strings.ReplaceAll(s, "\r", "&#xD;")
	return s
}

// numericField validates one fixed-width numeric field of a PDF date and
// reports whether it holds two digits inside the given range. A field that does
// not is a malformed date, not a date to be guessed at.
func numericField(field string, lo, hi int) (string, bool) {
	if len(field) != 2 {
		return "", false
	}
	for _, c := range field {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	n := int(field[0]-'0')*10 + int(field[1]-'0')
	if n < lo || n > hi {
		return "", false
	}
	return field, true
}

// pdfDateToXMP converts a PDF date string (D:YYYYMMDDHHmmSSOHH'mm')
// to an XMP/ISO 8601 date string (YYYY-MM-DDTHH:mm:ss+HH:MM).
// Returns an empty string if the input is unparseable.
func pdfDateToXMP(pdfDate string) string {
	s := strings.TrimPrefix(pdfDate, "D:")
	if len(s) < 4 {
		return ""
	}

	// Extract date/time components with defaults.
	get := func(offset, length int, fallback string) string {
		if offset+length > len(s) {
			return fallback
		}
		return s[offset : offset+length]
	}

	year := get(0, 4, "")
	if len(year) < 4 {
		return ""
	}
	for _, c := range year {
		if c < '0' || c > '9' {
			return ""
		}
	}

	// Every remaining component is a fixed-width numeric field (ISO 32000-1,
	// 7.9.4: D:YYYYMMDDHHmmSSOHH'mm). A producer that writes a shorter field
	// leaves the slice straddling the next separator, and copying it through
	// yields a value no ISO 8601 parser accepts: pdfjs issue18122.pdf carries
	// "D:202403083:4:5 ", whose hour slice is "3:", and litho injected
	// xmp:CreateDate "2024-03-08T3::4::5 Z" -- valid XMP syntax holding an
	// invalid Date, which ISO 19005-1 6.7.9 test 3 then reports. An
	// unparseable Info date yields no XMP date at all; the caller skips the
	// sync on an empty return, which is the right outcome, since writing a
	// wrong date is worse than leaving the property absent.
	month, okM := numericField(get(4, 2, "01"), 1, 12)
	day, okD := numericField(get(6, 2, "01"), 1, 31)
	hour, okH := numericField(get(8, 2, "00"), 0, 23)
	min, okMin := numericField(get(10, 2, "00"), 0, 59)
	// 60 admits a leap second, which ISO 8601 permits.
	sec, okS := numericField(get(12, 2, "00"), 0, 60)
	if !okM || !okD || !okH || !okMin || !okS {
		return ""
	}

	// Parse timezone. Starts at offset 14.
	tz := "Z"
	if len(s) > 14 {
		tzChar := s[14]
		switch tzChar {
		case 'Z':
			tz = "Z"
		case '+', '-':
			tzHour := get(15, 2, "00")
			// Skip the apostrophe between hours and minutes.
			tzMin := "00"
			if len(s) > 18 && s[17] == '\'' {
				tzMin = get(18, 2, "00")
			} else if len(s) > 17 {
				tzMin = get(17, 2, "00")
			}
			tz = string(tzChar) + tzHour + ":" + tzMin
		}
	}

	return year + "-" + month + "-" + day + "T" + hour + ":" + min + ":" + sec + tz
}
