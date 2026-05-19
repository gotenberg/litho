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

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/schematron"
)

// Bundled Schematron roots. Each constant names the root file inside
// [FS] for one rule set so callers can pick between EN 16931
// (cross-EU baseline) and XRechnung (German CIUS).
const (
	EN16931Root   = "en16931/EN16931-CII-validation.sch"
	XRechnungRoot = "xrechnung/XRechnung-CII-validation.sch"
)

//go:embed en16931/*.sch xrechnung/*.sch
var bundledFS embed.FS

// FS exposes the bundled rule set as an [io/fs.FS] for callers that
// want to introspect the bundle or pass it through to
// [pkg/pdf.WithSchematron] explicitly. The returned filesystem is
// read-only and safe to share.
func FS() fs.FS { return bundledFS }

// memoised one schema per process per bundle. Lazy load keeps the
// parse cost off the first ValidateFacturX call when no Schematron
// pass actually runs.
var (
	en16931Once   sync.Once
	en16931Schema *schematron.Schema
	//nolint:errname // memoised load result, not a sentinel error.
	en16931Fault error

	xrechnungOnce   sync.Once
	xrechnungSchema *schematron.Schema
	//nolint:errname // memoised load result, not a sentinel error.
	xrechnungFault error
)

// LoadDefault returns the bundled EN 16931 CII Schematron, loading
// it on first call and reusing the result on subsequent calls.
// Returns nil and the load error when the bundle fails to parse;
// callers fall back to the structural CII spine and XSD rules in
// that case.
func LoadDefault(ctx context.Context) (*schematron.Schema, error) {
	en16931Once.Do(func() {
		s, err := schematron.Load(ctx, bundledFS, EN16931Root)
		if err != nil {
			en16931Fault = fmt.Errorf("loading bundled EN 16931 Schematron: %w", err)
			return
		}
		en16931Schema = s
	})
	return en16931Schema, en16931Fault
}

// LoadXRechnung returns the bundled XRechnung CII Schematron - the
// German CIUS of EN 16931 published by KoSIT. Same lazy-load
// semantics as [LoadDefault]; same fallback behaviour on parse
// failure.
//
// Callers route documents to this rule set when the XMP
// fx:ConformanceLevel reads XRECHNUNG. Routing logic lives in
// the compliance package; this function is the pure loader.
func LoadXRechnung(ctx context.Context) (*schematron.Schema, error) {
	xrechnungOnce.Do(func() {
		s, err := schematron.Load(ctx, bundledFS, XRechnungRoot)
		if err != nil {
			xrechnungFault = fmt.Errorf("loading bundled XRechnung Schematron: %w", err)
			return
		}
		xrechnungSchema = s
	})
	return xrechnungSchema, xrechnungFault
}
