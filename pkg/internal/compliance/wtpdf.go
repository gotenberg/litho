package compliance

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// wtpdfDeclarationNamespace is the XMP namespace URI of the PDF
// Declarations schema, bound to the "pdfd" prefix by convention. WTPDF
// 1.0 identifies a conforming file by a pdfd:conformsTo entry rather
// than by a pdfuaid identification schema.
const wtpdfDeclarationNamespace = "http://pdfa.org/declarations/"

// WTPDF declaration URIs, one per conformance level.
const (
	wtpdfAccessibilityURI = "http://pdfa.org/declarations/wtpdf#accessibility1.0"
	wtpdfReuseURI         = "http://pdfa.org/declarations/wtpdf#reuse1.0"
)

// wtpdfDroppedBoth lists the ISO 14289-2 rules that neither WTPDF level
// states, keyed "clause/test". WTPDF identifies a file by its PDF
// Declaration, so the whole of PDF/UA-2 clause 5 (the pdfuaid
// identification schema) falls away.
var wtpdfDroppedBoth = map[string]bool{
	"5/1": true,
	"5/2": true,
	"5/3": true,
	"5/4": true,
	"5/5": true,
}

// wtpdfDroppedReuse lists the further ISO 14289-2 rules that the WTPDF
// reuse level drops, keyed "clause/test". Reuse targets machine
// consumption of content, so the requirements that exist to serve
// assistive technology (alternate descriptions, PUA text alternatives,
// optional-content naming, annotation intent) are not stated at that
// level. The accessibility level keeps every one of them.
var wtpdfDroppedReuse = map[string]bool{
	"8.11.2/1":     true, // ViewerPreferences /DisplayDocTitle
	"8.2.5.28.2/1": true, // Figure alternate description
	"8.2.5.29/1":   true, // math only inside a Formula
	"8.4.3/1":      true, // PUA content needs ActualText or Alt
	"8.4.3/2":      true, // ActualText shall not contain PUA
	"8.4.3/3":      true, // Alt shall not contain PUA
	"8.7/1":        true, // optional content configuration /Name
	"8.7/2":        true, // /AS forbidden in an OC configuration
	"8.9.2.3/2":    true, // /RC and /Contents textually equivalent
	"8.9.2.4.19/1": true, // 3D annotation description
	"8.9.2.4.19/2": true, // RichMedia annotation description
	"8.9.2.4.7/1":  true, // Stamp annotation intent
	"8.9.2.4.8/1":  true, // Ink annotation description
}

// wtpdfDropsRule reports whether the given WTPDF level leaves the rule
// unstated. Only ISO 14289-2 rules can be dropped: a WTPDF run reports
// PDF/UA-2 clauses because WTPDF adopts them, so the drop list is keyed
// to that specification and nothing else is filtered.
func wtpdfDropsRule(profile models.WTPDFProfile, rule Rule) bool {
	if rule.Specification != "ISO_14289_2" {
		return false
	}
	key := rule.Clause + "/" + strconv.Itoa(rule.TestNumber)
	if wtpdfDroppedBoth[key] {
		return true
	}
	return profile == models.WTPDF1Reuse && wtpdfDroppedReuse[key]
}

// wtpdfDeclarationRule returns the PDF Declaration rule and the URI it
// requires for the given level.
func wtpdfDeclarationRule(profile models.WTPDFProfile) (Rule, string) {
	switch profile {
	case models.WTPDF1Accessibility:
		return Rule_WTPDF_1_0_6_1_3_t01, wtpdfAccessibilityURI
	case models.WTPDF1Reuse:
		return Rule_WTPDF_1_0_6_1_2_t01, wtpdfReuseURI
	}
	return Rule{}, ""
}

// WTPDFDeclarationURI returns the PDF Declaration URI a WTPDF conformance
// level requires, or "" when the profile is not a WTPDF one. Remediation
// uses it to write the declaration this package validates, so both sides
// read the URI from one place.
func WTPDFDeclarationURI(profile string) string {
	_, uri := wtpdfDeclarationRule(models.WTPDFProfile(profile))
	return uri
}

// WTPDFDeclarationNamespace is the XMP namespace URI of the PDF
// Declarations schema, exported for the remediation fixer that writes a
// declaration into a packet.
const WTPDFDeclarationNamespace = wtpdfDeclarationNamespace

// maxWTPDFDeclarations bounds how many pdfd:conformsTo entries are read
// from one XMP packet. This is an anti-DoS guard, not a spec limit: a
// crafted packet can repeat the property indefinitely, while a real one
// carries one entry per conformance level it claims.
const maxWTPDFDeclarations = 64

// wtpdfDeclarations returns the conformance URIs the PDF Declarations of the
// XMP packet state: the pdfd:conformsTo field of every item of the top-level
// pdfd:declarations array. WTPDF 1.0, 6.1.2 and 6.1.3 require the URI as the
// pdfd:conformsTo entry of a PDF Declaration, and the PDF Declarations
// specification makes pdfd:declarations a Bag of Declaration structures. The
// packet is read as XMP, not as text: the prefix is whatever the packet binds
// to the namespace, a field may be an element or an attribute, and only a
// document property counts (ISO 16684-1, 7.4: the properties are the children
// of a top-level rdf:Description).
//
// A pdfd:conformsTo outside a declaration states nothing, whether it stands
// alone as a property or sits inside another structure, such as an item of
// pdfaExtension:schemas. veraPDF reads the same shape: VeraPDFMeta
// getDeclarations walks the top-level declarations property, its items and
// their conformsTo fields. It matches the field by local name only; litho
// also requires the declarations namespace the specification names.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-2__5-t02-fail-a, 5-t02-pass-a, 5-t03-fail-a,
//     5-t04-fail-a and 5-t05-fail-a: remediation spliced the declaration
//     into the pdfaExtension:schemas item, a text scan credited it and
//     veraPDF did not, 5 remediation disagreement points at each WTPDF level.
func wtpdfDeclarations(xmpData []byte) []string {
	var out []string
	for _, prop := range XMPProperties(xmpData) {
		if prop.NS != wtpdfDeclarationNamespace || prop.Name != "declarations" {
			continue
		}
		for _, item := range prop.Children {
			for _, field := range item.Children {
				if len(out) >= maxWTPDFDeclarations {
					return out
				}
				if field.NS == wtpdfDeclarationNamespace && field.Name == "conformsTo" {
					out = append(out, strings.TrimSpace(field.Value))
				}
			}
		}
	}
	return out
}

