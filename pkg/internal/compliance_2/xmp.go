package compliance2

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
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

	// RDF/XML structural namespaces (not property schemas).
	"http://www.w3.org/1999/02/22-rdf-syntax-ns#": true, // rdf
	"http://www.w3.org/XML/1998/namespace":        true, // xml
	"adobe:ns:meta/":                              true, // x:xmpmeta
	"http://www.w3.org/2001/XMLSchema-instance":   true, // xsi
	"http://www.w3.org/2001/XMLSchema":            true, // xs
	"http://www.w3.org/2000/xmlns/":               true, // xmlns
	"http://www.aiim.org/pdfa/":                   true, // pdfa

	// PDF/A extension schema machinery.
	"http://www.aiim.org/pdfa/ns/extension/": true,
	"http://www.aiim.org/pdfa/ns/schema#":    true,
	"http://www.aiim.org/pdfa/ns/property#":  true,
	"http://www.aiim.org/pdfa/ns/field#":     true,
	"http://www.aiim.org/pdfa/ns/type#":      true,
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
func checkXMPGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if isPDFUA(profile) {
		slog.Debug("compliance_2/xmp: skipping XMP checks for PDF/UA profile", "profile", profile)
		return nil
	}

	part := pdfaPart(profile)
	if part == 0 {
		slog.Debug("compliance_2/xmp: skipping XMP checks for unknown profile", "profile", profile)
		return nil
	}

	slog.Debug("compliance_2/xmp: starting XMP checks", "profile", profile, "part", part)

	var results []Result

	// 1. Catalog /Metadata required (§6.7.2 / §6.6.1).
	xmpData, metaStream := xmpLoadMetadata(ctx, doc, catalog, part, &results)
	if xmpData == nil {
		slog.Debug("compliance_2/xmp: no XMP data available, stopping checks")
		return results
	}

	if ctx.Err() != nil {
		return results
	}

	// 1b. XMP encoding must be UTF-8 for part 4 (§6.7.2.1 t05).
	if part == 4 { //nolint:mnd // PDF/A-4.
		xmpCheckEncoding(xmpData, part, &results)
	}

	if ctx.Err() != nil {
		return results
	}

	// 2. XMP packet header (§6.7.2.1 / §6.6.2.1).
	xmpCheckPacketHeader(xmpData, part, &results)

	if ctx.Err() != nil {
		return results
	}

	// 3. XMP schema validation (§6.7.9 / §6.6.2.3.1).
	xmpCheckNamespaces(xmpData, part, &results)

	if ctx.Err() != nil {
		return results
	}

	// 4. PDF/A identification (§6.7.11 / §6.6.4 / §6.7.3).
	xmpCheckPDFAIdentification(xmpData, profile, part, &results)

	if ctx.Err() != nil {
		return results
	}

	// 5. Extension schema structure (§6.7.8 / §6.6.2.3.3).
	xmpCheckExtensionSchemas(xmpData, part, &results)

	if ctx.Err() != nil {
		return results
	}

	// 6. DocInfo ↔ XMP consistency (§6.7.3) — parts 1 and 4 only.
	if part == 1 || part == 4 { //nolint:mnd // PDF/A parts with DocInfo sync requirement
		xmpCheckDocInfoSync(ctx, doc, xmpData, part, &results)
	}

	if ctx.Err() != nil {
		return results
	}

	// 7. XMP property types (§6.7.2 / §6.6.2.3.1).
	xmpCheckPropertyTypes(xmpData, part, metaStream, &results)

	slog.Debug("compliance_2/xmp: XMP checks complete", "violations", len(results))
	return results
}

// --- 1. Catalog /Metadata required ---

// xmpLoadMetadata retrieves and decodes the XMP metadata stream from the
// catalog. It validates that the stream dict has /Type Metadata and /Subtype XML.
func xmpLoadMetadata(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, results *[]Result) ([]byte, ast.PdfStream) {
	mdObj, ok := catalog["Metadata"]
	if !ok {
		slog.Debug("compliance_2/xmp: catalog missing /Metadata key")
		*results = append(*results, xmpMetadataRule(part, 1)...)
		return nil, ast.PdfStream{}
	}

	slog.Debug("compliance_2/xmp: resolving /Metadata stream")
	stream, err := document.ResolveStream(ctx, doc, mdObj)
	if err != nil || stream.Dict == nil {
		slog.Debug("compliance_2/xmp: failed to resolve /Metadata stream", "error", err)
		*results = append(*results, xmpMetadataRule(part, 1)...)
		return nil, ast.PdfStream{}
	}

	// Validate stream dict entries: Type=Metadata, Subtype=XML.
	typeName, _ := document.ResolveName(ctx, doc, stream.Dict["Type"])
	subtypeName, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
	slog.Debug("compliance_2/xmp: metadata stream dict",
		"Type", typeName, "Subtype", subtypeName)

	if typeName != "Metadata" || subtypeName != "XML" {
		slog.Debug("compliance_2/xmp: metadata stream has wrong Type/Subtype")
		*results = append(*results, xmpMetadataRule(part, 1)...)
		return nil, ast.PdfStream{}
	}

	// For part 1, the metadata stream must not have a /Filter key.
	if part == 1 {
		if _, hasFilter := stream.Dict["Filter"]; hasFilter {
			slog.Debug("compliance_2/xmp: metadata stream has /Filter (forbidden in part 1)")
			*results = append(*results, fail(Rule_19005_1_6_7_2_t02, "catalog metadata stream has Filter key")...)
		}
	}

	// Decode the stream content.
	slog.Debug("compliance_2/xmp: decoding metadata stream bytes")
	xmpData, err := stream.Bytes()
	if err != nil || len(xmpData) == 0 {
		slog.Debug("compliance_2/xmp: Bytes() returned empty, trying filter.DecodeStream")
		decoded, decErr := filter.DecodeStream(stream)
		if decErr != nil {
			slog.Debug("compliance_2/xmp: DecodeStream failed", "error", decErr)
			*results = append(*results, xmpMetadataRule(part, 1)...)
			return nil, ast.PdfStream{}
		}
		xmpData = decoded
	}

	if len(xmpData) == 0 {
		slog.Debug("compliance_2/xmp: metadata stream is empty")
		*results = append(*results, xmpMetadataRule(part, 1)...)
		return nil, ast.PdfStream{}
	}

	slog.Debug("compliance_2/xmp: loaded XMP data", "size", len(xmpData))
	return xmpData, stream
}

// xmpMetadataRule returns the correct rule for the missing/invalid metadata
// stream based on the PDF/A part number.
func xmpMetadataRule(part, test int) []Result {
	_ = test
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_2_t01, "catalog missing or invalid /Metadata stream")
	case 2: //nolint:mnd
		return fail(Rule_19005_2_6_6_2_1_t01, "catalog missing or invalid /Metadata stream")
	case 3: //nolint:mnd
		return fail(Rule_19005_3_6_6_2_1_t01, "catalog missing or invalid /Metadata stream")
	case 4: //nolint:mnd
		return fail(Rule_19005_4_6_7_2_1_t01, "catalog missing or invalid /Metadata stream")
	}
	return nil
}

// --- 2. XMP packet header ---

