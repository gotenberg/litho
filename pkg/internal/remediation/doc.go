// Package remediation applies targeted fixes to make PDF documents comply
// with PDF/A (ISO 19005) and PDF/UA (ISO 14289) profiles. Fixers are
// independent units that each handle a specific category of violations.
// The orchestrator validates, classifies violations, runs applicable
// fixers, and reports what was fixed and what remains.
//
// See README.md in this directory for architecture, the fixer contract,
// registry-ordering rules, and a guide to adding a new fixer.
package remediation
