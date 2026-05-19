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

// EN16931Root names the bundled root Schematron file. The bump
// process keeps this filename stable across rule-set revisions so
// the loader does not need a runtime catalog lookup.
const EN16931Root = "en16931/EN16931-CII-validation.sch"

//go:embed en16931/*.sch
var bundledFS embed.FS

// FS exposes the bundled rule set as an [io/fs.FS] for callers that
// want to introspect the bundle or pass it through to
// [pkg/pdf.WithSchematron] explicitly. The returned filesystem is
// read-only and safe to share.
func FS() fs.FS { return bundledFS }

var (
	loadOnce      sync.Once
	defaultSchema *schematron.Schema
	//nolint:errname // memoised load result, not a sentinel error.
	defaultFault error
)

// LoadDefault returns the bundled EN 16931 CII Schematron, loading
// it on first call and reusing the result on subsequent calls. The
// load happens lazily so a process that never runs a Schematron
// pass pays no parse cost.
//
// Returns nil and the load error when the bundle fails to parse;
// callers fall back to the structural CII spine and XSD rules in
// that case.
func LoadDefault(ctx context.Context) (*schematron.Schema, error) {
	loadOnce.Do(func() {
		s, err := schematron.Load(ctx, bundledFS, EN16931Root)
		if err != nil {
			defaultFault = fmt.Errorf("loading bundled EN 16931 Schematron: %w", err)
			return
		}
		defaultSchema = s
	})
	return defaultSchema, defaultFault
}