// xmpCheckPacketHeader checks that the XMP packet header does not use the
// forbidden 'bytes' or 'encoding' attributes.
func xmpCheckPacketHeader(data []byte, part int, results *[]Result) {
	header := data
	if len(header) > xmpPacketHeaderSize {
		header = header[:xmpPacketHeaderSize]
	}

	slog.Debug("compliance_2/xmp: checking XMP packet header", "headerLen", len(header))

	before, _, ok := bytes.Cut(header, []byte("?>"))
	if !ok {
		slog.Debug("compliance_2/xmp: no processing instruction found in header")
		return
	}
	pi := before

	slog.Debug("compliance_2/xmp: found processing instruction", "pi", string(pi))

	if bytes.Contains(pi, []byte("bytes=")) {
		slog.Debug("compliance_2/xmp: XMP packet header contains 'bytes' attribute")
		*results = append(*results, xmpPacketHeaderBytesRule(part)...)
	}

	if bytes.Contains(pi, []byte("encoding=")) {
		slog.Debug("compliance_2/xmp: XMP packet header contains 'encoding' attribute")
		*results = append(*results, xmpPacketHeaderEncodingRule(part)...)
	}
}

func xmpPacketHeaderBytesRule(part int) []Result {
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_5_t01, "")
	case 2: //nolint:mnd
		return fail(Rule_19005_2_6_6_2_1_t02, "")
	case 3: //nolint:mnd
		return fail(Rule_19005_3_6_6_2_1_t02, "")
	case 4: //nolint:mnd
		return fail(Rule_19005_4_6_7_2_1_t02, "")
	}
	return nil
}

func xmpPacketHeaderEncodingRule(part int) []Result {
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_5_t02, "")
	case 2: //nolint:mnd
		return fail(Rule_19005_2_6_6_2_1_t03, "")
	case 3: //nolint:mnd
		return fail(Rule_19005_3_6_6_2_1_t03, "")
	case 4: //nolint:mnd
		return fail(Rule_19005_4_6_7_2_1_t03, "")
	}
	return nil
}

// --- 1b. XMP encoding check ---

// xmpCheckEncoding verifies that the XMP packet is encoded as UTF-8.
// For PDF/A-4 (§6.7.2.1 t05), UTF-16 encoding is a violation.
func xmpCheckEncoding(data []byte, _ int, results *[]Result) {
	slog.Debug("compliance_2/xmp: checking XMP encoding")

	// Check for UTF-16 BOM: FE FF (BE) or FF FE (LE).
	if len(data) >= 2 { //nolint:mnd // BOM is 2 bytes.
		if data[0] == 0xFE && data[1] == 0xFF {
			slog.Debug("compliance_2/xmp: UTF-16BE BOM detected")
			*results = append(*results, failf(Rule_19005_4_6_7_2_1_t05, "UTF-16BE")...)
			return
		}
		if data[0] == 0xFF && data[1] == 0xFE {
			slog.Debug("compliance_2/xmp: UTF-16LE BOM detected")
			*results = append(*results, failf(Rule_19005_4_6_7_2_1_t05, "UTF-16LE")...)
			return
		}
	}

	// Also check XML declaration for encoding="UTF-16".
	headerLen := min(len(data), xmpPacketHeaderSize)
	header := strings.ToLower(string(data[:headerLen]))
	if strings.Contains(header, "encoding=\"utf-16") || strings.Contains(header, "encoding='utf-16") {
		slog.Debug("compliance_2/xmp: UTF-16 declared in XML processing instruction")
		*results = append(*results, failf(Rule_19005_4_6_7_2_1_t05, "UTF-16")...)
	}
}

// --- 3. XMP schema validation ---

// xmpCheckNamespaces walks the XMP XML and verifies that every namespace URI
// used by properties is either a predefined XMP schema or is declared in a
// PDF/A extension schema within the same XMP packet.
func xmpCheckNamespaces(data []byte, part int, results *[]Result) {
	slog.Debug("compliance_2/xmp: checking XMP namespace schemas")

	usedNamespaces, extensionNamespaces := xmpExtractNamespaces(data)

	slog.Debug("compliance_2/xmp: extracted namespaces",
		"used", len(usedNamespaces), "extensions", len(extensionNamespaces))

	for ns := range usedNamespaces {
		if knownXMPNamespaces[ns] || extensionNamespaces[ns] {
			continue
		}
		slog.Debug("compliance_2/xmp: unknown namespace without extension schema", "ns", ns)
		*results = append(*results, xmpSchemaRule(part, ns)...)
	}
}

// xmpExtractNamespaces parses XMP XML and returns the set of namespace URIs
// used by properties (including nested elements and attributes at all depths)
// and the set of namespace URIs declared in extension schemas.
func xmpExtractNamespaces(data []byte) (used map[string]bool, extensions map[string]bool) {
	used = make(map[string]bool)
	extensions = make(map[string]bool)

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	slog.Debug("compliance_2/xmp: starting namespace extraction")

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
	descDepth := 0 // >0 means we are inside an rdf:Description

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			slog.Debug("compliance_2/xmp: start element",
				"space", t.Name.Space, "local", t.Name.Local)

			if t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description" {
				descDepth++
				// Record namespaces from attributes on rdf:Description.
				for _, attr := range t.Attr {
					if !ignoredNS[attr.Name.Space] {
						slog.Debug("compliance_2/xmp: attr namespace used on Description", "ns", attr.Name.Space)
						used[attr.Name.Space] = true
					}
				}
				continue
			}

			// Record namespace of every element inside rdf:Description,
			// regardless of nesting depth. This catches properties nested
			// inside rdf:Bag, rdf:Seq, rdf:Alt, and structured value types.
			if descDepth > 0 && !ignoredNS[t.Name.Space] {
				slog.Debug("compliance_2/xmp: property namespace used", "ns", t.Name.Space, "local", t.Name.Local, "depth", descDepth)
				used[t.Name.Space] = true
			}

			// Also record namespaces from attributes on nested elements
			// (e.g., rdf:resource attributes with typed values).
			if descDepth > 0 {
				for _, attr := range t.Attr {
					if !ignoredNS[attr.Name.Space] {
						slog.Debug("compliance_2/xmp: nested attr namespace used", "ns", attr.Name.Space, "local", attr.Name.Local)
						used[attr.Name.Space] = true
					}
				}
			}

			// Track extension schema namespace declarations.
			if t.Name.Local == "schemas" && strings.Contains(t.Name.Space, "pdfa/ns/extension") {
				slog.Debug("compliance_2/xmp: entering extension schema block")
				inExtensionSchema = true
			}
			if inExtensionSchema && t.Name.Local == "namespaceURI" {
				if charTok, charErr := decoder.Token(); charErr == nil {
					if charData, ok := charTok.(xml.CharData); ok {
						ns := strings.TrimSpace(string(charData))
						if ns != "" {
							slog.Debug("compliance_2/xmp: extension schema namespace", "ns", ns)
							extensions[ns] = true
						}
					}
				}
			}

		case xml.EndElement:
			if t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description" {
				descDepth--
			}
			if t.Name.Local == "schemas" {
				inExtensionSchema = false
			}
		}
	}

	slog.Debug("compliance_2/xmp: namespace extraction complete",
		"usedCount", len(used), "extensionCount", len(extensions))
	return used, extensions
}

func xmpSchemaRule(part int, ns string) []Result {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_7_9_t02, "namespace %s not predefined or in extension schema", ns)
	case 2: //nolint:mnd
		return failf(Rule_19005_2_6_6_2_3_1_t01, "namespace %s not predefined or in extension schema", ns)
	case 3: //nolint:mnd
		return failf(Rule_19005_3_6_6_2_3_1_t01, "namespace %s not predefined or in extension schema", ns)
	case 4: //nolint:mnd
		return failf(Rule_19005_4_6_6_1_t01, "namespace %s not predefined or in extension schema", ns)
	}
	return nil
}

