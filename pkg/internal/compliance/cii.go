package compliance

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	cii_schematron "github.com/gotenberg/litho/pkg/internal/compliance/cii_schematron"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/schematron"
	"github.com/gotenberg/litho/pkg/internal/xsd"
)

// UN/CEFACT Cross-Industry Invoice namespace URIs. The XML decoder
// matches xml.Name.Space against the literal URI, so prefix aliasing
// in the source document is transparent to these constants.
const (
	ciiRootNS = "urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"
	ciiRamNS  = "urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100"
)

// ciiPresence captures which spine elements were observed in the CII
// payload. Boolean flags answer "did the element appear at all?" and
// string fields hold the trimmed text content of the leaf scalars the
// Factur-X 1.0.07 minimum-content requirements check.
type ciiPresence struct {
	documentContext       bool
	document              bool
	documentID            string
	documentIssueDateTime string
	tradeTransaction      bool
	headerTradeAgreement  bool
	sellerName            string
	headerTradeSettlement bool
	invoiceCurrencyCode   string
	grandTotalAmount      string
}

// variantHasCIIv100Payload reports whether v carries a CII payload
// using the UN/CEFACT v100 namespace
// (urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100).
// Factur-X 1.0+ / ZUGFeRD 2.1+ and ZUGFeRD 2.0 share that schema;
// legacy ZUGFeRD 1.0 uses urn:ferd:CrossIndustryDocument:invoice:1p0:*
// instead, so its CII spine rules and Schematron pass have to stay
// off to avoid emitting false positives.
func variantHasCIIv100Payload(v hybridInvoiceVariant) bool {
	return v.namespaceURI == variantFacturX.namespaceURI ||
		v.namespaceURI == variantZUGFeRD20.namespaceURI
}

// variantHasAnyCIIPayload reports whether v carries any flavour of
// CII payload the schematron layer can run rules against. v100
// covers EN 16931 / XRechnung / Factur-X profile schemas; the
// legacy urn:ferd:CrossIndustryDocument:invoice:1p0 payload pairs
// with a dedicated v1 Schematron bundle, so the gate here admits
// both so [checkInvoiceSchematron] can dispatch on the picked
// schema rather than the namespace alone.
func variantHasAnyCIIPayload(v hybridInvoiceVariant) bool {
	return variantHasCIIv100Payload(v) ||
		v.namespaceURI == variantZUGFeRD10.namespaceURI
}

// checkInvoiceCII reads the embedded invoice stream and runs the CII
// structural rules. The function is variant-aware: ZUGFeRD 2.0 and
// the Factur-X 1.0+ / ZUGFeRD 2.1+ variants share the CII v100
// payload that the spine rules cover. Legacy ZUGFeRD 1.0 short-
// circuits because its CII shape differs enough that the spine
// checks would emit false positives.
func checkInvoiceCII(ctx context.Context, doc *document.Document, invoice invoiceFileSpec, v hybridInvoiceVariant, lenient bool) []Violation {
	if !variantHasCIIv100Payload(v) {
		return nil
	}
	if !invoice.found {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, invoice.streamRef)
	if err != nil {
		return failf(Rule_cii_StreamReadable, "%s: resolving invoice stream %d: %v", v.label, invoice.streamRef.ObjectNum, err)
	}
	// Always go through DecodeStream: stream.Bytes returns the raw
	// post-decryption bytes which are still filter-encoded (FlateDecode
	// is common on embedded XML), and feeding those to the XML parser
	// trips an "invalid UTF-8" error.
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return failf(Rule_cii_StreamReadable, "%s: decoding invoice stream %d: %v", v.label, invoice.streamRef.ObjectNum, err)
	}
	if len(data) == 0 {
		return failf(Rule_cii_StreamReadable, "%s: invoice stream %d is empty", v.label, invoice.streamRef.ObjectNum)
	}
	return validateCIIBytes(data, v, lenient)
}

