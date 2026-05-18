// Package schematron provides a minimal ISO Schematron loader and
// evaluator scoped to the subset of constructs the EN 16931 and
// XRechnung rule sets use. It is not a general-purpose Schematron
// implementation: abstract patterns, phases-with-roles, diagnostics,
// and let-substitution beyond a single namespace's worth of bindings
// are intentionally out of scope.
//
// PR1 of the engine ships only the loader and the in-memory schema
// model. Rule evaluation, XPath integration (via
// github.com/antchfx/xpath), and Reader.ValidateFacturX wiring
// arrive in PR2 and PR3.
//
// Concurrency: a [Schema] is immutable once [Load] returns.
// Concurrent readers are safe. Mutating a Schema after Load is
// undefined.
package schematron