// --- 4. PDF/A identification ---

// xmpCheckPDFAIdentification verifies that the XMP contains pdfaid:part and
// pdfaid:conformance entries with correct values matching the declared profile.
func xmpCheckPDFAIdentification(data []byte, profile string, part int, results *[]Result) {
	slog.Debug("compliance_2/xmp: checking PDF/A identification", "profile", profile, "part", part)

	var partValue, confValue, revValue string
	hasPart := false
	hasConf := false
	var wrongPrefixPart, wrongPrefixConf, wrongPrefixRev bool

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			// Check attributes (inline form).
			for _, attr := range t.Attr {
				if attr.Name.Space == xmpPdfaIDNamespace {
					slog.Debug("compliance_2/xmp: pdfaid attr found",
						"local", attr.Name.Local, "value", attr.Value)
					switch attr.Name.Local {
					case "part":
						hasPart = true
						partValue = attr.Value
					case "conformance":
						hasConf = true
						confValue = attr.Value
					case "rev":
						revValue = attr.Value
					}
				} else if attr.Name.Space != "" && attr.Name.Space != xmpPdfaIDNamespace {
					// Attribute with matching local name but wrong namespace prefix.
					switch attr.Name.Local {
					case "part":
						slog.Debug("compliance_2/xmp: pdfaid:part attr has wrong namespace prefix",
							"space", attr.Name.Space, "expected", xmpPdfaIDNamespace)
						wrongPrefixPart = true
					case "conformance":
						slog.Debug("compliance_2/xmp: pdfaid:conformance attr has wrong namespace prefix",
							"space", attr.Name.Space, "expected", xmpPdfaIDNamespace)
						wrongPrefixConf = true
					case "rev":
						slog.Debug("compliance_2/xmp: pdfaid:rev attr has wrong namespace prefix",
							"space", attr.Name.Space, "expected", xmpPdfaIDNamespace)
						wrongPrefixRev = true
					}
				}
			}
			// Check element form — read text content.
			if t.Name.Space == xmpPdfaIDNamespace {
				slog.Debug("compliance_2/xmp: pdfaid element found", "local", t.Name.Local)
				if charTok, charErr := decoder.Token(); charErr == nil {
					if charData, ok := charTok.(xml.CharData); ok {
						val := strings.TrimSpace(string(charData))
						slog.Debug("compliance_2/xmp: pdfaid element value",
							"local", t.Name.Local, "value", val)
						switch t.Name.Local {
						case "part":
							hasPart = true
							partValue = val
						case "conformance":
							hasConf = true
							confValue = val
						case "rev":
							revValue = val
						}
					}
				}
			} else if t.Name.Space != "" && t.Name.Space != xmpPdfaIDNamespace &&
				t.Name.Space != xmpRDFNamespace {
				// Element with matching local name but wrong namespace prefix.
				switch t.Name.Local {
				case "part":
					slog.Debug("compliance_2/xmp: pdfaid:part element has wrong namespace prefix",
						"space", t.Name.Space, "expected", xmpPdfaIDNamespace)
					wrongPrefixPart = true
				case "conformance":
					slog.Debug("compliance_2/xmp: pdfaid:conformance element has wrong namespace prefix",
						"space", t.Name.Space, "expected", xmpPdfaIDNamespace)
					wrongPrefixConf = true
				case "rev":
					slog.Debug("compliance_2/xmp: pdfaid:rev element has wrong namespace prefix",
						"space", t.Name.Space, "expected", xmpPdfaIDNamespace)
					wrongPrefixRev = true
				}
			}
		}
	}

	slog.Debug("compliance_2/xmp: pdfaid parsed",
		"hasPart", hasPart, "partValue", partValue,
		"hasConf", hasConf, "confValue", confValue,
		"revValue", revValue,
		"wrongPrefixPart", wrongPrefixPart,
		"wrongPrefixConf", wrongPrefixConf,
		"wrongPrefixRev", wrongPrefixRev)

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

	// Fallback: byte scan for pdfaid presence in unparseable XMP.
	if !hasPart && !hasConf {
		hasPart = bytes.Contains(data, []byte("pdfaid:part")) ||
			bytes.Contains(data, []byte("pdfa/ns/id"))
		if !hasPart {
			slog.Debug("compliance_2/xmp: no pdfaid identification found at all")
			return
		}
		hasConf = bytes.Contains(data, []byte("pdfaid:conformance"))
	}

	level := pdfaLevel(profile)
	expectedPart := fmt.Sprintf("%d", part)

	// Check: pdfaid:part must exist.
	if !hasPart {
		slog.Debug("compliance_2/xmp: pdfaid:part missing")
		*results = append(*results, xmpPDFAIdMissingRule(part)...)
		return
	}

	// For parts 1-3, pdfaid:conformance must exist (unless level is 0 for part 4).
	if part < 4 && !hasConf { //nolint:mnd
		slog.Debug("compliance_2/xmp: pdfaid:conformance missing for part < 4")
		*results = append(*results, xmpPDFAIdMissingRule(part)...)
		return
	}

	// Validate pdfaid:part value.
	if partValue != "" && partValue != expectedPart {
		slog.Debug("compliance_2/xmp: pdfaid:part mismatch",
			"actual", partValue, "expected", expectedPart)
		*results = append(*results, xmpPDFAPartRule(part, partValue)...)
	}

	// Validate pdfaid:conformance value (parts 1-3).
	if part < 4 && confValue != "" && level != 0 { //nolint:mnd
		expectedConf := strings.ToUpper(string(rune(level)))
		if !strings.EqualFold(confValue, expectedConf) {
			slog.Debug("compliance_2/xmp: pdfaid:conformance mismatch",
				"actual", confValue, "expected", expectedConf)
			*results = append(*results, xmpPDFAConformanceRule(part, confValue)...)
		}
	}

	// Part 4 conformance validation depends on the sub-profile.
	if part == 4 { //nolint:mnd
		xmpCheckPart4Conformance(profile, hasConf, confValue, results)

		// Part 4: validate pdfaid:rev (must be "2020").
		if revValue != "" && revValue != "2020" {
			slog.Debug("compliance_2/xmp: pdfaid:rev invalid", "rev", revValue)
			*results = append(*results, failf(Rule_19005_4_6_7_3_t05, "rev=%s", revValue)...)
		}
	}
}

// xmpCheckPart4Conformance validates pdfaid:conformance for PDF/A-4 sub-profiles.
//   - PDF/A-4 (plain): conformance must be absent or empty.
//   - PDF/A-4e: conformance must be absent/empty or "E".
//   - PDF/A-4f: conformance must be absent/empty or "F".
func xmpCheckPart4Conformance(profile string, hasConf bool, confValue string, results *[]Result) {
	confUpper := strings.ToUpper(strings.TrimSpace(confValue))

	slog.Debug("compliance_2/xmp: checking part 4 conformance",
		"profile", profile, "hasConf", hasConf, "confValue", confValue, "confUpper", confUpper)

	switch profile {
	case "PDF/A-4":
		// Plain PDF/A-4: conformance must be absent or empty.
		if hasConf && confUpper != "" {
			slog.Debug("compliance_2/xmp: pdfaid:conformance present for plain PDF/A-4")
			*results = append(*results, failf(Rule_19005_4_6_7_3_t03, "conformance=%s but must be absent for plain PDF/A-4", confValue)...)
		}
	case "PDF/A-4e":
		// PDF/A-4e: conformance must be absent/empty or "E".
		if hasConf && confUpper != "" && confUpper != "E" {
			slog.Debug("compliance_2/xmp: pdfaid:conformance wrong for PDF/A-4e",
				"actual", confValue)
			*results = append(*results, failf(Rule_19005_4_6_7_3_t03, "conformance=%s but must be absent or 'E' for PDF/A-4e", confValue)...)
		}
	case "PDF/A-4f":
		// PDF/A-4f: conformance must be absent/empty or "F".
		if hasConf && confUpper != "" && confUpper != "F" {
			slog.Debug("compliance_2/xmp: pdfaid:conformance wrong for PDF/A-4f",
				"actual", confValue)
			*results = append(*results, failf(Rule_19005_4_6_7_3_t03, "conformance=%s but must be absent or 'F' for PDF/A-4f", confValue)...)
		}
	}
}

