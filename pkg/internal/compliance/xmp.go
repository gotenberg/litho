package compliance

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
	"golang.org/x/text/transform"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// knownXMPNamespaces lists the predefined XMP namespace URIs that do not
// require a PDF/A extension schema declaration.
//
//nolint:gochecknoglobals // static lookup table
var knownXMPNamespaces = map[string]bool{
	// Predefined PDF/A property schemas (ISO 19005, based on XMP Specification).
	"http://purl.org/dc/elements/1.1/":    true, // dc (Dublin Core)
	"http://ns.adobe.com/xap/1.0/":        true, // xmp (XMP Basic)
	"http://ns.adobe.com/xap/1.0/mm/":     true, // xmpMM (Media Management)
	"http://ns.adobe.com/xap/1.0/rights/": true, // xmpRights
	"http://ns.adobe.com/pdf/1.3/":        true, // pdf
	"http://www.aiim.org/pdfa/ns/id/":     true, // pdfaid

	// XMP Specification predefined schemas (ISO 16684-1).
	// These are registered XMP schema namespaces, so properties under
	// them need no PDF/A extension schema declaration.
	"http://ns.adobe.com/photoshop/1.0/":           true, // photoshop
	"http://ns.adobe.com/tiff/1.0/":                true, // tiff
	"http://ns.adobe.com/exif/1.0/":                true, // exif
	"http://ns.adobe.com/exif/1.0/aux/":            true, // exif aux
	"http://ns.adobe.com/camera-raw-settings/1.0/": true, // crs
	"http://ns.adobe.com/xmp/1.0/DynamicMedia/":    true, // xmpDM
	"http://ns.adobe.com/xmp/Identifier/qual/1.0/": true, // xmpidq
	"http://iptc.org/std/Iptc4xmpCore/1.0/xmlns/":  true, // Iptc4xmpCore
	"http://iptc.org/std/Iptc4xmpExt/2008-02-29/":  true, // Iptc4xmpExt

	// XMP value types used by predefined schemas.
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
	"http://ns.adobe.com/pdfx/1.3/":                    true, // pdfx
	"http://ns.adobe.com/AcrobatAdhocWorkflow/1.0/":    true, // adhocwf
	"http://www.aiim.org/pdfua/ns/id/":                 true, // pdfuaid (ISO 14289)

	// RDF/XML structural namespaces (not property schemas).
	"http://www.w3.org/1999/02/22-rdf-syntax-ns#": true, // rdf
	"http://www.w3.org/XML/1998/namespace":        true, // xml
	"adobe:ns:meta/":                              true, // x:xmpmeta
	"http://www.w3.org/2001/XMLSchema-instance":   true, // xsi
	"http://www.w3.org/2001/XMLSchema":            true, // xs
	"http://www.w3.org/2000/xmlns/":               true, // xmlns

	// PDF/A extension schema machinery. The bare family URI
	// "http://www.aiim.org/pdfa/" is deliberately absent: it is not a
	// registered XMP schema namespace, so properties under it need an
	// extension schema like any other custom namespace.
	"http://www.aiim.org/pdfa/ns/extension/": true,
	"http://www.aiim.org/pdfa/ns/schema#":    true,
	"http://www.aiim.org/pdfa/ns/property#":  true,
	"http://www.aiim.org/pdfa/ns/field#":     true,
	"http://www.aiim.org/pdfa/ns/type#":      true,
}

// xmpPart1Properties lists the properties that were defined in the 2004/2005
// XMP Specification (referenced by PDF/A-1). Properties in predefined namespaces
// that are NOT in this map are considered "unknown" for PDF/A-1 and require an
// extension schema declaration. Only namespaces whose property set differs from
// the full registry need entries here.
//
//nolint:gochecknoglobals // static lookup table
var xmpPart1Properties = map[string]map[string]bool{
	// XMP Basic (xmp:): 2004 spec defined all except Rating and Label
	// (Rating was introduced in the 2012 XMP spec).
	"http://ns.adobe.com/xap/1.0/": {
		"Advisory": true, "BaseURL": true, "CreateDate": true,
		"CreatorTool": true, "Identifier": true, "MetadataDate": true,
		"ModifyDate": true, "Nickname": true, "Thumbnails": true,
		// NOT included: Rating, Label (added in later XMP spec versions).
	},
	// Adobe PDF (pdf:): 2004 spec defined these properties.
	// Trapped was in the PDF Info dict but not in the 2004 XMP pdf schema.
	"http://ns.adobe.com/pdf/1.3/": {
		"Keywords": true, "PDFVersion": true, "Producer": true,
		// NOT included: Trapped.
	},
	// EXIF Aux. NOT in the 2004 XMP spec. No properties predefined for part 1.
	"http://ns.adobe.com/exif/1.0/aux/": {},
	// Camera Raw Settings. NOT in the 2004 XMP spec.
	"http://ns.adobe.com/camera-raw-settings/1.0/": {},
	// XMP Dynamic Media. NOT in the 2004 XMP spec.
	"http://ns.adobe.com/xmp/1.0/DynamicMedia/": {},
	// XMP Paged-Text (xmpTPg:): the 2004 XMP Specification referenced by
	// PDF/A-1 predefined only MaxPageSize and NPages. Colorants, Fonts,
	// PlateNames and SwatchGroups require extension schema declarations.
	"http://ns.adobe.com/xap/1.0/t/pg/": {
		"MaxPageSize": true, "NPages": true,
	},
	// PDF/A Identification (pdfaid:): only part, conformance, and amd
	// existed in PDF/A-1 (ISO 19005-1). corr and rev were added later.
	"http://www.aiim.org/pdfa/ns/id/": {
		"part": true, "conformance": true, "amd": true,
	},
}

// xmpPart1TypeOverrides lists properties whose type in the 2004 XMP Specification
// (referenced by PDF/A-1) differs from the type in the current full registry.
//
//nolint:gochecknoglobals // static lookup table
var xmpPart1TypeOverrides = map[string]map[string]xmpPropType{
	// photoshop:SupplementalCategories was Text in 2004, changed to Bag Text later.
	"http://ns.adobe.com/photoshop/1.0/": {
		"SupplementalCategories": xmpPropText,
	},
	// exif:GPSMeasureMode is Integer (2 or 3), not Text.
	"http://ns.adobe.com/exif/1.0/": {
		"GPSMeasureMode": xmpPropInteger,
	},
}

// pdfaExtension namespace URIs for schema, property, field, and type definitions.
const (
	xmpPdfaSchemaPrefix   = "http://www.aiim.org/pdfa/ns/schema#"
	xmpPdfaPropertyPrefix = "http://www.aiim.org/pdfa/ns/property#"
	xmpPdfaTypePrefix     = "http://www.aiim.org/pdfa/ns/type#"
	xmpPdfaFieldPrefix    = "http://www.aiim.org/pdfa/ns/field#"
	xmpPdfaIDNamespace    = "http://www.aiim.org/pdfa/ns/id/"
	xmpRDFNamespace       = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
)

// The only two values an extension schema property's category may take, per
// ISO 19005-1 §6.7.8 t10 and ISO 19005-2/3 §6.6.2.3.3 t9. The comparison is
// verbatim: neither case folding nor whitespace trimming applies, so " external"
// and "External" fail exactly as "sideways" does.
const (
	xmpCategoryExternal = "external"
	xmpCategoryInternal = "internal"
)

// xmpPacketHeaderSize is the maximum number of bytes to scan for the XMP
// processing instruction header.
const xmpPacketHeaderSize = 200

// checkXMPGroup validates XMP metadata compliance for PDF/A profiles.
// For PDF/UA profiles, it returns nil (XMP checks are not applicable).
func checkXMPGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile
	part := vctx.part

	if vctx.ua {
		return nil
	}

	if part == 0 {
		return nil
	}

	doc.Logger.Debug("compliance.xmp: starting XMP checks", "profile", profile, "part", part)

	results := make([]Violation, 0, 8)

	// Catalog /Metadata required (§6.7.2 / §6.6.1).
	xmpData, metaStream := xmpLoadMetadata(ctx, doc, catalog, part, &results)

	// An extension schema the main packet declares legitimises a property in any
	// packet, which is the isDefinedInMainPackage disjunct veraPDF's rule
	// carries, so the other packets are audited against the union of their own
	// declarations and the catalog's.
	var mainExtensionNS map[string]bool
	if len(xmpData) > 0 {
		_, mainExtensionNS = xmpExtractNamespaces(xmpData)
	}

	// The packet tests govern "all content of all XMP packets located in any
	// metadata stream present in the PDF" (ISO 19005-4 §6.7.2.1 t2..t5, ISO
	// 19005-2/3 §6.6.2.1 t2..t5), so they run over every other metadata
	// stream too. Independent of the catalog packet: a file whose catalog
	// packet is absent still has to carry well-formed page packets.
	results = append(results, xmpCheckOtherPackets(ctx, doc, catalog, part, mainExtensionNS)...)

	// §6.7.3 compares the document information dictionary against XMP, and a
	// document carrying no metadata stream at all is one of the ways the two can
	// disagree: the XMP side reads as null, so any Info entry present fails.
	// Run it before the early return below, which exists because the remaining
	// packet checks have nothing to examine without a packet.
	//
	// Gate on the catalog key, not on xmpData: xmpLoadMetadata also returns nil
	// for a /Metadata stream that is present but unusable (unresolvable, wrong
	// /Type or /Subtype, or undecodable). Treating those as "no XMP" reported
	// all eight tests on a file where veraPDF reads the packet successfully and
	// reports two, because veraPDF resolves properties litho failed to load.
	// An unreadable packet is a defect the §6.7.2 rules already report; it is
	// not evidence that every Info entry is unmatched.
	if xmpData == nil {
		if _, hasMetadata := catalog["Metadata"]; !hasMetadata && (part == 1 || part == 4) {
			xmpCheckDocInfoSync(ctx, doc, nil, part, &results)
		}
		return results
	}

	if ctx.Err() != nil {
		return results
	}

	// XMP encoding must be UTF-8 (PDF/A-2/3/4 require it explicitly;
	// PDF/A-1 does not). Normalise to UTF-8 so the downstream XML
	// parser and byte-scan checks see real content even when the
	// packet shipped as UTF-16.
	xmpData = xmpNormalizeForChecks(xmpData, part, &results)

	// A packet that is not well-formed XML fails to parse (PDF/A-2/3
	// §6.6.2.1 t4 + t5). Checked after normalisation so a well-formed
	// UTF-16/32 packet (decoded to UTF-8 here) is not misread as
	// malformed by the UTF-8-only XML parser.
	results = append(results, xmpPacketSerializationValidity(xmpData, part)...)

	if ctx.Err() != nil {
		return results
	}

	// XMP packet header (§6.7.2.1 / §6.6.2.1).
	xmpCheckPacketHeader(xmpData, part, &results)

	if ctx.Err() != nil {
		return results
	}

	// The property-level audits below are scoped to veraPDF's XMPProperty
	// object, which XMPCore builds only from a packet it parsed. A packet that
	// is not well-formed yields none, so it answers for its serialization
	// (reported just above) and for nothing inside it. Reporting a property
	// there named a schema violation in a packet litho could not read.
	packetParses := xmpWellFormed(xmpData)

	// XMP schema validation (§6.7.9 / §6.6.2.3.1).
	if packetParses {
		xmpCheckNamespaces(xmpData, part, &results)
	}

	if ctx.Err() != nil {
		return results
	}

	// Property-level predefined-schema audit (§6.7.9 / §6.6.2.3.1).
	if packetParses {
		xmpCheckPredefinedProperties(ctx, xmpData, part, nil, &results)
	}

	if ctx.Err() != nil {
		return results
	}

	// PDF/A identification (§6.7.11 / §6.6.4 / §6.7.3).
	xmpCheckPDFAIdentification(ctx, xmpData, profile, part, &results)

	if ctx.Err() != nil {
		return results
	}

	// Extension schema structure (§6.7.8 / §6.6.2.3.3).
	xmpCheckExtensionSchemas(ctx, xmpData, part, &results)

	if ctx.Err() != nil {
		return results
	}

	// Extension property valueType correspondence (§6.6.2.3.1 t02).
	xmpCheckExtensionPropertyTypes(xmpData, part, &results)

	if ctx.Err() != nil {
		return results
	}

	// Usage-side value-type resolution for extension-declared namespaces
	// (§6.7.9 t3 / §6.6.2.3.1 t2).
	xmpCheckExtensionPropertyResolution(xmpData, part, &results)

	if ctx.Err() != nil {
		return results
	}

	// DocInfo to XMP consistency (§6.7.3), parts 1 and 4 only.
	if part == 1 || part == 4 {
		xmpCheckDocInfoSync(ctx, doc, xmpData, part, &results)
	}

	if ctx.Err() != nil {
		return results
	}

	// XMP property types (§6.7.2 / §6.6.2.3.1).
	xmpCheckPropertyTypes(xmpData, part, metaStream, nil, &results)

	if ctx.Err() != nil {
		return results
	}

	// Structured value-type field names (§6.7.9 t3 / §6.6.2.3.1 t2).
	xmpCheckStructFieldNames(xmpData, part, &results)

	doc.Logger.Debug("compliance.xmp: XMP checks complete", "violations", len(results))
	return results
}

// xmpLoadMetadata retrieves and decodes the XMP metadata stream from the
// catalog. It validates that the stream dict has /Type Metadata and /Subtype XML.
func xmpLoadMetadata(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, results *[]Violation) ([]byte, ast.PdfStream) {
	mdObj, ok := catalog["Metadata"]
	if !ok {
		*results = append(*results, xmpMetadataRule(part)...)
		return nil, ast.PdfStream{}
	}

	stream, err := document.ResolveStream(ctx, doc, mdObj)
	if err != nil || stream.Dict == nil {
		doc.Logger.Debug("compliance.xmp: failed to resolve /Metadata stream", "error", err)
		*results = append(*results, xmpMetadataRule(part)...)
		return nil, ast.PdfStream{}
	}

	// Validate stream dict entries: Type=Metadata, Subtype=XML.
	typeName, _ := document.ResolveName(ctx, doc, stream.Dict["Type"])
	subtypeName, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])

	if typeName != "Metadata" || subtypeName != "XML" {
		*results = append(*results, xmpMetadataRule(part)...)
		return nil, ast.PdfStream{}
	}

	// For part 1, the metadata stream must not have a /Filter key.
	if part == 1 {
		if _, hasFilter := stream.Dict["Filter"]; hasFilter {
			*results = append(*results, fail(Rule_19005_1_6_7_2_t02, "catalog metadata stream has Filter key")...)
		}
	}

	// Decode the stream content: apply the stream's filters (a /Metadata stream
	// may legitimately be Flate-encoded in parts 2+). The XMP well-formedness,
	// encoding and property checks all run against the decoded XML packet, as
	// veraPDF does; reading the raw (still-compressed) bytes made a filtered
	// packet look like malformed XML (a spurious §6.6.2.1 t4/t5). Fall back to
	// the raw bytes only when decoding fails, so an undecodable stream still
	// gets checked rather than skipped.
	xmpData, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(xmpData) == 0 {
		raw, rawErr := stream.Bytes(ctx)
		if rawErr != nil || len(raw) == 0 {
			*results = append(*results, xmpMetadataRule(part)...)
			return nil, ast.PdfStream{}
		}
		xmpData = raw
	}

	if len(xmpData) == 0 {
		*results = append(*results, xmpMetadataRule(part)...)
		return nil, ast.PdfStream{}
	}

	return xmpData, stream
}

// maxXMPMetadataStreams bounds how many distinct metadata streams the
// non-catalog packet scan decodes. Every dictionary in the file may carry a
// /Metadata key and each accepted one costs a stream decode plus an XML parse,
// so a crafted file that hangs a metadata stream off thousands of objects
// would otherwise turn one validation into an unbounded decode loop. This is
// an anti-DoS guard, not spec-mandated: real producers attach XMP to the
// catalog and, at most, to each page, XObject or embedded file, so 4096
// packets covers even a per-page-XMP book.
const maxXMPMetadataStreams = 4096

// maxXMPContainerDepth bounds the descent into a container object's direct
// (non-reference) values while looking for /Metadata keys. Indirect objects
// are visited by the outer scan, so this recursion only ever walks the
// direct value tree of a single object, which cannot cycle. This is an
// anti-DoS guard, not spec-mandated: a real dictionary nests a handful of
// levels, so 32 leaves genuine documents untouched while capping the work a
// deeply nested crafted object can force.
const maxXMPContainerDepth = 32

// xmpCheckOtherPackets applies the packet-level XMP tests to every metadata
// stream in the file other than the document catalog's. ISO 19005-4 §6.7.2.1
// and ISO 19005-2/3 §6.6.2.1 scope those tests to "all content of all XMP
// packets located in any metadata stream present in the PDF", so a packet
// hanging off a page, XObject, annotation or embedded file is in scope for the
// header attributes (t2, t3), well-formedness (t4) and UTF-8 encoding (t5).
//
// ISO 19005-1 carries no such scope: veraPDF models a non-catalog packet as an
// XMPPackage only outside part 1 (GFPDMetadata.getXMPPackage guards the branch
// with !PDFFlavours.isFlavourPart(flavour, ISO_19005_1)), and a part-1 run over
// a page packet carrying bytes= reports nothing while the same attribute in the
// catalog packet reports §6.7.5 t1. Part 1 therefore returns early: widening it
// would raise violations no PDF/A-1 validator raises.
func xmpCheckOtherPackets(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, mainExtensionNS map[string]bool) []Violation {
	if part == 1 {
		return nil
	}

	metaNums := xmpCollectMetadataStreams(ctx, doc, catalog)
	if len(metaNums) == 0 {
		return nil
	}

	var results []Violation
	for _, objNum := range metaNums {
		if ctx.Err() != nil {
			return results
		}
		data := xmpPacketBytes(ctx, doc, objNum)
		if len(data) == 0 {
			continue
		}

		packet := make([]Violation, 0, 4)
		data = xmpNormalizeForChecks(data, part, &packet)
		serialization := xmpPacketSerializationValidity(data, part)
		packet = append(packet, serialization...)
		xmpCheckPacketHeader(data, part, &packet)

		// The property-level audits apply to every packet, not only the
		// catalog's: ISO 19005-2 §6.6.2.3.1 constrains the properties of "all
		// metadata streams present in the PDF". pdfcpu adobeImplOfPDFSpec.pdf
		// and adobe_errata.pdf each hang a 23 KB packet off a marked-content
		// property list carrying photoshop:History, which is not in the XMP 2005
		// Photoshop schema, and veraPDF reports it under t1 and t2 while litho
		// looked only at the catalog packet. A packet that does not parse has
		// nothing to audit, so those run only when the serialization check is
		// silent.
		if len(serialization) == 0 {
			xmpCheckPredefinedProperties(ctx, data, part, mainExtensionNS, &packet)
			xmpCheckPropertyTypes(data, part, ast.PdfStream{}, mainExtensionNS, &packet)
		}

		for _, v := range packet {
			results = append(results, xmpPacketViolation(v, objNum))
		}
	}
	return results
}