// WTPDFDeclares reports whether the XMP packet states uri as the
// pdfd:conformsTo entry of one of its PDF Declarations, read the way the
// WTPDF declaration check reads them. Remediation uses it to tell a
// declaration that counts from one it still has to write.
func WTPDFDeclares(xmpData []byte, uri string) bool {
	return slices.Contains(wtpdfDeclarations(xmpData), uri)
}

// checkWTPDFDeclaration validates the one rule WTPDF states of its own:
// the file shall carry a PDF Declaration naming the conformance level
// (WTPDF 1.0, 6.1.2 for reuse and 6.1.3 for accessibility). A document with
// no XMP metadata draws nothing here.
//
// veraPDF scopes both rules to the main XMP package (MainXMPPackage in
// WTPDF-1-0-Reuse.xml and WTPDF-1-0-Accessibility.xml), which a catalog with
// no /Metadata stream does not have, so the rule has nothing to test. The
// missing metadata itself fails ISO 14289-2, 8.11.1 t2, which the PDF/UA-2
// checks WTPDF runs report, so the document still fails. The PDF/UA
// identification rule of clause 5 has the same scope and the same exemption
// ([checkPDFUAIdentification]).
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-6-2-1-t01-fail-a,
//     PDF_A-4__veraPDF_test_suite_6-7-2-1-t01-fail-a and
//     PDF_UA-1__7.1-t08-fail-a: their catalogs name no /Metadata, and veraPDF
//     reports 8.11.1 t2 and no declaration rule at either level.
func checkWTPDFDeclaration(ctx context.Context, doc *document.Document, profile models.WTPDFProfile) []Violation {
	rule, uri := wtpdfDeclarationRule(profile)
	if rule.Specification == "" {
		return nil
	}

	catalog, err := resolveCatalog(ctx, doc)
	if err != nil {
		doc.Logger.Debug("compliance: wtpdf: cannot resolve catalog", "error", err)
		return nil
	}

	xmpData, ok := readCatalogXMP(ctx, doc, catalog)
	if !ok {
		return nil
	}

	if WTPDFDeclares(xmpData, uri) {
		return nil
	}
	return failf(rule, "XMP states no pdfd:conformsTo declaration %q", uri)
}

// ValidateWTPDF audits the document against a Well-Tagged PDF 1.0
// conformance level, returning all compliance violations found as
// errors. Each error is a [Violation] value. An empty slice means the
// document passes. See [ValidatePDFA] for the concurrency contract.
//
// WTPDF adopts the ISO 14289-2 requirements wholesale, so the run uses
// the PDF/UA-2 checks and reports ISO_14289_2 clauses. The levels differ
// from PDF/UA-2 only by subtraction (the rules in [wtpdfDroppedBoth] and
// [wtpdfDroppedReuse], which WTPDF does not state) and by the PDF
// Declaration each one adds.
func ValidateWTPDF(ctx context.Context, doc *document.Document, profile models.WTPDFProfile) ([]error, error) {
	doc.Logger.Debug("compliance: starting WTPDF validation", "profile", profile)

	if !profile.IsValid() {
		doc.Logger.Error("compliance: unknown WTPDF profile", "profile", profile)
		return nil, fmt.Errorf("%w: WTPDF %s", ErrUnknownProfile, profile)
	}

	groups, ok := pdfuaCheckGroups[models.PDFUA2]
	if !ok {
		return nil, fmt.Errorf("%w: PDF/UA %s", ErrUnknownProfile, models.PDFUA2)
	}

	release := doc.LockValidationRun()
	defer release()

	// The declaration check below reads the file as the check groups do; see
	// runValidationViolations.
	ctx = document.OpenedObjectsOnly(ctx)

	// The checks run under the PDF/UA-2 identifier so every profile test
	// inside them selects the ISO 14289-2 rules. Keying them on the WTPDF
	// identifier instead would silence the whole rule set, since those
	// tests compare the profile string literally.
	violations, err := runValidationViolations(ctx, doc, string(models.PDFUA2), groups)
	if err != nil {
		return nil, err
	}

	violations = slices.DeleteFunc(violations, func(v Violation) bool {
		if !wtpdfDropsRule(profile, v.Rule) {
			return false
		}
		doc.Logger.Debug("compliance: wtpdf: rule not stated at this level",
			"clause", v.Rule.Clause, "test", v.Rule.TestNumber, "profile", profile)
		return true
	})

	violations = append(violations, checkWTPDFDeclaration(ctx, doc, profile)...)

	doc.Logger.Debug("compliance: WTPDF validation complete",
		"profile", profile, "violations", len(violations))
	return violationsToErrors(violations), nil
}