func xmpPDFAIdMissingRule(part int) []Result {
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_11_t01, "missing PDF/A Identification Schema")
	case 2: //nolint:mnd
		return fail(Rule_19005_2_6_6_4_t01, "missing PDF/A Identification Schema")
	case 3: //nolint:mnd
		return fail(Rule_19005_3_6_6_4_t01, "missing PDF/A Identification Schema")
	case 4: //nolint:mnd
		return fail(Rule_19005_4_6_7_3_t01, "missing PDF/A Identification Schema")
	}
	return nil
}

func xmpPDFAPartRule(part int, actual string) []Result {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_7_11_t02, "pdfaid:part=%s", actual)
	case 2: //nolint:mnd
		return failf(Rule_19005_2_6_6_4_t02, "pdfaid:part=%s", actual)
	case 3: //nolint:mnd
		return failf(Rule_19005_3_6_6_4_t02, "pdfaid:part=%s", actual)
	case 4: //nolint:mnd
		return failf(Rule_19005_4_6_7_3_t02, "pdfaid:part=%s", actual)
	}
	return nil
}

func xmpPDFAConformanceRule(part int, actual string) []Result {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_7_11_t03, "pdfaid:conformance=%s", actual)
	case 2: //nolint:mnd
		return failf(Rule_19005_2_6_6_4_t03, "pdfaid:conformance=%s", actual)
	case 3: //nolint:mnd
		return failf(Rule_19005_3_6_6_4_t03, "pdfaid:conformance=%s", actual)
	case 4: //nolint:mnd
		return failf(Rule_19005_4_6_7_3_t03, "pdfaid:conformance=%s", actual)
	}
	return nil
}

// xmpPDFAIdPrefixRule returns the appropriate rule for a pdfaid field that uses
// the wrong namespace prefix. The field parameter is "part", "conformance", or "rev".
func xmpPDFAIdPrefixRule(part int, field string) []Result {
	ctx := fmt.Sprintf("pdfaid:%s uses wrong namespace prefix", field)
	switch field {
	case "part":
		switch part {
		case 1:
			return fail(Rule_19005_1_6_7_11_t04, ctx)
		case 2: //nolint:mnd
			return fail(Rule_19005_2_6_6_4_t04, ctx)
		case 3: //nolint:mnd
			return fail(Rule_19005_3_6_6_4_t04, ctx)
		case 4: //nolint:mnd
			return fail(Rule_19005_4_6_7_3_t04, ctx)
		}
	case "conformance":
		switch part {
		case 1:
			return fail(Rule_19005_1_6_7_11_t05, ctx)
		case 2: //nolint:mnd
			return fail(Rule_19005_2_6_6_4_t05, ctx)
		case 3: //nolint:mnd
			return fail(Rule_19005_3_6_6_4_t05, ctx)
		}
	case "rev":
		if part == 4 { //nolint:mnd
			return fail(Rule_19005_4_6_7_3_t06, ctx)
		}
	}
	return nil
}

// --- 5. Extension schema structure ---

// xmpExtSchemaLevel identifies the nesting context within an extension schema.
type xmpExtSchemaLevel int

const (
	xmpExtLevelSchema    xmpExtSchemaLevel = iota // inside a schema rdf:li
	xmpExtLevelProperty                           // inside a property rdf:li
	xmpExtLevelValueType                          // inside a valueType rdf:li
	xmpExtLevelField                              // inside a field rdf:li
)

// xmpCheckExtensionSchemas validates that PDF/A extension schemas have all
// required fields per ISO 19005 clause 6.6.2.3.3 / 6.7.8.
// Each missing field emits a specific rule (t02–t18).
func xmpCheckExtensionSchemas(data []byte, part int, results *[]Result) {
	slog.Debug("compliance_2/xmp: checking extension schema structure")

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	var inSchemas bool

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
				slog.Debug("compliance_2/xmp: entering extension schemas block")
				inSchemas = true
				continue
			}
			if !inSchemas {
				continue
			}

			// When we see rdf:li, push the pending level.
			if t.Name.Local == "li" {
				if hasPending {
					levelStack = append(levelStack, pendingLevel)
					hasPending = false
				} else if len(levelStack) == 0 {
					levelStack = append(levelStack, xmpExtLevelSchema)
				}
				slog.Debug("compliance_2/xmp: pushed extension level",
					"level", currentLevel(), "depth", len(levelStack))
				continue
			}

			// Track fields within each definition level.
			// For each known field local name, verify the namespace prefix
			// matches the expected constant. If the local name matches but the
			// namespace is wrong, emit the corresponding rule (VeraPDF checks
			// namespace prefix correctness).
			switch {
			case t.Name.Space == xmpPdfaSchemaPrefix:
				slog.Debug("compliance_2/xmp: pdfaSchema element", "local", t.Name.Local)
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
					slog.Debug("compliance_2/xmp: schema field has wrong namespace prefix",
						"local", t.Name.Local, "space", t.Name.Space,
						"expected", xmpPdfaSchemaPrefix)
					*results = append(*results, xmpExtSchemaRule(part, t.Name.Local)...)
				case "valueType":
					slog.Debug("compliance_2/xmp: schema valueType has wrong namespace prefix",
						"space", t.Name.Space, "expected", xmpPdfaSchemaPrefix)
					// Still need to set up pending level for child parsing.
					pendingLevel = xmpExtLevelValueType
					hasPending = true
				}
			case t.Name.Space == xmpPdfaPropertyPrefix && currentLevel() == xmpExtLevelProperty:
				slog.Debug("compliance_2/xmp: pdfaProperty element", "local", t.Name.Local)
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
					slog.Debug("compliance_2/xmp: property field has wrong namespace prefix",
						"local", t.Name.Local, "space", t.Name.Space,
						"expected", xmpPdfaPropertyPrefix)
					*results = append(*results, xmpExtPropertyRule(part, t.Name.Local)...)
				}
			case t.Name.Space == xmpPdfaTypePrefix && currentLevel() == xmpExtLevelValueType:
				slog.Debug("compliance_2/xmp: pdfaType element", "local", t.Name.Local)
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
					slog.Debug("compliance_2/xmp: valueType field has wrong namespace prefix",
						"local", t.Name.Local, "space", t.Name.Space,
						"expected", xmpPdfaTypePrefix)
					*results = append(*results, xmpExtValueTypeRule(part, t.Name.Local)...)
				case "field":
					slog.Debug("compliance_2/xmp: valueType field element has wrong namespace prefix",
						"space", t.Name.Space, "expected", xmpPdfaTypePrefix)
					// Still need to set up pending level for child parsing.
					pendingLevel = xmpExtLevelField
					hasPending = true
				}
			case t.Name.Space == xmpPdfaFieldPrefix && currentLevel() == xmpExtLevelField:
				slog.Debug("compliance_2/xmp: pdfaField element", "local", t.Name.Local)
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
					slog.Debug("compliance_2/xmp: pdfaField element has wrong namespace prefix",
						"local", t.Name.Local, "space", t.Name.Space,
						"expected", xmpPdfaFieldPrefix)
					*results = append(*results, xmpExtFieldRule(part, t.Name.Local)...)
				}
			}

		case xml.EndElement:
			if !inSchemas {
				continue
			}

			if strings.Contains(t.Name.Space, "pdfa/ns/extension") && t.Name.Local == "schemas" {
				slog.Debug("compliance_2/xmp: leaving extension schemas block")
				inSchemas = false
				continue
			}

			if t.Name.Local != "li" || len(levelStack) == 0 {
				continue
			}

			// Pop the stack and validate the completed definition.
			level := levelStack[len(levelStack)-1]
			levelStack = levelStack[:len(levelStack)-1]

			slog.Debug("compliance_2/xmp: popped extension level",
				"level", level, "remaining", len(levelStack))

			switch level {
			case xmpExtLevelField:
				slog.Debug("compliance_2/xmp: validating field definition",
					"hasName", currentField.hasName,
					"hasValueType", currentField.hasValueType,
					"hasDesc", currentField.hasDescription)
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
				slog.Debug("compliance_2/xmp: validating valueType definition",
					"hasType", currentValueType.hasType,
					"hasNS", currentValueType.hasNamespaceURI,
					"hasPrefix", currentValueType.hasPrefix,
					"hasDesc", currentValueType.hasDescription)
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
				slog.Debug("compliance_2/xmp: validating property definition",
					"hasName", currentProp.hasName,
					"hasValueType", currentProp.hasValueType,
					"hasCategory", currentProp.hasCategory,
					"hasDesc", currentProp.hasDescription)
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
				slog.Debug("compliance_2/xmp: validating schema definition",
					"hasSchema", currentSchema.hasSchema,
					"hasNS", currentSchema.hasNamespaceURI,
					"hasPrefix", currentSchema.hasPrefix,
					"hasProperty", currentSchema.hasProperty)
				if !currentSchema.hasSchema {
					*results = append(*results, xmpExtSchemaRule(part, "schema")...)
				}
				if !currentSchema.hasNamespaceURI {
					*results = append(*results, xmpExtSchemaRule(part, "namespaceURI")...)
				}
				if !currentSchema.hasPrefix {
					*results = append(*results, xmpExtSchemaRule(part, "prefix")...)
				}
				if !currentSchema.hasProperty {
					*results = append(*results, xmpExtSchemaRule(part, "property")...)
				}
				currentSchema = schemaInfo{}
			}
		}
	}

	slog.Debug("compliance_2/xmp: extension schema check complete")
}

