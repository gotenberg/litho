package remediation

// Report describes what the remediation engine fixed and what it could not fix.
type Report struct {
	// Fixed lists human-readable descriptions of applied fixes.
	Fixed []string
	// Residual contains violations that could not be fixed automatically.
	// Each element is a compliance.Violation.
	Residual []error
}