// xmpCollectMetadataStreams returns the object numbers of the metadata streams
// referenced by any object in the file except the catalog's own packet, sorted
// ascending so the emitted violations keep a stable order. Each stream is
// reported once however many objects reference it.
func xmpCollectMetadataStreams(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []int {
	// The catalog packet is the document's main packet and is checked by the
	// caller. veraPDF skips it the same way, by /Type: GFCosDict.getMetadata
	// builds a non-main PDMetadata only when the container's /Type is not
	// /Catalog. Matching on the reference as well covers a catalog dict that
	// omits /Type, which would otherwise be reported twice.
	catalogMetaNum := -1
	if ref, ok := catalog["Metadata"].(ast.PdfIndirectRef); ok {
		catalogMetaNum = ref.ObjectNum
	}

	seen := make(map[int]bool)
	var metaNums []int

	for _, objNum := range sortedEntryNums(doc) {
		if ctx.Err() != nil || len(metaNums) >= maxXMPMetadataStreams {
			break
		}
		if doc.Index.Entries[objNum].Free {
			continue
		}
		obj, err := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			doc.Logger.Debug("compliance.xmp: cannot resolve object for metadata scan", "object", objNum, "error", err)
			continue
		}
		xmpCollectMetadataRefs(obj, catalogMetaNum, seen, &metaNums, 0)
	}

	slices.Sort(metaNums)
	return metaNums
}

// xmpCollectMetadataRefs appends the object number of obj's /Metadata
// reference, and of those of the direct dictionaries nested inside it, to
// metaNums. Indirect references other than the /Metadata value itself are not
// followed: the caller already visits every indirect object, so the descent
// stays inside one object's direct value tree and terminates.
func xmpCollectMetadataRefs(obj ast.PdfObject, catalogMetaNum int, seen map[int]bool, metaNums *[]int, depth int) {
	if depth > maxXMPContainerDepth || len(*metaNums) >= maxXMPMetadataStreams {
		return
	}

	var dict ast.PdfDict
	switch v := obj.(type) {
	case ast.PdfDict:
		dict = v
	case ast.PdfStream:
		dict = v.Dict
	case ast.PdfArray:
		for _, elem := range v {
			xmpCollectMetadataRefs(elem, catalogMetaNum, seen, metaNums, depth+1)
		}
		return
	default:
		return
	}

	if typeName, _ := dict["Type"].(ast.PdfName); typeName != "Catalog" {
		if ref, ok := dict["Metadata"].(ast.PdfIndirectRef); ok &&
			ref.ObjectNum != catalogMetaNum && !seen[ref.ObjectNum] {
			seen[ref.ObjectNum] = true
			*metaNums = append(*metaNums, ref.ObjectNum)
		}
	}

	for _, key := range slices.Sorted(maps.Keys(dict)) {
		if key == "Metadata" {
			continue
		}
		xmpCollectMetadataRefs(dict[key], catalogMetaNum, seen, metaNums, depth+1)
	}
}

// xmpPacketBytes returns the decoded XMP packet held by the metadata stream
// objNum, or nil when the object is not an XMP metadata stream or its bytes
// cannot be decoded. The /Type and /Subtype gate mirrors veraPDF's
// PDMetadata.isMetadataObject, which accepts a stream only when /Type is
// /Metadata and /Subtype is /XML. An undecodable stream yields nil rather than
// its raw bytes: reporting a malformed packet litho merely failed to decode
// would be a false positive.
func xmpPacketBytes(ctx context.Context, doc *document.Document, objNum int) []byte {
	stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
	if err != nil || stream.Dict == nil {
		doc.Logger.Debug("compliance.xmp: cannot resolve metadata stream", "object", objNum, "error", err)
		return nil
	}

	typeName, _ := document.ResolveName(ctx, doc, stream.Dict["Type"])
	subtypeName, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
	if typeName != "Metadata" || subtypeName != "XML" {
		return nil
	}

	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance.xmp: cannot decode metadata stream", "object", objNum, "error", err)
		return nil
	}
	return data
}

// xmpPacketViolation names the metadata stream a packet violation came from,
// so a report covering several packets stays traceable to an object.
func xmpPacketViolation(v Violation, objNum int) Violation {
	if v.Context == "" {
		v.Context = fmt.Sprintf("object %d: XMP packet", objNum)
		return v
	}
	v.Context = fmt.Sprintf("object %d: %s", objNum, v.Context)
	return v
}

// xmpMetadataRule returns the correct rule for the missing/invalid metadata
// stream based on the PDF/A part number.
func xmpMetadataRule(part int) []Violation {
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_2_t01, "catalog missing or invalid /Metadata stream")
	case 2:
		return fail(Rule_19005_2_6_6_2_1_t01, "catalog missing or invalid /Metadata stream")
	case 3:
		return fail(Rule_19005_3_6_6_2_1_t01, "catalog missing or invalid /Metadata stream")
	case 4:
		return fail(Rule_19005_4_6_7_2_1_t01, "catalog missing or invalid /Metadata stream")
	}
	return nil
}

// xmpCheckPacketHeader checks that the XMP packet header does not use the
// forbidden 'bytes' or 'encoding' attributes.
func xmpCheckPacketHeader(data []byte, part int, results *[]Violation) {
	header := data
	if len(header) > xmpPacketHeaderSize {
		header = header[:xmpPacketHeaderSize]
	}

	// Both rules judge the xpacket processing instruction, not whatever
	// instruction comes first. A packet may legitimately open with an XML
	// declaration, and the encoding= that declaration carries is its own;
	// cutting at the first "?>" read the declaration as the packet header and
	// reported an attribute the xpacket instruction does not carry. A packet
	// with no xpacket header at all carries neither attribute.
	start := bytes.Index(header, []byte("<?xpacket"))
	if start < 0 {
		return
	}

	before, _, ok := bytes.Cut(header[start:], []byte("?>"))
	if !ok {
		return
	}
	pi := before

	if bytes.Contains(pi, []byte("bytes=")) {
		*results = append(*results, xmpPacketHeaderBytesRule(part)...)
	}

	if bytes.Contains(pi, []byte("encoding=")) {
		*results = append(*results, xmpPacketHeaderEncodingRule(part)...)
	}
}

func xmpPacketHeaderBytesRule(part int) []Violation {
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_5_t01, "")
	case 2:
		return fail(Rule_19005_2_6_6_2_1_t02, "")
	case 3:
		return fail(Rule_19005_3_6_6_2_1_t02, "")
	case 4:
		return fail(Rule_19005_4_6_7_2_1_t02, "")
	}
	return nil
}

func xmpPacketHeaderEncodingRule(part int) []Violation {
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_5_t02, "")
	case 2:
		return fail(Rule_19005_2_6_6_2_1_t03, "")
	case 3:
		return fail(Rule_19005_3_6_6_2_1_t03, "")
	case 4:
		return fail(Rule_19005_4_6_7_2_1_t03, "")
	}
	return nil
}

// xmpPacketSerializationValidity fires PDF/A-2/3 §6.6.2.1 t4
// (isSerializationValid) and t5 (actualEncoding), and the PDF/A-4 §6.7.2.1
// t4/t5 equivalents, together when the XMP packet is not well-formed XML. A
// packet that does not parse has no valid serialization (t4) and no
// determinable encoding (t5): both derive from the same parse failure. litho
// parses XMP leniently everywhere else, so this dedicated strict pass catches a
// not-well-formed packet (for example a start/end tag whose case does
// not match). PDF/A-1 §6.7.9 t01 carries only the well-formedness half (there
// is no part-1 UTF-8/encoding equivalent), so part 1 returns a single rule.
func xmpPacketSerializationValidity(xmpData []byte, part int) []Violation {
	if part == 1 {
		if xmpWellFormed(xmpData) {
			return nil
		}
		return failf(Rule_19005_1_6_7_9_t01, "XMP packet is not well-formed XML")
	}
	var t4, t5 Rule
	switch part {
	case 2:
		t4, t5 = Rule_19005_2_6_6_2_1_t04, Rule_19005_2_6_6_2_1_t05
	case 3:
		t4, t5 = Rule_19005_3_6_6_2_1_t04, Rule_19005_3_6_6_2_1_t05
	case 4:
		t4, t5 = Rule_19005_4_6_7_2_1_t04, Rule_19005_4_6_7_2_1_t05
	default:
		return nil
	}
	if xmpWellFormed(xmpData) {
		return nil
	}
	results := failf(t4, "XMP packet is not well-formed XML")
	return append(results, failf(t5, "XMP packet encoding cannot be determined: packet is not well-formed XML")...)
}

// XMPWellFormed reports whether data is well-formed XML, the same test
// §6.7.2.1 t04 / §6.6.2.1 t04 use to flag a metadata packet. The
// remediation XMP fixer shares it so it never byte-patches a malformed
// packet, which can shift how a lenient reader parses pdfaid:part and
// pdfaid:rev and introduce §6.7.3 violations the input did not have.
func XMPWellFormed(data []byte) bool {
	return xmpWellFormed(data)
}

// xmpPassThroughCharset hands the decoder its own bytes back whatever encoding
// the packet declares.
//
// [xmpNormalizeForChecks] has already decoded a genuinely wide packet to UTF-8
// by the time any of these decoders run, so the bytes here are UTF-8 and the
// declaration is stale. Go's encoding/xml refuses outright when a declaration
// names anything but UTF-8 and no CharsetReader is set, which cost a UTF-8
// packet declaring encoding="UTF-16" both its serialization verdict and its
// whole PDF/A identification. veraPDF's XMPCore reads the bytes and reports
// neither, because a declaration is not evidence about the bytes.
func xmpPassThroughCharset(_ string, input io.Reader) (io.Reader, error) {
	return input, nil
}

// xmpWellFormed reports whether data is well-formed XML. An XMP packet
// is an XML document, so a packet that is not well-formed XML cannot be
// a valid serialization.
func xmpWellFormed(data []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(xmpRepairForParse(data)))
	dec.CharsetReader = xmpPassThroughCharset
	dec.Strict = true
	declared := map[string]bool{xmlBuiltinNamespace: true}
	rooted := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			// XML 1.0, 2.1 makes the root element part of the document
			// production, so bytes carrying no element at all are not a
			// well-formed document however cleanly they tokenise. Go's
			// encoding/xml reads any element-free input as one run of character
			// data and reports no error, so binary that is not XML in any sense
			// passed: pdfjs bug1826783.pdf carries 1536 bytes of ciphertext as
			// its /Metadata, which veraPDF rejects under ISO 19005-2 §6.6.2.1
			// t4 and litho called well-formed.
			return rooted
		}
		if err != nil {
			return false
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		rooted = true
		if hasDuplicateAttr(start) {
			return false
		}
		if usesUndeclaredPrefix(start, declared) {
			return false
		}
	}
}

// xmlBuiltinNamespace is the URI the xml prefix is bound to without any
// declaration (Namespaces in XML 1.0, §3).
const xmlBuiltinNamespace = "http://www.w3.org/XML/1998/namespace"

// usesUndeclaredPrefix reports whether start names anything through a prefix no
// declaration binds, recording the declarations start itself carries.
//
// Namespaces in XML 1.0, §3 requires every prefix to be declared, and veraPDF's
// XMPCore treats a violation as a parse failure. Go's encoding/xml does not:
// it leaves the raw prefix in Name.Space instead of a URI and reads on, so a
// packet naming <zz:bogus> with no xmlns:zz parsed cleanly here. That is the
// signal this uses, because a resolved name always carries the declared URI.
func usesUndeclaredPrefix(start xml.StartElement, declared map[string]bool) bool {
	for _, attr := range start.Attr {
		// xmlns:foo="uri" arrives as space "xmlns", local "foo"; the default
		// declaration xmlns="uri" as local "xmlns" with no space.
		if attr.Name.Space == "xmlns" || (attr.Name.Space == "" && attr.Name.Local == "xmlns") {
			declared[attr.Value] = true
		}
	}
	if start.Name.Space != "" && !declared[start.Name.Space] {
		return true
	}
	for _, attr := range start.Attr {
		if attr.Name.Space == "" || attr.Name.Space == "xmlns" {
			continue
		}
		if !declared[attr.Name.Space] {
			return true
		}
	}
	return false
}

// hasDuplicateAttr reports whether start carries the same attribute twice.
//
// XML 1.0, §3.1 (WFC: Unique Att Spec) forbids a name appearing more than once
// in a start tag, and Namespaces in XML 1.0, §5.3 extends that to two prefixes
// bound to the same URI. Go's encoding/xml accepts both, so a packet carrying
// `rdf:about` twice parsed cleanly here while veraPDF's XMPCore rejected it,
// and litho reported neither the invalid serialization nor the identification
// the unparseable packet takes with it.
func hasDuplicateAttr(start xml.StartElement) bool {
	if len(start.Attr) < 2 {
		return false
	}
	seen := make(map[xml.Name]bool, len(start.Attr))
	for _, attr := range start.Attr {
		if seen[attr.Name] {
			return true
		}
		seen[attr.Name] = true
	}
	return false
}

// xmpRepairForParse returns data with everything veraPDF repairs before it
// parses a packet: a C0 control character (see [xmpFixControlChars]) and a byte
// run that is not valid UTF-8.
//
// XML 1.0 (Fifth Edition) §2.2 admits only characters, so a malformed byte run
// is not well-formed by the letter of the clause. XMPCore relaxes exactly this:
// measured against veraPDF 1.30.2, a packet carrying a raw 0xFF 0xFE run in
// character data draws neither an invalid-serialization verdict (§6.6.2.1 t4 /
// §6.7.2.1 t4 / §6.7.9 t1) nor an encoding one (t5), and its PDF/A
// identification still reads. Refusing to parse cost litho all three.
func xmpRepairForParse(data []byte) []byte {
	return bytes.ToValidUTF8(xmpFixControlChars(data), []byte(" "))
}

// maxXMPCharRefDigits bounds the digit run a numeric character reference may
// carry before [xmpFixControlChars] stops looking for its terminator. It
// mirrors the 8-character pushback buffer of veraPDF's
// FixASCIIControlsReader, and doubles as an anti-DoS guard, not spec-mandated:
// no legal reference needs more digits, so a crafted "&#000...0;" run cannot
// force an unbounded scan.
const maxXMPCharRefDigits = 8

// xmpFixControlChars replaces every character veraPDF repairs before it calls
// a packet unparseable: a raw C0 control character, and a numeric character
// reference denoting one, both become a space.
//
// XML 1.0 (Third Edition) §2.2 excludes those characters from the Char
// production, so a packet carrying one is not well-formed by the letter of
// §6.6.2.1 t4 / §6.7.2.1 t4. veraPDF relaxes exactly this: its default
// ParseOptions sets FIX_CONTROL_CHARS (ParseOptions.<init> sets bits 8|16), so
// XMPMetaParser re-parses through FixASCIIControlsReader, which substitutes a
// space for any character matching Utils.isControlChar (c <= 0x1F or c ==
// 0x7F, excluding tab, LF and CR). Real producers emit these: Adobe Illustrator
// writes dc:description values holding "&#x5;" (seen on the govdocs corpus),
// and reporting them as malformed packets diverges from every validator built
// on XMPCore.
func xmpFixControlChars(data []byte) []byte {
	if !bytes.ContainsFunc(data, func(r rune) bool { return xmpIsControlChar(int(r)) }) &&
		!bytes.Contains(data, []byte("&#")) {
		return data
	}

	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		if xmpIsControlChar(int(data[i])) {
			out = append(out, ' ')
			i++
			continue
		}
		if value, width, ok := xmpCharRef(data[i:]); ok && xmpIsControlChar(value) {
			out = append(out, ' ')
			i += width
			continue
		}
		out = append(out, data[i])
		i++
	}
	return out
}

// xmpIsControlChar mirrors org.verapdf.xmp.impl.Utils.isControlChar, the
// predicate veraPDF's parser uses to decide a character needs repairing.
func xmpIsControlChar(c int) bool {
	return (c <= 0x1F || c == 0x7F) && c != '\t' && c != '\n' && c != '\r'
}

// xmpCharRef decodes the numeric character reference starting at data[0],
// returning its value and total byte width. Named entities, malformed
// references and references longer than [maxXMPCharRefDigits] digits report
// false and are left untouched.
func xmpCharRef(data []byte) (value int, width int, ok bool) {
	if len(data) < 4 || data[0] != '&' || data[1] != '#' {
		return 0, 0, false
	}
	pos, base := 2, 10
	if data[pos] == 'x' || data[pos] == 'X' {
		pos, base = pos+1, 16
	}
	digits := 0
	for pos < len(data) && digits <= maxXMPCharRefDigits {
		d := int(data[pos])
		switch {
		case d >= '0' && d <= '9':
			d -= '0'
		case base == 16 && d >= 'a' && d <= 'f':
			d = d - 'a' + 10
		case base == 16 && d >= 'A' && d <= 'F':
			d = d - 'A' + 10
		case d == ';' && digits > 0:
			return value, pos + 1, true
		default:
			return 0, 0, false
		}
		value = value*base + d
		digits++
		pos++
	}
	return 0, 0, false
}