// xmpExtSchemaRule returns the rule for a missing field in a schema definition.
// Mapping: schema->t02, namespaceURI->t03, prefix->t04, property->t05.
func xmpExtSchemaRule(part int, field string) []Result {
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
	case 2: //nolint:mnd
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
	case 3: //nolint:mnd
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
	case 4: //nolint:mnd
		// Part 4 uses the general extension schema rule from 6.6.1.
		return fail(Rule_19005_4_6_6_1_t01, ctx)
	}
	return nil
}

// xmpExtPropertyRule returns the rule for a missing field in a property definition.
// Mapping: name->t07, valueType->t08, category->t09, description->t10.
func xmpExtPropertyRule(part int, field string) []Result {
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
	case 2: //nolint:mnd
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
	case 3: //nolint:mnd
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
	case 4: //nolint:mnd
		return fail(Rule_19005_4_6_6_1_t01, ctx)
	}
	return nil
}

// xmpExtValueTypeRule returns the rule for a missing field in a valueType definition.
// Mapping: type->t11, namespaceURI->t12, prefix->t13, description->t14.
func xmpExtValueTypeRule(part int, field string) []Result {
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
	case 2: //nolint:mnd
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
	case 3: //nolint:mnd
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
	case 4: //nolint:mnd
		return fail(Rule_19005_4_6_6_1_t01, ctx)
	}
	return nil
}

// xmpExtFieldRule returns the rule for a missing field in a field definition.
// Mapping: name->t16, valueType->t17, description->t18.
func xmpExtFieldRule(part int, field string) []Result {
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
	case 2: //nolint:mnd
		switch field {
		case "name":
			return fail(Rule_19005_2_6_6_2_3_3_t16, ctx)
		case "valueType":
			return fail(Rule_19005_2_6_6_2_3_3_t17, ctx)
		case "description":
			return fail(Rule_19005_2_6_6_2_3_3_t18, ctx)
		}
	case 3: //nolint:mnd
		switch field {
		case "name":
			return fail(Rule_19005_3_6_6_2_3_3_t16, ctx)
		case "valueType":
			return fail(Rule_19005_3_6_6_2_3_3_t17, ctx)
		case "description":
			return fail(Rule_19005_3_6_6_2_3_3_t18, ctx)
		}
	case 4: //nolint:mnd
		return fail(Rule_19005_4_6_6_1_t01, ctx)
	}
	return nil
}

// --- 6. DocInfo ↔ XMP consistency ---

// xmpCheckDocInfoSync verifies that document info dict entries are consistent
// with their XMP equivalents (§6.7.3).
func xmpCheckDocInfoSync(ctx context.Context, doc *document.Document, xmpData []byte, part int, results *[]Result) {
	slog.Debug("compliance_2/xmp: checking DocInfo ↔ XMP consistency")

	infoObj, ok := doc.Index.Trailer["Info"]
	if !ok {
		slog.Debug("compliance_2/xmp: no Info dict in trailer, skipping sync check")
		return
	}
	infoDict, err := document.ResolveDict(ctx, doc, infoObj)
	if err != nil || infoDict == nil {
		slog.Debug("compliance_2/xmp: failed to resolve Info dict", "error", err)
		return
	}

	xmpValues := xmpParseForSync(xmpData)
	if xmpValues == nil {
		slog.Debug("compliance_2/xmp: failed to parse XMP for sync")
		return
	}

	slog.Debug("compliance_2/xmp: parsed XMP sync values", "count", len(xmpValues))

	syncPairs := []struct {
		infoKey string
		xmpKey  string
		testNum int
	}{
		{"CreationDate", "xmp:CreateDate", 1},
		{"Title", "dc:title", 2},
		{"Author", "dc:creator", 3},
		{"Subject", "dc:description", 4},
		{"Keywords", "pdf:Keywords", 5},
		{"Creator", "xmp:CreatorTool", 6},
		{"Producer", "pdf:Producer", 7},
		{"ModDate", "xmp:ModifyDate", 8},
	}

	for _, pair := range syncPairs {
		infoVal := xmpGetInfoString(ctx, doc, infoDict, pair.infoKey)
		if infoVal == "" {
			slog.Debug("compliance_2/xmp: DocInfo key empty or absent, skipping sync",
				"key", pair.infoKey)
			continue
		}

		xmpVal, hasXMP := xmpValues[pair.xmpKey]

		// If Info dict has a non-empty entry but XMP is missing the
		// corresponding property, that is a sync violation.
		if !hasXMP || xmpVal == "" {
			slog.Debug("compliance_2/xmp: DocInfo present but XMP missing",
				"infoKey", pair.infoKey, "xmpKey", pair.xmpKey, "infoVal", infoVal)
			*results = append(*results, xmpDocInfoSyncRule(part, pair.testNum, pair.infoKey, infoVal, pair.xmpKey, "")...)
			continue
		}

		normInfo := strings.TrimSpace(infoVal)
		normXMP := strings.TrimSpace(xmpVal)
		if strings.HasPrefix(normInfo, "D:") {
			normInfo = xmpNormalizePDFDate(normInfo)
		}
		normInfo = xmpNormalizeISO8601(normInfo)
		normXMP = xmpNormalizeISO8601(normXMP)

		if normInfo != normXMP {
			slog.Debug("compliance_2/xmp: DocInfo/XMP mismatch",
				"key", pair.infoKey, "info", infoVal, "xmp", xmpVal)
			*results = append(*results, xmpDocInfoSyncRule(part, pair.testNum, pair.infoKey, infoVal, pair.xmpKey, xmpVal)...)
		}
	}
}

