// Package cii_schemas bundles the UN/CEFACT Cross-Industry Invoice
// XSD set litho ships by default. The validator wires the bundled
// schemas into [pkg/pdf.Reader.ValidateFacturX] so end users get full
// XSD validation without having to source the schemas themselves.
//
// See README.md for the bundle layout and the bump process.
//
// Concurrency: [LoadDefault] memoises one [*xsd.Schema] per process
// behind a [sync.Once] and is safe to call concurrently.
package cii_schemas

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/xsd"
)

// CIINamespace is the canonical UN/CEFACT CII namespace URI for
// Factur-X 1.0+ / ZUGFeRD 2.1+. Callers route documents whose root
// element lies in this namespace through the bundled D16B schema set.
const CIINamespace = "urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"

// D16BRoot names the bundled root XSD file. The bump process keeps
// this filename stable across schema revisions so the loader does
// not need a runtime catalog lookup.
const D16BRoot = "d16b/CrossIndustryInvoice_100pD16B.xsd"

//go:embed d16b/*.xsd
var bundledFS embed.FS

// FS exposes the bundled schemas as an [io/fs.FS] for callers that
// want to introspect the bundle or pass it to [pkg/pdf.WithCIISchemas]
// explicitly. The returned filesystem is read-only and safe to share.
func FS() fs.FS { return bundledFS }

var (
	loadOnce      sync.Once
	defaultSchema *xsd.Schema
	//nolint:errname // memoised load result, not a sentinel error.
	defaultFault error
)

// LoadDefault returns the bundled D16B schema set, loading it on
// first call and reusing the result on subsequent calls. The load
// happens lazily so a process that never validates a Factur-X
// document pays no XSD parsing cost.
//
// Returns nil and the load error when the bundle fails to parse;
// callers fall back to the structural CII spine rules in that case.
func LoadDefault(ctx context.Context) (*xsd.Schema, error) {
	loadOnce.Do(func() {
		s, err := xsd.Load(ctx, bundledFS, D16BRoot)
		if err != nil {
			defaultFault = fmt.Errorf("loading bundled CII D16B schemas: %w", err)
			return
		}
		defaultSchema = s
	})
	return defaultSchema, defaultFault
}
