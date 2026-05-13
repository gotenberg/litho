package compliance

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// hybridInvoiceVariant captures the format-specific values that
// distinguish Factur-X 1.0+ / ZUGFeRD 2.1+ from the legacy ZUGFeRD 1.0
// and 2.0 generations. The validator multiplexes a single check
// pipeline across these variants so the rule list stays linear.
type hybridInvoiceVariant struct {
	label           string
	fileName        string
	relationship    string
	namespaceURI    string
	namespacePrefix string
	documentVersion string
	allowedLevels   map[string]bool
	conflictNames   []string
}

//nolint:gochecknoglobals // immutable variant descriptors.
var (
	variantFacturX = hybridInvoiceVariant{
		label:           "Factur-X 1.0+/ZUGFeRD 2.1+",
		fileName:        "factur-x.xml",
		relationship:    "Data",
		namespaceURI:    "urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#",
		namespacePrefix: "fx",
		documentVersion: "1.0",
		allowedLevels: map[string]bool{
			"MINIMUM": true, "BASIC WL": true, "BASIC": true,
			"EN 16931": true, "EXTENDED": true, "XRECHNUNG": true,
		},
		conflictNames: []string{"zugferd-invoice.xml", "ZUGFeRD-invoice.xml"},
	}
	variantZUGFeRD20 = hybridInvoiceVariant{
		label:           "ZUGFeRD 2.0",
		fileName:        "zugferd-invoice.xml",
		relationship:    "Alternative",
		namespaceURI:    "urn:zugferd:pdfa:CrossIndustryDocument:invoice:2p0#",
		namespacePrefix: "zf",
		documentVersion: "2p0",
		allowedLevels: map[string]bool{
			"MINIMUM": true, "BASIC": true, "COMFORT": true, "EXTENDED": true,
		},
		conflictNames: []string{"factur-x.xml", "ZUGFeRD-invoice.xml"},
	}
	variantZUGFeRD10 = hybridInvoiceVariant{
		label:           "ZUGFeRD 1.0",
		fileName:        "ZUGFeRD-invoice.xml",
		relationship:    "Alternative",
		namespaceURI:    "urn:ferd:pdfa:CrossIndustryDocument:invoice:1p0#",
		namespacePrefix: "zf",
		documentVersion: "1p0",
		allowedLevels: map[string]bool{
			"BASIC": true, "COMFORT": true, "EXTENDED": true,
		},
		conflictNames: []string{"factur-x.xml", "zugferd-invoice.xml"},
	}
)

// ValidateFacturX audits the document against the joint Factur-X 1.0+
// / ZUGFeRD 2.1+ specification. The returned slice contains the
// PDF/A-3 violations of the archival base plus any hybrid-invoice
// rule violations as [Violation] values.
//
// The function holds the document's validation run mutex during the
// PDF/A-3 pass; see [ValidatePDFA] for the concurrency contract.
func ValidateFacturX(ctx context.Context, doc *document.Document) ([]error, error) {
	return validateHybridInvoice(ctx, doc, variantFacturX)
}

// ValidateZUGFeRD audits the document against the ZUGFeRD version
// identified by ver. ZUGFeRD 2.1 reuses the joint Factur-X 1.0+
// variant; 1.0 and 2.0 carry their own filename, relationship, and
// namespace.
//
// Returns an error if ver is not one of [models.ZUGFeRD1],
// [models.ZUGFeRD2], or [models.ZUGFeRD21].
func ValidateZUGFeRD(ctx context.Context, doc *document.Document, ver models.ZUGFeRDVersion) ([]error, error) {
	variant, ok := zugferdVariantFor(ver)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProfile, ver)
	}
	return validateHybridInvoice(ctx, doc, variant)
}

// zugferdVariantFor maps a [models.ZUGFeRDVersion] to its variant
// descriptor. Returns ok=false for unknown versions.
func zugferdVariantFor(ver models.ZUGFeRDVersion) (hybridInvoiceVariant, bool) {
	switch ver {
	case models.ZUGFeRD1:
		return variantZUGFeRD10, true
	case models.ZUGFeRD2:
		return variantZUGFeRD20, true
	case models.ZUGFeRD21:
		return variantFacturX, true
	}
	return hybridInvoiceVariant{}, false
}