func xmpDocInfoSyncRule(part, testNum int, infoKey, infoVal, xmpKey, xmpVal string) []Result {
	ctx := fmt.Sprintf("/%s=%q vs %s=%q", infoKey, infoVal, xmpKey, xmpVal)
	switch part {
	case 1:
		return xmpDocInfoSyncRulePart1(testNum, ctx)
	case 4: //nolint:mnd
		// Part 4 uses 6.7.3.3/6.7.3.4 clause numbering — map test numbers
		// to the appropriate rules.
		return xmpDocInfoSyncRulePart4(testNum, ctx)
	}
	return nil
}

func xmpDocInfoSyncRulePart1(testNum int, ctx string) []Result {
	switch testNum {
	case 1:
		return fail(Rule_19005_1_6_7_3_t01, ctx)
	case 2: //nolint:mnd
		return fail(Rule_19005_1_6_7_3_t02, ctx)
	case 3: //nolint:mnd
		return fail(Rule_19005_1_6_7_3_t03, ctx)
	case 4: //nolint:mnd
		return fail(Rule_19005_1_6_7_3_t04, ctx)
	case 5: //nolint:mnd
		return fail(Rule_19005_1_6_7_3_t05, ctx)
	case 6: //nolint:mnd
		return fail(Rule_19005_1_6_7_3_t06, ctx)
	case 7: //nolint:mnd
		return fail(Rule_19005_1_6_7_3_t07, ctx)
	case 8: //nolint:mnd
		return fail(Rule_19005_1_6_7_3_t08, ctx)
	}
	return nil
}

func xmpDocInfoSyncRulePart4(testNum int, ctx string) []Result {
	// Part 4 uses Rule_19005_4_6_7_3_t07 for value mismatches and t08
	// for missing XMP properties corresponding to Info dict entries.
	// We use t07 for all sync mismatches uniformly.
	switch testNum {
	case 1:
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 2: //nolint:mnd
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 3: //nolint:mnd
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 4: //nolint:mnd
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 5: //nolint:mnd
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 6: //nolint:mnd
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 7: //nolint:mnd
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 8: //nolint:mnd
		return fail(Rule_19005_4_6_7_3_t08, ctx)
	}
	return nil
}

// xmpGetInfoString retrieves a string value from the document info dict.
func xmpGetInfoString(ctx context.Context, doc *document.Document, infoDict ast.PdfDict, key string) string {
	valObj, ok := infoDict[ast.PdfName(key)]
	if !ok {
		return ""
	}
	s, err := document.ResolveString(ctx, doc, valObj)
	if err == nil && s != "" {
		return document.DecodePdfString(s)
	}
	return ""
}

// xmpParseForSync extracts key XMP property values for DocInfo comparison.
// It handles both inline attribute values and rdf:Alt/rdf:Seq container
// values. For Alt containers, the x-default language entry is preferred;
// if absent, the first entry is used. For Seq containers, the first entry
// is used.
func xmpParseForSync(data []byte) map[string]string {
	result := make(map[string]string)
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	slog.Debug("compliance_2/xmp: parsing XMP for DocInfo sync")

	var propNS, propLocal string
	var inAlt, inSeqOrBag, inLI bool
	var liLang string // xml:lang attribute on current rdf:li
	depth := 0
	inDesc := false

	// altDefaults tracks x-default values from rdf:Alt containers so they
	// can override any earlier first-entry value.
	altDefaults := make(map[string]string)
	// altFirst tracks the first rdf:li value in Alt containers as fallback.
	altFirst := make(map[string]string)

	// nsToPrefix maps namespace URIs to the shorthand prefixes used in sync keys.
	nsToPrefix := map[string]string{
		"http://purl.org/dc/elements/1.1/":   "dc",
		"http://ns.adobe.com/xap/1.0/":       "xmp",
		"http://ns.adobe.com/pdf/1.3/":       "pdf",
		"http://ns.adobe.com/xap/1.0/mm/":    "xmpMM",
		"http://ns.adobe.com/photoshop/1.0/": "photoshop",
	}

	makeKey := func(ns, local string) string {
		if prefix, ok := nsToPrefix[ns]; ok {
			return prefix + ":" + local
		}
		return ns + ":" + local
	}

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description" {
				inDesc = true
				depth = 0
				for _, attr := range t.Attr {
					if attr.Name.Space != "" &&
						attr.Name.Space != "xmlns" &&
						attr.Name.Space != xmpRDFNamespace &&
						attr.Name.Space != "http://www.w3.org/XML/1998/namespace" {
						key := makeKey(attr.Name.Space, attr.Name.Local)
						slog.Debug("compliance_2/xmp: sync attr", "key", key, "value", attr.Value)
						result[key] = attr.Value
					}
				}
				continue
			}
			if inDesc {
				depth++
				if depth == 1 && t.Name.Space != "" && t.Name.Space != xmpRDFNamespace {
					propNS = t.Name.Space
					propLocal = t.Name.Local
					slog.Debug("compliance_2/xmp: sync entering property",
						"ns", propNS, "local", propLocal, "depth", depth)
				}
				if depth == 2 && t.Name.Space == xmpRDFNamespace { //nolint:mnd
					switch t.Name.Local {
					case "Alt":
						inAlt = true
						slog.Debug("compliance_2/xmp: sync entering rdf:Alt",
							"prop", makeKey(propNS, propLocal))
					case "Seq", "Bag":
						inSeqOrBag = true
						slog.Debug("compliance_2/xmp: sync entering rdf:Seq/Bag",
							"prop", makeKey(propNS, propLocal), "type", t.Name.Local)
					}
				}
				if (inAlt || inSeqOrBag) && t.Name.Space == xmpRDFNamespace && t.Name.Local == "li" {
					inLI = true
					// Extract xml:lang attribute for Alt container preference.
					liLang = ""
					for _, attr := range t.Attr {
						if attr.Name.Local == "lang" &&
							(attr.Name.Space == "http://www.w3.org/XML/1998/namespace" || attr.Name.Space == "xml") {
							liLang = attr.Value
							slog.Debug("compliance_2/xmp: sync rdf:li lang",
								"lang", liLang, "prop", makeKey(propNS, propLocal))
						}
					}
				}
			}
		case xml.CharData:
			if !inDesc {
				continue
			}
			text := strings.TrimSpace(string(t))
			if text == "" {
				continue
			}
			if inLI && propNS != "" {
				key := makeKey(propNS, propLocal)
				if inAlt {
					// For Alt containers, track x-default and first entries separately.
					if liLang == "x-default" {
						slog.Debug("compliance_2/xmp: sync Alt x-default value",
							"key", key, "value", text)
						altDefaults[key] = text
					}
					if _, exists := altFirst[key]; !exists {
						slog.Debug("compliance_2/xmp: sync Alt first value",
							"key", key, "value", text, "lang", liLang)
						altFirst[key] = text
					}
				} else {
					// Seq/Bag: take the first entry.
					if _, exists := result[key]; !exists {
						slog.Debug("compliance_2/xmp: sync Seq/Bag li value",
							"key", key, "value", text)
						result[key] = text
					}
				}
			} else if depth == 1 && propNS != "" {
				key := makeKey(propNS, propLocal)
				slog.Debug("compliance_2/xmp: sync text value", "key", key, "value", text)
				result[key] = text
			}
		case xml.EndElement:
			if t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description" {
				inDesc = false
				depth = 0
				propNS = ""
				propLocal = ""
				inAlt = false
				inSeqOrBag = false
				inLI = false
				liLang = ""
			} else if inDesc {
				if t.Name.Space == xmpRDFNamespace && t.Name.Local == "li" {
					inLI = false
					liLang = ""
				}
				if depth == 2 && t.Name.Space == xmpRDFNamespace { //nolint:mnd
					switch t.Name.Local {
					case "Alt":
						inAlt = false
					case "Seq", "Bag":
						inSeqOrBag = false
					}
				}
				if depth == 1 {
					propNS = ""
					propLocal = ""
				}
				depth--
			}
		}
	}

	// Merge Alt container values: prefer x-default, fall back to first entry.
	for key, val := range altDefaults {
		slog.Debug("compliance_2/xmp: sync using Alt x-default", "key", key, "value", val)
		result[key] = val
	}
	for key, val := range altFirst {
		if _, exists := result[key]; !exists {
			slog.Debug("compliance_2/xmp: sync using Alt first entry (no x-default)",
				"key", key, "value", val)
			result[key] = val
		}
	}

	slog.Debug("compliance_2/xmp: sync parsing complete", "values", len(result))
	return result
}

