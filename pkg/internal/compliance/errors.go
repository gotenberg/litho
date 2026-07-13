package compliance

import (
	"errors"
	"fmt"
)

// ErrUnknownProfile is returned by [ValidatePDFA] and [ValidatePDFUA]
// when the supplied profile identifier does not match any known
// PDF/A or PDF/UA conformance profile. Callers can branch on this
// sentinel via [errors.Is] without inspecting the wrapped message.
var ErrUnknownProfile = errors.New("compliance: unknown profile")

// errUndecodableContent is returned by resolvePageContentBytes for a page whose
// content stream carries a /Filter litho cannot decode. The bytes stay
// filter-encoded, so content-operator analysis is skipped rather than run on
// them; the undecodable filter itself is reported by the §6.1.6.2 filter check.
var errUndecodableContent = errors.New("compliance: content stream has an undecodable filter")

// ErrSchematronRuleSkipped marks an advisory entry in the violations
// slice returned by [ValidateFacturX] and [ValidateZUGFeRD]. It fires
// when a Schematron rule's XPath could not be compiled or evaluated;
// the rule's verdict on the document is unknown. The public
// [pdf.ErrSchematronRuleSkipped] sentinel re-exports the same value so
// callers can match with [errors.Is] without depending on the internal
// package.
var ErrSchematronRuleSkipped = errors.New("schematron rule skipped (xpath construct unsupported)")

// schematronSkippedError wraps a single skipped Schematron rule so it
// can ride along with violations in the same []error return.
// Implements the [errors.Is] target so [ErrSchematronRuleSkipped]
// matches without depending on the wrapped message.
type schematronSkippedError struct {
	RuleID  string
	Context string
	Reason  string
}

func (a *schematronSkippedError) Error() string {
	if a.Context != "" {
		return fmt.Sprintf("schematron rule %s skipped at %s: %s", a.RuleID, a.Context, a.Reason)
	}
	return fmt.Sprintf("schematron rule %s skipped: %s", a.RuleID, a.Reason)
}

func (a *schematronSkippedError) Is(target error) bool {
	return target == ErrSchematronRuleSkipped
}