// validateHybridInvoice runs the PDF/A-3 pass and then the
// hybrid-invoice rules against doc. Variant-specific filenames and
// XMP namespace details flow through v.
func validateHybridInvoice(ctx context.Context, doc *document.Document, v hybridInvoiceVariant) ([]error, error) {
	pdfaErrs, err := ValidatePDFA(ctx, doc, models.PDFA3b)
	if err != nil {
		return nil, fmt.Errorf("validating PDF/A-3 base for %s: %w", v.label, err)
	}

	catalog, err := resolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}

	invoice := locateInvoiceFileSpec(ctx, doc, catalog, v)
	hybrid := make([]Violation, 0, 8)
	hybrid = append(hybrid, checkInvoiceAF(catalog, invoice, v)...)
	hybrid = append(hybrid, checkInvoiceAFRelationship(ctx, doc, invoice, v)...)
	hybrid = append(hybrid, checkInvoiceStreamSubtype(ctx, doc, invoice, v)...)
	hybrid = append(hybrid, checkInvoiceConflictingVariant(ctx, doc, v)...)
	hybrid = append(hybrid, checkInvoiceXMP(ctx, doc, catalog, invoice, v)...)
	hybrid = append(hybrid, tagPDFA3Base(pdfaErrs, v)...)

	out := violationsToErrors(hybrid)
	out = append(out, pdfaErrs...)
	return out, nil
}

// invoiceFileSpec captures the resolved filespec for the variant's
// invoice file plus the object number of its embedded stream so later
// checks can reach into the stream dictionary without re-resolving.
type invoiceFileSpec struct {
	dict       ast.PdfDict
	objNum     int
	filename   string
	streamRef  ast.PdfIndirectRef
	streamDict ast.PdfDict
	found      bool
}

// locateInvoiceFileSpec walks the catalog /AF array looking for a
// filespec whose /F or /UF entry matches v.fileName. If found, the
// embedded stream metadata is resolved alongside so later checks can
// inspect /Subtype without re-walking the graph.
func locateInvoiceFileSpec(ctx context.Context, doc *document.Document, catalog ast.PdfDict, v hybridInvoiceVariant) invoiceFileSpec {
	afObj, ok := catalog["AF"]
	if !ok {
		return invoiceFileSpec{}
	}
	afArr, err := document.ResolveArray(ctx, doc, afObj)
	if err != nil {
		return invoiceFileSpec{}
	}
	for _, entry := range afArr {
		ref, refOK := entry.(ast.PdfIndirectRef)
		if !refOK {
			continue
		}
		dict, err := document.ResolveDict(ctx, doc, ref)
		if err != nil || dict == nil {
			continue
		}
		name := filespecName(ctx, doc, dict)
		if name != v.fileName {
			continue
		}
		spec := invoiceFileSpec{
			dict:     dict,
			objNum:   ref.ObjectNum,
			filename: name,
			found:    true,
		}
		if ef, ok := dict["EF"]; ok {
			efDict, err := document.ResolveDict(ctx, doc, ef)
			if err == nil && efDict != nil {
				if streamObj, ok := efDict["F"]; ok {
					if sref, ok := streamObj.(ast.PdfIndirectRef); ok {
						spec.streamRef = sref
						if stream, err := document.ResolveStream(ctx, doc, sref); err == nil {
							spec.streamDict = stream.Dict
						}
					}
				}
			}
		}
		return spec
	}
	return invoiceFileSpec{}
}

// filespecName returns the resolved /UF entry, falling back to /F.
// Both keys store a PDF string; UTF-16 BOM handling is done by the
// document layer.
func filespecName(ctx context.Context, doc *document.Document, dict ast.PdfDict) string {
	if uf, ok := dict["UF"]; ok {
		if s, err := document.ResolveString(ctx, doc, uf); err == nil {
			return string(s)
		}
	}
	if f, ok := dict["F"]; ok {
		if s, err := document.ResolveString(ctx, doc, f); err == nil {
			return string(s)
		}
	}
	return ""
}

