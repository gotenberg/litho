package remediation

// Report describes what the remediation engine fixed and what it could not fix.
type Report struct {
	// Fixed lists human-readable descriptions of applied fixes.
	Fixed []string
	// Residual contains violations that could not be fixed automatically.
	// Each element is a compliance.Violation.
	Residual []error
	// NeedsFullRewrite is set when file structure violations require a full
	// rewrite to fix (clean object serialization, xref tables, headers).
	NeedsFullRewrite bool
	// RequiredVersion overrides the PDF version in the output header when
	// the target profile requires a specific version (e.g., "2.0" for PDF/A-4).
	RequiredVersion string
}
