// Command remediate applies litho's PDF/A remediation engine to every
// failing file in the VeraPDF corpus and reports aggregate before / after
// violation counts, regressions, and residual breakdowns.
//
// Usage:
//
//	go run ./test/integration/cmd/remediate [flags]
//
// Flags (kept consistent with ./test/integration/cmd/corpus where relevant):
//
//	--corpus <path>        Path to corpus testdata (auto-detected if empty)
//	--format text|json     Output format (default: text)
//	--profile PDF_A-2b     Filter to a single profile directory
//	--aggressive           Enable content-modifying fixers (Tier 2)
//	--detail               After the summary, print a residual-violation
//	                       clause breakdown. Text format only.
//	--max-regressions N    Exit non-zero if more than N files regress
//	                       (-1 = no limit). Default -1.
//	--min-fixed-pct P      Exit non-zero if the aggregate fix percentage
//	                       drops below P (0-100). Default 0.
//
// The corpus must be provisioned beforehand via:
//
//	go run ./test/integration/cmd/corpus verapdf --init
package main
