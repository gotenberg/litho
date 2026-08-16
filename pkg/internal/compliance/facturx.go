package compliance

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/schematron"
	"github.com/gotenberg/litho/pkg/internal/xsd"
	"github.com/gotenberg/litho/pkg/models"
)

// FacturXValidateOption configures [ValidateFacturX] and
// [ValidateZUGFeRD]. Options are designed to be safe to call from any
// goroutine and immutable after construction.
type FacturXValidateOption func(*facturXValidateOpts)

type facturXValidateOpts struct {
	xsdSchema       *xsd.Schema
	schematron      *schematron.Schema
	schematronPhase string
	// schematronBundle, when set, routes the Schematron pass by XMP
	// conformance level: XRECHNUNG-level documents validate against
	// the XRechnung schema and everything else falls back to the
	// EN 16931 baseline. The explicit schematron field above wins
	// when both are configured.
	schematronBundleEN16931   *schematron.Schema
	schematronBundleXRechnung *schematron.Schema
	// schematronBundleFacturXProfiles is a per-conformance-level
	// map of Factur-X profile schematrons. Keyed by the XMP
	// fx:ConformanceLevel value (canonical Factur-X 1.0.07 spelling:
	// "MINIMUM" / "BASIC WL" / "BASIC" / "EN 16931" / "EXTENDED").
	// When set, the profile match takes precedence over the
	// EN 16931 baseline; the XRechnung CIUS still wins over both.
	schematronBundleFacturXProfiles map[string]*schematron.Schema
	// schematronBundleZUGFeRD1 is the legacy ZUGFeRD 1.0 schematron
	// keyed off the urn:ferd:CrossIndustryDocument:invoice:1p0
	// namespace. The v100-family EN 16931 / XRechnung / Factur-X
	// profile rules don't match v1's element structure, so v1
	// documents take this separate route.
	schematronBundleZUGFeRD1 *schematron.Schema
	// schematronBundlePeppol carries the PEPPOL BIS 3.0 EN 16931 CII
	// schematron. It overlays the EN 16931 baseline for documents
	// whose CII profile is the plain "urn:cen.eu:en16931:2017"
	// identifier with no CIUS suffix.
	schematronBundlePeppol *schematron.Schema
	// xsdSchemaFacturXProfiles is the per-conformance-level XSD set.
	// Keyed by Factur-X 1.0.07 spelling ("MINIMUM" / "BASIC WL" /
	// "BASIC" / "EN 16931" / "EXTENDED"); the validator picks the
	// matching schema when the XMP claim selects a profile, falling
	// back to xsdSchema (the cross-profile D16B set) when no entry
	// matches.
	xsdSchemaFacturXProfiles map[string]*xsd.Schema
	// xsdSchemaZUGFeRD1 wires the legacy v1 XSD set against
	// urn:ferd:CrossIndustryDocument:invoice:1p0 documents. The
	// v100-family schemas (D16B and the Factur-X profile bundles) do
	// not share the v1 namespace and cannot validate v1 content.
	xsdSchemaZUGFeRD1 *xsd.Schema
	// schematronOverrides registers Go evaluators for rules whose
	// XPath the bundled engine cannot compile. The map is threaded
	// into [schematron.Validate] as [schematron.WithRuleOverrides].
	schematronOverrides map[string]schematron.RuleOverride
	// lenient relaxes the consumer-path tolerances the spec does not
	// sanction (non-spec /AFRelationship, application/xml subtype,
	// non-spec XMP Version). When false (the default, strict) those
	// values surface as warning-severity violations; when true they are
	// accepted silently.
	lenient bool
}

// WithFacturXLenient relaxes the Factur-X / ZUGFeRD consumer-path
// tolerances. By default the validator is strict: a tolerated but
// non-spec /AFRelationship, an application/xml subtype, or a non-spec
// XMP Version surfaces as a warning-severity [Violation]. Passing
// WithFacturXLenient(true) accepts those values silently, matching the
// behaviour of producers that carry them over from earlier ZUGFeRD
// generations. It does not relax fatal structural rules.
func WithFacturXLenient(lenient bool) FacturXValidateOption {
	return func(o *facturXValidateOpts) { o.lenient = lenient }
}