// xmpCheckEncoding verifies that the XMP packet is encoded as UTF-8.
// For PDF/A-4 (§6.7.2.1 t05), UTF-16 encoding is a violation.
// xmpDetectEncoding returns the byte encoding of an XMP packet. The
// PDF/A family requires UTF-8 from part 2 onward, so anything else
// here triggers a clause violation. Detection looks at:
//
//   - A leading byte-order mark (00 00 FE FF for UTF-32BE,
//     FF FE 00 00 for UTF-32LE, FE FF for UTF-16BE,
//     FF FE for UTF-16LE).
//   - A BOM-less heuristic: regular zero stride (3 zero bytes per
//     code unit for UTF-32, 1 zero byte per code unit for UTF-16).
//   - An XML declaration "encoding=…" hint inside the first
//     [xmpPacketHeaderSize] bytes.
//
// Returns one of "UTF-8", "UTF-16BE", "UTF-16LE", "UTF-32BE",
// "UTF-32LE". An empty input is treated as UTF-8 so the caller does
// not double-report a missing metadata stream.
func xmpDetectEncoding(data []byte) string {
	// 4-byte BOMs first; FF FE 00 00 also matches the 2-byte UTF-16 LE
	// BOM, so the longer prefix has to win.
	if len(data) >= 4 {
		switch {
		case data[0] == 0x00 && data[1] == 0x00 && data[2] == 0xFE && data[3] == 0xFF:
			return "UTF-32BE"
		case data[0] == 0xFF && data[1] == 0xFE && data[2] == 0x00 && data[3] == 0x00:
			return "UTF-32LE"
		}
	}
	if len(data) >= 2 {
		switch {
		case data[0] == 0xFE && data[1] == 0xFF:
			return "UTF-16BE"
		case data[0] == 0xFF && data[1] == 0xFE:
			return "UTF-16LE"
		}
	}

	// BOM-less heuristics. UTF-32 patterns must come before UTF-16
	// patterns: a UTF-32 LE byte stream "X 00 00 00" also satisfies
	// the UTF-16 LE pattern "X 00".
	if len(data) >= 8 {
		switch {
		case data[0] != 0 && data[1] == 0 && data[2] == 0 && data[3] == 0 &&
			data[4] != 0 && data[5] == 0 && data[6] == 0 && data[7] == 0:
			return "UTF-32LE"
		case data[0] == 0 && data[1] == 0 && data[2] == 0 && data[3] != 0 &&
			data[4] == 0 && data[5] == 0 && data[6] == 0 && data[7] != 0:
			return "UTF-32BE"
		}
	}
	if len(data) >= 4 {
		switch {
		case data[0] != 0 && data[1] == 0 && data[2] != 0 && data[3] == 0:
			return "UTF-16LE"
		case data[0] == 0 && data[1] != 0 && data[2] == 0 && data[3] != 0:
			return "UTF-16BE"
		}
	}

	headerLen := min(len(data), xmpPacketHeaderSize)

	// A declaration is only worth consulting when the bytes could be wide. XML
	// opens with "<?xml" or "<?xpacket", so a UTF-16 or UTF-32 packet
	// necessarily interleaves NUL bytes through that syntax, and a prefix
	// carrying none is UTF-8 whatever it claims. Believing the declaration
	// instead decoded a UTF-8 packet as UTF-16, which cost it its encoding
	// verdict and its whole identification; veraPDF's XMPCore reports neither.
	if !bytes.ContainsRune(data[:headerLen], 0) {
		return "UTF-8"
	}

	header := strings.ToLower(string(data[:headerLen]))
	switch {
	case strings.Contains(header, "encoding=\"utf-32le") || strings.Contains(header, "encoding='utf-32le"):
		return "UTF-32LE"
	case strings.Contains(header, "encoding=\"utf-32be") || strings.Contains(header, "encoding='utf-32be") ||
		strings.Contains(header, "encoding=\"utf-32") || strings.Contains(header, "encoding='utf-32"):
		return "UTF-32BE"
	case strings.Contains(header, "encoding=\"utf-16le") || strings.Contains(header, "encoding='utf-16le"):
		return "UTF-16LE"
	case strings.Contains(header, "encoding=\"utf-16be") || strings.Contains(header, "encoding='utf-16be") ||
		strings.Contains(header, "encoding=\"utf-16") || strings.Contains(header, "encoding='utf-16"):
		return "UTF-16BE"
	}
	return "UTF-8"
}

// xmpDecodeToUTF8 returns data re-encoded as UTF-8 from the encoding
// reported by [xmpDetectEncoding]. UTF-8 input passes through. The
// BOM, if present, is stripped so downstream string operations do not
// see a stray U+FEFF in the first character.
func xmpDecodeToUTF8(data []byte, enc string) ([]byte, error) {
	switch enc {
	case "UTF-8":
		return data, nil
	case "UTF-16BE", "UTF-16LE":
		order := unicode.BigEndian
		if enc == "UTF-16LE" {
			order = unicode.LittleEndian
		}
		dec := unicode.UTF16(order, unicode.UseBOM).NewDecoder()
		out, _, err := transform.Bytes(dec, data)
		return out, err
	case "UTF-32BE", "UTF-32LE":
		order := utf32.BigEndian
		if enc == "UTF-32LE" {
			order = utf32.LittleEndian
		}
		dec := utf32.UTF32(order, utf32.UseBOM).NewDecoder()
		out, _, err := transform.Bytes(dec, data)
		return out, err
	}
	return data, nil
}

// xmpEncodingRule returns the PDF/A clause that requires the XMP
// packet to be UTF-8 for the given part. PDF/A-1 (ISO 19005-1) does
// not mandate UTF-8 explicitly, so part 1 returns nil and the caller
// suppresses the violation.
func xmpEncodingRule(part int, encoding string) []Violation {
	switch part {
	case 2:
		return failf(Rule_19005_2_6_6_2_1_t05, "XMP encoded as %s, must be UTF-8", encoding)
	case 3:
		return failf(Rule_19005_3_6_6_2_1_t05, "XMP encoded as %s, must be UTF-8", encoding)
	case 4:
		return failf(Rule_19005_4_6_7_2_1_t05, "XMP encoded as %s, must be UTF-8", encoding)
	}
	return nil
}

// xmpNormalizeForChecks emits the encoding-violation rule when [data]
// is non-UTF-8, then returns the UTF-8 form so subsequent checks
// (packet header, namespace audit, identification) can parse and
// validate the values inside the packet. A decoding failure leaves
// the bytes untouched; downstream checks degrade gracefully (xml
// parser bails, byte-scan fallback finds nothing).
func xmpNormalizeForChecks(data []byte, part int, results *[]Violation) []byte {
	enc := xmpDetectEncoding(data)
	if enc == "UTF-8" {
		return data
	}
	*results = append(*results, xmpEncodingRule(part, enc)...)
	decoded, err := xmpDecodeToUTF8(data, enc)
	if err != nil || len(decoded) == 0 {
		return data
	}
	return decoded
}

// xmpCheckNamespaces walks the XMP XML and verifies that every namespace URI
// used by properties is either a predefined XMP schema or is declared in a
// PDF/A extension schema within the same XMP packet.
func xmpCheckNamespaces(data []byte, part int, results *[]Violation) {
	usedNamespaces, extensionNamespaces := xmpExtractNamespaces(data)

	for ns := range usedNamespaces {
		if knownXMPNamespaces[ns] || extensionNamespaces[ns] {
			continue
		}
		*results = append(*results, xmpSchemaRule(part, ns)...)
	}
}

// xmpExtractNamespaces parses XMP XML and returns the set of namespace URIs
// used by top-level properties (direct element children of rdf:Description
// plus attribute-form properties on the Description tag) and the set of
// namespace URIs declared in extension schemas. Nested elements are
// excluded deliberately: ISO 19005 §6.7.9 / §6.6.2.3.1 govern
// properties (direct schema children), not the fields of a structured
// value, so a structure field's namespace needs no declaration of its
// own and flagging it is a false positive.
func xmpExtractNamespaces(data []byte) (used map[string]bool, extensions map[string]bool) {
	used = make(map[string]bool)
	extensions = make(map[string]bool)

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = xmpPassThroughCharset
	decoder.Strict = false

	// ignoredNS lists structural namespaces that should not be counted as
	// property namespaces.
	ignoredNS := map[string]bool{
		"":                                     true,
		"xmlns":                                true,
		xmpRDFNamespace:                        true,
		"http://www.w3.org/XML/1998/namespace": true,
		"http://www.w3.org/2000/xmlns/":        true,
		"http://www.w3.org/2001/XMLSchema-instance": true,
		"http://www.w3.org/2001/XMLSchema":          true,
		"adobe:ns:meta/":                            true,
	}

	var inExtensionSchema bool
	descDepth := 0 // count of enclosing top-level rdf:Description elements
	elemDepth := 0 // element depth below the innermost top-level Description

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:

			if t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description" && elemDepth == 0 {
				descDepth++
				// Attribute-form properties on the Description tag are
				// top-level properties.
				for _, attr := range t.Attr {
					if !ignoredNS[attr.Name.Space] {
						used[attr.Name.Space] = true
					}
				}
				continue
			}

			if descDepth > 0 {
				elemDepth++
				// Only a direct child of a top-level rdf:Description is a
				// property; deeper elements are array items and structure
				// fields, which carry no namespace-declaration obligation.
				if elemDepth == 1 && !ignoredNS[t.Name.Space] {
					used[t.Name.Space] = true
				}
			}

			// Track extension schema namespace declarations at any depth.
			if t.Name.Local == "schemas" && strings.Contains(t.Name.Space, "pdfa/ns/extension") {
				inExtensionSchema = true
			}
			if inExtensionSchema && t.Name.Local == "namespaceURI" {
				if charTok, charErr := decoder.Token(); charErr == nil {
					if charData, ok := charTok.(xml.CharData); ok {
						ns := strings.TrimSpace(string(charData))
						if ns != "" {
							extensions[ns] = true
						}
					}
				}
			}

			// RDF/XML §2.5 abbreviates a property as an attribute on the node
			// element, so an iText-written Factur-X schema declares its URI as
			// pdfaSchema:namespaceURI="..." on a nested rdf:Description. Reading
			// only the child-element spelling left the declared namespace
			// unknown, reporting a false ISO 19005-2/3 §6.6.2.3.1 t1 on every
			// property that used it.
			if inExtensionSchema {
				for _, attr := range t.Attr {
					if attr.Name.Local != "namespaceURI" ||
						(attr.Name.Space != xmpPdfaSchemaPrefix && attr.Name.Space != xmpPdfaTypePrefix) {
						continue
					}
					if ns := strings.TrimSpace(attr.Value); ns != "" {
						extensions[ns] = true
					}
				}
			}

		case xml.EndElement:
			if t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description" && elemDepth == 0 {
				descDepth--
			} else if descDepth > 0 && elemDepth > 0 {
				elemDepth--
			}
			if t.Name.Local == "schemas" {
				inExtensionSchema = false
			}
		}
	}

	// Pure-data helper (no *document.Document in scope): use the
	// package-level slog. See compliance/doc.go "silent-skip pattern".
	slog.Debug("compliance.xmp: namespace extraction complete",
		"usedCount", len(used), "extensionCount", len(extensions))
	return used, extensions
}

// xmpSchemaRule reports a property whose namespace is neither predefined nor
// declared by an extension schema.
//
// It answers both tests of the clause. veraPDF splits them as
// "isPredefinedInXMP2005 || isDefinedInMainPackage || isDefinedInCurrentPackage"
// and "isValueTypeCorrect", and an undeclared property fails the second for the
// same reason it fails the first: with no schema there is no declared type to
// hold its value to. Answering only the first left the remediated
// 6-6-4-t01-fail-a reporting two findings where veraPDF reports three.
func xmpSchemaRule(part int, ns string) []Violation {
	const undeclared = "namespace %s not predefined or in extension schema"
	switch part {
	case 1:
		return append(
			failf(Rule_19005_1_6_7_9_t02, undeclared, ns),
			failf(Rule_19005_1_6_7_9_t03, undeclared, ns)...)
	case 2:
		return append(
			failf(Rule_19005_2_6_6_2_3_1_t01, undeclared, ns),
			failf(Rule_19005_2_6_6_2_3_1_t02, undeclared, ns)...)
	case 3:
		return append(
			failf(Rule_19005_3_6_6_2_3_1_t01, undeclared, ns),
			failf(Rule_19005_3_6_6_2_3_1_t02, undeclared, ns)...)
	case 4:
		// ISO 19005-4 drops the extension-schema requirements of parts
		// 1-3 (XMP 2010 packets are self-describing); its §6.6.1 covers
		// actions, not metadata.
		return nil
	}
	return nil
}

// xmpTopLevelProperty identifies one top-level XMP property: a direct
// child, in element or attribute form, of a top-level rdf:Description.
type xmpTopLevelProperty struct {
	ns   string
	name string
}

// xmpExtractTopLevelProperties parses XMP XML and returns the top-level
// properties in document order, deduplicated by namespace and name. Only
// direct children of a top-level rdf:Description count: ISO 19005 §6.7.9 /
// §6.6.2.3.1 apply to properties, not to structure fields, so fields of
// structured values, including ones serialized as nested rdf:Description
// elements, must stay invisible here.
func xmpExtractTopLevelProperties(data []byte) []xmpTopLevelProperty {
	// Ancestor classification: structural wrappers (document root,
	// x:xmpmeta, rdf:RDF), a top-level rdf:Description, or content
	// (property elements and everything below them).
	const (
		xmpFrameStructural = iota
		xmpFrameDescription
		xmpFrameContent
	)

	ignored := map[string]bool{
		"":                                     true,
		"xmlns":                                true,
		xmpRDFNamespace:                        true,
		"http://www.w3.org/XML/1998/namespace": true,
		"http://www.w3.org/2000/xmlns/":        true,
		"http://www.w3.org/2001/XMLSchema-instance": true,
		"http://www.w3.org/2001/XMLSchema":          true,
		"adobe:ns:meta/":                            true,
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = xmpPassThroughCharset
	decoder.Strict = false

	seen := make(map[xmpTopLevelProperty]bool)
	var props []xmpTopLevelProperty
	record := func(ns, name string) {
		p := xmpTopLevelProperty{ns: ns, name: name}
		if !seen[p] {
			seen[p] = true
			props = append(props, p)
		}
	}

	var stack []int
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			parent := xmpFrameStructural
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			frame := xmpFrameContent
			switch parent {
			case xmpFrameStructural:
				switch {
				case t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description":
					frame = xmpFrameDescription
					// Attribute-form properties on the top-level
					// rdf:Description.
					for _, attr := range t.Attr {
						if !ignored[attr.Name.Space] {
							record(attr.Name.Space, attr.Name.Local)
						}
					}
				case t.Name.Space == "adobe:ns:meta/",
					t.Name.Space == xmpRDFNamespace && t.Name.Local == "RDF":
					frame = xmpFrameStructural
				}
			case xmpFrameDescription:
				if !ignored[t.Name.Space] {
					record(t.Name.Space, t.Name.Local)
				}
			}
			stack = append(stack, frame)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return props
}

// xmpCheckPredefinedProperties verifies that every top-level property in a
// predefined XMP schema namespace uses a property name that the XMP
// Specification variant for the part actually defines. ISO 19005-1 §6.7.9
// (XMP 2004) and ISO 19005-2/3 §6.6.2.3.1 (XMP 2005) require every
// property to be either predefined or declared by a PDF/A extension
// schema; [xmpCheckNamespaces] enforces the namespace half, this enforces
// the property-name half for namespaces with a predefined catalog.
// Presence only: value-type correspondence stays with
// [xmpCheckPropertyTypes].
func xmpCheckPredefinedProperties(ctx context.Context, data []byte, part int, extraExtensionNS map[string]bool, results *[]Violation) {
	catalog := xmpPredefinedPropertyCatalog(part)
	if catalog == nil {
		// ISO 19005-4 drops the extension-schema requirements of parts
		// 1-3; see xmpSchemaRule.
		return
	}
	_, extensionNS := xmpExtractNamespaces(data)
	maps.Copy(extensionNS, extraExtensionNS)
	prefixes := xmpExtractPrefixMap(ctx, data)

	for _, p := range xmpExtractTopLevelProperties(data) {
		// The pdfaid identification schema participates like any other
		// predefined namespace: the catalog for the part decides which
		// names are predefined (the 2005 catalog lacks rev, the 2004 one
		// lacks corr and rev), so a leftover PDF/A-4 pdfaid:rev fails here.
		// Value-level pdfaid checks stay with xmpCheckPDFAIdentification.
		// Extension schemas may extend predefined schemas: a PDF/A
		// extension schema declares properties by namespace and name with
		// no restriction against predefined namespaces, so litho keeps its
		// namespace-level leniency for extension-declared namespaces.
		if extensionNS[p.ns] {
			continue
		}
		known, ok := catalog[p.ns]
		if !ok {
			// Non-catalog namespaces are handled by xmpCheckNamespaces.
			continue
		}
		if _, defined := known[p.name]; defined {
			continue
		}
		prefixed := p.ns + ":" + p.name
		if prefix := prefixes[p.ns]; prefix != "" {
			prefixed = prefix + ":" + p.name
		}
		*results = append(*results, xmpSchemaPropertyRule(part, prefixed)...)
	}
}

// xmpSchemaPropertyRule returns ISO 19005-1 §6.7.9 t2 and ISO 19005-2/3
// §6.6.2.3.1 t1 for a property whose namespace is predefined but whose
// name is not in the predefined property set and not declared by an
// extension schema.
func xmpSchemaPropertyRule(part int, prop string) []Violation {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_7_9_t02, "property %s not predefined in XMP 2004 and not declared in extension schema", prop)
	case 2:
		return failf(Rule_19005_2_6_6_2_3_1_t01, "property %s not predefined in XMP 2005 and not declared in extension schema", prop)
	case 3:
		return failf(Rule_19005_3_6_6_2_3_1_t01, "property %s not predefined in XMP 2005 and not declared in extension schema", prop)
	case 4:
		// ISO 19005-4 drops the extension-schema requirements of parts
		// 1-3; see xmpSchemaRule.
		return nil
	}
	return nil
}