func checkInvoiceAF(catalog ast.PdfDict, invoice invoiceFileSpec, v hybridInvoiceVariant) []Violation {
	if _, ok := catalog["AF"]; !ok {
		return failf(Rule_invoice_AFArrayPresent, "%s: catalog has no /AF array", v.label)
	}
	if !invoice.found {
		return failf(Rule_invoice_FileSpecPresent, "%s: no filespec named %q in /AF", v.label, v.fileName)
	}
	return nil
}

func checkInvoiceAFRelationship(ctx context.Context, doc *document.Document, invoice invoiceFileSpec, v hybridInvoiceVariant) []Violation {
	if !invoice.found {
		return nil
	}
	afObj, ok := invoice.dict["AFRelationship"]
	if !ok {
		return failf(Rule_invoice_AFRelationship, "%s: filespec object %d missing /AFRelationship", v.label, invoice.objNum)
	}
	got, err := document.ResolveName(ctx, doc, afObj)
	if err != nil || string(got) != v.relationship {
		return failf(Rule_invoice_AFRelationship, "%s: filespec object %d /AFRelationship = %q, want %q", v.label, invoice.objNum, got, v.relationship)
	}
	return nil
}

func checkInvoiceStreamSubtype(ctx context.Context, doc *document.Document, invoice invoiceFileSpec, v hybridInvoiceVariant) []Violation {
	if !invoice.found || invoice.streamDict == nil {
		return nil
	}
	subtype, err := document.ResolveName(ctx, doc, invoice.streamDict["Subtype"])
	if err != nil || string(subtype) != "application/xml" {
		return failf(Rule_invoice_StreamSubtype, "%s: embedded stream %d /Subtype = %q, want %q", v.label, invoice.streamRef.ObjectNum, subtype, "application/xml")
	}
	return nil
}

func checkInvoiceConflictingVariant(ctx context.Context, doc *document.Document, v hybridInvoiceVariant) []Violation {
	var results []Violation
	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			return results
		}
		if entry.Free {
			continue
		}
		dict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || dict == nil {
			continue
		}
		typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
		if typeName != "Filespec" {
			continue
		}
		name := filespecName(ctx, doc, dict)
		for _, conflict := range v.conflictNames {
			if name == conflict {
				results = append(results, failf(Rule_invoice_NoConflictingVariant,
					"%s: filespec object %d carries conflicting variant name %q", v.label, objNum, conflict)...)
				break
			}
		}
	}
	return results
}

// xmpProperties holds the four hybrid-invoice properties decoded from
// the XMP packet. Empty strings indicate the property was absent or
// could not be decoded.
type xmpProperties struct {
	documentType     string
	documentFileName string
	version          string
	conformanceLevel string
	namespaceFound   bool
	extensionSchema  bool
}

func checkInvoiceXMP(ctx context.Context, doc *document.Document, catalog ast.PdfDict, invoice invoiceFileSpec, v hybridInvoiceVariant) []Violation {
	xmpBytes := readInvoiceXMP(ctx, doc, catalog)
	if xmpBytes == nil {
		return failf(Rule_invoice_XMPNamespace, "%s: catalog /Metadata stream is missing or unreadable", v.label)
	}
	props := decodeHybridInvoiceXMP(xmpBytes, v)

	var results []Violation
	if !props.namespaceFound {
		results = append(results, failf(Rule_invoice_XMPNamespace,
			"%s: XMP packet does not declare namespace %q", v.label, v.namespaceURI)...)
	}
	if props.documentType == "" {
		results = append(results, failf(Rule_invoice_XMPDocumentType,
			"%s: XMP %s:DocumentType missing or empty", v.label, v.namespacePrefix)...)
	}
	if props.documentFileName == "" {
		results = append(results, failf(Rule_invoice_XMPDocumentFileName,
			"%s: XMP %s:DocumentFileName missing or empty", v.label, v.namespacePrefix)...)
	}
	if props.version != v.documentVersion {
		results = append(results, failf(Rule_invoice_XMPVersion,
			"%s: XMP %s:Version = %q, want %q", v.label, v.namespacePrefix, props.version, v.documentVersion)...)
	}
	if !v.allowedLevels[props.conformanceLevel] {
		results = append(results, failf(Rule_invoice_XMPConformanceLevel,
			"%s: XMP %s:ConformanceLevel = %q, not in allowed set", v.label, v.namespacePrefix, props.conformanceLevel)...)
	}
	if !props.extensionSchema {
		results = append(results, failf(Rule_invoice_XMPExtensionSchema,
			"%s: pdfaExtension:schemas does not declare namespace %q", v.label, v.namespaceURI)...)
	}
	if invoice.found && props.documentFileName != "" && props.documentFileName != invoice.filename {
		results = append(results, failf(Rule_invoice_FileNameCrossCheck,
			"%s: XMP %s:DocumentFileName %q does not match filespec name %q",
			v.label, v.namespacePrefix, props.documentFileName, invoice.filename)...)
	}
	return results
}

