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

func matchXMP(v compliance.Violation) bool {
	return descriptionContains(v, "pdfaid") ||
		descriptionContains(v, "pdfuaid") ||
		descriptionContains(v, "Metadata key") ||
		descriptionContains(v, "metadata stream") ||
		descriptionContains(v, "dc:title")
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
		fmt.Fprintf(&sb, "  <pdfaid:conformance>%s</pdfaid:conformance>\n", conformance)
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

	// Inject dc:title if missing.
	if !strings.Contains(xmp, "dc:title") && info.Title != "" {
		title := fmt.Sprintf("  <dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">%s</rdf:li></rdf:Alt></dc:title>\n", xmlEscape(info.Title))
		if idx := strings.Index(xmp, "</rdf:Description>"); idx >= 0 {
			xmp = xmp[:idx] + title + xmp[idx:]
			fixes = append(fixes, "injected dc:title into XMP")
		}
	}

	return xmp, fixes
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