// xmpCheckPDFAIdentification verifies that the XMP contains pdfaid:part and
// pdfaid:conformance entries with correct values matching the declared profile.
func xmpCheckPDFAIdentification(ctx context.Context, data []byte, profile string, part int, results *[]Violation) {
	var partValue, confValue, revValue string
	hasPart := false
	hasConf := false
	var hasAmd, hasCorr bool
	var wrongPrefixPart, wrongPrefixConf, wrongPrefixRev bool
	var wrongPrefixAmd, wrongPrefixCorr bool

	// A packet that is not well-formed XML carries no PDFAIdentification at
	// all. veraPDF's XMPCore parse fails outright, Identification_size reads 0,
	// and it reports the missing-identification rule without evaluating any
	// field. litho's lenient scan used to recover pdfaid:part out of a packet
	// whose defect sits AFTER it, so a packet litho had just reported as an
	// invalid serialization still counted as identified.
	if !xmpWellFormed(data) {
		*results = append(*results, xmpPDFAIdMissingRule(part)...)
		return
	}

	// Scan the same bytes xmpWellFormed judged, so a control character veraPDF
	// repairs before parsing does not cost the packet its identification here.
	decoder := xml.NewDecoder(bytes.NewReader(xmpRepairForParse(data)))
	decoder.CharsetReader = xmpPassThroughCharset
	decoder.Strict = false

	// Only properties in the pdfaid namespace URI carry the identification
	// and prefix obligations. A matching local name under a different URI
	// (pdfuaid:part on a dual PDF/A + PDF/UA file, prism:part, ...) belongs
	// to another schema and must not be flagged: these obligations are
	// keyed to the pdfaid namespace alone.
	var parseClean bool
	for {
		tok, err := decoder.Token()
		if err != nil {
			parseClean = errors.Is(err, io.EOF)
			break
		}
		t, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		// Check attributes (inline form).
		for _, attr := range t.Attr {
			if attr.Name.Space != xmpPdfaIDNamespace {
				continue
			}
			switch attr.Name.Local {
			case "part":
				hasPart = true
				partValue = attr.Value
			case "conformance":
				hasConf = true
				confValue = attr.Value
			case "rev":
				revValue = attr.Value
			case "amd":
				hasAmd = true
			case "corr":
				hasCorr = true
			}
		}
		// Check element form: read text content.
		if t.Name.Space == xmpPdfaIDNamespace {
			if charTok, charErr := decoder.Token(); charErr == nil {
				if charData, ok := charTok.(xml.CharData); ok {
					val := strings.TrimSpace(string(charData))
					switch t.Name.Local {
					case "part":
						hasPart = true
						partValue = val
					case "conformance":
						hasConf = true
						confValue = val
					case "rev":
						revValue = val
					case "amd":
						hasAmd = true
					case "corr":
						hasCorr = true
					}
				}
			}
		}
	}

	// Detect wrong prefix via raw byte scan. The XML parser resolves
	// namespaces by URI, so "pdfa:part" with the correct URI appears correct
	// to the parser. The actual serialized prefix must be inspected.
	if hasPart && !wrongPrefixPart {
		wrongPrefixPart = xmpHasWrongPdfaidPrefix(ctx, data, "part")
	}
	if hasConf && !wrongPrefixConf {
		wrongPrefixConf = xmpHasWrongPdfaidPrefix(ctx, data, "conformance")
	}
	if revValue != "" && !wrongPrefixRev {
		wrongPrefixRev = xmpHasWrongPdfaidPrefix(ctx, data, "rev")
	}
	if hasAmd && !wrongPrefixAmd {
		wrongPrefixAmd = xmpHasWrongPdfaidPrefix(ctx, data, "amd")
	}
	if hasCorr && !wrongPrefixCorr {
		wrongPrefixCorr = xmpHasWrongPdfaidPrefix(ctx, data, "corr")
	}

	// Emit prefix violations for pdfaid fields using wrong namespace.
	if wrongPrefixPart {
		*results = append(*results, xmpPDFAIdPrefixRule(part, "part")...)
	}
	if wrongPrefixConf {
		*results = append(*results, xmpPDFAIdPrefixRule(part, "conformance")...)
	}
	if wrongPrefixRev {
		*results = append(*results, xmpPDFAIdPrefixRule(part, "rev")...)
	}
	if wrongPrefixAmd {
		*results = append(*results, xmpPDFAIdPrefixRule(part, "amd")...)
	}
	if wrongPrefixCorr {
		*results = append(*results, xmpPDFAIdPrefixRule(part, "corr")...)
	}

	// A packet that is not well-formed XML cannot carry a parseable PDF/A
	// Identification schema. Per ISO 19005-4 §6.7.3 (and §6.7.11 / §6.6.4 for
	// parts 1-3) the identification shall be present and valid. veraPDF models
	// this as MainXMPPackage.Identification_size == 1, which is 0 for an
	// unparseable packet: it reports the missing-identification rule (t01) and
	// never evaluates the field-level rules (pdfaid:part / conformance / rev),
	// which are scoped to a successfully extracted PDFAIdentification object.
	// litho matches that object model: when the lenient XML parse did not reach
	// a clean EOF and recovered no pdfaid field, report t01 rather than
	// byte-scanning for field presence and emitting a value-level violation
	// (for example "rev missing") for a packet litho never managed to parse.
	if !parseClean && !hasPart && !hasConf && revValue == "" && !wrongPrefixPart && !wrongPrefixConf && !wrongPrefixRev {
		*results = append(*results, xmpPDFAIdMissingRule(part)...)
		return
	}

	level := PDFALevel(profile)
	expectedPart := fmt.Sprintf("%d", part)

	// veraPDF fires the missing-identification rule (t1,
	// MainXMPPackage.containsPDFAIdentification) only when the pdfaid schema is
	// entirely absent. Any recovered pdfaid field (part, conformance, rev, amd or
	// corr) means the PDFAIdentification object exists, so a missing or wrong
	// part or conformance is a value-level violation (t2/t3) scoped to that
	// object, not a missing-schema one.
	if !hasPart && !hasConf && revValue == "" && !hasAmd && !hasCorr {
		*results = append(*results, xmpPDFAIdMissingRule(part)...)
		return
	}

	// Validate pdfaid:part presence and value (t2): an absent part fails the
	// `part == expected` test just as a wrong value does. Absent conformance for
	// parts 1-3 fails the ConformanceSatisfies check below (t3).
	if !hasPart || (partValue != "" && partValue != expectedPart) {
		*results = append(*results, xmpPDFAPartRule(part, partValue)...)
	}

	// Validate pdfaid:conformance value (parts 1-3). Per ISO 19005 the
	// levels nest A ⊃ U ⊃ B, so a stricter XMP claim is allowed to
	// cover a looser validation profile (e.g. a "Level A" file is also
	// a valid "Level B" file). [ConformanceSatisfies] encodes that
	// hierarchy.
	if part < 4 && level != 0 && !ConformanceSatisfies(confValue, level) {
		*results = append(*results, xmpPDFAConformanceRule(part, confValue)...)
	}

	// Part 4 conformance validation depends on the sub-profile.
	if part == 4 {
		xmpCheckPart4Conformance(profile, hasConf, confValue, results)

		// Part 4: validate pdfaid:rev (must be present and "2020").
		if revValue == "" {
			*results = append(*results, failf(Rule_19005_4_6_7_3_t05, "rev missing (must be 2020)")...)
		} else if revValue != "2020" {
			*results = append(*results, failf(Rule_19005_4_6_7_3_t05, "rev=%s", revValue)...)
		}
	}
}

// xmpCheckPart4Conformance validates pdfaid:conformance for PDF/A-4 sub-profiles.
//   - PDF/A-4 (plain): conformance must be absent or empty.
//   - PDF/A-4e: conformance must be absent/empty or "E".
//   - PDF/A-4f: conformance must be absent/empty or "F".
func xmpCheckPart4Conformance(profile string, hasConf bool, confValue string, results *[]Violation) {
	switch profile {
	case "PDF/A-4":
		// Plain PDF/A-4: conformance must be absent entirely.
		// An empty conformance attribute is still a violation.
		if hasConf {
			*results = append(*results, failf(Rule_19005_4_6_7_3_t03, "conformance=%q but must be absent for plain PDF/A-4", confValue)...)
		}
	case "PDF/A-4e":
		// PDF/A-4e: conformance MUST be exactly "E" (case-sensitive).
		if confValue != "E" {
			*results = append(*results, failf(Rule_19005_4_6_7_3_t03, "conformance=%q but must be 'E' for PDF/A-4e", confValue)...)
		}
	case "PDF/A-4f":
		// PDF/A-4f: conformance MUST be exactly "F" (case-sensitive).
		if confValue != "F" {
			*results = append(*results, failf(Rule_19005_4_6_7_3_t03, "conformance=%q but must be 'F' for PDF/A-4f", confValue)...)
		}
	}
}

// xmpHasWrongPdfaidPrefix scans raw XMP bytes to check if a pdfaid-namespace
// field is serialized with a non-"pdfaid" prefix (e.g., "pdfa:part" with
// xmlns:pdfa bound to the pdfaid URI). The XML parser resolves namespaces by
// URI, masking the actual prefix, so the serialization must be inspected.
// A prefix bound to a different URI (pdfuaid:part, prism:part) belongs to
// another schema and is not flagged. Declarations with whitespace around
// "=" are not recognized; real-world XMP serializers do not emit them.
// Returns false on context cancellation; outer callers re-check ctx.
func xmpHasWrongPdfaidPrefix(ctx context.Context, data []byte, field string) bool {
	needle := []byte(":" + field)
	for off := 0; ; {
		if ctx.Err() != nil {
			return false
		}
		idx := bytes.Index(data[off:], needle)
		if idx < 0 {
			break
		}
		pos := off + idx
		off = pos + len(needle)

		// The match must end at a name boundary so ":part" does not hit
		// ":partial".
		if end := pos + len(needle); end < len(data) && isXMLNameChar(data[end]) {
			continue
		}

		prefix := xmlPrefixBefore(data, pos)
		if prefix == "" || prefix == "pdfaid" {
			continue
		}
		if bytes.Contains(data, []byte(`xmlns:`+prefix+`="`+xmpPdfaIDNamespace+`"`)) ||
			bytes.Contains(data, []byte(`xmlns:`+prefix+`='`+xmpPdfaIDNamespace+`'`)) {
			return true
		}
	}
	return false
}

// xmlPrefixBefore returns the XML name token immediately preceding the colon
// at colonPos, or "" when none.
func xmlPrefixBefore(data []byte, colonPos int) string {
	start := colonPos
	for start > 0 && isXMLNameChar(data[start-1]) {
		start--
	}
	if start == colonPos {
		return ""
	}
	return string(data[start:colonPos])
}

func isXMLNameChar(b byte) bool {
	return isASCIILetter(b) || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.'
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func xmpPDFAIdMissingRule(part int) []Violation {
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_11_t01, "missing PDF/A Identification Schema")
	case 2:
		return fail(Rule_19005_2_6_6_4_t01, "missing PDF/A Identification Schema")
	case 3:
		return fail(Rule_19005_3_6_6_4_t01, "missing PDF/A Identification Schema")
	case 4:
		return fail(Rule_19005_4_6_7_3_t01, "missing PDF/A Identification Schema")
	}
	return nil
}

func xmpPDFAPartRule(part int, actual string) []Violation {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_7_11_t02, "pdfaid:part=%s", actual)
	case 2:
		return failf(Rule_19005_2_6_6_4_t02, "pdfaid:part=%s", actual)
	case 3:
		return failf(Rule_19005_3_6_6_4_t02, "pdfaid:part=%s", actual)
	case 4:
		return failf(Rule_19005_4_6_7_3_t02, "pdfaid:part=%s", actual)
	}
	return nil
}

func xmpPDFAConformanceRule(part int, actual string) []Violation {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_7_11_t03, "pdfaid:conformance=%s", actual)
	case 2:
		return failf(Rule_19005_2_6_6_4_t03, "pdfaid:conformance=%s", actual)
	case 3:
		return failf(Rule_19005_3_6_6_4_t03, "pdfaid:conformance=%s", actual)
	case 4:
		return failf(Rule_19005_4_6_7_3_t03, "pdfaid:conformance=%s", actual)
	}
	return nil
}

// xmpPDFAIdPrefixRule returns the appropriate rule for a pdfaid field that uses
// the wrong namespace prefix. The field parameter is "part", "conformance",
// "rev", "amd", or "corr". A field with no rule for the given part returns nil
// (for example PDF/A-4 has no amd/corr prefix rule, and part 1 has no corr).
func xmpPDFAIdPrefixRule(part int, field string) []Violation {
	ctx := fmt.Sprintf("pdfaid:%s uses wrong namespace prefix", field)
	switch field {
	case "part":
		switch part {
		case 1:
			return fail(Rule_19005_1_6_7_11_t04, ctx)
		case 2:
			return fail(Rule_19005_2_6_6_4_t04, ctx)
		case 3:
			return fail(Rule_19005_3_6_6_4_t04, ctx)
		case 4:
			return fail(Rule_19005_4_6_7_3_t04, ctx)
		}
	case "conformance":
		switch part {
		case 1:
			return fail(Rule_19005_1_6_7_11_t05, ctx)
		case 2:
			return fail(Rule_19005_2_6_6_4_t05, ctx)
		case 3:
			return fail(Rule_19005_3_6_6_4_t05, ctx)
		}
	case "rev":
		if part == 4 {
			return fail(Rule_19005_4_6_7_3_t06, ctx)
		}
	case "amd":
		switch part {
		case 1:
			return fail(Rule_19005_1_6_7_11_t06, ctx)
		case 2:
			return fail(Rule_19005_2_6_6_4_t06, ctx)
		case 3:
			return fail(Rule_19005_3_6_6_4_t06, ctx)
		}
	case "corr":
		switch part {
		case 2:
			return fail(Rule_19005_2_6_6_4_t07, ctx)
		case 3:
			return fail(Rule_19005_3_6_6_4_t07, ctx)
		}
	}
	return nil
}

// xmpExtSchemaLevel identifies the nesting context within an extension schema.
type xmpExtSchemaLevel int

const (
	xmpExtLevelSchema    xmpExtSchemaLevel = iota // inside a schema rdf:li
	xmpExtLevelProperty                           // inside a property rdf:li
	xmpExtLevelValueType                          // inside a valueType rdf:li
	xmpExtLevelField                              // inside a field rdf:li
)

// xmpExpectedPrefix maps extension schema namespace URIs to the expected
// XML prefix name per the PDF/A extension schema specification.
//
//nolint:gochecknoglobals // static lookup table
var xmpExpectedPrefix = map[string]string{
	xmpPdfaSchemaPrefix:   "pdfaSchema",
	xmpPdfaPropertyPrefix: "pdfaProperty",
	xmpPdfaTypePrefix:     "pdfaType",
	xmpPdfaFieldPrefix:    "pdfaField",
}

// xmpExtensionLevel is one level of the PDF/A extension schema, its fields, and
// the rule that reports a field of that level.
type xmpExtensionLevel struct {
	uri    string
	fields []string
	rule   func(part int, field string) []Violation
}

// xmpExtensionLevels lists the extension schema levels in a fixed order so the
// prefix scan emits the same violations for the same bytes. Ranging
// [xmpExpectedPrefix] instead made the report depend on Go's map iteration
// order. The field lists mirror the per-field tests of ISO 19005-1 §6.7.8 and
// ISO 19005-2/3 §6.6.2.3.3.
//
//nolint:gochecknoglobals // static lookup table
var xmpExtensionLevels = []xmpExtensionLevel{
	{xmpPdfaSchemaPrefix, []string{"schema", "namespaceURI", "prefix", "property", "valueType"}, xmpExtSchemaRule},
	{xmpPdfaPropertyPrefix, []string{"name", "valueType", "category", "description"}, xmpExtPropertyRule},
	{xmpPdfaTypePrefix, []string{"type", "namespaceURI", "prefix", "description", "field"}, xmpExtValueTypeRule},
	{xmpPdfaFieldPrefix, []string{"name", "valueType", "description"}, xmpExtFieldRule},
}

// dcTitleLangAltMalformed reports whether the XMP dc:title is a language
// alternative (rdf:Alt) that veraPDF cannot resolve to a default value: two or
// more rdf:li entries with none qualified xml:lang="x-default". veraPDF's
// dc_title getter returns null for that shape, so the document fails ISO
// 14289-1 §7.1 t9 / ISO 14289-2 §8.11.1 t1 as if dc:title were absent. A single
// li (implicitly the default) or an explicit x-default li resolves; any other
// shape is treated as resolvable to avoid false positives.
func dcTitleLangAltMalformed(data []byte) bool {
	const dcNamespace = "http://purl.org/dc/elements/1.1/"
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = xmpPassThroughCharset
	decoder.Strict = false
	depth := 0 // > 0 while inside a dc:title element
	liCount := 0
	hasXDefault := false
	malformed := false
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				if t.Name.Space == dcNamespace && t.Name.Local == "title" {
					depth = 1
					liCount, hasXDefault = 0, false
				}
				continue
			}
			depth++
			if t.Name.Local == "li" {
				liCount++
				for _, a := range t.Attr {
					if a.Name.Local == "lang" && a.Value == "x-default" {
						hasXDefault = true
					}
				}
			}
		case xml.EndElement:
			if depth > 0 {
				depth--
				if depth == 0 && liCount >= 2 && !hasXDefault {
					malformed = true
				}
			}
		}
	}
	return malformed
}

