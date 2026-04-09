package remediation

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"maps"
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
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	// Gather metadata from /Info dict for syncing.
	info := gatherInfoMetadata(ctx, doc)

	// Determine PDF/A part and conformance from profile string.
	part, conformance := parsePDFAProfile(profile)
	uaPart := parsePDFUAProfile(profile)

	// Check if XMP metadata stream already exists.
	var existingXMP string
	if metaObj, ok := catalogDict["Metadata"]; ok {
		if stream, err := document.ResolveStream(ctx, doc, metaObj); err == nil {
			if data, err := stream.Bytes(); err == nil {
				existingXMP = string(data)
			}
		}
	}

	var xmpBytes []byte
	var fixes []string

	if existingXMP != "" {
		// Patch existing XMP.
		patched, patchFixes := patchExistingXMP(existingXMP, part, conformance, uaPart, info)
		if len(patchFixes) == 0 {
			return nil, nil // nothing to patch
		}
		xmpBytes = []byte(patched)
		fixes = patchFixes
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

	return fixes, nil
}

type infoMetadata struct {
	Title    string
	Author   string
	Subject  string
	Creator  string
	Producer string
	Keywords string
}

func gatherInfoMetadata(ctx context.Context, doc *document.Document) infoMetadata {
	var info infoMetadata
	infoObj, ok := doc.Index.Trailer["Info"]
	if !ok {
		return info
	}
	infoDict, err := document.ResolveDict(ctx, doc, infoObj)
	if err != nil || infoDict == nil {
		return info
	}

	readField := func(key ast.PdfName) string {
		if v, ok := infoDict[key]; ok {
			if s, ok := v.(ast.PdfString); ok {
				return document.DecodePdfString(s)
			}
		}
		return ""
	}

	info.Title = readField("Title")
	info.Author = readField("Author")
	info.Subject = readField("Subject")
	info.Creator = readField("Creator")
	info.Producer = readField("Producer")
	info.Keywords = readField("Keywords")
	return info
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
	}

	fmt.Fprintf(&sb, "  <dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">%s</rdf:li></rdf:Alt></dc:title>\n", title)
	if info.Author != "" {
		fmt.Fprintf(&sb, "  <dc:creator><rdf:Seq><rdf:li>%s</rdf:li></rdf:Seq></dc:creator>\n", xmlEscape(info.Author))
	}
	if info.Subject != "" {
		fmt.Fprintf(&sb, "  <dc:description><rdf:Alt><rdf:li xml:lang=\"x-default\">%s</rdf:li></rdf:Alt></dc:description>\n", xmlEscape(info.Subject))
	}
	fmt.Fprintf(&sb, "  <xmp:CreatorTool>%s</xmp:CreatorTool>\n", creator)
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

func patchExistingXMP(xmp string, part int, conformance string, uaPart int, info infoMetadata) (string, []string) {
	var fixes []string

	// PDF/A-4 (plain): remove pdfaid:conformance if present (forbidden).
	// Skip patching entirely if we can't cleanly remove it — avoid making
	// already-broken XMP worse.
	if part == 4 && conformance != "E" && conformance != "F" && strings.Contains(xmp, "pdfaid:conformance") {
		start := strings.Index(xmp, "<pdfaid:conformance>")
		if start >= 0 {
			end := strings.Index(xmp[start:], "</pdfaid:conformance>")
			if end >= 0 {
				end += start + len("</pdfaid:conformance>")
				for end < len(xmp) && (xmp[end] == '\n' || xmp[end] == '\r' || xmp[end] == ' ') {
					end++
				}
				xmp = xmp[:start] + xmp[end:]
				fixes = append(fixes, "removed pdfaid:conformance (forbidden for plain PDF/A-4)")
			} else {
				// Can't cleanly remove (attribute form or malformed) — skip.
				return xmp, nil
			}
		} else if strings.Contains(xmp, "pdfaid:conformance=") {
			// Attribute form — too complex to patch safely.
			return xmp, nil
		}
	}

	// Inject pdfaid if missing.
	if part > 0 && !strings.Contains(xmp, "pdfaid:part") {
		injection := fmt.Sprintf(
			"  xmlns:pdfaid=\"http://www.aiim.org/pdfa/ns/id/\"\n>\n  <pdfaid:part>%d</pdfaid:part>\n  <pdfaid:conformance>%s</pdfaid:conformance>\n",
			part, conformance)
		// Insert after the first rdf:Description opening.
		if idx := strings.Index(xmp, "rdf:about=\"\""); idx >= 0 {
			// Find the closing > of rdf:Description.
			closeIdx := strings.Index(xmp[idx:], ">")
			if closeIdx >= 0 {
				pos := idx + closeIdx
				// Replace the existing closing (might be > or />) with namespace + properties.
				xmp = xmp[:pos] + "\n" + injection + xmp[pos+1:]
				fixes = append(fixes, "injected pdfaid:part and pdfaid:conformance into XMP")
			}
		}
	}

	// Inject pdfuaid if missing.
	if uaPart > 0 && !strings.Contains(xmp, "pdfuaid:part") {
		injection := fmt.Sprintf(
			"  xmlns:pdfuaid=\"http://www.aiim.org/pdfua/ns/id/\"\n>\n  <pdfuaid:part>%d</pdfuaid:part>\n",
			uaPart)
		if idx := strings.Index(xmp, "rdf:about=\"\""); idx >= 0 {
			closeIdx := strings.Index(xmp[idx:], ">")
			if closeIdx >= 0 {
				pos := idx + closeIdx
				xmp = xmp[:pos] + "\n" + injection + xmp[pos+1:]
				fixes = append(fixes, "injected pdfuaid:part into XMP")
			}
		}
	}

	// PDF/A-4: inject pdfaid:rev=2020 if missing, but only if pdfaid namespace is declared.
	if part == 4 && !strings.Contains(xmp, "pdfaid:rev") && strings.Contains(xmp, "pdfaid") {
		rev := "  <pdfaid:rev>2020</pdfaid:rev>\n"
		if idx := strings.Index(xmp, "</rdf:Description>"); idx >= 0 {
			xmp = xmp[:idx] + rev + xmp[idx:]
			fixes = append(fixes, "injected pdfaid:rev=2020 into XMP")
		}
	}

	// Inject dc:title if missing.
	if !strings.Contains(xmp, "dc:title") && info.Title != "" {
		title := fmt.Sprintf("  <dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">%s</rdf:li></rdf:Alt></dc:title>\n", xmlEscape(info.Title))
		if idx := strings.Index(xmp, "</rdf:Description>"); idx >= 0 {
			xmp = xmp[:idx] + title + xmp[idx:]
			fixes = append(fixes, "injected dc:title into XMP")
		}
	}

	// Sync dc:creator with Info /Author.
	if info.Author != "" {
		escaped := xmlEscape(info.Author)
		if strings.Contains(xmp, "dc:creator") {
			// Replace existing dc:creator value.
			if start := strings.Index(xmp, "<dc:creator>"); start >= 0 {
				if end := strings.Index(xmp[start:], "</dc:creator>"); end >= 0 {
					end += start + len("</dc:creator>")
					replacement := fmt.Sprintf("<dc:creator><rdf:Seq><rdf:li>%s</rdf:li></rdf:Seq></dc:creator>", escaped)
					xmp = xmp[:start] + replacement + xmp[end:]
					fixes = append(fixes, "synced dc:creator with Info /Author")
				}
			}
		} else {
			// Inject dc:creator.
			creator := fmt.Sprintf("  <dc:creator><rdf:Seq><rdf:li>%s</rdf:li></rdf:Seq></dc:creator>\n", escaped)
			if idx := strings.Index(xmp, "</rdf:Description>"); idx >= 0 {
				xmp = xmp[:idx] + creator + xmp[idx:]
				fixes = append(fixes, "injected dc:creator from Info /Author")
			}
		}
	}

	// Sync pdf:Producer with Info /Producer.
	if info.Producer != "" && !strings.Contains(xmp, "pdf:Producer") {
		producer := fmt.Sprintf("  <pdf:Producer>%s</pdf:Producer>\n", xmlEscape(info.Producer))
		if idx := strings.Index(xmp, "</rdf:Description>"); idx >= 0 {
			xmp = xmp[:idx] + producer + xmp[idx:]
			fixes = append(fixes, "injected pdf:Producer from Info dict")
		}
	}

	// Sync xmp:CreatorTool with Info /Creator.
	if info.Creator != "" && !strings.Contains(xmp, "xmp:CreatorTool") {
		tool := fmt.Sprintf("  <xmp:CreatorTool>%s</xmp:CreatorTool>\n", xmlEscape(info.Creator))
		if idx := strings.Index(xmp, "</rdf:Description>"); idx >= 0 {
			xmp = xmp[:idx] + tool + xmp[idx:]
			fixes = append(fixes, "injected xmp:CreatorTool from Info dict")
		}
	}

	// Generate extension schema declarations for non-predefined namespaces.
	if !strings.Contains(xmp, "pdfaExtension:schemas") {
		schemas := extractUnknownNamespaceProperties([]byte(xmp))
		if len(schemas) > 0 {
			block := generateExtensionSchemaBlock(schemas)
			// Inject before </rdf:Description>.
			if idx := strings.Index(xmp, "</rdf:Description>"); idx >= 0 {
				// Also add namespace declarations to rdf:Description.
				nsDecls := `  xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"` + "\n" +
					`  xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"` + "\n" +
					`  xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"` + "\n"
				if descIdx := strings.Index(xmp, "rdf:about=\"\""); descIdx >= 0 {
					closeIdx := strings.Index(xmp[descIdx:], ">")
					if closeIdx >= 0 {
						pos := descIdx + closeIdx
						xmp = xmp[:pos] + "\n" + nsDecls + xmp[pos:]
						// Recalculate idx after insertion.
						idx = strings.Index(xmp, "</rdf:Description>")
					}
				}
				xmp = xmp[:idx] + block + xmp[idx:]
				fixes = append(fixes, fmt.Sprintf("generated extension schema declarations for %d custom namespaces", len(schemas)))
			}
		}
	}

	return xmp, fixes
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
	"http://www.aiim.org/pdfa/":                        true,
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
	properties []string // property local names
}

// extractUnknownNamespaceProperties parses XMP XML and returns non-predefined
// namespaces with their prefixes and property names.
func extractUnknownNamespaceProperties(data []byte) []extSchemaInfo {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	// Track namespace URI → prefix mapping from xmlns declarations.
	nsPrefixes := make(map[string]string)
	// Track namespace URI → set of property names.
	nsProps := make(map[string]map[string]bool)

	ignoredNS := map[string]bool{
		"": true, "xmlns": true,
		"http://www.w3.org/1999/02/22-rdf-syntax-ns#": true,
		"http://www.w3.org/XML/1998/namespace":        true,
		"http://www.w3.org/2000/xmlns/":               true,
		"http://www.w3.org/2001/XMLSchema-instance":   true,
		"http://www.w3.org/2001/XMLSchema":            true,
		"adobe:ns:meta/":                              true,
	}

	descDepth := 0
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == "http://www.w3.org/1999/02/22-rdf-syntax-ns#" && t.Name.Local == "Description" {
				descDepth++
				// Collect xmlns declarations.
				for _, attr := range t.Attr {
					if attr.Name.Space == "xmlns" && attr.Value != "" {
						nsPrefixes[attr.Value] = attr.Name.Local
					}
				}
				continue
			}

			// Inside rdf:Description: record properties in non-predefined namespaces.
			if descDepth > 0 && !ignoredNS[t.Name.Space] && !knownXMPNamespaces[t.Name.Space] {
				if _, ok := nsProps[t.Name.Space]; !ok {
					nsProps[t.Name.Space] = make(map[string]bool)
				}
				nsProps[t.Name.Space][t.Name.Local] = true
			}

			// Also collect xmlns from nested elements.
			for _, attr := range t.Attr {
				if attr.Name.Space == "xmlns" && attr.Value != "" {
					nsPrefixes[attr.Value] = attr.Name.Local
				}
			}

		case xml.EndElement:
			if t.Name.Space == "http://www.w3.org/1999/02/22-rdf-syntax-ns#" && t.Name.Local == "Description" {
				descDepth--
			}
		}
	}

	// Build result.
	schemas := make([]extSchemaInfo, 0, len(nsProps))
	for nsURI, props := range nsProps {
		prefix := nsPrefixes[nsURI]
		if prefix == "" {
			prefix = "ns" + fmt.Sprintf("%d", len(schemas)+1)
		}
		propNames := make([]string, 0, len(props))
		for p := range props {
			propNames = append(propNames, p)
		}
		sort.Strings(propNames)
		schemas = append(schemas, extSchemaInfo{nsURI: nsURI, prefix: prefix, properties: propNames})
	}
	sort.Slice(schemas, func(i, j int) bool { return schemas[i].nsURI < schemas[j].nsURI })
	return schemas
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
				fmt.Fprintf(&sb, "              <pdfaProperty:name>%s</pdfaProperty:name>\n", xmlEscape(prop))
				sb.WriteString("              <pdfaProperty:valueType>Text</pdfaProperty:valueType>\n")
				sb.WriteString("              <pdfaProperty:category>internal</pdfaProperty:category>\n")
				fmt.Fprintf(&sb, "              <pdfaProperty:description>%s</pdfaProperty:description>\n", xmlEscape(prop))
				sb.WriteString("            </rdf:li>\n")
			}
			sb.WriteString("          </rdf:Seq>\n        </pdfaSchema:property>\n")
		}

		sb.WriteString("      </rdf:li>\n")
	}

	sb.WriteString("    </rdf:Bag>\n  </pdfaExtension:schemas>\n")
	return sb.String()
}

func parsePDFAProfile(profile string) (int, string) {
	// "PDF/A-2b" → part=2, conformance="B"
	if !strings.HasPrefix(profile, "PDF/A-") {
		return 0, ""
	}
	rest := profile[6:] // "2b", "1a", "4", "4e", etc.
	if len(rest) == 0 {
		return 0, ""
	}
	part := int(rest[0] - '0')
	conformance := "B" // default
	if len(rest) > 1 {
		conformance = strings.ToUpper(string(rest[1]))
	}
	return part, conformance
}

func parsePDFUAProfile(profile string) int {
	if !strings.HasPrefix(profile, "PDF/UA-") {
		return 0
	}
	rest := profile[7:]
	if len(rest) == 0 {
		return 0
	}
	return int(rest[0] - '0')
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
