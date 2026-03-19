// Package compliance implements PDF/A and PDF/UA ISO compliance validation.
// It inspects a parsed document's catalog, page tree, fonts, annotations, and
// structure tree to detect violations of the various conformance levels.
//
// This package is internal; the public API is exposed via pkg/pdf.
package compliance