// xmpLangAltOnlyXDefault reports whether a top-level property of the packet is
// a language alternative holding a single item qualified xml:lang="x-default".
//
// ISO 14289-1, 7.2 t33 is judged on veraPDF's XMPLangAlt: a top-level property
// (VeraPDFMeta.getProperties) whose items all carry an xml:lang qualifier
// (VeraPDFXMPNode.isLanguageAlternative). It fails when xDefault holds, which
// AXLXMPLangAlt.getxDefault sets for exactly one item qualified "x-default", so
// an alternative naming its language beside x-default passes and another
// property's specific tag does not rescue it. The array form does not matter: a
// Bag qualifies as readily as an Alt. Structure fields are not top-level
// properties and are not judged.
//
// XMPCore frames the packet by the first rdf:RDF element in the RDF namespace
// (XMPMetaParser.findRootNode), so a packet binding the rdf prefix to another
// URI holds no property at all: pdfjs issue15833.pdf carries packets rewritten
// by a library proxy, and veraPDF reports none of them. Measured against veraPDF
// on a fixture of each shape.
//
// The packet is read through [xmpRepairForParse], as [xmpWellFormed] reads it:
// govdocs thread_300_300842.pdf writes &#x5; inside its x-default item, which
// XMPCore repairs and veraPDF reports, while the raw decoder stopped there.
func xmpLangAltOnlyXDefault(data []byte) bool {
	const (
		frameOutside = iota
		frameRDF
		frameDescription
		frameProperty
		frameArray
		frameOther
	)

	decoder := xml.NewDecoder(bytes.NewReader(xmpRepairForParse(data)))
	decoder.CharsetReader = xmpPassThroughCharset
	decoder.Strict = false

	var (
		stack     []int
		items     int
		firstLang string
	)
	for {
		tok, err := decoder.Token()
		if err != nil {
			return false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			parent := frameOutside
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			rdf := t.Name.Space == xmpRDFNamespace
			frame := frameOther
			switch parent {
			case frameOutside:
				frame = frameOutside
				if rdf && t.Name.Local == "RDF" {
					frame = frameRDF
				}
			case frameRDF:
				if rdf && t.Name.Local == "Description" {
					frame = frameDescription
				}
			case frameDescription:
				if !rdf {
					frame = frameProperty
					items, firstLang = 0, ""
				}
			case frameProperty:
				if rdf && (t.Name.Local == "Alt" || t.Name.Local == "Bag" || t.Name.Local == "Seq") {
					frame = frameArray
				}
			case frameArray:
				if rdf && t.Name.Local == "li" {
					items++
					if items == 1 {
						for _, a := range t.Attr {
							if a.Name.Space == "http://www.w3.org/XML/1998/namespace" && a.Name.Local == "lang" {
								firstLang = a.Value
								break
							}
						}
					}
				}
			}
			stack = append(stack, frame)
		case xml.EndElement:
			if len(stack) == 0 {
				continue
			}
			frame := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			switch frame {
			case frameProperty:
				if items == 1 && firstLang == "x-default" {
					return true
				}
			case frameRDF:
				// XMPCore reads the first rdf:RDF only.
				return false
			}
		}
	}
}

// xmpExtractPrefixMap scans raw XMP bytes for xmlns:prefix="uri" declarations
// and returns a map from namespace URI to the actual prefix name used. When
// a URI is bound to multiple prefixes, the last occurrence wins; callers
// that need every binding must use [xmpExtractAllPrefixesForURI] instead.
func xmpExtractPrefixMap(ctx context.Context, data []byte) map[string]string {
	result := make(map[string]string)
	for _, b := range xmpNamespaceBindings(ctx, data) {
		result[b.uri] = b.prefix
	}
	return result
}

// xmpExtractAllPrefixesForURI returns every prefix bound to targetURI in
// the XMP bytes. XMP allows multiple xmlns declarations to point at the
// same URI; some producers deliberately alias the PDF/UA Identification
// namespace to a non-canonical prefix and then expose identification
// properties under that alias. ISO 14289 still requires the canonical
// "pdfuaid" prefix for those properties, so callers need to check every
// binding rather than whichever one last-write-wins leaves in the map.
func xmpExtractAllPrefixesForURI(ctx context.Context, data []byte, targetURI string) []string {
	var result []string
	for _, b := range xmpNamespaceBindings(ctx, data) {
		if b.uri == targetURI {
			result = append(result, b.prefix)
		}
	}
	return result
}

type xmpNSBinding struct {
	prefix string
	uri    string
}

// xmpNamespaceBindings walks every xmlns:prefix="uri" declaration in the
// raw XMP bytes and returns them in order of appearance. Duplicates are
// preserved so callers can distinguish "URI bound to one prefix" from
// "URI bound to several prefixes". Returns nil on context cancellation.
func xmpNamespaceBindings(ctx context.Context, data []byte) []xmpNSBinding {
	var bindings []xmpNSBinding
	s := string(data)
	for {
		if ctx.Err() != nil {
			return nil
		}
		idx := strings.Index(s, "xmlns:")
		if idx < 0 {
			break
		}
		s = s[idx+6:] // skip "xmlns:"
		eqIdx := strings.IndexByte(s, '=')
		if eqIdx < 0 {
			break
		}
		prefix := s[:eqIdx]
		s = s[eqIdx+1:]
		if len(s) == 0 {
			break
		}
		quote := s[0]
		if quote != '"' && quote != '\'' {
			continue
		}
		s = s[1:]
		endIdx := strings.IndexByte(s, quote)
		if endIdx < 0 {
			break
		}
		bindings = append(bindings, xmpNSBinding{prefix: prefix, uri: s[:endIdx]})
		s = s[endIdx+1:]
	}
	return bindings
}

// xmpCheckExtensionSchemas validates that PDF/A extension schemas have all
// required fields per ISO 19005 clause 6.6.2.3.3 / 6.7.8.
// Each missing field emits a specific rule (t02–t18).
// It also validates that extension schema elements use the expected namespace
// prefix names (e.g., "pdfaSchema" not "nonpdfaSchema").
func xmpCheckExtensionSchemas(ctx context.Context, data []byte, part int, results *[]Violation) {
	// Check that pdfa extension schema namespace URIs use the expected
	// prefix names. Only a binding that is actually USED on elements
	// counts: a dangling xmlns declaration aliasing one of these URIs
	// (a common leftover after remediation empties a defective
	// rdf:Description) binds no element, so it cannot violate the
	// prefix requirement. Prefix correctness is a property of the
	// elements that use a binding, not of the declaration itself.
	// Each rule of the clause names one field, so only the fields the packet
	// actually carries under the wrong prefix are reported. Reporting every
	// field of the level instead invented violations for fields that are not
	// there, and returning early hid every other defect in the packet.
	//
	// The URIs are visited in a fixed order. Ranging the map and returning on
	// the first hit made the output depend on Go's randomised map iteration:
	// on a packet misprefixed at two levels, repeated runs over identical
	// bytes reported different rule sets.
	for _, level := range xmpExtensionLevels {
		expectedPrefix := xmpExpectedPrefix[level.uri]
		actualPrefix := ""
		for _, p := range xmpExtractAllPrefixesForURI(ctx, data, level.uri) {
			if p != expectedPrefix && bytes.Contains(data, []byte("<"+p+":")) {
				actualPrefix = p
				break
			}
		}
		if actualPrefix == "" {
			continue
		}
		for _, field := range level.fields {
			if bytes.Contains(data, []byte("<"+actualPrefix+":"+field)) {
				*results = append(*results, level.rule(part, field)...)
			}
		}
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = xmpPassThroughCharset
	decoder.Strict = false

	var inSchemas bool
	// justEnteredSchemas is set right after the pdfaExtension:schemas element so
	// the immediately following rdf container can be checked for being an
	// rdf:Bag (ISO 19005-1 §6.7.8 t2 / ISO 19005-2/3 §6.6.2.3.3 t1).
	var justEnteredSchemas bool

	type schemaInfo struct {
		hasSchema, hasNamespaceURI, hasPrefix, hasProperty bool
	}
	type propInfo struct {
		hasName, hasValueType, hasCategory, hasDescription bool
		// category holds the value as written, so the constrained-value check
		// can compare it the way veraPDF does. Presence alone satisfies no
		// rule: ISO 19005-1 §6.7.8 t10 and ISO 19005-2/3 §6.6.2.3.3 t9 both
		// require the value itself to be "external" or "internal".
		category string
	}
	type valueTypeInfo struct {
		hasType, hasNamespaceURI, hasPrefix, hasDescription bool
	}
	type fieldInfo struct {
		hasName, hasValueType, hasDescription bool
	}

	var currentSchema schemaInfo
	var currentProp propInfo
	var currentValueType valueTypeInfo
	var currentField fieldInfo

	var levelStack []xmpExtSchemaLevel
	pendingLevel := xmpExtLevelSchema
	hasPending := false
	// foreignDepth tracks descent into a non-pdfa subtree (dc:creator,
	// xmp:* and friends that producers embed inside a schema's
	// rdf:Description). Only pdfa-namespace content belongs to the
	// extension schema grammar, so such a subtree must be skipped whole:
	// its nested rdf:Seq/rdf:li would otherwise be misread as a second
	// schema definition whose close resets currentSchema, making the real
	// schema report every field missing.
	foreignDepth := 0
	// reportedUndefinedField guards the §6.6.2.3.2 t1 emit so a schema with
	// several foreign fields reports the defect once for the document.
	reportedUndefinedField := false
	// containerStack tracks, per OPEN rdf:Seq/rdf:Bag, the level its
	// rdf:li items push. A stack, not a flag: a nested container (the
	// field Seq inside a valueType definition) must not destroy the
	// outer container's context, or every definition after the first in
	// the outer Seq is evaluated at the wrong level (the phantom
	// missing-field storms of the 792-cell corpus pool).
	var containerStack []xmpExtSchemaLevel
	// inCategory marks an open pdfaProperty:category element, whose character
	// data carries the value the category rule constrains. The text arrives as
	// its own token after the start element, and may arrive in several pieces,
	// so it is accumulated until the matching end element.
	inCategory := false
	var categoryText strings.Builder

	// A schema entry's value-type references are judged against the types that
	// same entry declares, so both are collected and evaluated when the entry
	// closes. The usual XMP layout puts pdfaSchema:property before
	// pdfaSchema:valueType, so the decision cannot be made when a property
	// closes, and veraPDF 1.30.2 confirms a type declared in a different entry
	// satisfies nothing in either direction.
	inTypeText := false
	typeTextKind := xmpTypeTextProperty
	var typeText strings.Builder
	var entryRefs []xmpTypeRef
	entryDeclared := map[string]bool{}

	currentLevel := func() xmpExtSchemaLevel {
		if len(levelStack) == 0 {
			return xmpExtLevelSchema
		}
		return levelStack[len(levelStack)-1]
	}

	// RDF/XML §2.5 abbreviates a property as an attribute on the node
	// element, so pdfaSchema:schema="X" satisfies the same requirement as a
	// <pdfaSchema:schema>X</pdfaSchema:schema> child. iText writes Factur-X
	// extension schemas in that form and veraPDF accepts it, so scanning
	// child elements alone reported every required field of such a schema as
	// missing (ISO 19005-2/3 §6.6.2.3.3 t2-t4 and t7-t10).
	harvestAttrFields := func(attrs []xml.Attr) {
		for _, a := range attrs {
			if strings.TrimSpace(a.Value) == "" {
				continue
			}
			switch {
			case a.Name.Space == xmpPdfaSchemaPrefix && currentLevel() == xmpExtLevelSchema:
				switch a.Name.Local {
				case "schema":
					currentSchema.hasSchema = true
				case "namespaceURI":
					currentSchema.hasNamespaceURI = true
				case "prefix":
					currentSchema.hasPrefix = true
				}
			case a.Name.Space == xmpPdfaPropertyPrefix && currentLevel() == xmpExtLevelProperty:
				switch a.Name.Local {
				case "name":
					currentProp.hasName = true
				case "valueType":
					currentProp.hasValueType = true
					entryRefs = append(entryRefs, xmpTypeRef{name: a.Value})
				case "category":
					currentProp.hasCategory = true
					currentProp.category = a.Value
				case "description":
					currentProp.hasDescription = true
				}
			case a.Name.Space == xmpPdfaTypePrefix && currentLevel() == xmpExtLevelValueType:
				switch a.Name.Local {
				case "type":
					currentValueType.hasType = true
					entryDeclared[xmpSimplifyValueType(a.Value)] = true
				case "namespaceURI":
					currentValueType.hasNamespaceURI = true
				case "prefix":
					currentValueType.hasPrefix = true
				case "description":
					currentValueType.hasDescription = true
				}
			case a.Name.Space == xmpPdfaFieldPrefix && currentLevel() == xmpExtLevelField:
				switch a.Name.Local {
				case "name":
					currentField.hasName = true
				case "valueType":
					currentField.hasValueType = true
					entryRefs = append(entryRefs, xmpTypeRef{name: a.Value, field: true})
				case "description":
					currentField.hasDescription = true
				}
			}
		}
	}

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if strings.Contains(t.Name.Space, "pdfa/ns/extension") && t.Name.Local == "schemas" {
				inSchemas = true
				justEnteredSchemas = true
				continue
			}
			if !inSchemas {
				continue
			}

			// Skip any subtree rooted at a foreign-namespace element: it is
			// not part of the PDF/A extension schema grammar.
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			if t.Name.Space != xmpRDFNamespace && !strings.Contains(t.Name.Space, "pdfa/ns") {
				// A field whose namespace is neither rdf nor a pdfa/ns/*
				// namespace is outside the extension schema vocabulary, an
				// undefined field per ISO 19005-2/3 §6.6.2.3.2 t1. The decision
				// is by URI, not prefix: a wrong prefix bound to a real pdfa URI
				// (e.g. nonpdfaSchema) keeps its pdfa namespace here and is
				// reported as a §6.6.2.3.3 prefix defect, not this rule.
				if !reportedUndefinedField {
					*results = append(*results, xmpExtUndefinedFieldRule(part)...)
					reportedUndefinedField = true
				}
				foreignDepth = 1
				continue
			}

			// The element directly inside pdfaExtension:schemas must be an
			// rdf:Bag. Fire only when it is explicitly an rdf:Seq (a wrong but
			// well-formed container); other shapes are left to the other checks.
			if justEnteredSchemas {
				justEnteredSchemas = false
				if t.Name.Local == "Seq" {
					*results = append(*results, xmpExtBagRule(part)...)
				}
			}

			// rdf:Seq/rdf:Bag: open a container whose rdf:li items push the
			// pending definition level (or the enclosing container's when
			// no definition element preceded it).
			// rdf:Alt counts as a container even though no rule requires one:
			// an unrecognised container leaves its rdf:li children attributed to
			// the enclosing schema container, which read them as a second schema
			// entry and reported its fields missing.
			if t.Name.Local == "Seq" || t.Name.Local == "Bag" || t.Name.Local == "Alt" {
				level := xmpExtLevelSchema
				switch {
				case hasPending:
					// The valueType, field and property containers must each be an
					// rdf:Seq: ISO 19005-1 §6.7.8 t7, t16 and t6, ISO 19005-2/3
					// §6.6.2.3.3 t6, t15 and t5. Only an explicit rdf:Bag is
					// flagged; veraPDF 1.30.2 accepts an rdf:Alt in all three.
					if t.Name.Local == "Bag" {
						switch pendingLevel {
						case xmpExtLevelValueType:
							*results = append(*results, xmpExtValueTypeSeqRule(part)...)
						case xmpExtLevelField:
							*results = append(*results, xmpExtFieldSeqRule(part)...)
						case xmpExtLevelProperty:
							*results = append(*results, xmpExtPropertySeqRule(part)...)
						case xmpExtLevelSchema:
							// The schemas container is an rdf:Bag by definition
							// (ISO 19005-1 §6.7.8 t2), checked by isValidBag.
						}
					}
					level = pendingLevel
					hasPending = false
				case len(containerStack) > 0:
					level = containerStack[len(containerStack)-1]
				}
				containerStack = append(containerStack, level)
				continue
			}

			// On rdf:li, push the enclosing container's level.
			if t.Name.Local == "li" {
				if len(containerStack) > 0 {
					levelStack = append(levelStack, containerStack[len(containerStack)-1])
				} else if len(levelStack) == 0 {
					levelStack = append(levelStack, xmpExtLevelSchema)
				}
				// An attribute-form rdf:li carries its whole definition here
				// and is usually self-closing, so harvest before descending.
				harvestAttrFields(t.Attr)
				continue
			}

			harvestAttrFields(t.Attr)

			// Track fields within each definition level.
			// For each known field local name, verify the namespace prefix
			// matches the expected constant. Extension schema fields must use
			// the canonical pdfa* namespaces, so a known local name under the
			// wrong namespace is a prefix error and emits the matching rule.
			switch {
			case t.Name.Space == xmpPdfaSchemaPrefix:
				switch t.Name.Local {
				case "schema":
					currentSchema.hasSchema = true
				case "namespaceURI":
					currentSchema.hasNamespaceURI = true
				case "prefix":
					currentSchema.hasPrefix = true
				case "property":
					currentSchema.hasProperty = true
					pendingLevel = xmpExtLevelProperty
					hasPending = true
				case "valueType":
					pendingLevel = xmpExtLevelValueType
					hasPending = true
				}
			case t.Name.Space != xmpPdfaSchemaPrefix && currentLevel() == xmpExtLevelSchema:
				// Wrong namespace prefix for a schema-level field.
				switch t.Name.Local {
				case "schema", "namespaceURI", "prefix", "property":
					*results = append(*results, xmpExtSchemaRule(part, t.Name.Local)...)
				case "valueType":
					// Still need to set up pending level for child parsing.
					pendingLevel = xmpExtLevelValueType
					hasPending = true
				}
			case t.Name.Space == xmpPdfaPropertyPrefix && currentLevel() == xmpExtLevelProperty:
				switch t.Name.Local {
				case "name":
					currentProp.hasName = true
				case "valueType":
					currentProp.hasValueType = true
					inTypeText, typeTextKind = true, xmpTypeTextProperty
					typeText.Reset()
				case "category":
					currentProp.hasCategory = true
					inCategory = true
					categoryText.Reset()
				case "description":
					currentProp.hasDescription = true
				}
			case t.Name.Space != xmpPdfaPropertyPrefix && currentLevel() == xmpExtLevelProperty:
				// Wrong namespace prefix for a property-level field.
				switch t.Name.Local {
				case "name", "valueType", "category", "description":
					*results = append(*results, xmpExtPropertyRule(part, t.Name.Local)...)
				}
			case t.Name.Space == xmpPdfaTypePrefix && currentLevel() == xmpExtLevelValueType:
				switch t.Name.Local {
				case "type":
					currentValueType.hasType = true
					inTypeText, typeTextKind = true, xmpTypeTextDeclaration
					typeText.Reset()
				case "namespaceURI":
					currentValueType.hasNamespaceURI = true
				case "prefix":
					currentValueType.hasPrefix = true
				case "description":
					currentValueType.hasDescription = true
				case "field":
					pendingLevel = xmpExtLevelField
					hasPending = true
				}
			case t.Name.Space != xmpPdfaTypePrefix && currentLevel() == xmpExtLevelValueType:
				// Wrong namespace prefix for a valueType-level field.
				switch t.Name.Local {
				case "type", "namespaceURI", "prefix", "description":
					*results = append(*results, xmpExtValueTypeRule(part, t.Name.Local)...)
				case "field":
					// Still need to set up pending level for child parsing.
					pendingLevel = xmpExtLevelField
					hasPending = true
				}
			case t.Name.Space == xmpPdfaFieldPrefix && currentLevel() == xmpExtLevelField:
				switch t.Name.Local {
				case "name":
					currentField.hasName = true
				case "valueType":
					currentField.hasValueType = true
					inTypeText, typeTextKind = true, xmpTypeTextField
					typeText.Reset()
				case "description":
					currentField.hasDescription = true
				}
			case t.Name.Space != xmpPdfaFieldPrefix && currentLevel() == xmpExtLevelField:
				// Wrong namespace prefix for a field-level field.
				switch t.Name.Local {
				case "name", "valueType", "description":
					*results = append(*results, xmpExtFieldRule(part, t.Name.Local)...)
				}
			}

		case xml.CharData:
			if inTypeText {
				typeText.Write(t)
			}
			if inCategory {
				categoryText.Write(t)
			}

		case xml.EndElement:
			if !inSchemas {
				continue
			}

			// Unwind a skipped foreign subtree without touching schema state.
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}

			if inCategory && t.Name.Space == xmpPdfaPropertyPrefix && t.Name.Local == "category" {
				inCategory = false
				currentProp.category = categoryText.String()
			}

			if inTypeText && t.Name.Local == "valueType" || inTypeText && t.Name.Local == "type" {
				inTypeText = false
				value := strings.TrimSpace(typeText.String())
				switch typeTextKind {
				case xmpTypeTextDeclaration:
					entryDeclared[xmpSimplifyValueType(value)] = true
				case xmpTypeTextField:
					entryRefs = append(entryRefs, xmpTypeRef{name: value, field: true})
				default:
					entryRefs = append(entryRefs, xmpTypeRef{name: value})
				}
			}

			// Close the innermost container. Alt opens one too, so it has to
			// close one or the stack leaks into the following siblings.
			if t.Name.Local == "Seq" || t.Name.Local == "Bag" || t.Name.Local == "Alt" {
				if len(containerStack) > 0 {
					containerStack = containerStack[:len(containerStack)-1]
				}
			}

			// A definition element that closes while its level is still pending
			// never opened a container at all, so it holds a bare value where the
			// clause requires an rdf:Seq. ISO 19005-1 §6.7.8 t6 and
			// ISO 19005-2/3 §6.6.2.3.3 t5 report that for the property container.
			if hasPending && t.Name.Space == xmpPdfaSchemaPrefix && t.Name.Local == "property" {
				if pendingLevel == xmpExtLevelProperty {
					*results = append(*results, xmpExtPropertySeqRule(part)...)
				}
				hasPending = false
			}

			if strings.Contains(t.Name.Space, "pdfa/ns/extension") && t.Name.Local == "schemas" {
				inSchemas = false
				continue
			}

			if t.Name.Local != "li" || len(levelStack) == 0 {
				continue
			}

			// Pop the stack and validate the completed definition.
			level := levelStack[len(levelStack)-1]
			levelStack = levelStack[:len(levelStack)-1]

			switch level {
			case xmpExtLevelField:
				if !currentField.hasName {
					*results = append(*results, xmpExtFieldRule(part, "name")...)
				}
				if !currentField.hasValueType {
					*results = append(*results, xmpExtFieldRule(part, "valueType")...)
				}
				if !currentField.hasDescription {
					*results = append(*results, xmpExtFieldRule(part, "description")...)
				}
				currentField = fieldInfo{}
			case xmpExtLevelValueType:
				if !currentValueType.hasType {
					*results = append(*results, xmpExtValueTypeRule(part, "type")...)
				}
				if !currentValueType.hasNamespaceURI {
					*results = append(*results, xmpExtValueTypeRule(part, "namespaceURI")...)
				}
				if !currentValueType.hasPrefix {
					*results = append(*results, xmpExtValueTypeRule(part, "prefix")...)
				}
				if !currentValueType.hasDescription {
					*results = append(*results, xmpExtValueTypeRule(part, "description")...)
				}
				currentValueType = valueTypeInfo{}
			case xmpExtLevelProperty:
				if !currentProp.hasName {
					*results = append(*results, xmpExtPropertyRule(part, "name")...)
				}
				if !currentProp.hasValueType {
					*results = append(*results, xmpExtPropertyRule(part, "valueType")...)
				}
				switch {
				case !currentProp.hasCategory:
					*results = append(*results, xmpExtPropertyRule(part, "category")...)
				case currentProp.category != xmpCategoryExternal && currentProp.category != xmpCategoryInternal:
					*results = append(*results, xmpExtPropertyCategoryRule(part, currentProp.category)...)
				}
				if !currentProp.hasDescription {
					*results = append(*results, xmpExtPropertyRule(part, "description")...)
				}
				currentProp = propInfo{}
			case xmpExtLevelSchema:
				if !currentSchema.hasSchema {
					*results = append(*results, xmpExtSchemaRule(part, "schema")...)
				}
				if !currentSchema.hasNamespaceURI {
					*results = append(*results, xmpExtSchemaRule(part, "namespaceURI")...)
				}
				if !currentSchema.hasPrefix {
					*results = append(*results, xmpExtSchemaRule(part, "prefix")...)
				}
				// pdfaSchema:property and pdfaSchema:valueType are optional.
				// A schema may declare only properties, only valueTypes, or both.
				*results = append(*results, xmpExtUndefinedValueTypes(part, entryRefs, entryDeclared)...)
				entryRefs = nil
				entryDeclared = map[string]bool{}
				currentSchema = schemaInfo{}
			}
		}
	}

	// Pure-data helper (no *document.Document in scope): use slog.
	slog.Debug("compliance.xmp: extension schema check complete")
}