// xmpNormalizePDFDate converts a PDF date string (D:YYYYMMDDHHmmSS+HH'mm')
// to ISO 8601 format for comparison with XMP dates.
func xmpNormalizePDFDate(pdfDate string) string {
	s := strings.TrimPrefix(pdfDate, "D:")
	if len(s) < 4 { //nolint:mnd // minimum: YYYY
		return pdfDate
	}

	year := s[:4]
	s = s[4:]

	get2 := func(def string) string {
		if len(s) < 2 { //nolint:mnd
			return def
		}
		val := s[:2]
		s = s[2:]
		return val
	}

	month := get2("01")
	day := get2("01")
	hour := get2("00")
	min := get2("00")
	sec := get2("00")

	tz := "" // No timezone means "relationship to UT is unknown" per PDF spec.
	s = strings.TrimSpace(s)
	if len(s) > 0 {
		switch s[0] {
		case 'Z':
			tz = "Z"
		case '+', '-':
			sign := string(s[0])
			s = s[1:]
			tzH := "00"
			tzM := "00"
			if len(s) >= 2 { //nolint:mnd
				tzH = s[:2]
				s = s[2:]
			}
			s = strings.TrimPrefix(s, "'")
			if len(s) >= 2 { //nolint:mnd
				tzM = s[:2]
			}
			tz = sign + tzH + ":" + tzM
		}
	}

	slog.Debug("compliance_2/xmp: normalized PDF date",
		"input", pdfDate, "tz", tz)

	return year + "-" + month + "-" + day + "T" + hour + ":" + min + ":" + sec + tz
}

// xmpNormalizeISO8601 canonicalizes an ISO 8601 date string for comparison.
// It normalizes +00:00 and -00:00 to Z (all represent UTC), pads HH:MM to
// HH:MM:SS, but preserves the absence of timezone when none is specified.
func xmpNormalizeISO8601(s string) string {
	if !strings.Contains(s, "T") {
		return s
	}

	// Normalize explicit UTC offsets (+00:00 / -00:00) to Z.
	if before, ok := strings.CutSuffix(s, "+00:00"); ok {
		s = before + "Z"
	} else if before, ok := strings.CutSuffix(s, "-00:00"); ok {
		s = before + "Z"
	}
	// Do NOT append Z when no timezone is present — an absent timezone
	// means "unknown" and must not compare equal to explicit UTC.

	tIdx := strings.LastIndex(s, "T")
	timePart := s[tIdx+1:]
	tzSuffix := ""
	for _, tz := range []string{"Z", "+", "-"} {
		if idx := strings.Index(timePart, tz); idx >= 0 {
			tzSuffix = timePart[idx:]
			timePart = timePart[:idx]
			break
		}
	}

	if strings.Count(timePart, ":") == 1 {
		timePart += ":00"
	}

	return s[:tIdx+1] + timePart + tzSuffix
}

