package models

import (
	"errors"
	"fmt"
)

// FacturXReport partitions the output of a
// [github.com/gotenberg/litho/pkg/pdf.Reader.ValidateFacturXReport] call into
// hard violations and Schematron advisories. Violations are the PDF/A-3 plus
// hybrid-invoice rule failures that callers must act on; Advisories list
// Schematron rules whose XPath the bundled engine could not evaluate and whose
// verdict is therefore unknown.
//
// Values are immutable once returned; callers may share a FacturXReport
// across goroutines without synchronisation.
type FacturXReport struct {
	// Violations are hard rule failures: PDF/A-3 base, hybrid-invoice
	// AF/XMP/CII checks, and Schematron assertions that fired.
	Violations []error
	// Advisories list Schematron rules skipped because the bundled XPath
	// engine does not support the construct the rule uses. Each entry wraps
	// [github.com/gotenberg/litho/pkg/pdf.ErrSchematronRuleSkipped] and names
	// the rule id plus the unsupported construct in its error message.
	Advisories []error
}

// Structured returns the hard violations as structured [Violation]
// values carrying clause, test number, and severity. Unlike PDF/A and
// PDF/UA, a Factur-X result mixes [SeverityFatal] (PDF/A-3 base and
// fired Schematron asserts) with [SeverityWarning] (soft Schematron
// rules and report notices).
func (rep FacturXReport) Structured() []Violation {
	return toViolations(rep.Violations)
}

// Fatals returns the structured violations whose severity is
// [SeverityFatal]: the failures that make the document non-conformant.
func (rep FacturXReport) Fatals() []Violation {
	return filterBySeverity(rep.Violations, SeverityFatal)
}

// Warnings returns the structured violations whose severity is
// [SeverityWarning]: soft Schematron findings that do not by themselves
// fail conformance.
func (rep FacturXReport) Warnings() []Violation {
	return filterBySeverity(rep.Violations, SeverityWarning)
}

// Severity grades a compliance [Violation]. The token set (Fatal,
// Warning, Info) follows the EN 16931 / Schematron Quality Framework
// convention; it is standards-informed, not a closed vocabulary mandated
// by ISO/IEC 19757-3, whose `@flag` attribute is open-valued.
//
// Only the Factur-X / CII Schematron path produces non-fatal severities
// today, and it distinguishes only Fatal from Warning (Info folds into
// Warning). PDF/A, PDF/UA and WTPDF are binary conformance: every
// violation is Fatal.
// Severity values are immutable and safe for concurrent use.
type Severity int

// Severity levels, ordered most to least serious.
const (
	// SeverityFatal marks a violation that fails conformance.
	SeverityFatal Severity = iota
	// SeverityWarning marks a soft violation: a Schematron assertion the
	// schema author flagged warning/info, or an advisory report firing.
	SeverityWarning
	// SeverityInfo is reserved for purely informational findings. The CII
	// path currently folds info into [SeverityWarning]; no validator emits
	// SeverityInfo yet.
	SeverityInfo
)

// String returns the lowercase severity token ("fatal", "warning", "info").
func (s Severity) String() string {
	switch s {
	case SeverityFatal:
		return "fatal"
	case SeverityWarning:
		return "warning"
	case SeverityInfo:
		return "info"
	default:
		return "unknown"
	}
}

// Violation is a structured compliance violation. It carries the spec
// coordinates (Specification, Clause, TestNumber) and a [Severity]. It
// implements error with the same text as the flat form, so it can be used
// interchangeably.
// Concurrent reads of a value are safe; a write needs synchronization.
//
//nolint:errname // Violation is the public domain name; "ViolationError" would be redundant and it mirrors compliance.Violation.
type Violation struct {
	// Specification is the ISO standard identifier (e.g. "ISO_19005_1").
	Specification string
	// Clause is the standard clause number (e.g. "6.1.2").
	Clause string
	// TestNumber identifies the specific test within a clause.
	TestNumber int
	// Description is the normative requirement text.
	Description string
	// Context describes where the violation occurred (page, object, font).
	Context string
	// Severity grades the violation.
	Severity Severity
}

// Error implements the error interface with the same text as the flat
// validation results, so structured and flat forms compare equal.
func (v Violation) Error() string {
	if v.Context != "" {
		return fmt.Sprintf("clause %s test %d: %s [%s]", v.Clause, v.TestNumber, v.Description, v.Context)
	}
	return fmt.Sprintf("clause %s test %d: %s", v.Clause, v.TestNumber, v.Description)
}

// structuredViolation is an error that carries a [Violation]: a violation
// of the conformance engine, which [FacturXReport] and [RemediationReport]
// hold as errors.
type structuredViolation interface {
	error
	Structured() Violation
}

// violationOf returns the [Violation] e carries. An error that carries
// none, such as a Schematron advisory, maps to a fatal Violation holding
// its message, so that no information is lost.
func violationOf(e error) Violation {
	if v, ok := errors.AsType[structuredViolation](e); ok {
		return v.Structured()
	}
	return Violation{Description: e.Error(), Severity: SeverityFatal}
}

// toViolations returns the [Violation] each of errs carries, in order.
func toViolations(errs []error) []Violation {
	out := make([]Violation, 0, len(errs))
	for _, e := range errs {
		out = append(out, violationOf(e))
	}
	return out
}

// filterBySeverity returns the [Violation] values errs carry whose
// severity is sev, in order.
func filterBySeverity(errs []error, sev Severity) []Violation {
	var out []Violation
	for _, e := range errs {
		if v := violationOf(e); v.Severity == sev {
			out = append(out, v)
		}
	}
	return out
}