// xmpExtUndefinedFieldRule returns the "extension schema object contains a
// field not defined by the container schema" rule by part: ISO 19005-1 §6.7.8
// t1, ISO 19005-2/3 §6.6.2.3.2 t1. All three are the same containsUndefinedFields
// test. PDF/A-4 defines no extension-schema vocabulary rule, so part 4 returns
// nothing.
func xmpExtUndefinedFieldRule(part int) []Violation {
	const ctx = "extension schema object contains a field outside the PDF/A extension schema vocabulary"
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_8_t01, ctx)
	case 2:
		return fail(Rule_19005_2_6_6_2_3_2_t01, ctx)
	case 3:
		return fail(Rule_19005_3_6_6_2_3_2_t01, ctx)
	}
	return nil
}

// xmpExtBagRule returns the "extension schema container shall be a valid
// rdf:Bag" rule by part: ISO 19005-1 §6.7.8 t2, ISO 19005-2/3 §6.6.2.3.3 t1.
func xmpExtBagRule(part int) []Violation {
	const ctx = "pdfaExtension:schemas container is not a valid rdf:Bag"
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_8_t02, ctx)
	case 2:
		return fail(Rule_19005_2_6_6_2_3_3_t01, ctx)
	case 3:
		return fail(Rule_19005_3_6_6_2_3_3_t01, ctx)
	}
	return nil
}

// xmpExtValueTypeSeqRule returns the "schema valueType container shall be a
// valid rdf:Seq" rule by part: ISO 19005-1 §6.7.8 t7, ISO 19005-2/3 §6.6.2.3.3 t6.
func xmpExtValueTypeSeqRule(part int) []Violation {
	const ctx = "pdfaSchema:valueType container is not a valid rdf:Seq"
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_8_t07, ctx)
	case 2:
		return fail(Rule_19005_2_6_6_2_3_3_t06, ctx)
	case 3:
		return fail(Rule_19005_3_6_6_2_3_3_t06, ctx)
	}
	return nil
}

// xmpExtPropertySeqRule returns the "schema property container shall be a valid
// rdf:Seq" rule by part: ISO 19005-1 §6.7.8 t6, ISO 19005-2/3 §6.6.2.3.3 t5.
func xmpExtPropertySeqRule(part int) []Violation {
	const ctx = "pdfaSchema:property container is not a valid rdf:Seq"
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_8_t06, ctx)
	case 2:
		return fail(Rule_19005_2_6_6_2_3_3_t05, ctx)
	case 3:
		return fail(Rule_19005_3_6_6_2_3_3_t05, ctx)
	}
	return nil
}

// xmpExtFieldSeqRule returns the "valueType field container shall be a valid
// rdf:Seq" rule by part: ISO 19005-1 §6.7.8 t16, ISO 19005-2/3 §6.6.2.3.3 t15.
func xmpExtFieldSeqRule(part int) []Violation {
	const ctx = "pdfaType:field container is not a valid rdf:Seq"
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_8_t16, ctx)
	case 2:
		return fail(Rule_19005_2_6_6_2_3_3_t15, ctx)
	case 3:
		return fail(Rule_19005_3_6_6_2_3_3_t15, ctx)
	}
	return nil
}

// xmpExtSchemaRule returns the rule for a missing field in a schema definition.
// Mapping: schema->t03, namespaceURI->t04, prefix->t05, property->t06.
func xmpExtSchemaRule(part int, field string) []Violation {
	ctx := fmt.Sprintf("extension schema missing required field '%s'", field)
	switch part {
	case 1:
		switch field {
		case "schema":
			return fail(Rule_19005_1_6_7_8_t03, ctx)
		case "namespaceURI":
			return fail(Rule_19005_1_6_7_8_t04, ctx)
		case "prefix":
			return fail(Rule_19005_1_6_7_8_t05, ctx)
		case "property":
			return fail(Rule_19005_1_6_7_8_t06, ctx)
		}
	case 2:
		switch field {
		case "schema":
			return fail(Rule_19005_2_6_6_2_3_3_t02, ctx)
		case "namespaceURI":
			return fail(Rule_19005_2_6_6_2_3_3_t03, ctx)
		case "prefix":
			return fail(Rule_19005_2_6_6_2_3_3_t04, ctx)
		case "property":
			return fail(Rule_19005_2_6_6_2_3_3_t05, ctx)
		}
	case 3:
		switch field {
		case "schema":
			return fail(Rule_19005_3_6_6_2_3_3_t02, ctx)
		case "namespaceURI":
			return fail(Rule_19005_3_6_6_2_3_3_t03, ctx)
		case "prefix":
			return fail(Rule_19005_3_6_6_2_3_3_t04, ctx)
		case "property":
			return fail(Rule_19005_3_6_6_2_3_3_t05, ctx)
		}
	case 4:
		// ISO 19005-4 drops the extension-schema requirements of parts
		// 1-3 (XMP 2010 packets are self-describing); its §6.6.1 covers
		// actions, not metadata.
		return nil
	}
	return nil
}

// xmpExtPropertyRule returns the rule for a missing field in a property definition.
// Mapping: name->t08, valueType->t09, category->t10, description->t11.
func xmpExtPropertyRule(part int, field string) []Violation {
	return xmpExtPropertyRuleCtx(part, field, fmt.Sprintf("extension property missing required field '%s'", field))
}

// xmpExtPropertyCategoryRule reports a category outside {external, internal}.
// ISO 19005-1 §6.7.8 t10 and ISO 19005-2/3 §6.6.2.3.3 t9 constrain the value,
// not just its presence, and compare it verbatim.
func xmpExtPropertyCategoryRule(part int, value string) []Violation {
	return xmpExtPropertyRuleCtx(part, "category",
		fmt.Sprintf("extension property category %q is neither %q nor %q", value, xmpCategoryExternal, xmpCategoryInternal))
}

// xmpExtPropertyRuleCtx maps a property field to its per-part rule, reporting it
// with the caller's context.
func xmpExtPropertyRuleCtx(part int, field, ctx string) []Violation {
	switch part {
	case 1:
		switch field {
		case "name":
			return fail(Rule_19005_1_6_7_8_t08, ctx)
		case "valueType":
			return fail(Rule_19005_1_6_7_8_t09, ctx)
		case "category":
			return fail(Rule_19005_1_6_7_8_t10, ctx)
		case "description":
			return fail(Rule_19005_1_6_7_8_t11, ctx)
		}
	case 2:
		switch field {
		case "name":
			return fail(Rule_19005_2_6_6_2_3_3_t07, ctx)
		case "valueType":
			return fail(Rule_19005_2_6_6_2_3_3_t08, ctx)
		case "category":
			return fail(Rule_19005_2_6_6_2_3_3_t09, ctx)
		case "description":
			return fail(Rule_19005_2_6_6_2_3_3_t10, ctx)
		}
	case 3:
		switch field {
		case "name":
			return fail(Rule_19005_3_6_6_2_3_3_t07, ctx)
		case "valueType":
			return fail(Rule_19005_3_6_6_2_3_3_t08, ctx)
		case "category":
			return fail(Rule_19005_3_6_6_2_3_3_t09, ctx)
		case "description":
			return fail(Rule_19005_3_6_6_2_3_3_t10, ctx)
		}
	case 4:
		// ISO 19005-4 drops the extension-schema requirements of parts 1-3.
		return nil
	}
	return nil
}

// xmpExtValueTypeRule returns the rule for a missing field in a valueType definition.
// Mapping: type->t12, namespaceURI->t13, prefix->t14, description->t15.
// A missing description IS flagged: the extension schema grammar requires
// a description on every valueType definition, so its absence is a missing
// required field, not an empty optional one.
func xmpExtValueTypeRule(part int, field string) []Violation {
	ctx := fmt.Sprintf("extension valueType missing required field '%s'", field)
	switch part {
	case 1:
		switch field {
		case "type":
			return fail(Rule_19005_1_6_7_8_t12, ctx)
		case "namespaceURI":
			return fail(Rule_19005_1_6_7_8_t13, ctx)
		case "prefix":
			return fail(Rule_19005_1_6_7_8_t14, ctx)
		case "description":
			return fail(Rule_19005_1_6_7_8_t15, ctx)
		}
	case 2:
		switch field {
		case "type":
			return fail(Rule_19005_2_6_6_2_3_3_t11, ctx)
		case "namespaceURI":
			return fail(Rule_19005_2_6_6_2_3_3_t12, ctx)
		case "prefix":
			return fail(Rule_19005_2_6_6_2_3_3_t13, ctx)
		case "description":
			return fail(Rule_19005_2_6_6_2_3_3_t14, ctx)
		}
	case 3:
		switch field {
		case "type":
			return fail(Rule_19005_3_6_6_2_3_3_t11, ctx)
		case "namespaceURI":
			return fail(Rule_19005_3_6_6_2_3_3_t12, ctx)
		case "prefix":
			return fail(Rule_19005_3_6_6_2_3_3_t13, ctx)
		case "description":
			return fail(Rule_19005_3_6_6_2_3_3_t14, ctx)
		}
	case 4:
		// ISO 19005-4 drops the extension-schema requirements of parts 1-3.
		return nil
	}
	return nil
}

// xmpExtFieldRule returns the rule for a missing field in a field definition.
// Mapping: name->t17, valueType->t18, description->t19.
func xmpExtFieldRule(part int, field string) []Violation {
	ctx := fmt.Sprintf("extension field missing required field '%s'", field)
	switch part {
	case 1:
		switch field {
		case "name":
			return fail(Rule_19005_1_6_7_8_t17, ctx)
		case "valueType":
			return fail(Rule_19005_1_6_7_8_t18, ctx)
		case "description":
			return fail(Rule_19005_1_6_7_8_t19, ctx)
		}
	case 2:
		switch field {
		case "name":
			return fail(Rule_19005_2_6_6_2_3_3_t16, ctx)
		case "valueType":
			return fail(Rule_19005_2_6_6_2_3_3_t17, ctx)
		case "description":
			return fail(Rule_19005_2_6_6_2_3_3_t18, ctx)
		}
	case 3:
		switch field {
		case "name":
			return fail(Rule_19005_3_6_6_2_3_3_t16, ctx)
		case "valueType":
			return fail(Rule_19005_3_6_6_2_3_3_t17, ctx)
		case "description":
			return fail(Rule_19005_3_6_6_2_3_3_t18, ctx)
		}
	case 4:
		// ISO 19005-4 drops the extension-schema requirements of parts 1-3.
		return nil
	}
	return nil
}

// xmpPredefinedValueTypes lists XMP value type names that do not require a
// custom valueType definition in the extension schema.  This covers types from
// the XMP Specification (ISO 16684-1) and the PDF specification.
//
//nolint:gochecknoglobals // static lookup table
var xmpPredefinedValueTypes = map[string]bool{
	// Core simple types
	"Text": true, "URI": true, "URL": true, "Boolean": true, "Date": true,
	"Integer": true, "Real": true, "MIMEType": true, "AgentName": true,
	"RenditionClass": true, "GUID": true, "Locale": true, "XPath": true,
	"ProperName": true, "Rational": true, "Part": true, "Lang Alt": true,
	"Choice": true,
	// Structured types predefined in XMP
	"ResourceRef": true, "ResourceEvent": true, "Version": true,
	"Rendition": true, "Job": true, "Thumbnail": true, "Dimensions": true,
	"Font": true, "Colorant": true, "Flash": true, "CFAPattern": true,
	"DeviceSettings": true, "OECF/SFR": true, "GPS": true,
	// Container shorthand types
	"bag Text": true, "bag ProperName": true, "bag Locale": true,
	"bag Job": true, "bag Xpath": true, "seq Text": true,
	"seq ResourceEvent": true, "seq ResourceRef": true, "seq Version": true,
	"alt Text": true,
	// open/closed choice
	"open Choice of Text": true, "closed Choice of Text": true,
	"open Choice of Integer": true, "closed Choice of Integer": true,
	"open Choice of Real": true, "closed Choice of Real": true,
	"open Choice of Date": true, "closed Choice of Date": true,
	// Ordered/unordered array (PDF spec style)
	"Bag Text": true, "Seq Text": true, "Bag ProperName": true,
	"Seq ResourceEvent": true, "Seq ResourceRef": true, "Seq Version": true,
	"Bag Job": true, "Bag Xpath": true, "Bag Locale": true,
}

