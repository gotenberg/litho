// Package remediation applies targeted fixes to make PDF documents comply with
// PDF/A (ISO 19005) and PDF/UA (ISO 14289) profiles. Fixers are independent
// units that each handle a specific category of violations. The orchestrator
// validates, classifies violations, runs applicable fixers, and reports what
// was fixed and what remains.
package remediation
