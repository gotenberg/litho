package compliance

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

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

// checkInvoiceCII reads the embedded invoice stream and runs the CII
// structural rules. The function is variant-aware: only the Factur-X
// 1.0+ / ZUGFeRD 2.1+ variant currently has a CII rule set. Legacy
// ZUGFeRD generations short-circuit because their CII shape differs
// enough that the spine checks would emit false positives.
func checkInvoiceCII(ctx context.Context, doc *document.Document, invoice invoiceFileSpec, v hybridInvoiceVariant) []Violation {
	if v.namespaceURI != variantFacturX.namespaceURI {
		return nil
	}
	if !invoice.found {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, invoice.streamRef)
	if err != nil {
		return failf(Rule_cii_StreamReadable, "%s: resolving invoice stream %d: %v", v.label, invoice.streamRef.ObjectNum, err)
	}
	data, err := stream.Bytes(ctx)
	if err != nil || len(data) == 0 {
		decoded, decErr := doc.DecodeStream(ctx, stream)
		if decErr != nil {
			return failf(Rule_cii_StreamReadable, "%s: decoding invoice stream %d: %v", v.label, invoice.streamRef.ObjectNum, decErr)
		}
		data = decoded
	}
	if len(data) == 0 {
		return failf(Rule_cii_StreamReadable, "%s: invoice stream %d is empty", v.label, invoice.streamRef.ObjectNum)
	}
	return validateCIIBytes(data, v)
}

// validateCIIBytes runs the CII spine checks against an in-memory XML
// payload. Extracted from [checkInvoiceCII] so unit tests can exercise
// the structural rules without round-tripping through a PDF.
func validateCIIBytes(payload []byte, v hybridInvoiceVariant) []Violation {
	presence, rootSeen, wellFormed, parseErr := scanCIIPresence(payload)
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

// scanCIIPresence walks the XML token stream once and fills the
// presence struct. The walk records boolean flags for required
// containers and trimmed text for leaf scalars whose value the
// rules need to inspect.
func scanCIIPresence(payload []byte) (p ciiPresence, rootSeen bool, wellFormed bool, parseErr error) {
	dec := xml.NewDecoder(bytes.NewReader(payload))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity

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
// for presence-check rules: a wrong-shaped element is fielded by
// the deeper XSD validator (Mustang) elsewhere in the matrix.
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
// found or the variant is not Factur-X (the engine's rule set only
// fits the joint format).
func checkInvoiceCIIXSD(ctx context.Context, doc *document.Document, invoice invoiceFileSpec, v hybridInvoiceVariant, schema *xsd.Schema) []Violation {
	if schema == nil {
		return nil
	}
	if v.namespaceURI != variantFacturX.namespaceURI {
		return nil
	}
	if !invoice.found || invoice.streamDict == nil {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, invoice.streamRef)
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

// checkInvoiceSchematron runs the supplied Schematron schema against
// the embedded CII XML and translates fired assertions into compliance
// violations. The variant guard mirrors checkInvoiceCIIXSD: only the
// Factur-X 1.0+ / ZUGFeRD 2.1+ variant carries a CII payload that the
// EN 16931 / XRechnung Schematron knows how to validate.
func checkInvoiceSchematron(ctx context.Context, doc *document.Document, invoice invoiceFileSpec, v hybridInvoiceVariant, schema *schematron.Schema, phase string) []Violation {
	if schema == nil {
		return nil
	}
	if v.namespaceURI != variantFacturX.namespaceURI {
		return nil
	}
	if !invoice.found || invoice.streamDict == nil {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, invoice.streamRef)
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

	var opts []schematron.EvalOption
	if phase != "" {
		opts = append(opts, schematron.WithPhase(phase))
	}
	violations, err := schematron.Validate(ctx, schema, data, opts...)
	if err != nil {
		// Parse failure on the CII payload surfaces via the CII spine
		// checks in checkInvoiceCII; do not double-report it here.
		return nil
	}
	out := make([]Violation, 0, len(violations))
	for _, sv := range violations {
		out = append(out, Violation{
			Rule:    ruleForSchematronSeverity(sv.Flag),
			Context: formatSchematronContext(v, sv),
		})
	}
	return out
}

// ruleForSchematronSeverity picks Fatal versus Warning based on the
// Schematron flag attribute. EN 16931 uses "fatal" for blockers and
// "warning"/"info" for softer signals; anything that does not match
// "warning" or "info" defaults to Fatal so unknown severities surface
// loudly rather than being silently downgraded.
func ruleForSchematronSeverity(flag string) Rule {
	switch flag {
	case "warning", "info":
		return Rule_cii_sch_Warning
	}
	return Rule_cii_sch_Fatal
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
