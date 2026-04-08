package remediation

// Options configures remediation behavior.
type Options struct {
	// Lang is the BCP 47 language tag for the catalog /Lang entry.
	// Defaults to "en" if empty.
	Lang string
}