// xmpCheckExtensionPropertyTypes validates that extension schema properties
// declare valueTypes that correspond to either predefined XMP types or custom
// value types with proper field definitions.  If a property uses a custom
// valueType that has no field definitions in the extension schema, the property
// "does not correspond to" its declared type (clause 6.6.2.3.1 test 2).
// xmpExtSchemaDef holds one pdfaSchema entry parsed from the
// pdfaExtension:schemas bag: its namespace URI, declared property value
// types, and declared custom value types.
type xmpExtSchemaDef struct {
	nsURI      string
	propTypes  map[string]string // property name → declared valueType
	typeFields map[string]bool   // custom type name → hasFields
}

// xmpExtSchemaItemDepth is the rdf:li nesting depth of a single property or
// value-type declaration. Depth 1 is the pdfaSchema entry inside the
// pdfaExtension:schemas bag; depth 2 is one item of its pdfaSchema:property
// or pdfaSchema:valueType Seq (ISO 19005-2/3 §6.6.2.3.3 tests 5 and 6).
const xmpExtSchemaItemDepth = 2

// xmpParseExtensionSchemas parses the pdfaExtension:schemas bag and returns
// one entry per pdfaSchema rdf:li in document order. Entries with the same
// namespace URI are NOT merged: the effective extension type map is keyed
// by URI, so a later same-URI entry replaces the earlier one, and callers
// need document order to resolve which entry wins.
func xmpParseExtensionSchemas(data []byte) []xmpExtSchemaDef {
	var schemas []xmpExtSchemaDef
	var current *xmpExtSchemaDef

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = xmpPassThroughCharset
	decoder.Strict = false

	var inSchemas bool
	var inProperty, inValueType bool
	var propName, propVT string
	var vtName string
	var vtHasField bool
	depth := 0 // nesting depth within a schema rdf:li

	// pdfaSchema:property and pdfaSchema:valueType each hold a Seq of
	// declarations, so one accumulator has to be committed per Seq item.
	// Committing only at the end of the enclosing block kept the last item
	// alone and left every earlier property undeclared, which reported a
	// false ISO 19005-2/3 §6.6.2.3.1 t2 on every ZUGFeRD/Factur-X file: the
	// four-property zf schema resolved only its final entry.
	flushProperty := func() {
		if current != nil && propName != "" && propVT != "" {
			current.propTypes[propName] = propVT
		}
		propName = ""
		propVT = ""
	}
	flushValueType := func() {
		if current != nil && vtName != "" {
			current.typeFields[vtName] = vtHasField
		}
		vtName = ""
		vtHasField = false
	}

	// RDF/XML §2.5 abbreviates a property as an attribute on the node
	// element, so pdfaProperty:name="X" means the same as a
	// <pdfaProperty:name>X</pdfaProperty:name> child. iText writes the whole
	// Factur-X extension schema that way, and veraPDF accepts both spellings.
	// Reading child elements alone left such a schema with no namespace URI
	// and no properties, reporting a false ISO 19005-2/3 §6.6.2.3.1 t1.
	harvestAttrs := func(attrs []xml.Attr) {
		for _, a := range attrs {
			value := strings.TrimSpace(a.Value)
			if value == "" {
				continue
			}
			switch a.Name.Space {
			case xmpPdfaSchemaPrefix:
				if a.Name.Local == "namespaceURI" && current != nil {
					current.nsURI = value
				}
			case xmpPdfaPropertyPrefix:
				if !inProperty {
					continue
				}
				switch a.Name.Local {
				case "name":
					propName = value
				case "valueType":
					propVT = value
				}
			case xmpPdfaTypePrefix:
				if inValueType && a.Name.Local == "type" {
					vtName = value
				}
			}
		}
	}

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if strings.Contains(t.Name.Space, "pdfa/ns/extension") && t.Name.Local == "schemas" {
				inSchemas = true
				continue
			}
			if !inSchemas {
				continue
			}

			switch {
			case t.Name.Local == "li" && t.Name.Space == xmpRDFNamespace:
				depth++
				if depth == 1 {
					// New schema definition.
					s := xmpExtSchemaDef{
						propTypes:  make(map[string]string),
						typeFields: make(map[string]bool),
					}
					schemas = append(schemas, s)
					current = &schemas[len(schemas)-1]
				}
			case t.Name.Local == "Seq" || t.Name.Local == "Bag":
				// Container inside property or valueType block, ignore.
			case t.Name.Space == xmpPdfaSchemaPrefix:
				switch t.Name.Local {
				case "namespaceURI":
					if current != nil {
						if charTok, err2 := decoder.Token(); err2 == nil {
							if cd, ok := charTok.(xml.CharData); ok {
								current.nsURI = strings.TrimSpace(string(cd))
							}
						}
					}
				case "property":
					inProperty = true
				case "valueType":
					inValueType = true
				}
			case t.Name.Space == xmpPdfaPropertyPrefix && inProperty:
				switch t.Name.Local {
				case "name":
					if charTok, err2 := decoder.Token(); err2 == nil {
						if cd, ok := charTok.(xml.CharData); ok {
							propName = strings.TrimSpace(string(cd))
						}
					}
				case "valueType":
					if charTok, err2 := decoder.Token(); err2 == nil {
						if cd, ok := charTok.(xml.CharData); ok {
							propVT = strings.TrimSpace(string(cd))
						}
					}
				}
			case t.Name.Space == xmpPdfaTypePrefix && inValueType:
				switch t.Name.Local {
				case "type":
					if charTok, err2 := decoder.Token(); err2 == nil {
						if cd, ok := charTok.(xml.CharData); ok {
							vtName = strings.TrimSpace(string(cd))
						}
					}
				case "field":
					vtHasField = true
				}
			}

			harvestAttrs(t.Attr)

		case xml.EndElement:
			if t.Name.Local == "schemas" {
				inSchemas = false
				continue
			}
			if !inSchemas {
				continue
			}

			switch {
			case t.Name.Local == "li" && t.Name.Space == xmpRDFNamespace:
				if depth == xmpExtSchemaItemDepth {
					if inProperty {
						flushProperty()
					}
					if inValueType {
						flushValueType()
					}
				}
				depth--
				if depth < 0 {
					depth = 0
				}
			case t.Name.Space == xmpPdfaSchemaPrefix && t.Name.Local == "property":
				// Catches a declaration written without the Seq/rdf:li
				// wrapper; the well-formed case already flushed per item.
				flushProperty()
				inProperty = false
			case t.Name.Space == xmpPdfaSchemaPrefix && t.Name.Local == "valueType":
				flushValueType()
				inValueType = false
			}
		}
	}

	return schemas
}

// xmpCheckExtensionPropertyTypes flags an extension-schema property whose
// declared valueType resolves to neither a predefined XMP type nor a custom
// value type that declares fields, but only when the property is actually used
// in the packet. ISO 19005-2/3 §6.6.2.3.1 t2 is an XMPProperty (usage) rule:
// veraPDF flags fs:property1 on the 6-6-2-3-3-t03-fail-b original, where the
// property is used as a struct whose field is not a member of its fieldless
// type, yet judges the remediated output (same fieldless declaration, property
// removed) compliant. Gating on usage matches both verdicts and stops the
// remediated declaration from reporting a false positive.
func xmpCheckExtensionPropertyTypes(data []byte, part int, results *[]Violation) {
	schemas := xmpParseExtensionSchemas(data)

	// A custom type counts as resolvable only when it declares fields: a
	// fieldless declaration cannot satisfy a struct-valued usage. The declared
	// valueType may wrap the custom element type in a container keyword ("Bag
	// declaration" declares a bag of the custom type "declaration"), and the
	// type definition may live in any schema entry of the packet, so the lookup
	// strips the container prefix and searches every schema (the literal-string
	// same-schema lookup produced a 798-cell phantom pool against
	// pdfd:declarations).
	allTypes := make(map[string]bool)
	for _, s := range schemas {
		for name, hasFields := range s.typeFields {
			if hasFields {
				allTypes[name] = true
			}
		}
	}
	used := xmpExtractPropertyValues(data)
	for _, s := range schemas {
		for pName, vt := range s.propTypes {
			if xmpPredefinedValueTypes[vt] {
				continue
			}
			if allTypes[vt] || allTypes[stripContainerKeyword(vt)] {
				continue
			}
			if _, isUsed := used[s.nsURI][pName]; !isUsed {
				continue
			}
			*results = append(*results, xmpPropertyTypeRule(part, s.nsURI, pName,
				fmt.Sprintf("does not correspond to type %s", vt))...)
		}
	}
}

// xmpCheckExtensionPropertyResolution resolves the value type of every
// property used in an extension-declared namespace. The effective
// extension type map is keyed by namespace URI, so a later pdfaSchema
// entry with the same URI replaces the earlier one. A property declared
// only by a replaced entry then has no resolvable value type and violates
// ISO 19005-2/3 §6.6.2.3.1 t2 / ISO 19005-1 §6.7.9 t3.
// Verified live: PDF_UA-1__5-t04-fail-a under PDF/A-2b fails t2 on
// pdfuaid:part, declared by the first of two same-URI entries; blanking the
// second entry clears the failure.
func xmpCheckExtensionPropertyResolution(data []byte, part int, results *[]Violation) {
	schemas := xmpParseExtensionSchemas(data)
	if len(schemas) == 0 {
		return
	}
	effective := make(map[string]map[string]string, len(schemas))
	for _, s := range schemas {
		if s.nsURI != "" {
			effective[s.nsURI] = s.propTypes
		}
	}
	for ns, props := range xmpExtractPropertyValues(data) {
		declared, isExt := effective[ns]
		if !isExt {
			continue
		}
		// Extension schemas may extend predefined schemas; a predefined
		// member of the namespace stays resolvable from the predefined
		// catalog even when an extension schema declares the same URI.
		predefined := xmpPropertyRegistry[ns]
		for prop := range props {
			if _, ok := declared[prop]; ok {
				continue
			}
			if _, ok := predefined[prop]; ok {
				continue
			}
			*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
				"value type not resolvable: property not declared by the namespace's effective extension schema entry")...)
		}
	}
}

// stripContainerKeyword removes a leading Bag/Seq/Alt container keyword
// (either case convention) from a declared valueType string, returning
// the element type name.
func stripContainerKeyword(vt string) string {
	for _, prefix := range []string{"bag ", "seq ", "alt ", "Bag ", "Seq ", "Alt "} {
		if rest, ok := strings.CutPrefix(vt, prefix); ok {
			return rest
		}
	}
	return vt
}

// xmpContainerType represents observed XMP container types.
type xmpContainerType int

const (
	xmpContSimple xmpContainerType = iota
	xmpContAlt
	xmpContSeq
	xmpContBag
)

// xmpContainerElementType maps container properties to the expected value type
// of their items. For example, exif:ISOSpeedRatings is a Seq of Integer, so
// each rdf:li item must be a valid integer string.
//
//nolint:gochecknoglobals // static lookup table.
var xmpContainerElementType = map[string]map[string]xmpPropType{
	"http://ns.adobe.com/exif/1.0/": {
		"ISOSpeedRatings":         xmpPropInteger, // Seq Integer
		"SubjectArea":             xmpPropInteger, // Seq Integer
		"SubjectLocation":         xmpPropInteger, // Seq Integer
		"ComponentsConfiguration": xmpPropInteger, // Seq Integer
		"LensInfo":                xmpPropReal,    // Seq Rational (approximate as Real)
	},
	"http://ns.adobe.com/tiff/1.0/": {
		"BitsPerSample":    xmpPropInteger, // Seq Integer
		"YCbCrSubSampling": xmpPropInteger, // Seq Integer
		"TransferFunction": xmpPropInteger, // Seq Integer
	},
	"http://purl.org/dc/elements/1.1/": {
		"date": xmpPropDate, // Seq Date
	},
}

// xmpCheckPropertyTypes validates that XMP properties use the correct
// value/container types per their schema definitions. It also validates
// simple property value formats, container item value formats, Alt container
// lang attributes, and struct property structure. It flags unknown property
// names in known (predefined) schemas.
func xmpCheckPropertyTypes(data []byte, part int, _ ast.PdfStream, extraExtensionNS map[string]bool, results *[]Violation) {
	observed := xmpExtractPropertyValues(data)
	_, extensionNS := xmpExtractNamespaces(data)
	maps.Copy(extensionNS, extraExtensionNS)

	// Sorted, because both maps are iterated to EMIT violations and Go
	// randomises map order: the same binary on the same file otherwise reported
	// identical findings in a different order, which reads as a diff to
	// anything comparing two reports.
	for _, ns := range slices.Sorted(maps.Keys(observed)) {
		props := observed[ns]

		// Skip structural namespaces.
		if ns == xmpRDFNamespace || ns == "adobe:ns:meta/" || ns == "http://www.w3.org/XML/1998/namespace" {
			continue
		}
		// Skip extension-declared namespaces. Their types are defined by the extension.
		if extensionNS[ns] {
			continue
		}
		// Skip PDF/A extension schema machinery namespaces.
		if ns == xmpPdfaSchemaPrefix || ns == xmpPdfaPropertyPrefix ||
			ns == xmpPdfaTypePrefix || ns == xmpPdfaFieldPrefix ||
			ns == "http://www.aiim.org/pdfa/ns/extension/" {
			continue
		}

		registry, hasSchema := xmpPropertyRegistry[ns]
		if !hasSchema {
			// Namespace not in registry, already handled by xmpCheckNamespaces.
			continue
		}

		for _, prop := range slices.Sorted(maps.Keys(props)) {
			pv := props[prop]

			// veraPDF instantiates one object per observed property and asks it
			// `isValueTypeCorrect`, so however many ways a property fails its
			// predefined type, the rule answers for it once.
			if details := xmpPropertyTypeDetails(part, ns, prop, pv, registry); len(details) > 0 {
				*results = append(*results, xmpPropertyTypeRule(part, ns, prop, strings.Join(details, "; "))...)
			}
		}
	}
}

// xmpPropertyTypeDetails reports every way one observed XMP property fails the
// type its predefined schema gives it. The caller states the rule once for the
// property, whatever comes back.
func xmpPropertyTypeDetails(part int, ns, prop string, pv xmpPropertyValue, registry map[string]xmpPropType) []string {
	expectedType, known := registry[prop]
	// pdfaid:rev exists only in the PDF/A-4 identification schema. The XMP 2005
	// catalog (ISO 19005-2/3 §6.6.2.3.1, mirrored in xmpPredefined2005Extra)
	// predefines part/conformance/amd/corr only, so rev is an unknown property
	// for parts 2 and 3.
	if known && (part == 2 || part == 3) && ns == xmpPdfaIDNamespace && prop == "rev" {
		known = false
	}
	// For PDF/A-1: properties must be in the 2004 XMP spec. Properties added in
	// later spec versions are "unknown."
	if known && part == 1 {
		if part1Props, ok := xmpPart1Properties[ns]; ok {
			if _, inPart1 := part1Props[prop]; !inPart1 {
				known = false
			}
		}
		// Apply part-1-specific type overrides for properties whose type
		// differs between the 2004 and later XMP specs.
		if known {
			if nsOverrides, ok := xmpPart1TypeOverrides[ns]; ok {
				if overrideType, ok := nsOverrides[prop]; ok {
					expectedType = overrideType
				}
			}
		}
	}
	if !known {
		return []string{"unknown property name"}
	}

	// xmpPropAny matches anything.
	if expectedType == xmpPropAny {
		return nil
	}

	var details []string

	// Check container type properties (Alt/Seq/Bag).
	if isContainerPropType(expectedType) {
		expectedCont := propTypeToContainerType(expectedType)
		if pv.containerType != expectedCont {
			// No simple-as-Bag shorthand exemption: in the 2005 XMP spec (parts
			// 2/3) photoshop:SupplementalCategories is a Bag Text, so a simple
			// value is a type mismatch. Part 1 reads it as plain Text via
			// xmpPart1TypeOverrides, so it never reaches here.
			details = append(details, fmt.Sprintf("observed=%s expected=%s",
				xmpContainerTypeName(pv.containerType),
				xmpContainerTypeName(expectedCont)))
		}

		// Validate Alt container items have xml:lang attribute. Struct-valued
		// Alt items (e.g., xmp:Thumbnails) don't need xml:lang.
		if expectedType == xmpPropAlt && !pv.hasLangAttr && !pv.isStructured {
			details = append(details, "Alt container items missing xml:lang attribute")
		}

		// Validate container item value formats using the element type registry.
		if elemTypes, nsOK := xmpContainerElementType[ns]; nsOK {
			if elemType, propOK := elemTypes[prop]; propOK {
				for i, item := range pv.items {
					if !validateXMPValue(elemType, item) {
						details = append(details,
							fmt.Sprintf("item[%d] value %q does not match expected type", i, item))
					}
				}
			}
		}

		return details
	}

	// Check simple value type properties.
	if isSimplePropType(expectedType) {
		// Expected simple value. Should NOT be in a container.
		if pv.containerType != xmpContSimple {
			details = append(details, fmt.Sprintf("observed=%s expected=Simple",
				xmpContainerTypeName(pv.containerType)))
		}

		// Validate simple property value format.
		if pv.containerType == xmpContSimple && pv.value != "" {
			valid := validateXMPValue(expectedType, pv.value)
			// EXIF, TIFF, and xmpDM properties typed as Real accept rational "N/D" notation.
			if !valid && expectedType == xmpPropReal && acceptsRationalNotation(ns) {
				valid = isValidXMPRealOrRational(pv.value)
			}
			if !valid {
				details = append(details,
					fmt.Sprintf("value %q does not match expected type", pv.value))
			}
		}

		return details
	}

	// Check struct type properties.
	if expectedType == xmpPropStruct {
		// Expected struct. Should NOT be in a container.
		if pv.containerType != xmpContSimple {
			details = append(details, fmt.Sprintf("observed=%s expected=Simple",
				xmpContainerTypeName(pv.containerType)))
		}

		// Verify struct property has sub-elements, not plain text.
		if !pv.isStructured && pv.containerType == xmpContSimple {
			details = append(details, "struct property has plain text value instead of sub-elements")
		}
	}

	return details
}