// readInvoiceXMP resolves the catalog /Metadata stream and decodes it,
// returning nil when no XMP packet is reachable.
func readInvoiceXMP(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []byte {
	mdObj, ok := catalog["Metadata"]
	if !ok {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, mdObj)
	if err != nil {
		return nil
	}
	data, err := stream.Bytes(ctx)
	if err != nil || len(data) == 0 {
		decoded, decErr := doc.DecodeStream(ctx, stream)
		if decErr != nil {
			return nil
		}
		data = decoded
	}
	if len(data) == 0 {
		return nil
	}
	return data
}

// decodeHybridInvoiceXMP scans xmp for the variant's four invoice
// properties plus the extension-schema declaration. The scan tolerates
// arbitrary attribute ordering and both element-form and attribute-form
// property serializations.
func decodeHybridInvoiceXMP(xmp []byte, v hybridInvoiceVariant) xmpProperties {
	var props xmpProperties
	dec := xml.NewDecoder(bytes.NewReader(xmp))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		startElem, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		// Detect namespace declaration on any element.
		for _, attr := range startElem.Attr {
			if attr.Name.Space == "xmlns" && attr.Value == v.namespaceURI {
				props.namespaceFound = true
			}
			// Attribute-form property serialization.
			if attr.Name.Space == v.namespaceURI {
				assignHybridProperty(&props, attr.Name.Local, attr.Value)
			}
		}
		// Element-form property serialization.
		if startElem.Name.Space == v.namespaceURI {
			text, readErr := readElementText(dec)
			if readErr == nil {
				assignHybridProperty(&props, startElem.Name.Local, strings.TrimSpace(text))
			}
		}
	}

	// Extension-schema declaration: scan the raw bytes for an entry
	// pointing at the variant namespace URI. The schema block is
	// generated in a stable shape by the writer, so a substring check
	// is reliable enough for this purpose.
	if bytes.Contains(xmp, []byte("<pdfaExtension:schemas")) {
		marker := []byte("<pdfaSchema:namespaceURI>" + v.namespaceURI + "</pdfaSchema:namespaceURI>")
		if bytes.Contains(xmp, marker) {
			props.extensionSchema = true
		}
	}
	return props
}

// assignHybridProperty writes the decoded value into the matching
// xmpProperties field. Unknown property names are ignored so the
// decoder remains forward-compatible with future Factur-X versions.
func assignHybridProperty(props *xmpProperties, local, value string) {
	switch local {
	case "DocumentType":
		props.documentType = value
	case "DocumentFileName":
		props.documentFileName = value
	case "Version":
		props.version = value
	case "ConformanceLevel":
		props.conformanceLevel = value
	}
}

// readElementText drains tokens from dec until the matching EndElement
// and returns the concatenated CharData. Nested element start tags
// abort the read so structured values surface as empty rather than
// being misinterpreted as text.
func readElementText(dec *xml.Decoder) (string, error) {
	var sb strings.Builder
	depth := 1
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			if depth == 1 {
				sb.Write(t)
			}
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
			if depth == 0 {
				return sb.String(), nil
			}
		}
	}
}

// tagPDFA3Base appends a single hybrid-invoice violation flagging the
// PDF/A-3 base as non-compliant when ValidatePDFA returned any errors.
// The original PDF/A violations propagate verbatim alongside this tag
// so callers can pinpoint the specific base failures.
func tagPDFA3Base(pdfaErrs []error, v hybridInvoiceVariant) []Violation {
	if len(pdfaErrs) == 0 {
		return nil
	}
	return failf(Rule_invoice_PDFA3Base, "%s: %d PDF/A-3 violations on the archival base", v.label, len(pdfaErrs))
}
