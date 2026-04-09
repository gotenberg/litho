package remediation

// Options configures remediation behavior.
type Options struct {
	// Lang is the BCP 47 language tag for the catalog /Lang entry.
	// Defaults to "en" if empty.
	Lang string
	// PreserveContent controls whether fixers may alter visible page content.
	// When true (default), only structural/metadata fixes are applied.
	// When false, aggressive fixers are enabled that may change rendering
	// (e.g., stripping unknown operators, clamping integers, generating
	// empty annotation appearances, re-encoding LZW streams).
	PreserveContent bool
}