// validateCIIBytes runs the CII spine checks against an in-memory XML
// payload. Extracted from [checkInvoiceCII] so unit tests can exercise
// the structural rules without round-tripping through a PDF.
func validateCIIBytes(payload []byte, v hybridInvoiceVariant, lenient bool) []Violation {
	presence, rootSeen, wellFormed, parseErr := scanCIIPresence(payload, lenient)
	if !wellFormed {
		return failf(Rule_cii_WellFormed, "%s: invoice XML is not well-formed: %v", v.label, parseErr)
	}
	if !rootSeen {
		return failf(Rule_cii_RootElement,
			"%s: root element is not CrossIndustryInvoice in namespace %q",
			v.label, ciiRootNS)
	}

	results := make([]Violation, 0, 6)
	if !presence.documentContext {
		results = append(results, failf(Rule_cii_DocumentContext,
			"%s: ExchangedDocumentContext missing under CrossIndustryInvoice", v.label)...)
	}
	if !presence.document {
		results = append(results, failf(Rule_cii_ExchangedDocument,
			"%s: ExchangedDocument missing under CrossIndustryInvoice", v.label)...)
	} else if presence.documentID == "" || presence.documentIssueDateTime == "" {
		results = append(results, failf(Rule_cii_ExchangedDocument,
			"%s: ExchangedDocument missing ID=%q IssueDateTime=%q",
			v.label, presence.documentID, presence.documentIssueDateTime)...)
	}
	if !presence.tradeTransaction {
		results = append(results, failf(Rule_cii_TradeTransaction,
			"%s: SupplyChainTradeTransaction missing under CrossIndustryInvoice", v.label)...)
	}
	if !presence.headerTradeAgreement || presence.sellerName == "" {
		results = append(results, failf(Rule_cii_SellerTradeParty,
			"%s: SellerTradeParty/Name missing under ApplicableHeaderTradeAgreement (sellerName=%q)",
			v.label, presence.sellerName)...)
	}
	if !presence.headerTradeSettlement || presence.invoiceCurrencyCode == "" {
		results = append(results, failf(Rule_cii_InvoiceCurrency,
			"%s: InvoiceCurrencyCode missing under ApplicableHeaderTradeSettlement (got %q)",
			v.label, presence.invoiceCurrencyCode)...)
	}
	if presence.grandTotalAmount == "" {
		results = append(results, failf(Rule_cii_GrandTotalAmount,
			"%s: GrandTotalAmount missing under SpecifiedTradeSettlementHeaderMonetarySummation",
			v.label)...)
	}
	return results
}

// ciiPath captures the qualified element names from the document root
// down to the current decoder position. A path-based match is the
// simplest way to avoid false positives on namespaces that reuse the
// same local name (e.g. "ID" appears in many CII sub-elements).
type ciiPath []xml.Name

func (p ciiPath) push(n xml.Name) ciiPath { return append(p, n) }
func (p ciiPath) pop() ciiPath {
	if len(p) == 0 {
		return p
	}
	return p[:len(p)-1]
}

// matches reports whether the path ends with the given (ns, local) pairs
// in order. Used to flag spine elements regardless of intermediate
// optional wrappers the spec leaves room for.
func (p ciiPath) matches(steps ...xml.Name) bool {
	if len(p) < len(steps) {
		return false
	}
	tail := p[len(p)-len(steps):]
	for i, want := range steps {
		if tail[i] != want {
			return false
		}
	}
	return true
}

// newComplianceXMLDecoder builds the XML decoder used across the
// Factur-X / CII validation path. Strict by default: a malformed
// document or an HTML named entity (which CII and XMP never define) is
// rejected, so the well-formedness check produces a violation. When
// lenient is true the decoder tolerates both, matching producers that
// emit not-quite-conformant XML. No spec sanctions the lenient mode; it
// is opt-in via WithFacturXLenient.
func newComplianceXMLDecoder(payload []byte, lenient bool) *xml.Decoder {
	dec := xml.NewDecoder(bytes.NewReader(payload))
	if lenient {
		dec.Strict = false
		dec.Entity = xml.HTMLEntity
	}
	return dec
}

