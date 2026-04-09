package pdf

// RemediateOption configures the behavior of [Writer.RemediatePDFA] and
// [Writer.RemediatePDFUA].
type RemediateOption func(*remediateOpts)

type remediateOpts struct {
	lang            string
	preserveContent bool
}

// WithLanguage sets the BCP 47 language tag for the catalog /Lang entry.
// Defaults to "en" if not specified.
func WithLanguage(lang string) RemediateOption {
	return func(o *remediateOpts) {
		o.lang = lang
	}
}

// WithAggressiveFixes enables fixers that may alter visible page content in
// exchange for better compliance. This includes stripping unknown content
// stream operators, clamping out-of-range integers, generating empty
// annotation appearances, and re-encoding LZW streams as FlateDecode.
func WithAggressiveFixes() RemediateOption {
	return func(o *remediateOpts) {
		o.preserveContent = false
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
