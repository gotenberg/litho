// Package cii_schemas bundles the UN/CEFACT Cross-Industry Invoice
// XSD set litho ships by default. The validator wires the bundled
// schemas into pdf.Reader.ValidateFacturX so end users get full
// XSD validation without having to source the schemas themselves.
//
// See README.md for the bundle layout and the bump process.
//
// Concurrency: [LoadDefault] memoises one [*xsd.Schema] per process
// behind a [sync.Once] and is safe to call concurrently.
package cii_schemas