// scanCIIPresence walks the XML token stream once and fills the
// presence struct. The walk records boolean flags for required
// containers and trimmed text for leaf scalars whose value the
// rules need to inspect.
func scanCIIPresence(payload []byte, lenient bool) (p ciiPresence, rootSeen bool, wellFormed bool, parseErr error) {
	dec := newComplianceXMLDecoder(payload, lenient)

	root := xml.Name{Space: ciiRootNS, Local: "CrossIndustryInvoice"}
	docContext := xml.Name{Space: ciiRootNS, Local: "ExchangedDocumentContext"}
	document := xml.Name{Space: ciiRootNS, Local: "ExchangedDocument"}
	tradeTx := xml.Name{Space: ciiRootNS, Local: "SupplyChainTradeTransaction"}

	idElem := xml.Name{Space: ciiRamNS, Local: "ID"}
	issueDate := xml.Name{Space: ciiRamNS, Local: "IssueDateTime"}
	headerAgreement := xml.Name{Space: ciiRamNS, Local: "ApplicableHeaderTradeAgreement"}
	sellerParty := xml.Name{Space: ciiRamNS, Local: "SellerTradeParty"}
	sellerName := xml.Name{Space: ciiRamNS, Local: "Name"}
	headerSettlement := xml.Name{Space: ciiRamNS, Local: "ApplicableHeaderTradeSettlement"}
	currencyCode := xml.Name{Space: ciiRamNS, Local: "InvoiceCurrencyCode"}
	monetarySummation := xml.Name{Space: ciiRamNS, Local: "SpecifiedTradeSettlementHeaderMonetarySummation"}
	grandTotal := xml.Name{Space: ciiRamNS, Local: "GrandTotalAmount"}

	wellFormed = true
	var path ciiPath
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return ciiPresence{}, false, false, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			path = path.push(t.Name)
			// Spine: identify the root the first time it appears at depth 1.
			if len(path) == 1 && path[0] == root {
				rootSeen = true
			}
			if !rootSeen {
				continue
			}
			// Container flags first, then leaves. Leaves consume tokens
			// through their matching EndElement and pop the path
			// themselves so the outer loop's EndElement handler stays
			// balanced.
			switch {
			case path.matches(root, docContext):
				p.documentContext = true
			case path.matches(root, document):
				p.document = true
			case path.matches(root, tradeTx):
				p.tradeTransaction = true
			case path.matches(root, tradeTx, headerAgreement):
				p.headerTradeAgreement = true
			case path.matches(root, tradeTx, headerSettlement):
				p.headerTradeSettlement = true
			case path.matches(root, document, idElem):
				p.documentID = readLeafText(dec, t.Name)
				path = path.pop()
			case path.matches(root, document, issueDate):
				v := readLeafText(dec, t.Name)
				if p.documentIssueDateTime == "" {
					p.documentIssueDateTime = v
				}
				path = path.pop()
			case path.matches(root, tradeTx, headerAgreement, sellerParty, sellerName):
				v := readLeafText(dec, t.Name)
				if p.sellerName == "" {
					p.sellerName = v
				}
				path = path.pop()
			case path.matches(root, tradeTx, headerSettlement, currencyCode):
				p.invoiceCurrencyCode = readLeafText(dec, t.Name)
				path = path.pop()
			case path.matches(root, tradeTx, headerSettlement, monetarySummation, grandTotal):
				v := readLeafText(dec, t.Name)
				if p.grandTotalAmount == "" {
					p.grandTotalAmount = v
				}
				path = path.pop()
			}
		case xml.EndElement:
			path = path.pop()
		}
	}
	return p, rootSeen, wellFormed, nil
}

// readLeafText drains tokens from dec until the matching EndElement
// for name and returns the concatenated, trimmed CharData found at
// any depth inside. Several CII scalars wrap a udt: child (notably
// ram:IssueDateTime → udt:DateTimeString); collecting CharData
// regardless of nesting treats the wrapper as transparent. The
// downside, accepting mixed content as a flat string, is acceptable
// for presence-check rules: a wrong-shaped element is caught by
// the XSD pass under clause CII_XSD.
func readLeafText(dec *xml.Decoder, name xml.Name) string {
	var sb strings.Builder
	depth := 1
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
			if depth == 0 && t.Name == name {
				return strings.TrimSpace(sb.String())
			}
		}
	}
}