// --- 7. XMP container types ---

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
func xmpCheckPropertyTypes(data []byte, part int, _ ast.PdfStream, results *[]Result) {
	slog.Debug("compliance_2/xmp: checking XMP property types")

	observed := xmpExtractPropertyValues(data)
	_, extensionNS := xmpExtractNamespaces(data)

	slog.Debug("compliance_2/xmp: extracted property values",
		"namespaces", len(observed), "extensionNS", len(extensionNS))

	for ns, props := range observed {
		// Skip structural namespaces.
		if ns == xmpRDFNamespace || ns == "adobe:ns:meta/" || ns == "http://www.w3.org/XML/1998/namespace" {
			slog.Debug("compliance_2/xmp: skipping structural namespace", "ns", ns)
			continue
		}
		// Skip extension-declared namespaces — their types are defined by the extension.
		if extensionNS[ns] {
			slog.Debug("compliance_2/xmp: skipping extension-declared namespace", "ns", ns)
			continue
		}
		// Skip PDF/A extension schema machinery namespaces.
		if ns == xmpPdfaSchemaPrefix || ns == xmpPdfaPropertyPrefix ||
			ns == xmpPdfaTypePrefix || ns == xmpPdfaFieldPrefix ||
			ns == "http://www.aiim.org/pdfa/ns/extension/" {
			slog.Debug("compliance_2/xmp: skipping pdfa extension machinery namespace", "ns", ns)
			continue
		}

		registry, hasSchema := xmpPropertyRegistry[ns]
		if !hasSchema {
			// Namespace not in registry — already handled by xmpCheckNamespaces.
			slog.Debug("compliance_2/xmp: namespace not in property registry, skipping", "ns", ns)
			continue
		}

		for prop, pv := range props {
			expectedType, known := registry[prop]
			if !known {
				// Property name not defined in predefined schema — violation.
				slog.Debug("compliance_2/xmp: unknown property in predefined schema",
					"ns", ns, "prop", prop)
				*results = append(*results, xmpPropertyTypeRule(part, ns, prop, "unknown property name")...)
				continue
			}

			// xmpPropAny matches anything.
			if expectedType == xmpPropAny {
				slog.Debug("compliance_2/xmp: property type is Any, skipping validation",
					"ns", ns, "prop", prop)
				continue
			}

			// Check container type properties (Alt/Seq/Bag).
			if isContainerPropType(expectedType) {
				expectedCont := propTypeToContainerType(expectedType)
				if pv.containerType != expectedCont {
					slog.Debug("compliance_2/xmp: container type mismatch",
						"ns", ns, "prop", prop,
						"observed", xmpContainerTypeName(pv.containerType),
						"expected", xmpContainerTypeName(expectedCont))
					*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
						fmt.Sprintf("observed=%s expected=%s",
							xmpContainerTypeName(pv.containerType),
							xmpContainerTypeName(expectedCont)))...)
				}

				// Validate Alt container items have xml:lang attribute.
				if expectedType == xmpPropAlt && !pv.hasLangAttr {
					slog.Debug("compliance_2/xmp: Alt container missing xml:lang on items",
						"ns", ns, "prop", prop)
					*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
						"Alt container items missing xml:lang attribute")...)
				}

				// Validate container item value formats using the element type registry.
				if elemTypes, nsOK := xmpContainerElementType[ns]; nsOK {
					if elemType, propOK := elemTypes[prop]; propOK {
						slog.Debug("compliance_2/xmp: validating container item values",
							"ns", ns, "prop", prop, "items", len(pv.items), "elemType", elemType)
						for i, item := range pv.items {
							if !validateXMPValue(elemType, item) {
								slog.Debug("compliance_2/xmp: container item value format mismatch",
									"ns", ns, "prop", prop, "index", i, "value", item, "expectedType", elemType)
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
				// Expected simple value — should NOT be in a container.
				if pv.containerType != xmpContSimple {
					slog.Debug("compliance_2/xmp: simple property wrapped in container",
						"ns", ns, "prop", prop,
						"observed", xmpContainerTypeName(pv.containerType),
						"expected", "Simple")
					*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
						fmt.Sprintf("observed=%s expected=Simple",
							xmpContainerTypeName(pv.containerType)))...)
				}

				// Validate simple property value format.
				if pv.containerType == xmpContSimple && pv.value != "" {
					valid := validateXMPValue(expectedType, pv.value)
					// EXIF and TIFF properties typed as Real also accept rational "N/D" notation.
					if !valid && expectedType == xmpPropReal && isEXIFNamespace(ns) {
						valid = isValidXMPRealOrRational(pv.value)
					}
					if !valid {
						slog.Debug("compliance_2/xmp: simple value format mismatch",
							"ns", ns, "prop", prop, "value", pv.value, "expectedType", expectedType)
						*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
							fmt.Sprintf("value %q does not match expected type", pv.value))...)
					} else {
						slog.Debug("compliance_2/xmp: simple value format OK",
							"ns", ns, "prop", prop, "expectedType", expectedType)
					}
				}

				continue
			}

			// Check struct type properties.
			if expectedType == xmpPropStruct {
				// Expected struct — should NOT be in a container.
				if pv.containerType != xmpContSimple {
					slog.Debug("compliance_2/xmp: struct property wrapped in container",
						"ns", ns, "prop", prop,
						"observed", xmpContainerTypeName(pv.containerType),
						"expected", "Simple")
					*results = append(*results, xmpPropertyTypeRule(part, ns, prop,
						fmt.Sprintf("observed=%s expected=Simple",
							xmpContainerTypeName(pv.containerType)))...)
				}

				// Verify struct property has sub-elements, not plain text.
				if !pv.isStructured && pv.containerType == xmpContSimple {
					slog.Debug("compliance_2/xmp: struct property has plain text instead of sub-elements",
						"ns", ns, "prop", prop, "value", pv.value)
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
func xmpPropertyTypeRule(part int, ns, prop, detail string) []Result {
	ctx := fmt.Sprintf("%s:%s %s", ns, prop, detail)
	switch part {
	case 1:
		return fail(Rule_19005_1_6_7_9_t03, ctx)
	case 2: //nolint:mnd
		return fail(Rule_19005_2_6_6_2_3_1_t02, ctx)
	case 3: //nolint:mnd
		return fail(Rule_19005_3_6_6_2_3_1_t02, ctx)
	case 4: //nolint:mnd
		return fail(Rule_19005_4_6_6_1_t01, ctx)
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

	slog.Debug("compliance_2/xmp: extracting property values")

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
						slog.Debug("compliance_2/xmp: attribute-form property value",
							"ns", attr.Name.Space, "local", attr.Name.Local, "value", attr.Value)
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
					// Default to simple until we see a container child.
					pv := getOrCreate(currentNS, currentLocal)
					pv.containerType = xmpContSimple
					record(currentNS, currentLocal, pv)
					slog.Debug("compliance_2/xmp: entering property element",
						"ns", currentNS, "local", currentLocal)
				}
				if depth == 2 && currentNS != "" { //nolint:mnd
					if t.Name.Space == xmpRDFNamespace {
						switch t.Name.Local {
						case "Alt":
							pv := getOrCreate(currentNS, currentLocal)
							pv.containerType = xmpContAlt
							record(currentNS, currentLocal, pv)
							inContainer = true
							slog.Debug("compliance_2/xmp: found Alt container",
								"ns", currentNS, "local", currentLocal)
						case "Seq":
							pv := getOrCreate(currentNS, currentLocal)
							pv.containerType = xmpContSeq
							record(currentNS, currentLocal, pv)
							inContainer = true
							slog.Debug("compliance_2/xmp: found Seq container",
								"ns", currentNS, "local", currentLocal)
						case "Bag":
							pv := getOrCreate(currentNS, currentLocal)
							pv.containerType = xmpContBag
							record(currentNS, currentLocal, pv)
							inContainer = true
							slog.Debug("compliance_2/xmp: found Bag container",
								"ns", currentNS, "local", currentLocal)
						}
					} else {
						// Child element that is not an rdf container → structured.
						pv := getOrCreate(currentNS, currentLocal)
						pv.isStructured = true
						record(currentNS, currentLocal, pv)
						slog.Debug("compliance_2/xmp: property has structured sub-element",
							"ns", currentNS, "local", currentLocal, "child", t.Name.Local)
					}
				}
				if depth == 3 && inContainer && t.Name.Space == xmpRDFNamespace && t.Name.Local == "li" { //nolint:mnd
					inLi = true
					currentCharData = ""
					// Check for xml:lang attribute.
					for _, attr := range t.Attr {
						if attr.Name.Space == "http://www.w3.org/XML/1998/namespace" && attr.Name.Local == "lang" {
							pv := getOrCreate(currentNS, currentLocal)
							pv.hasLangAttr = true
							record(currentNS, currentLocal, pv)
							slog.Debug("compliance_2/xmp: rdf:li has xml:lang attribute",
								"ns", currentNS, "local", currentLocal, "lang", attr.Value)
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
						slog.Debug("compliance_2/xmp: captured simple element text",
							"ns", currentNS, "local", currentLocal, "value", trimmed)
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
				if depth == 3 && inLi { //nolint:mnd
					// End of rdf:li — capture accumulated text as an item.
					trimmed := strings.TrimSpace(currentCharData)
					if currentNS != "" {
						pv := getOrCreate(currentNS, currentLocal)
						pv.items = append(pv.items, trimmed)
						record(currentNS, currentLocal, pv)
						slog.Debug("compliance_2/xmp: captured container item",
							"ns", currentNS, "local", currentLocal, "item", trimmed)
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
				if depth == 3 && inLi { //nolint:mnd
					// CharData inside rdf:li.
					currentCharData += string(t)
				}
			}
		}
	}

	slog.Debug("compliance_2/xmp: extracted property values", "namespaces", len(result))
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

// isEXIFNamespace returns true if the namespace is EXIF or TIFF, where
// Real properties accept rational "N/D" notation.
func isEXIFNamespace(ns string) bool {
	return ns == "http://ns.adobe.com/exif/1.0/" ||
		ns == "http://ns.adobe.com/exif/1.0/aux/" ||
		ns == "http://ns.adobe.com/tiff/1.0/"
}
