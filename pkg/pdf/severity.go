package pdf

import (
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// Severity grades a compliance [Violation]. The token set (Fatal,
// Warning, Info) follows the EN 16931 / Schematron Quality Framework
// convention; it is standards-informed, not a closed vocabulary mandated
// by ISO/IEC 19757-3, whose `@flag` attribute is open-valued.
//
// Only the Factur-X / CII Schematron path produces non-fatal severities
// today, and it distinguishes only Fatal from Warning (Info folds into
// Warning). PDF/A and PDF/UA are binary conformance: every violation is
// Fatal.
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
// coordinates (Specification, Clause, TestNumber) the flat `[]error`
// validation results stringify away, plus a [Severity]. It implements
// error with the same text as the flat form, so it can be used
// interchangeably.
// Values must not be shared across goroutines without synchronization.
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

// toViolation translates an internal compliance violation (carried as an
// error) into a public [Violation]. An error that is not a compliance
// violation, such as a Schematron advisory, maps to a fatal Violation
// carrying its message so no information is lost.
func toViolation(e error) Violation {
	if cv, ok := errors.AsType[compliance.Violation](e); ok {
		sev := SeverityFatal
		if compliance.SeverityOf(cv) == compliance.SeverityWarning {
			sev = SeverityWarning
		}
		return Violation{
			Specification: cv.Rule.Specification,
			Clause:        cv.Rule.Clause,
			TestNumber:    cv.Rule.TestNumber,
			Description:   cv.Rule.Description,
			Context:       cv.Context,
			Severity:      sev,
		}
	}
	return Violation{Description: e.Error(), Severity: SeverityFatal}
}

// toViolations translates a flat slice of validation errors into
// structured [Violation] values.
func toViolations(errs []error) []Violation {
	out := make([]Violation, 0, len(errs))
	for _, e := range errs {
		out = append(out, toViolation(e))
	}
	return out
}

// filterBySeverity returns the structured violations matching sev.
func filterBySeverity(errs []error, sev Severity) []Violation {
	var out []Violation
	for _, e := range errs {
		if v := toViolation(e); v.Severity == sev {
			out = append(out, v)
		}
	}
	return out
}