// WithFacturXXSD registers a loaded XSD schema set against which the
// validator runs a full XML Schema pass over the embedded CII XML.
// When the option is absent, the validator only runs the existing
// hand-rolled spine rules under clause CII; with the option present
// it additionally surfaces violations under clause CII_XSD covering
// the full XSD subset the engine supports.
//
// The schema set must be the Factur-X / ZUGFeRD 2.1+ CII D16B
// hierarchy; passing a different schema set will surface every
// element as a root or structure violation. PR5 leaves bundling of
// the canonical XSDs to the caller (use [xsd.Load] with an embed.FS
// or os.DirFS) so litho keeps no UN/CEFACT artefacts in-tree.
func WithFacturXXSD(s *xsd.Schema) FacturXValidateOption {
	return func(o *facturXValidateOpts) { o.xsdSchema = s }
}

// WithFacturXProfileXSDBundle registers per-conformance-level XSD
// sets. The map keys follow the Factur-X 1.0.07 spelling
// ("MINIMUM" / "BASIC WL" / "BASIC" / "EN 16931" / "EXTENDED");
// the validator picks the matching schema for the document's XMP
// fx:ConformanceLevel and runs the full XSD pass against it.
//
// Profile XSDs are tighter than the generic D16B set: MINIMUM drops
// most of the BasicInformation branch, BASIC-WL trims line-item
// detail, etc. Threading them through here makes the XSD layer
// surface profile-specific structural violations that the D16B
// fallback otherwise silently lets through.
func WithFacturXProfileXSDBundle(profiles map[string]*xsd.Schema) FacturXValidateOption {
	return func(o *facturXValidateOpts) { o.xsdSchemaFacturXProfiles = profiles }
}

// WithZUGFeRD1XSD registers the legacy ZUGFeRD 1.0 XSD set against
// which [ValidateZUGFeRD] for v1 documents runs the full XSD pass
// over the embedded urn:ferd:CrossIndustryDocument:invoice:1p0
// payload. The v100-family schemas do not match v1 content.
//
// Pass nil (or omit the option) to skip the v1 XSD pass; the
// validator then relies on the structural CII spine and the wrapper
// rules alone for v1 documents.
func WithZUGFeRD1XSD(s *xsd.Schema) FacturXValidateOption {
	return func(o *facturXValidateOpts) { o.xsdSchemaZUGFeRD1 = s }
}

// WithFacturXSchematron registers a loaded Schematron schema set
// against which the validator runs a business-rule pass over the
// embedded CII XML. The optional phase argument restricts evaluation
// to that phase's active patterns; pass an empty string to run every
// pattern. Violations surface under clause CII_SCH alongside the
// existing INVOICE, CII, and CII_XSD rules.
func WithFacturXSchematron(s *schematron.Schema, phase string) FacturXValidateOption {
	return func(o *facturXValidateOpts) {
		o.schematron = s
		o.schematronPhase = phase
	}
}

// WithFacturXSchematronBundle wires both the EN 16931 baseline and
// the XRechnung CIUS rule sets. The validator inspects the document's
// XMP fx:ConformanceLevel and routes XRECHNUNG-level documents to
// xrechnung; everything else uses en16931. Either argument may be nil
// to disable that route (e.g. pass nil for xrechnung if only the
// EN 16931 bundle is loaded).
func WithFacturXSchematronBundle(en16931, xrechnung *schematron.Schema) FacturXValidateOption {
	return func(o *facturXValidateOpts) {
		o.schematronBundleEN16931 = en16931
		o.schematronBundleXRechnung = xrechnung
	}
}

// WithFacturXProfileSchematronBundle registers per-conformance-level
// Factur-X profile schematrons. The map's keys are the canonical
// fx:ConformanceLevel values from Factur-X 1.0.07 ("MINIMUM",
// "BASIC WL", "BASIC", "EN 16931", "EXTENDED"); the values are
// loaded schemas that wrap CEN EN 16931 + PEPPOL-EN16931 +
// Factur-X-specific BR-FX-EN-* rules into one rule set per profile.
//
// The validator consults this map after [WithFacturXSchematron] and
// before [WithFacturXSchematronBundle]: when the document's level
// matches a key here, that profile's schematron runs instead of the
// EN 16931 baseline. Documents whose level reads XRECHNUNG continue
// routing to the XRechnung CIUS regardless.
func WithFacturXProfileSchematronBundle(profiles map[string]*schematron.Schema) FacturXValidateOption {
	return func(o *facturXValidateOpts) {
		o.schematronBundleFacturXProfiles = profiles
	}
}

