package remediation

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// FacturXNS is the canonical XMP namespace URI for the Factur-X
// properties declared by the Factur-X 1.0.07 specification.
const FacturXNS = "urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#"

// FacturXOptions configures [RemediateFacturX]. Values must not be
// shared across goroutines without synchronization.
type FacturXOptions struct {
	// InvoiceXML is the Cross-Industry Invoice (CII) XML payload that
	// will be embedded as the Factur-X data file. Required, non-empty.
	InvoiceXML []byte
	// Level is the Factur-X conformance level written into the XMP
	// `fx:ConformanceLevel` property.
	Level models.FacturXLevel
	// Profile is the PDF/A-3 conformance level used as the underlying
	// archival base. Must be one of [models.PDFA3a], [models.PDFA3b],
	// or [models.PDFA3u].
	Profile models.PDFAProfile
	// FileName is the embedded file name. Factur-X mandates
	// "factur-x.xml"; ZUGFeRD 2.0 used "zugferd-invoice.xml". The same
	// value is written into the XMP `fx:DocumentFileName` property.
	FileName string
	// DocumentType is the value of the XMP `fx:DocumentType` property.
	// Defaults to "INVOICE" when empty.
	DocumentType string
	// Relationship is the embedded-file `/AFRelationship` value.
	// Factur-X mandates "Data"; ZUGFeRD 2.0 legacy producers used
	// "Alternative".
	Relationship string
}

// RemediateFacturX embeds a Factur-X invoice XML in doc, remediates the
// document against the given PDF/A-3 profile, and injects the four
// `fx:` XMP properties along with the extension schema declaration
// required by ISO 19005 for non-predefined namespaces.
//
// The caller is the boundary that validates [FacturXOptions.Level],
// [FacturXOptions.Profile], and the InvoiceXML payload; this entry
// point trusts the input.
//
// The returned [Report] aggregates the underlying PDF/A-3 fixes plus
// the Factur-X-specific operations.
func RemediateFacturX(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, cfg FacturXOptions, opts Options) (*Report, error) {
	attachErr := mutations.AddAttachmentWithOptions(ctx, alloc, doc, cfg.FileName, cfg.InvoiceXML, mutations.AttachmentOptions{
		MimeType:       "application/xml",
		AFRelationship: cfg.Relationship,
		AddToCatalogAF: true,
	})
	if attachErr != nil {
		return nil, fmt.Errorf("factur-x: embedding invoice: %w", attachErr)
	}

	report, err := RemediatePDFA(ctx, doc, alloc, cfg.Profile, opts)
	if err != nil {
		return nil, fmt.Errorf("factur-x: pdf/a-3 remediation: %w", err)
	}

	fixes, err := injectFacturXXMP(ctx, alloc, doc, cfg)
	if err != nil {
		return nil, fmt.Errorf("factur-x: injecting xmp: %w", err)
	}
	report.Fixed = append(report.Fixed, fixes...)
	report.Fixed = append(report.Fixed, fmt.Sprintf("embedded %s as factur-x associated file", cfg.FileName))
	return report, nil
}

// injectFacturXXMP rewrites the catalog Metadata stream to declare the
// `fx:` namespace, set the four Factur-X properties, and refresh the
// `pdfaExtension:schemas` block so the new namespace is registered.
// The function assumes the PDF/A-3 remediation pass has already run, so
// a Metadata stream is present and PDF/A-conformant before patching.
func injectFacturXXMP(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, cfg FacturXOptions) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	metaObj, ok := catalogDict["Metadata"]
	if !ok {
		return nil, fmt.Errorf("catalog missing /Metadata after pdf/a-3 remediation")
	}
	xmp, err := readXMPStream(ctx, alloc, doc, metaObj)
	if err != nil {
		return nil, fmt.Errorf("reading xmp stream: %w", err)
	}

	docType := cfg.DocumentType
	if docType == "" {
		docType = "INVOICE"
	}

	buf, fixes, err := patchFacturXXMP(xmp, cfg.Level, cfg.FileName, docType)
	if err != nil {
		return nil, err
	}

	xmpDict := ast.PdfDict{
		"Type":    ast.PdfName("Metadata"),
		"Subtype": ast.PdfName("XML"),
	}
	xmpStream := ast.NewStream(xmpDict, buf)
	xmpRef := alloc.Alloc(xmpStream)

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Metadata"] = xmpRef
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return fixes, nil
}

