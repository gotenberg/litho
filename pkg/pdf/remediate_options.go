package pdf

// RemediateOption configures the behavior of [Writer.RemediatePDFA] and
// [Writer.RemediatePDFUA].
type RemediateOption func(*remediateOpts)

type remediateOpts struct {
	lang string
}

// WithLanguage sets the BCP 47 language tag for the catalog /Lang entry.
// Defaults to "en" if not specified.
func WithLanguage(lang string) RemediateOption {
	return func(o *remediateOpts) {
		o.lang = lang
	}
}

// RemediationReport describes what the remediation engine fixed and what it
// could not fix automatically.
type RemediationReport struct {
	// Fixed lists human-readable descriptions of applied fixes.
	Fixed []string
	// Residual contains violations that could not be fixed automatically.
	// Each element is a compliance.Violation.
	Residual []error
}