// WithZUGFeRD1Schematron registers the legacy ZUGFeRD 1.0 Schematron
// so [ValidateZUGFeRD] for v1 documents runs a business-rule pass
// over the embedded urn:ferd:CrossIndustryDocument:invoice:1p0
// payload. ZUGFeRD 1.0's CII shape pre-dates the v100 namespace
// reorganisation, so the EN 16931 / XRechnung / Factur-X profile
// rule sets bundled for v2.x payloads do not apply.
//
// Pass nil (or omit the option) to skip the v1 Schematron pass; the
// validator then relies on the structural CII spine and the wrapper
// rules alone for v1 documents.
func WithZUGFeRD1Schematron(s *schematron.Schema) FacturXValidateOption {
	return func(o *facturXValidateOpts) {
		o.schematronBundleZUGFeRD1 = s
	}
}

// WithPeppolSchematron registers the PEPPOL BIS 3.0 EN 16931 CII
// schematron so [ValidateFacturX] runs it alongside the matching
// per-profile rules. Documents whose CII profile is plain
// "urn:cen.eu:en16931:2017" without a CIUS suffix get the PEPPOL
// rules applied as an overlay, matching the PEPPOL BIS 3.0
// publication's scope.
//
// Pass nil (or omit the option) to skip the PEPPOL overlay; the
// validator then only runs the configured baseline / profile
// schematron.
func WithPeppolSchematron(s *schematron.Schema) FacturXValidateOption {
	return func(o *facturXValidateOpts) {
		o.schematronBundlePeppol = s
	}
}

// WithFacturXSchematronOverrides registers Go evaluators that
// take precedence over the bundled XPath engine for the named
// Schematron rule ids. Used for rules whose XPath relies on
// XPath 2.0 constructs antchfx/xpath does not implement (BR-DE-19's
// IBAN checksum is the canonical example).
//
// The companion package cii_schematron ships
// [cii_schematron.BuiltinRuleOverrides]; callers can pass that map
// directly or merge it with their own entries.
func WithFacturXSchematronOverrides(m map[string]schematron.RuleOverride) FacturXValidateOption {
	return func(o *facturXValidateOpts) { o.schematronOverrides = m }
}

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
	// altVersions lists additional acceptable values for the XMP
	// Version property. Real-world producers vary on whether they
	// write the metadata schema number ("1.0") or the spec version
	// ("2p0"); a non-empty altVersions list relaxes the check to
	// admit both.
	altVersions []string
	// altRelationships lists additional acceptable values for the
	// embedded filespec's /AFRelationship. Factur-X 1.0+ mandates
	// /Data; many real-world producers carry over /Alternative from
	// ZUGFeRD 2.0 and the spec text tolerates either spelling.
	altRelationships []string
	allowedLevels    map[string]bool
	conflictNames    []string
}