// propTypeToContainerType converts a container xmpPropType to xmpContainerType.
func propTypeToContainerType(t xmpPropType) xmpContainerType {
	switch t {
	case xmpPropAlt:
		return xmpContAlt
	case xmpPropSeq:
		return xmpContSeq
	case xmpPropBag:
		return xmpContBag
	case xmpPropText, xmpPropURI, xmpPropInteger, xmpPropReal, xmpPropBoolean, xmpPropDate, xmpPropStruct, xmpPropAny:
		return xmpContSimple
	}
	return xmpContSimple
}

// xmpPropertyTypeRule returns the appropriate violation for a property type
// mismatch in the given part.
func xmpPropertyTypeRule(part int, ns, prop, detail string) []Violation {
	ctx := fmt.Sprintf("%s:%s %s", ns, prop, detail)
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_9_t03, ctx)
	case 2:
		return fail(Rule_19005_2_6_6_2_3_1_t02, ctx)
	case 3:
		return fail(Rule_19005_3_6_6_2_3_1_t02, ctx)
	case 4:
		// ISO 19005-4 drops the extension-schema requirements of parts 1-3.
		return nil
	}
	return nil
}

// xmpStructTypeFields maps each standard XMP structured value type namespace to
// the field names it defines. Sourced from the Adobe XMP Specification Part 2
// (Additional Properties), the structured types referenced by the predefined
// PDF/A schemas. A struct field whose namespace is one of these but whose local
// name is absent here is not a member of the type.
//
// ResourceRef and ResourceEvent hold the members the 2005 XMP Specification
// defines, the edition ISO 19005-2 normatively references and the one litho
// already judges property names against (§6.6.2.3.1 t1 reports a property "not
// predefined in XMP 2005"). Adobe added members later, and a file using one
// states a value the referenced type does not define: pdfjs issue15220.pdf
// writes stRef:originalDocumentID inside xmpMM:DerivedFrom and stEvt:changed
// inside xmpMM:History, and veraPDF reports both under §6.6.2.3.1 t2. Every
// name here was confirmed against veraPDF one field per fixture; every later
// addition it rejects is absent.
//
// The remaining types keep comprehensive field sets: no corpus file exercises
// them and none was checked against the oracle, so an omitted-but-valid field
// would be a false positive where a listed-but-invalid one only costs a
// missed detection.
//
//nolint:gochecknoglobals // static lookup table mirroring the XMP Specification.
var xmpStructTypeFields = map[string]map[string]bool{
	// ResourceEvent (stEvt), XMP 2005. "changed" is a later addition.
	"http://ns.adobe.com/xap/1.0/sType/ResourceEvent#": {
		"action": true, "instanceID": true,
		"parameters": true, "softwareAgent": true, "when": true,
	},
	// ResourceRef (stRef), XMP 2005. originalDocumentID, filePath, fromPart,
	// toPart, lastModifyDate, maskMarkers, partMapping, alternatePaths and the
	// three placed* members are later additions.
	"http://ns.adobe.com/xap/1.0/sType/ResourceRef#": {
		"documentID": true, "instanceID": true, "manager": true,
		"managerVariant": true, "manageTo": true, "manageUI": true,
		"renditionClass": true, "renditionParams": true, "versionID": true,
	},
	// Version (stVer).
	"http://ns.adobe.com/xap/1.0/sType/Version#": {
		"comments": true, "event": true, "modifyDate": true,
		"modifier": true, "version": true,
	},
	// Job (stJob).
	"http://ns.adobe.com/xap/1.0/sType/Job#": {
		"id": true, "name": true, "url": true,
	},
	// Dimensions (stDim).
	"http://ns.adobe.com/xap/1.0/sType/Dimensions#": {
		"h": true, "w": true, "unit": true,
	},
	// Font (stFnt).
	"http://ns.adobe.com/xap/1.0/sType/Font#": {
		"childFontFiles": true, "composite": true, "fontFace": true,
		"fontFamily": true, "fontFileName": true, "fontName": true,
		"fontType": true, "versionString": true,
	},
}

// xmpCheckStructFieldNames flags a property whose structured value carries a
// field that belongs to a standard XMP value-type namespace (stEvt, stRef,
// stVer, stJob, stDim, stFnt) but is not a member of that type. veraPDF reports
// such a property as not corresponding to its declared structured type (e.g.
// "does not correspond to type seq resourceevent"), violating ISO 19005-1
// §6.7.9 t3 / ISO 19005-2/3 §6.6.2.3.1 t2. litho otherwise validates only a
// property's container type and never descends into its struct fields, so a
// misspelled field (stEvt:When for stEvt:when) slips through. The violation is
// attributed to the enclosing top-level property, matching veraPDF's context
// node; xmpPropertyTypeRule returns nil for part 4, which drops the requirement.
func xmpCheckStructFieldNames(data []byte, part int, results *[]Violation) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = xmpPassThroughCharset
	decoder.Strict = false

	inDescription := false
	depth := 0
	var propNS, propLocal string
	flagged := make(map[string]bool) // dedup per enclosing property

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description" {
				inDescription = true
				depth = 0
				continue
			}
			if !inDescription {
				continue
			}
			depth++
			if depth == 1 && t.Name.Space != "" && t.Name.Space != xmpRDFNamespace {
				propNS = t.Name.Space
				propLocal = t.Name.Local
				continue
			}
			fields, isStructType := xmpStructTypeFields[t.Name.Space]
			if !isStructType || propLocal == "" || fields[t.Name.Local] {
				continue
			}
			key := propNS + " " + propLocal
			if flagged[key] {
				continue
			}
			flagged[key] = true
			*results = append(*results, xmpPropertyTypeRule(part, propNS, propLocal,
				fmt.Sprintf("structured value contains field %s not defined by its value type", t.Name.Local))...)
		case xml.EndElement:
			if t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description" {
				inDescription = false
				depth = 0
				propNS, propLocal = "", ""
				continue
			}
			if !inDescription {
				continue
			}
			if depth == 1 {
				propNS, propLocal = "", ""
			}
			depth--
		}
	}
}

// xmpPropertyValue holds the observed container type and text content of an XMP property.
type xmpPropertyValue struct {
	containerType xmpContainerType
	value         string   // text content for simple values (attribute or element text)
	items         []string // text items for container elements (rdf:li values)
	hasLangAttr   bool     // true if any rdf:li has xml:lang attribute
	isStructured  bool     // true if the property contains sub-elements (not just text)
}

// xmpExtractPropertyValues parses XMP XML and returns the observed container
// type plus text content for each top-level property.
func xmpExtractPropertyValues(data []byte) map[string]map[string]xmpPropertyValue {
	result := make(map[string]map[string]xmpPropertyValue)
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = xmpPassThroughCharset
	decoder.Strict = false

	inDescription := false
	depth := 0
	var currentNS, currentLocal string
	inContainer := false // true when inside rdf:Alt/Seq/Bag at depth 2
	inLi := false        // true when inside rdf:li at depth 3
	var currentCharData string

	record := func(ns, local string, pv xmpPropertyValue) {
		if result[ns] == nil {
			result[ns] = make(map[string]xmpPropertyValue)
		}
		result[ns][local] = pv
	}

	getOrCreate := func(ns, local string) xmpPropertyValue {
		if m, ok := result[ns]; ok {
			if pv, ok := m[local]; ok {
				return pv
			}
		}
		return xmpPropertyValue{containerType: xmpContSimple}
	}

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description" {
				// Scan attributes for attribute-form property values.
				for _, attr := range t.Attr {
					if attr.Name.Space != "" &&
						attr.Name.Space != xmpRDFNamespace &&
						attr.Name.Space != "http://www.w3.org/XML/1998/namespace" &&
						attr.Name.Space != "http://www.w3.org/2000/xmlns/" {
						record(attr.Name.Space, attr.Name.Local, xmpPropertyValue{
							containerType: xmpContSimple,
							value:         attr.Value,
						})
					}
				}
				inDescription = true
				depth = 0
				continue
			}
			if inDescription {
				depth++
				if depth == 1 && t.Name.Space != "" && t.Name.Space != xmpRDFNamespace {
					currentNS = t.Name.Space
					currentLocal = t.Name.Local
					currentCharData = ""
					inContainer = false
					inLi = false
					// Default to simple until a container child appears.
					pv := getOrCreate(currentNS, currentLocal)
					pv.containerType = xmpContSimple
					// rdf:parseType="Resource" is the RDF/XML shorthand that makes
					// the element's own content a struct (RDF/XML Syntax
					// Specification, 2.3.3), which the XMP Specification uses for
					// ResourceRef values. It says so whether or not any field
					// follows, so an empty struct written that way is structured:
					// govdocs thread_100_100847.pdf carries
					// <xapMM:DerivedFrom rdf:parseType="Resource"></...> and litho
					// read its whitespace as a plain text value.
					for _, attr := range t.Attr {
						if attr.Name.Space == xmpRDFNamespace && attr.Name.Local == "parseType" && attr.Value == "Resource" {
							pv.isStructured = true
							break
						}
					}
					record(currentNS, currentLocal, pv)
				}
				if depth == 2 && currentNS != "" {
					if t.Name.Space == xmpRDFNamespace {
						switch t.Name.Local {
						case "Alt":
							pv := getOrCreate(currentNS, currentLocal)
							pv.containerType = xmpContAlt
							record(currentNS, currentLocal, pv)
							inContainer = true
						case "Seq":
							pv := getOrCreate(currentNS, currentLocal)
							pv.containerType = xmpContSeq
							record(currentNS, currentLocal, pv)
							inContainer = true
						case "Bag":
							pv := getOrCreate(currentNS, currentLocal)
							pv.containerType = xmpContBag
							record(currentNS, currentLocal, pv)
							inContainer = true
						}
					} else {
						// Child element that is not an rdf container → structured.
						pv := getOrCreate(currentNS, currentLocal)
						pv.isStructured = true
						record(currentNS, currentLocal, pv)
					}
				}
				// Inside an rdf:li, a non-rdf sub-element indicates structured content.
				if depth > 3 && inLi && t.Name.Space != xmpRDFNamespace {
					pv := getOrCreate(currentNS, currentLocal)
					pv.isStructured = true
					record(currentNS, currentLocal, pv)
				}
				if depth == 3 && inContainer && t.Name.Space == xmpRDFNamespace && t.Name.Local == "li" {
					inLi = true
					currentCharData = ""
					// Check for xml:lang attribute.
					for _, attr := range t.Attr {
						if attr.Name.Space == "http://www.w3.org/XML/1998/namespace" && attr.Name.Local == "lang" {
							pv := getOrCreate(currentNS, currentLocal)
							pv.hasLangAttr = true
							record(currentNS, currentLocal, pv)
							break
						}
					}
				}
			}
		case xml.EndElement:
			if t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description" {
				inDescription = false
				depth = 0
				currentNS = ""
				currentLocal = ""
				inContainer = false
				inLi = false
			} else if inDescription {
				if depth == 1 && currentNS != "" && !inContainer {
					// End of simple property element: capture accumulated text.
					trimmed := strings.TrimSpace(currentCharData)
					if trimmed != "" {
						pv := getOrCreate(currentNS, currentLocal)
						pv.value = trimmed
						record(currentNS, currentLocal, pv)
					}
					currentNS = ""
					currentLocal = ""
				}
				if depth == 1 {
					currentNS = ""
					currentLocal = ""
					inContainer = false
					inLi = false
				}
				if depth == 3 && inLi {
					// End of rdf:li: capture accumulated text as an item.
					trimmed := strings.TrimSpace(currentCharData)
					if currentNS != "" {
						pv := getOrCreate(currentNS, currentLocal)
						pv.items = append(pv.items, trimmed)
						record(currentNS, currentLocal, pv)
					}
					inLi = false
					currentCharData = ""
				}
				depth--
			}
		case xml.CharData:
			if inDescription {
				if depth == 1 && currentNS != "" && !inContainer {
					// CharData at depth 1 (simple property text).
					currentCharData += string(t)
				}
				if depth == 3 && inLi {
					// CharData inside rdf:li.
					currentCharData += string(t)
				}
			}
		}
	}

	return result
}

func xmpContainerTypeName(t xmpContainerType) string {
	switch t {
	case xmpContSimple:
		return "Simple"
	case xmpContAlt:
		return "Alt"
	case xmpContSeq:
		return "Seq"
	case xmpContBag:
		return "Bag"
	}
	return "Unknown"
}

// acceptsRationalNotation returns true if the namespace allows Real properties
// to use rational "N/D" notation (e.g., "72/1", "768/702").
func acceptsRationalNotation(ns string) bool {
	return ns == "http://ns.adobe.com/exif/1.0/" ||
		ns == "http://ns.adobe.com/exif/1.0/aux/" ||
		ns == "http://ns.adobe.com/tiff/1.0/" ||
		ns == "http://ns.adobe.com/xmp/1.0/DynamicMedia/"
}

// xmpTypeRef is one value-type reference made by a schema entry, either from a
// pdfaProperty:valueType or, when field is true, a pdfaField:valueType.
type xmpTypeRef struct {
	name  string
	field bool
}

// Which element a captured type name came from.
const (
	xmpTypeTextProperty = iota
	xmpTypeTextField
	xmpTypeTextDeclaration
)

// xmpValueTypeArrayPrefixes are the array wrappers veraPDF resolves before
// looking a type up, so "Bag Text" resolves through "text".
//
//nolint:gochecknoglobals // static lookup table.
var xmpValueTypeArrayPrefixes = []string{"alt ", "bag ", "seq "}

// xmpChoicePattern mirrors veraPDF's ValidatorsContainer.getSimplifiedType,
// which strips an open or closed choice qualifier before the lookup. That is
// why "Closed Choice of Text" resolves through "text".
//
//nolint:gochecknoglobals // compiled-once pattern.
var xmpChoicePattern = regexp.MustCompile(`^(open |closed )?(choice |choice$)(of )?`)

// xmpSimplifyValueType normalises a value type the way veraPDF does before
// looking it up: lower-case, choice qualifier removed, array wrapper removed,
// surrounding space collapsed.
func xmpSimplifyValueType(name string) string {
	v := strings.ToLower(strings.TrimSpace(name))
	v = strings.TrimSpace(xmpChoicePattern.ReplaceAllString(v, ""))
	for _, prefix := range xmpValueTypeArrayPrefixes {
		if after, ok := strings.CutPrefix(v, prefix); ok {
			v = strings.TrimSpace(after)
			break
		}
	}
	return v
}

// xmpPredefinedValueTypes1 is the value-type vocabulary ISO 19005-1 files are
// judged against. Taken from veraPDF 1.30.2's
// ValidatorsContainerCreator.getPredefinedContainerForPDFA_1, whose registry
// this mirrors key for key.
//
//nolint:gochecknoglobals // static lookup table.
var xmpPredefinedValueTypes1 = map[string]bool{
	"agentname": true, "boolean": true, "cfapattern": true, "date": true,
	"devicesettings": true, "dimensions": true, "flash": true, "gpscoordinate": true,
	"integer": true, "job": true, "lang alt": true, "locale": true, "mimetype": true,
	"oecf/sfr": true, "propername": true, "rational": true, "real": true,
	"renditionclass": true, "resourceevent": true, "resourceref": true, "text": true,
	"thumbnail": true, "uri": true, "url": true, "version": true, "xpath": true,
}

// xmpPredefinedValueTypes23 extends [xmpPredefinedValueTypes1] with the ten
// types ISO 19005-2 and -3 add, from veraPDF's
// getPredefinedContainerForPDFA_2_3. The split is real: Colorant and Timecode
// are undefined under part 1 and predefined under parts 2 and 3.
//
//nolint:gochecknoglobals // static lookup table.
var xmpPredefinedValueTypes23 = map[string]bool{
	"beatsplicestretch": true, "colorant": true, "font": true, "marker": true,
	"media": true, "projectlink": true, "resamplestretch": true, "time": true,
	"timecode": true, "timescalestretch": true,
}

// xmpValueTypeDefined reports whether a normalised type resolves for the part,
// either as a predefined type or as one the schema entry declares itself.
func xmpValueTypeDefined(part int, simplified string, declared map[string]bool) bool {
	if simplified == "" || declared[simplified] || xmpPredefinedValueTypes1[simplified] {
		return true
	}
	return part != 1 && xmpPredefinedValueTypes23[simplified]
}

// xmpExtUndefinedValueTypes reports every value type a schema entry names that
// neither the part's predefined vocabulary nor the entry itself defines, per
// ISO 19005-1 §6.7.8 t9 and t18 and ISO 19005-2/3 §6.6.2.3.3 t8 and t17.
func xmpExtUndefinedValueTypes(part int, refs []xmpTypeRef, declared map[string]bool) []Violation {
	var results []Violation
	for _, ref := range refs {
		if xmpValueTypeDefined(part, xmpSimplifyValueType(ref.name), declared) {
			continue
		}
		ctx := fmt.Sprintf("value type %q is not defined", ref.name)
		switch {
		case ref.field && part == 1:
			results = append(results, fail(Rule_19005_1_6_7_8_t18, ctx)...)
		case ref.field && part == 2:
			results = append(results, fail(Rule_19005_2_6_6_2_3_3_t17, ctx)...)
		case ref.field && part == 3:
			results = append(results, fail(Rule_19005_3_6_6_2_3_3_t17, ctx)...)
		case part == 1:
			results = append(results, fail(Rule_19005_1_6_7_8_t09, ctx)...)
		case part == 2:
			results = append(results, fail(Rule_19005_2_6_6_2_3_3_t08, ctx)...)
		case part == 3:
			results = append(results, fail(Rule_19005_3_6_6_2_3_3_t08, ctx)...)
		}
	}
	return results
}
