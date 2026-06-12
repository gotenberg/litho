package compliance

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	// VeraPDF accepts these without extension schema declarations.
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
	// registered XMP schema namespace and veraPDF's predefined-schema
	// list does not include it, so properties under it need an extension
	// schema like any other custom namespace.
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
	// XMP Paged-Text (xmpTPg:): for PDF/A-1, VeraPDF only accepts
	// MaxPageSize and NPages as predefined. Colorants, Fonts, PlateNames
	// and SwatchGroups require extension schema declarations.
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

	var results []Violation

	// Catalog /Metadata required (§6.7.2 / §6.6.1).
	xmpData, metaStream := xmpLoadMetadata(ctx, doc, catalog, part, &results)
	if xmpData == nil {
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

	if ctx.Err() != nil {
		return results
	}

	// XMP packet header (§6.7.2.1 / §6.6.2.1).
	xmpCheckPacketHeader(xmpData, part, &results)

	if ctx.Err() != nil {
		return results
	}

	// XMP schema validation (§6.7.9 / §6.6.2.3.1).
	xmpCheckNamespaces(xmpData, part, &results)

	if ctx.Err() != nil {
		return results
	}

	// Property-level predefined-schema audit (§6.7.9 / §6.6.2.3.1).
	xmpCheckPredefinedProperties(ctx, xmpData, part, &results)

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
	xmpCheckPropertyTypes(xmpData, part, metaStream, &results)

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

	// Decode the stream content.
	xmpData, err := stream.Bytes(ctx)
	if err != nil || len(xmpData) == 0 {
		decoded, decErr := doc.DecodeStream(ctx, stream)
		if decErr != nil {
			*results = append(*results, xmpMetadataRule(part)...)
			return nil, ast.PdfStream{}
		}
		xmpData = decoded
	}

	if len(xmpData) == 0 {
		*results = append(*results, xmpMetadataRule(part)...)
		return nil, ast.PdfStream{}
	}

	return xmpData, stream
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

	before, _, ok := bytes.Cut(header, []byte("?>"))
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
// excluded deliberately: veraPDF builds its property model from direct
// schema children only, so a structure field's namespace needs no
// declaration of its own and flagging it is a false positive.
func xmpExtractNamespaces(data []byte) (used map[string]bool, extensions map[string]bool) {
	used = make(map[string]bool)
	extensions = make(map[string]bool)

	decoder := xml.NewDecoder(bytes.NewReader(data))
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
				// fields, which veraPDF never checks for declarations.
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

func xmpSchemaRule(part int, ns string) []Violation {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_7_9_t02, "namespace %s not predefined or in extension schema", ns)
	case 2:
		return failf(Rule_19005_2_6_6_2_3_1_t01, "namespace %s not predefined or in extension schema", ns)
	case 3:
		return failf(Rule_19005_3_6_6_2_3_1_t01, "namespace %s not predefined or in extension schema", ns)
	case 4:
		// ISO 19005-4 drops the extension-schema requirements of parts
		// 1-3 (XMP 2010 packets are self-describing) and the veraPDF
		// PDFA-4 profile carries no such rule; its 6.6.1 covers actions.
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
// direct children of a top-level rdf:Description count: veraPDF builds its
// XMPProperty objects the same way (VeraPDFMeta.addSchema walks schema-node
// children only), so fields of structured values, including ones serialized
// as nested rdf:Description elements, must stay invisible here. ISO 19005
// §6.7.9 / §6.6.2.3.1 apply to properties, not to structure fields.
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
func xmpCheckPredefinedProperties(ctx context.Context, data []byte, part int, results *[]Violation) {
	catalog := xmpPredefinedPropertyCatalog(part)
	if catalog == nil {
		// ISO 19005-4 drops the extension-schema requirements of parts
		// 1-3; see xmpSchemaRule.
		return
	}
	_, extensionNS := xmpExtractNamespaces(data)
	prefixes := xmpExtractPrefixMap(ctx, data)

	for _, p := range xmpExtractTopLevelProperties(data) {
		// The pdfaid identification schema participates like any other
		// predefined namespace: the catalog for the part decides which
		// names are predefined (the 2005 catalog lacks rev, the 2004 one
		// lacks corr and rev), so a leftover PDF/A-4 pdfaid:rev fails here
		// exactly as it does under veraPDF. Value-level pdfaid checks stay
		// with xmpCheckPDFAIdentification.
		// Extension schemas may extend predefined schemas: veraPDF
		// registers extension properties per namespace and name with no
		// predefined-namespace restriction (veraPDF-library
		// SchemasDefinitionCreator.createExtendedSchemasDefinition), and
		// litho keeps its namespace-level leniency for extension-declared
		// namespaces.
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

// xmpSchemaPropertyRule mirrors veraPDF rules ISO 19005-1 §6.7.9 t2 and
// ISO 19005-2/3 §6.6.2.3.1 t1 for a property whose namespace is predefined
// but whose name is not in the predefined property set and not declared by
// an extension schema.
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

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	// Only properties in the pdfaid namespace URI carry the identification
	// and prefix obligations. A matching local name under a different URI
	// (pdfuaid:part on a dual PDF/A + PDF/UA file, prism:part, ...) belongs
	// to another schema and must not be flagged; veraPDF keys these rules
	// to the pdfaid namespace only.
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

	// Fallback: byte scan for pdfaid presence in unparseable XMP only. In
	// well-formed XMP, absence of the pdfaid namespace means the
	// identification schema is missing, even when pdfaid-looking text
	// appears under a different namespace URI.
	if !parseClean && !hasPart && !hasConf && revValue == "" && !wrongPrefixPart && !wrongPrefixConf && !wrongPrefixRev {
		hasPart = bytes.Contains(data, []byte("pdfaid:part")) ||
			bytes.Contains(data, []byte("pdfa/ns/id"))
		if !hasPart {
			return
		}
		hasConf = bytes.Contains(data, []byte("pdfaid:conformance"))
	}

	level := PDFALevel(profile)
	expectedPart := fmt.Sprintf("%d", part)

	// Check: pdfaid:part must exist.
	if !hasPart {
		*results = append(*results, xmpPDFAIdMissingRule(part)...)
		return
	}

	// For parts 1-3, pdfaid:conformance must exist (unless level is 0 for part 4).
	if part < 4 && !hasConf {
		*results = append(*results, xmpPDFAIdMissingRule(part)...)
		return
	}

	// Validate pdfaid:part value.
	if partValue != "" && partValue != expectedPart {
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
	// (typical decoy in the veraPDF 6-6-2-3-3 suite, and a common
	// leftover after remediation empties a defective rdf:Description)
	// is harmless, and firing on it produced a 266-cell-per-profile
	// litho-only pool. veraPDF evaluates prefix correctness per element
	// (e.g. descriptionPrefix == "pdfaType"), never per declaration.
	for uri, expectedPrefix := range xmpExpectedPrefix {
		actualPrefix := ""
		for _, p := range xmpExtractAllPrefixesForURI(ctx, data, uri) {
			if p != expectedPrefix && bytes.Contains(data, []byte("<"+p+":")) {
				actualPrefix = p
				break
			}
		}
		if actualPrefix != "" {
			switch uri {
			case xmpPdfaSchemaPrefix:
				*results = append(*results, xmpExtSchemaRule(part, "schema")...)
				*results = append(*results, xmpExtSchemaRule(part, "namespaceURI")...)
				*results = append(*results, xmpExtSchemaRule(part, "prefix")...)
				*results = append(*results, xmpExtSchemaRule(part, "property")...)
				*results = append(*results, xmpExtSchemaRule(part, "valueType")...)
			case xmpPdfaPropertyPrefix:
				*results = append(*results, xmpExtPropertyRule(part, "name")...)
				*results = append(*results, xmpExtPropertyRule(part, "valueType")...)
				*results = append(*results, xmpExtPropertyRule(part, "category")...)
				*results = append(*results, xmpExtPropertyRule(part, "description")...)
			case xmpPdfaTypePrefix:
				*results = append(*results, xmpExtValueTypeRule(part, "type")...)
				*results = append(*results, xmpExtValueTypeRule(part, "namespaceURI")...)
				*results = append(*results, xmpExtValueTypeRule(part, "prefix")...)
				*results = append(*results, xmpExtValueTypeRule(part, "description")...)
				*results = append(*results, xmpExtValueTypeRule(part, "field")...)
			case xmpPdfaFieldPrefix:
				*results = append(*results, xmpExtFieldRule(part, "name")...)
				*results = append(*results, xmpExtFieldRule(part, "valueType")...)
				*results = append(*results, xmpExtFieldRule(part, "description")...)
			}
			// Skip SAX-based checks. Prefix mismatch means Go's XML parser
			// resolved the URI correctly but the document uses the wrong prefix.
			// The SAX parser would see correct URIs and not detect the issue.
			return
		}
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
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
	// containerStack tracks, per OPEN rdf:Seq/rdf:Bag, the level its
	// rdf:li items push. A stack, not a flag: a nested container (the
	// field Seq inside a valueType definition) must not destroy the
	// outer container's context, or every definition after the first in
	// the outer Seq is evaluated at the wrong level (the phantom
	// missing-field storms of the 792-cell corpus pool).
	var containerStack []xmpExtSchemaLevel

	currentLevel := func() xmpExtSchemaLevel {
		if len(levelStack) == 0 {
			return xmpExtLevelSchema
		}
		return levelStack[len(levelStack)-1]
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
			if t.Name.Local == "Seq" || t.Name.Local == "Bag" {
				level := xmpExtLevelSchema
				switch {
				case hasPending:
					// §6.7.8 t7/t16: the valueType and field containers must be
					// rdf:Seq; flag an explicit rdf:Bag.
					if t.Name.Local == "Bag" {
						switch pendingLevel {
						case xmpExtLevelValueType:
							*results = append(*results, xmpExtValueTypeSeqRule(part)...)
						case xmpExtLevelField:
							*results = append(*results, xmpExtFieldSeqRule(part)...)
						case xmpExtLevelSchema, xmpExtLevelProperty:
							// The schema and property containers are validated
							// elsewhere (isValidBag and the existing property checks).
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
				continue
			}

			// Track fields within each definition level.
			// For each known field local name, verify the namespace prefix
			// matches the expected constant. If the local name matches but the
			// namespace is wrong, emit the corresponding rule (VeraPDF checks
			// namespace prefix correctness).
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
				case "category":
					currentProp.hasCategory = true
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

		case xml.EndElement:
			if !inSchemas {
				continue
			}

			// Close the innermost container when its Seq/Bag closes.
			if t.Name.Local == "Seq" || t.Name.Local == "Bag" {
				if len(containerStack) > 0 {
					containerStack = containerStack[:len(containerStack)-1]
				}
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
				if !currentProp.hasCategory {
					*results = append(*results, xmpExtPropertyRule(part, "category")...)
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
				currentSchema = schemaInfo{}
			}
		}
	}

	// Pure-data helper (no *document.Document in scope): use slog.
	slog.Debug("compliance.xmp: extension schema check complete")
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
		// 1-3 (XMP 2010 packets are self-describing) and the veraPDF
		// PDFA-4 profiles carry no such rule; 6.6.1 there covers
		// actions, not metadata.
		return nil
	}
	return nil
}

// xmpExtPropertyRule returns the rule for a missing field in a property definition.
// Mapping: name->t08, valueType->t09, category->t10, description->t11.
func xmpExtPropertyRule(part int, field string) []Violation {
	ctx := fmt.Sprintf("extension property missing required field '%s'", field)
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
		// ISO 19005-4 drops the extension-schema requirements of parts
		// 1-3; the veraPDF PDFA-4 profile carries no such rule.
		return nil
	}
	return nil
}

// xmpExtValueTypeRule returns the rule for a missing field in a valueType definition.
// Mapping: type->t12, namespaceURI->t13, prefix->t14, description->t15.
// A missing description IS flagged: veraPDF fails t14 on the
// description-less valueType in 6-6-2-3-3-t03-fail-a (golden), so the
// `isDescriptionValidText` test treats absence as invalid.
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
		// ISO 19005-4 drops the extension-schema requirements of parts
		// 1-3; the veraPDF PDFA-4 profile carries no such rule.
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
		// ISO 19005-4 drops the extension-schema requirements of parts
		// 1-3; the veraPDF PDFA-4 profile carries no such rule.
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

// xmpParseExtensionSchemas parses the pdfaExtension:schemas bag and returns
// one entry per pdfaSchema rdf:li in document order. Entries with the same
// namespace URI are NOT merged: veraPDF keys its extension type map by URI
// with later entries replacing earlier ones, and callers need the order to
// mirror that.
func xmpParseExtensionSchemas(data []byte) []xmpExtSchemaDef {
	var schemas []xmpExtSchemaDef
	var current *xmpExtSchemaDef

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	var inSchemas bool
	var inProperty, inValueType bool
	var propName, propVT string
	var vtName string
	var vtHasField bool
	depth := 0 // nesting depth within a schema rdf:li

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
				depth--
				if depth < 0 {
					depth = 0
				}
			case t.Name.Space == xmpPdfaSchemaPrefix && t.Name.Local == "property":
				// End of property block: record accumulated property.
				if current != nil && propName != "" && propVT != "" {
					current.propTypes[propName] = propVT
				}
				propName = ""
				propVT = ""
				inProperty = false
			case t.Name.Space == xmpPdfaSchemaPrefix && t.Name.Local == "valueType":
				// End of valueType block: record accumulated type.
				if current != nil && vtName != "" {
					current.typeFields[vtName] = vtHasField
				}
				vtName = ""
				vtHasField = false
				inValueType = false
			}
		}
	}

	return schemas
}

func xmpCheckExtensionPropertyTypes(data []byte, part int, results *[]Violation) {
	schemas := xmpParseExtensionSchemas(data)

	// Validate: for each property whose valueType is not predefined, check
	// that the custom type is defined with field declarations. The
	// declared string may wrap the custom element type in a container
	// keyword ("Bag declaration" declares a bag of the custom type
	// "declaration"), and the type definition may live in any schema
	// entry of the packet, so the lookup strips the container prefix and
	// searches every schema (the literal-string same-schema lookup
	// produced a 798-cell phantom pool against pdfd:declarations).
	allTypes := make(map[string]bool)
	for _, s := range schemas {
		for name, hasFields := range s.typeFields {
			if hasFields {
				allTypes[name] = true
			}
		}
	}
	for _, s := range schemas {
		for pName, vt := range s.propTypes {
			if xmpPredefinedValueTypes[vt] {
				continue
			}
			if allTypes[vt] || allTypes[stripContainerKeyword(vt)] {
				continue
			}
			*results = append(*results, xmpPropertyTypeRule(part, s.nsURI, pName,
				fmt.Sprintf("does not correspond to type %s", vt))...)
		}
	}
}

// xmpCheckExtensionPropertyResolution mirrors veraPDF's value-type
// resolution for properties used in extension-declared namespaces. veraPDF
// keys its extension type map by namespace URI and a later pdfaSchema entry
// with the same URI replaces the earlier one, so a property declared only
// by a replaced entry has no resolvable value type and fails
// isValueTypeCorrect (ISO 19005-2/3 §6.6.2.3.1 t2, ISO 19005-1 §6.7.9 t3).
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
		// Extension schemas may extend predefined schemas; veraPDF still
		// resolves the predefined members from its built-in catalogs.
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
func xmpCheckPropertyTypes(data []byte, part int, _ ast.PdfStream, results *[]Violation) {
	observed := xmpExtractPropertyValues(data)
	_, extensionNS := xmpExtractNamespaces(data)

	for ns, props := range observed {
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

		for prop, pv := range props {
			expectedType, known := registry[prop]
			// pdfaid:rev exists only in the PDF/A-4 identification schema.
			// The XMP 2005 catalog (ISO 19005-2/3 §6.6.2.3.1, mirrored in
			// xmpPredefined2005Extra) predefines part/conformance/amd/corr
			// only, so rev is an unknown property for parts 2 and 3.
			if known && (part == 2 || part == 3) && ns == xmpPdfaIDNamespace && prop == "rev" {
				known = false
			}
			// For PDF/A-1: properties must be in the 2004 XMP spec.
			// Properties added in later spec versions are "unknown."
			if known && part == 1 {
				if part1Props, ok := xmpPart1Properties[ns]; ok {
					if _, inPart1 := part1Props[prop]; !inPart1 {
						known = false
					}
				}
				// Apply part-1-specific type overrides for properties
				// whose type differs between the 2004 and later XMP specs.
				if known {
					if nsOverrides, ok := xmpPart1TypeOverrides[ns]; ok {
						if overrideType, ok := nsOverrides[prop]; ok {
							expectedType = overrideType
						}
					}
				}
			}
			if !known {
				// Property name not defined in predefined schema. Violation.
				*results = append(*results, xmpPropertyTypeRule(part, ns, prop, "unknown property name")...)
				continue
			}

			// xmpPropAny matches anything.
			if expectedType == xmpPropAny {
				continue
			}

			// Check container type properties (Alt/Seq/Bag).
			if isContainerPropType(expectedType) {
				expectedCont := propTypeToContainerType(expectedType)
				if pv.containerType != expectedCont {
					// No simple-as-Bag shorthand exemption: veraPDF 1.30
					// fails isValueTypeCorrect for a simple-valued
					// photoshop:SupplementalCategories under parts 2/3
					// (verified live), and part 1 reads it as plain Text
					// via xmpPart1TypeOverrides so it never reaches here.
					*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
						fmt.Sprintf("observed=%s expected=%s",
							xmpContainerTypeName(pv.containerType),
							xmpContainerTypeName(expectedCont)))...)
				}

				// Validate Alt container items have xml:lang attribute.
				// Struct-valued Alt items (e.g., xmp:Thumbnails) don't need xml:lang.
				if expectedType == xmpPropAlt && !pv.hasLangAttr && !pv.isStructured {
					*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
						"Alt container items missing xml:lang attribute")...)
				}

				// Validate container item value formats using the element type registry.
				if elemTypes, nsOK := xmpContainerElementType[ns]; nsOK {
					if elemType, propOK := elemTypes[prop]; propOK {
						for i, item := range pv.items {
							if !validateXMPValue(elemType, item) {
								*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
									fmt.Sprintf("item[%d] value %q does not match expected type", i, item))...)
							}
						}
					}
				}

				continue
			}

			// Check simple value type properties.
			if isSimplePropType(expectedType) {
				// Expected simple value. Should NOT be in a container.
				if pv.containerType != xmpContSimple {
					*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
						fmt.Sprintf("observed=%s expected=Simple",
							xmpContainerTypeName(pv.containerType)))...)
				}

				// Validate simple property value format.
				if pv.containerType == xmpContSimple && pv.value != "" {
					valid := validateXMPValue(expectedType, pv.value)
					// EXIF, TIFF, and xmpDM properties typed as Real accept rational "N/D" notation.
					if !valid && expectedType == xmpPropReal && acceptsRationalNotation(ns) {
						valid = isValidXMPRealOrRational(pv.value)
					}
					if !valid {
						*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
							fmt.Sprintf("value %q does not match expected type", pv.value))...)
					}
				}

				continue
			}

			// Check struct type properties.
			if expectedType == xmpPropStruct {
				// Expected struct. Should NOT be in a container.
				if pv.containerType != xmpContSimple {
					*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
						fmt.Sprintf("observed=%s expected=Simple",
							xmpContainerTypeName(pv.containerType)))...)
				}

				// Verify struct property has sub-elements, not plain text.
				if !pv.isStructured && pv.containerType == xmpContSimple {
					*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
						"struct property has plain text value instead of sub-elements")...)
				}
			}
		}
	}
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
		// ISO 19005-4 drops the extension-schema requirements of parts
		// 1-3; the veraPDF PDFA-4 profile carries no such rule.
		return nil
	}
	return nil
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