//nolint:gochecknoglobals // immutable variant descriptors.
var (
	variantFacturX = hybridInvoiceVariant{
		label:            "Factur-X 1.0+/ZUGFeRD 2.1+",
		fileName:         "factur-x.xml",
		relationship:     "Data",
		altRelationships: []string{"Alternative"},
		namespaceURI:     "urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#",
		namespacePrefix:  "fx",
		// fx:Version is the metadata extension schema version, not
		// the spec version. Factur-X 1.0.07 fixes it at "1.0";
		// some producers reuse the spec version "2.1" instead and
		// the validator accepts both forms.
		documentVersion: "1.0",
		altVersions:     []string{"2.1"},
		// Factur-X 1.0.07 conformance levels (fx:ConformanceLevel),
		// EN 16931 / CEN/TS 16931-2 profiles plus the German XRECHNUNG.
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
		// ZUGFeRD 2.0 producers split on the zf:Version value: some
		// follow the spec letter ("2p0"), others reuse the metadata
		// schema number ("1.0"). Tolerate both spellings.
		documentVersion: "2p0",
		altVersions:     []string{"1.0"},
		// ZUGFeRD 2.0 introduced the EN 16931 (CIUS) profile and the
		// MINIMUM / BASIC WL / XRECHNUNG levels alongside the legacy
		// BASIC / COMFORT / EXTENDED that v1 carried. All six are
		// admissible per the ZUGFeRD 2.0 specification.
		allowedLevels: map[string]bool{
			"MINIMUM": true, "BASIC WL": true, "BASIC": true,
			"EN 16931": true, "COMFORT": true, "EXTENDED": true,
			"XRECHNUNG": true,
		},
		conflictNames: []string{"factur-x.xml", "ZUGFeRD-invoice.xml"},
	}
	variantZUGFeRD10 = hybridInvoiceVariant{
		label:           "ZUGFeRD 1.0",
		fileName:        "ZUGFeRD-invoice.xml",
		relationship:    "Alternative",
		namespaceURI:    "urn:ferd:pdfa:CrossIndustryDocument:invoice:1p0#",
		namespacePrefix: "zf",
		// Some ZUGFeRD 1.0 producers ship zf:Version = "1.0" even
		// though the spec letter is "1p0". Tolerate both.
		documentVersion: "1p0",
		altVersions:     []string{"1.0"},
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
func ValidateFacturX(ctx context.Context, doc *document.Document, opts ...FacturXValidateOption) ([]error, error) {
	return validateHybridInvoice(ctx, doc, variantFacturX, opts...)
}

// ValidateZUGFeRD audits the document against the ZUGFeRD version
// identified by ver. ZUGFeRD 2.1 reuses the joint Factur-X 1.0+
// variant; 1.0 and 2.0 carry their own filename, relationship, and
// namespace.
//
// Returns an error if ver is not one of [models.ZUGFeRD1],
// [models.ZUGFeRD2], or [models.ZUGFeRD21].
func ValidateZUGFeRD(ctx context.Context, doc *document.Document, ver models.ZUGFeRDVersion, opts ...FacturXValidateOption) ([]error, error) {
	variant, ok := zugferdVariantFor(ver)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProfile, ver)
	}
	return validateHybridInvoice(ctx, doc, variant, opts...)
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
func validateHybridInvoice(ctx context.Context, doc *document.Document, v hybridInvoiceVariant, opts ...FacturXValidateOption) ([]error, error) {
	var cfg facturXValidateOpts
	for _, o := range opts {
		o(&cfg)
	}
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
	hybrid = append(hybrid, checkInvoiceAFRelationship(ctx, doc, invoice, v, cfg.lenient)...)
	hybrid = append(hybrid, checkInvoiceStreamSubtype(ctx, doc, invoice, v, cfg.lenient)...)
	hybrid = append(hybrid, checkInvoiceConflictingVariant(ctx, doc, v)...)
	hybrid = append(hybrid, checkInvoiceXMP(ctx, doc, catalog, invoice, v, cfg.lenient)...)
	hybrid = append(hybrid, checkInvoiceCII(ctx, doc, invoice, v, cfg.lenient)...)
	xsdSchema := pickXSD(ctx, doc, catalog, v, &cfg)
	if xsdSchema != nil {
		hybrid = append(hybrid, checkInvoiceCIIXSD(ctx, doc, invoice, v, xsdSchema)...)
	}
	schSchema, schPhase, schOverlay := pickSchematron(ctx, doc, catalog, invoice, v, &cfg)
	var schAdvisories []error
	if schSchema != nil {
		schViolations, advisories := checkInvoiceSchematron(ctx, doc, invoice, v, schSchema, schPhase, schOverlay, cfg.schematronOverrides)
		hybrid = append(hybrid, schViolations...)
		schAdvisories = advisories
	}
	// PEPPOL BIS 3.0 overlays the EN 16931 baseline when the CII
	// profile is the plain "urn:cen.eu:en16931:2017" identifier with
	// no CIUS suffix. Factur-X / XRechnung / other CIUS-tagged
	// documents skip this pass because their per-profile schematron
	// already embeds the relevant PEPPOL extensions.
	if cfg.schematronBundlePeppol != nil && invoiceClaimsPlainEN16931(ctx, doc, invoice, v) {
		peppolViolations, peppolAdvisories := checkInvoiceSchematron(ctx, doc, invoice, v, cfg.schematronBundlePeppol, "", false, cfg.schematronOverrides)
		hybrid = append(hybrid, peppolViolations...)
		schAdvisories = append(schAdvisories, peppolAdvisories...)
	}
	hybrid = append(hybrid, tagPDFA3Base(pdfaErrs, v)...)

	out := violationsToErrors(hybrid)
	out = append(out, pdfaErrs...)
	out = append(out, schAdvisories...)
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
	// /AF should be an array per Factur-X spec, but some producers
	// ship a single filespec reference when only one file is
	// embedded. Tolerate both forms by promoting the bare ref into
	// a one-element array before iterating.
	var afArr ast.PdfArray
	if arr, err := document.ResolveArray(ctx, doc, afObj); err == nil {
		afArr = arr
	} else if ref, ok := afObj.(ast.PdfIndirectRef); ok {
		afArr = ast.PdfArray{ref}
	} else {
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
			return document.DecodePdfString(s)
		}
	}
	if f, ok := dict["F"]; ok {
		if s, err := document.ResolveString(ctx, doc, f); err == nil {
			return document.DecodePdfString(s)
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

func checkInvoiceAFRelationship(ctx context.Context, doc *document.Document, invoice invoiceFileSpec, v hybridInvoiceVariant, lenient bool) []Violation {
	if !invoice.found {
		return nil
	}
	afObj, ok := invoice.dict["AFRelationship"]
	if !ok {
		return failf(Rule_invoice_AFRelationship, "%s: filespec object %d missing /AFRelationship", v.label, invoice.objNum)
	}
	got, err := document.ResolveName(ctx, doc, afObj)
	if err != nil {
		return failf(Rule_invoice_AFRelationship, "%s: filespec object %d /AFRelationship unreadable: %v", v.label, invoice.objNum, err)
	}
	if string(got) == v.relationship {
		return nil
	}
	if slices.Contains(v.altRelationships, string(got)) {
		if lenient {
			return nil
		}
		return failf(Rule_invoice_AFRelationshipNonSpec, "%s: filespec object %d /AFRelationship = %q is tolerated but the spec mandates %q", v.label, invoice.objNum, got, v.relationship)
	}
	return failf(Rule_invoice_AFRelationship, "%s: filespec object %d /AFRelationship = %q, want %q", v.label, invoice.objNum, got, v.relationship)
}

func checkInvoiceStreamSubtype(ctx context.Context, doc *document.Document, invoice invoiceFileSpec, v hybridInvoiceVariant, lenient bool) []Violation {
	if !invoice.found || invoice.streamDict == nil {
		return nil
	}
	subtype, err := document.ResolveName(ctx, doc, invoice.streamDict["Subtype"])
	if err != nil {
		return failf(Rule_invoice_StreamSubtype, "%s: embedded stream %d /Subtype unreadable: %v", v.label, invoice.streamRef.ObjectNum, err)
	}
	// Factur-X 1.0.07 / ZUGFeRD 2.1+ mandate /text#2Fxml (text/xml) but a
	// sizeable portion of third-party producers ship /application#2Fxml
	// instead. Strict validation surfaces that as a warning; lenient
	// accepts it silently. The writer side always emits text/xml.
	switch string(subtype) {
	case "text/xml":
		return nil
	case "application/xml":
		if lenient {
			return nil
		}
		return failf(Rule_invoice_StreamSubtypeNonSpec, "%s: embedded stream %d /Subtype = application/xml is tolerated but the spec mandates text/xml", v.label, invoice.streamRef.ObjectNum)
	}
	return failf(Rule_invoice_StreamSubtype, "%s: embedded stream %d /Subtype = %q, want %q", v.label, invoice.streamRef.ObjectNum, subtype, "text/xml")
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

func checkInvoiceXMP(ctx context.Context, doc *document.Document, catalog ast.PdfDict, invoice invoiceFileSpec, v hybridInvoiceVariant, lenient bool) []Violation {
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
	switch {
	case props.version == v.documentVersion:
		// Matches the spec-fixed version.
	case slices.Contains(v.altVersions, props.version):
		if !lenient {
			results = append(results, failf(Rule_invoice_VersionNonSpec,
				"%s: XMP %s:Version = %q is tolerated but the spec fixes %q", v.label, v.namespacePrefix, props.version, v.documentVersion)...)
		}
	default:
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
	// Decode first: [ast.PdfStream.Bytes] hands back the raw body, so a
	// /Metadata stream carrying a filter (ISO 32000-1, 14.3.2 permits one)
	// yields compressed bytes in which none of the invoice properties can be
	// found, and every XMP clause of the variant fires on a conformant file.
	// Raw remains the fallback for a stream the filter pipeline cannot decode.
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(data) == 0 {
		raw, rawErr := stream.Bytes(ctx)
		if rawErr != nil {
			return nil
		}
		data = raw
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
	// XMP property extraction is best-effort: pull the conformance level
	// and version even from slightly non-conformant XMP so routing and the
	// value checks have something to inspect. The strict well-formedness
	// gate for the CII payload lives in scanCIIPresence.
	dec := newComplianceXMLDecoder(xmp, true)

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