// checkInvoiceCIIXSD runs the pure-Go XSD engine against the embedded
// CII XML and translates each XSD violation into a Violation under
// the CII_XSD clause. Skips silently when the invoice file is not
// found. The gate admits ZUGFeRD 1.0 alongside the v100-family CII
// variants because the caller pairs the variant with the matching
// XSD set; the schema's own root element binding catches
// namespace mismatches.
func checkInvoiceCIIXSD(ctx context.Context, doc *document.Document, invoice invoiceFileSpec, v hybridInvoiceVariant, schema *xsd.Schema) []Violation {
	if schema == nil {
		return nil
	}
	if !variantHasAnyCIIPayload(v) {
		return nil
	}
	if !invoice.found || invoice.streamDict == nil {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, invoice.streamRef)
	if err != nil {
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(data) == 0 {
		return nil
	}

	xsdViolations, err := xsd.Validate(ctx, schema, data)
	if err != nil {
		// Parse failure already surfaced via the structural spine
		// rules in checkInvoiceCII; do not double-report it.
		return nil
	}
	out := make([]Violation, 0, len(xsdViolations))
	for _, xv := range xsdViolations {
		out = append(out, Violation{
			Rule:    ruleForXSDClass(xv.Rule),
			Context: formatXSDContext(v, xv),
		})
	}
	return out
}

// ruleForXSDClass picks the matching CII_XSD rule constant for the
// engine's per-category violation kind. Unknown categories fall back
// to the structural rule so the violation still surfaces with a
// sensible classification.
func ruleForXSDClass(class string) Rule {
	switch class {
	case xsd.RuleRoot:
		return Rule_cii_xsd_Root
	case xsd.RuleStructure:
		return Rule_cii_xsd_Structure
	case xsd.RuleCardinality:
		return Rule_cii_xsd_Cardinality
	case xsd.RuleLexical:
		return Rule_cii_xsd_Lexical
	case xsd.RuleFacet:
		return Rule_cii_xsd_Facet
	case xsd.RuleAttribute:
		return Rule_cii_xsd_Attribute
	}
	return Rule_cii_xsd_Structure
}

// formatXSDContext renders the variant label, the violating path,
// and the engine's reason text into a single Context string. The
// scoreboard surfaces this verbatim alongside the rule description.
func formatXSDContext(v hybridInvoiceVariant, xv xsd.Violation) string {
	if xv.Path == "" {
		return v.label + ": " + xv.Reason
	}
	return v.label + " at " + xv.Path + ": " + xv.Reason
}

// pickSchematron resolves the Schematron schema to run against the
// document. The override path (WithFacturXSchematron) always wins.
// Falling back to the bundle path, the chooser layers two signals:
//
//   - The CII payload's
//     rsm:ExchangedDocumentContext/ram:GuidelineSpecifiedDocumentContextParameter/ram:ID.
//     Documents that carry the XRechnung CIUS marker in this field
//     (including the extended form
//     "urn:cen.eu:en16931:2017#compliant#urn:xoev-de:kosit:standard:xrechnung_*"
//     that documents whose XMP claims plain EN 16931 still emit)
//     route to the XRechnung CIUS so the EN 16931 profile
//     schematron's FX-SCH-A-000026 does not reject the extended ID.
//   - fx:ConformanceLevel from the XMP packet, used as the final
//     tie-breaker. XRECHNUNG levels route to the XRechnung CIUS,
//     anything matching a Factur-X profile bundle entry routes to
//     that profile, and the EN 16931 baseline catches the rest.
//
// The second return value is the chosen schema's phase string; the
// third reports whether the schema runs as an overlay (CII-ID route
// while the XMP claim is not XRECHNUNG). Overlay firings carry
// notice severity and are demoted in [checkInvoiceSchematron].
// Returns (nil, "", false) when no schema applies.
func pickSchematron(ctx context.Context, doc *document.Document, catalog ast.PdfDict, invoice invoiceFileSpec, v hybridInvoiceVariant, cfg *facturXValidateOpts) (*schematron.Schema, string, bool) {
	if cfg.schematron != nil {
		return cfg.schematron, cfg.schematronPhase, false
	}
	// Legacy ZUGFeRD 1.0 documents take a dedicated schematron route:
	// the v100-family EN 16931 / XRechnung / Factur-X profile rule
	// sets are keyed off the v100 namespace and never match v1
	// content, so they can't substitute.
	if v.namespaceURI == variantZUGFeRD10.namespaceURI {
		return cfg.schematronBundleZUGFeRD1, "", false
	}
	if cfg.schematronBundleEN16931 == nil &&
		cfg.schematronBundleXRechnung == nil &&
		len(cfg.schematronBundleFacturXProfiles) == 0 {
		return nil, "", false
	}
	level := ""
	if xmpBytes := readInvoiceXMP(ctx, doc, catalog); xmpBytes != nil {
		level = decodeHybridInvoiceXMP(xmpBytes, v).conformanceLevel
	}
	if cfg.schematronBundleXRechnung != nil && invoiceClaimsXRechnungCIUS(ctx, doc, invoice, v) {
		// Overlay mode: the document claims a non-XRechnung profile
		// in XMP but its CII GuidelineSpecifiedDocumentContextParameter
		// /ID carries the XRechnung CIUS marker. The XRechnung
		// schematron runs in advisory mode for these, so every
		// firing surfaces as a notice rather than an error.
		return cfg.schematronBundleXRechnung, "", level != "XRECHNUNG"
	}
	if level == "XRECHNUNG" && cfg.schematronBundleXRechnung != nil {
		return cfg.schematronBundleXRechnung, "", false
	}
	if s, ok := cfg.schematronBundleFacturXProfiles[level]; ok && s != nil {
		return s, "", false
	}
	return cfg.schematronBundleEN16931, "", false
}

// pickXSD resolves the XSD schema to run against the document. The
// override path (WithFacturXXSD) wins when no profile-specific
// schema is registered. For ZUGFeRD 1.0 the v1 schema short-
// circuits because the v100-family bundles do not match v1
// content. For v2.x documents the chooser tries the Factur-X
// profile bundle matching the XMP fx:ConformanceLevel first,
// falling back to the generic D16B schema when no profile match
// applies.
//
// Returns nil when no XSD applies (the validator then runs the
// structural spine rules without an XSD pass).
func pickXSD(ctx context.Context, doc *document.Document, catalog ast.PdfDict, v hybridInvoiceVariant, cfg *facturXValidateOpts) *xsd.Schema {
	if v.namespaceURI == variantZUGFeRD10.namespaceURI {
		return cfg.xsdSchemaZUGFeRD1
	}
	if len(cfg.xsdSchemaFacturXProfiles) > 0 {
		level := ""
		if xmpBytes := readInvoiceXMP(ctx, doc, catalog); xmpBytes != nil {
			level = decodeHybridInvoiceXMP(xmpBytes, v).conformanceLevel
		}
		if s, ok := cfg.xsdSchemaFacturXProfiles[level]; ok && s != nil {
			return s
		}
	}
	return cfg.xsdSchema
}

// invoiceClaimsPlainEN16931 reports whether the embedded CII payload
// declares the plain EN 16931 profile identifier
// ("urn:cen.eu:en16931:2017") with no CIUS suffix in its
// GuidelineSpecifiedDocumentContextParameter/ID. Used by the PEPPOL
// overlay so its rules fire only on the documents the PEPPOL BIS
// spec targets - Factur-X / XRechnung / other CIUS profiles rely on
// their own per-profile bundle.
func invoiceClaimsPlainEN16931(ctx context.Context, doc *document.Document, invoice invoiceFileSpec, v hybridInvoiceVariant) bool {
	if !variantHasCIIv100Payload(v) || !invoice.found || invoice.streamDict == nil {
		return false
	}
	stream, err := document.ResolveStream(ctx, doc, invoice.streamRef)
	if err != nil {
		return false
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(data) == 0 {
		return false
	}
	id := scanCIIProfileID(data)
	return strings.TrimSpace(id) == "urn:cen.eu:en16931:2017"
}

// invoiceClaimsXRechnungCIUS reports whether the embedded CII payload
// declares an XRechnung CIUS in its GuidelineSpecifiedDocumentContextParameter
// /ID. Used by pickSchematron to override the XMP-derived
// conformance level when the document carries the XRechnung CIUS
// marker, applying the XRechnung schematron as an advisory overlay
// regardless of the XMP claim.
func invoiceClaimsXRechnungCIUS(ctx context.Context, doc *document.Document, invoice invoiceFileSpec, v hybridInvoiceVariant) bool {
	if !variantHasCIIv100Payload(v) || !invoice.found || invoice.streamDict == nil {
		return false
	}
	stream, err := document.ResolveStream(ctx, doc, invoice.streamRef)
	if err != nil {
		return false
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(data) == 0 {
		return false
	}
	id := scanCIIProfileID(data)
	if id == "" {
		return false
	}
	return strings.Contains(strings.ToLower(id), "xrechnung")
}

// scanCIIProfileID extracts the
// CrossIndustryInvoice/ExchangedDocumentContext/GuidelineSpecifiedDocumentContextParameter/ID
// leaf text and returns its trimmed value, or empty when the field is
// absent or the payload is not well-formed CII. The decoder stops at
// the first match so an early return covers the typical case where
// the document context sits near the document head.
func scanCIIProfileID(payload []byte) string {
	// Profile detection is best-effort routing, not a verdict: extract the
	// id even from slightly non-conformant XML so the right Schematron set
	// is selected. The strict well-formedness gate lives in scanCIIPresence.
	dec := newComplianceXMLDecoder(payload, true)

	root := xml.Name{Space: ciiRootNS, Local: "CrossIndustryInvoice"}
	docContext := xml.Name{Space: ciiRootNS, Local: "ExchangedDocumentContext"}
	guideline := xml.Name{Space: ciiRamNS, Local: "GuidelineSpecifiedDocumentContextParameter"}
	idElem := xml.Name{Space: ciiRamNS, Local: "ID"}

	var path ciiPath
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		switch t := tok.(type) {
		case xml.StartElement:
			path = path.push(t.Name)
			if path.matches(root, docContext, guideline, idElem) {
				return readLeafText(dec, t.Name)
			}
		case xml.EndElement:
			path = path.pop()
		}
	}
}

// checkInvoiceSchematron runs the supplied Schematron schema against
// the embedded CII XML and translates fired assertions into compliance
// violations. Rules whose XPath the engine could not compile or
// evaluate surface as a parallel advisory slice; each entry wraps
// [ErrSchematronRuleSkipped] so callers can branch on the sentinel
// without scanning message text. The variant guard mirrors
// checkInvoiceCIIXSD: only the Factur-X 1.0+ / ZUGFeRD 2.1+ variant
// carries a CII payload that the EN 16931 / XRechnung Schematron
// knows how to validate.
func checkInvoiceSchematron(ctx context.Context, doc *document.Document, invoice invoiceFileSpec, v hybridInvoiceVariant, schema *schematron.Schema, phase string, overlay bool, overrides map[string]schematron.RuleOverride) ([]Violation, []error) {
	if schema == nil {
		return nil, nil
	}
	if !variantHasAnyCIIPayload(v) {
		return nil, nil
	}
	if !invoice.found || invoice.streamDict == nil {
		return nil, nil
	}
	stream, err := document.ResolveStream(ctx, doc, invoice.streamRef)
	if err != nil {
		return nil, nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	var opts []schematron.EvalOption
	if phase != "" {
		opts = append(opts, schematron.WithPhase(phase))
	}
	if len(overrides) > 0 {
		opts = append(opts, schematron.WithRuleOverrides(overrides))
	}
	violations, skipped, err := schematron.Validate(ctx, schema, data, opts...)
	if err != nil {
		// Parse failure on the CII payload surfaces via the CII spine
		// checks in checkInvoiceCII; do not double-report it here.
		return nil, nil
	}
	out := make([]Violation, 0, len(violations))
	for _, sv := range violations {
		rule := ruleForSchematronSeverity(sv)
		if overlay && rule == Rule_cii_sch_Fatal {
			// Overlay mode demotes every fatal firing to warning so
			// the overall verdict treats the overlay schematron as
			// advisory when the document does not claim the
			// overlay's profile.
			rule = Rule_cii_sch_Warning
		}
		out = append(out, Violation{
			Rule:    rule,
			Context: formatSchematronContext(v, sv),
		})
	}
	advisories := make([]error, 0, len(skipped))
	for _, s := range skipped {
		advisories = append(advisories, &schematronSkippedError{
			RuleID:  s.RuleID,
			Context: v.label,
			Reason:  s.Reason,
		})
	}
	if len(skipped) > 0 {
		doc.Logger.Debug("schematron: rules skipped (XPath unsupported in the engine subset)",
			"variant", v.label, "skipped_count", len(skipped),
			"first", skipped[0].RuleID)
	}
	return out, advisories
}

// ruleForSchematronSeverity picks Fatal versus Warning for one fired
// Schematron rule. Three cases land in Warning:
//
//   - flag="warning" or flag="info" (the schema author tagged the
//     rule as soft on purpose; the bundled Factur-X profile
//     schematrons mark "still status warning" PEPPOL rules this way);
//   - <sch:report> firings without an explicit fatal flag. ISO
//     Schematron treats sch:report as informational by design (the
//     XSLT compilation surfaces them as <svrl:successful-report>
//     notices, not failed asserts), so litho keeps them out of the
//     validity bit to avoid spurious failures on profile-restriction
//     reports (e.g. "Attribute @schemeID is marked as not used in
//     the given context");
//   - the assertion message carries a "[RULE-ID]" prefix whose
//     EN 16931 baseline entry has flag="warning" or flag="info".
//     The bundled Factur-X profile schematrons rebadge baseline
//     rules under fresh FX-SCH-A-* ids and lose the baseline's flag
//     attribute, so rules like [CII-SR-465] would silently flip
//     Fatal under per-profile mode. Inheriting the baseline flag
//     restores the published severity without forcing a regeneration
//     of the bundled .sch files.
//
// Everything else defaults to Fatal so unknown severities surface
// loudly rather than being silently downgraded.
func ruleForSchematronSeverity(sv schematron.Violation) Rule {
	switch sv.Flag {
	case "warning", "info":
		return Rule_cii_sch_Warning
	case "fatal":
		return Rule_cii_sch_Fatal
	}
	if sv.Negate {
		return Rule_cii_sch_Warning
	}
	if id := baselineRuleID(sv.Message); id != "" {
		if flag, ok := cii_schematron.BaselineFlagForRule(id); ok {
			switch flag {
			case "warning", "info":
				return Rule_cii_sch_Warning
			}
		}
	}
	return Rule_cii_sch_Fatal
}

// baselineRuleID extracts the "[RULE-ID]" prefix the bundled Factur-X
// profile schematrons embed in assertion messages, mapping a per-
// profile firing back to its EN 16931 baseline rule id. Returns the
// empty string when no bracketed identifier sits at the head of msg
// or when the brackets carry something other than an upper-case
// identifier token. The lookup is non-greedy so a message like
// "[CII-SR-465]-Only one BT-41 element is allowed" yields
// "CII-SR-465" cleanly.
func baselineRuleID(msg string) string {
	s := strings.TrimLeft(msg, " \t\n\r")
	if s == "" || s[0] != '[' {
		return ""
	}
	end := strings.IndexByte(s, ']')
	if end <= 1 {
		return ""
	}
	id := s[1:end]
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c != '-' && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return ""
		}
	}
	return id
}

// formatSchematronContext renders one fired assertion into the
// compliance violation Context string. RuleID is the source
// assertion id (e.g. BR-CO-25), Path locates the offending node.
func formatSchematronContext(v hybridInvoiceVariant, sv schematron.Violation) string {
	prefix := "assert"
	if sv.Negate {
		prefix = "report"
	}
	if sv.Path == "" {
		return fmt.Sprintf("%s: %s %s: %s", v.label, prefix, sv.RuleID, sv.Message)
	}
	return fmt.Sprintf("%s at %s: %s %s: %s", v.label, sv.Path, prefix, sv.RuleID, sv.Message)
}
