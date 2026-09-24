// Package cii_schematron bundles the EN 16931 CII Schematron rule
// set litho ships by default. Reader.ValidateFacturX wires the
// bundled schema into the Schematron pass when no caller-supplied
// override is in place.
//
// See README.md for the bundle layout, the upstream source, and the
// bump process.
//
// Concurrency: [LoadDefault] memoises one [*schematron.Schema] per
// process behind a [sync.Once] and is safe to call concurrently.
package cii_schematron