// patchFacturXXMP splices the four Factur-X properties into xmp and
// refreshes the extension schema block. Exposed for unit testing.
func patchFacturXXMP(xmp []byte, level models.FacturXLevel, fileName, docType string) ([]byte, []string, error) {
	var fixes []string
	buf := xmp

	out, err := ensureNamespaceDecl(buf, "fx", FacturXNS)
	if err != nil {
		return nil, nil, fmt.Errorf("declaring fx namespace: %w", err)
	}
	buf = out

	properties := []struct {
		local string
		value string
	}{
		{"DocumentType", docType},
		{"DocumentFileName", fileName},
		{"Version", "1.0"},
		{"ConformanceLevel", string(level)},
	}
	for _, p := range properties {
		escaped := xmlEscape(p.value)
		if loc := findXMPProperty(buf, FacturXNS, p.local); loc.Form != xmpPropertyNotFound {
			replaced, err := replacePropertyValue(buf, FacturXNS, p.local, escaped)
			if err != nil {
				return nil, nil, fmt.Errorf("replacing fx:%s: %w", p.local, err)
			}
			buf = replaced
			fixes = append(fixes, fmt.Sprintf("set fx:%s to %s", p.local, p.value))
			continue
		}
		inserted, err := insertChildElement(buf, fmt.Sprintf("<fx:%s>%s</fx:%s>", p.local, escaped, p.local))
		if err != nil {
			return nil, nil, fmt.Errorf("inserting fx:%s: %w", p.local, err)
		}
		buf = inserted
		fixes = append(fixes, fmt.Sprintf("injected fx:%s into XMP", p.local))
	}

	buf, schemaFixes, err := refreshExtensionSchemaForFacturX(buf)
	if err != nil {
		return nil, nil, err
	}
	fixes = append(fixes, schemaFixes...)
	return buf, fixes, nil
}

// refreshExtensionSchemaForFacturX strips the existing extension schema
// element (if any) and regenerates it from the live namespace bindings
// so the Factur-X entry sits alongside any prior custom-namespace
// declarations. ISO 19005 §6.7.8 requires every non-predefined property
// to appear in the schema declaration; the splicer regenerates rather
// than patching in place because the existing block may already have a
// valid shape that an in-place insertion would break. Unlike
// [stripExtensionSchemasBlock], which discards the entire enclosing
// rdf:Description, this helper removes only the pdfaExtension:schemas
// element so sibling property elements survive a round trip.
func refreshExtensionSchemaForFacturX(xmp []byte) ([]byte, []string, error) {
	var fixes []string
	buf := xmp
	if stripped, removed := stripExtensionSchemasElement(buf); removed {
		buf = stripped
		fixes = append(fixes, "stripped prior extension schema block to refresh fx entry")
	}

	schemas := extractUnknownNamespaceProperties(buf)
	if len(schemas) == 0 {
		return buf, fixes, nil
	}

	for prefix, uri := range map[string]string{
		"pdfaExtension": pdfaExtensionNS,
		"pdfaSchema":    pdfaSchemaNS,
		"pdfaProperty":  pdfaPropertyNS,
	} {
		out, err := ensureNamespaceDecl(buf, prefix, uri)
		if err != nil {
			return nil, nil, fmt.Errorf("declaring %s namespace: %w", prefix, err)
		}
		buf = out
	}

	block := generateExtensionSchemaBlock(schemas)
	inserted, err := insertChildElement(buf, strings.TrimSpace(block))
	if err != nil {
		return nil, nil, fmt.Errorf("inserting extension schema block: %w", err)
	}
	buf = inserted
	fixes = append(fixes, fmt.Sprintf("regenerated extension schema declarations for %d custom namespaces", len(schemas)))
	return buf, fixes, nil
}

// stripExtensionSchemasElement removes a single
// `<pdfaExtension:schemas>...</pdfaExtension:schemas>` element along
// with any preceding indentation and the trailing newline. Property
// elements that sit in the same rdf:Description survive untouched,
// which is the behaviour the Factur-X refresh path needs.
func stripExtensionSchemasElement(xmp []byte) ([]byte, bool) {
	openMark := []byte("<pdfaExtension:schemas")
	start := bytes.Index(xmp, openMark)
	if start < 0 {
		return xmp, false
	}
	closeMark := []byte("</pdfaExtension:schemas>")
	closeIdx := bytes.Index(xmp[start:], closeMark)
	if closeIdx < 0 {
		return xmp, false
	}
	end := start + closeIdx + len(closeMark)
	for start > 0 && (xmp[start-1] == ' ' || xmp[start-1] == '\t') {
		start--
	}
	if end < len(xmp) && xmp[end] == '\n' {
		end++
	}
	out := make([]byte, 0, len(xmp)-(end-start))
	out = append(out, xmp[:start]...)
	out = append(out, xmp[end:]...)
	return out, true
}

// readXMPStream returns the bytes of the XMP packet referenced by obj.
// If obj is an indirect reference that has been overwritten by a
// pending allocator entry (typical when [RemediatePDFA] regenerates
// the Metadata stream), the pending version takes precedence over the
// xref entry. Direct streams are read inline.
func readXMPStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) ([]byte, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
			if s, ok := pending.(ast.PdfStream); ok {
				return s.Bytes(ctx)
			}
		}
	}
	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return nil, err
	}
	return stream.Bytes(ctx)
}
