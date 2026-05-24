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

// Factur-X profile XSD roots. Each constant names the per-profile
// root file inside [FS] for the matching XMP conformance level.
// Profile XSDs ship the same CII v100 namespace but tighten the
// permitted element set: MINIMUM strips most of the BasicInformation
// branch, BASIC-WL keeps the document header but trims line items
// out, BASIC adds back the line-level details, EN 16931 lifts the
// cross-EU baseline, and EXTENDED keeps Factur-X-specific
// extensions. The bundled XSD files come from the Factur-X 1.0.07
// publication.
const (
	FacturXMinimumRoot  = "facturx_profiles/MINIMUM/FACTUR-X_MINIMUM.xsd"
	FacturXBasicWLRoot  = "facturx_profiles/BASIC-WL/FACTUR-X_BASIC-WL.xsd"
	FacturXBasicRoot    = "facturx_profiles/BASIC/FACTUR-X_BASIC.xsd"
	FacturXEN16931Root  = "facturx_profiles/EN16931/FACTUR-X_EN16931.xsd"
	FacturXExtendedRoot = "facturx_profiles/EXTENDED/FACTUR-X_EXTENDED.xsd"
)

// ZUGFeRD1p0Root names the bundled root for the legacy ZUGFeRD 1.0
// XSD set. ZUGFeRD 1.0 uses the urn:ferd:CrossIndustryDocument:invoice:1p0
// namespace and the v12/v15 ram/qdt/udt child types, none of which
// the v100 CII schemas cover.
const ZUGFeRD1p0Root = "zugferd1p0/ZUGFeRD1p0.xsd"

//go:embed d16b/*.xsd facturx_profiles/*/*.xsd zugferd1p0/*.xsd
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

	zugferd1p0Once   sync.Once
	zugferd1p0Schema *xsd.Schema
	//nolint:errname // memoised load result, not a sentinel error.
	zugferd1p0Fault error
)

//nolint:gochecknoglobals // memoised load results, not sentinel errors.
var (
	facturxProfileOnce sync.Map // root path -> *sync.Once
	facturxProfileMu   sync.Mutex
	facturxProfileMem  = map[string]*xsd.Schema{}
	facturxProfileErr  = map[string]error{}
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

// LoadFacturXProfile returns the bundled Factur-X profile XSD set
// matching the supplied conformance level. Levels follow Factur-X
// 1.0.07's canonical spelling ("MINIMUM" / "BASIC WL" / "BASIC" /
// "EN 16931" / "EXTENDED"). Returns (nil, nil) when level is empty
// or unrecognised so callers can branch on the absence without
// inspecting the error.
//
// Each profile loads lazily once per process via sync.Once keyed on
// the bundle path; the load cost is paid only when the validator
// routes a document of that profile through the XSD pass.
func LoadFacturXProfile(ctx context.Context, level string) (*xsd.Schema, error) {
	root, ok := facturxProfileRoot(level)
	if !ok {
		return nil, nil
	}
	onceObj, _ := facturxProfileOnce.LoadOrStore(root, &sync.Once{})
	once := onceObj.(*sync.Once)
	once.Do(func() {
		s, err := xsd.Load(ctx, bundledFS, root)
		facturxProfileMu.Lock()
		defer facturxProfileMu.Unlock()
		if err != nil {
			facturxProfileErr[root] = fmt.Errorf("loading bundled Factur-X %s XSD: %w", level, err)
			return
		}
		facturxProfileMem[root] = s
	})
	facturxProfileMu.Lock()
	defer facturxProfileMu.Unlock()
	return facturxProfileMem[root], facturxProfileErr[root]
}

// LoadZUGFeRD1p0 returns the bundled legacy ZUGFeRD 1.0 XSD set,
// loaded lazily once per process. Returns nil and the load error
// when the bundle fails to parse; the v1 validator then falls back
// to the structural CII spine rules without an XSD pass.
//
// Callers route documents to this schema when the embedded invoice
// declares the urn:ferd:CrossIndustryDocument:invoice:1p0 namespace;
// the v100 CII schemas pre-date that namespace and do not match.
func LoadZUGFeRD1p0(ctx context.Context) (*xsd.Schema, error) {
	zugferd1p0Once.Do(func() {
		s, err := xsd.Load(ctx, bundledFS, ZUGFeRD1p0Root)
		if err != nil {
			zugferd1p0Fault = fmt.Errorf("loading bundled ZUGFeRD 1.0 XSD: %w", err)
			return
		}
		zugferd1p0Schema = s
	})
	return zugferd1p0Schema, zugferd1p0Fault
}

// facturxProfileRoot maps a conformance-level string to the matching
// bundle root path. Returns ("", false) for unknown / unsupported
// levels so the loader can short-circuit without producing an error.
func facturxProfileRoot(level string) (string, bool) {
	switch level {
	case "MINIMUM":
		return FacturXMinimumRoot, true
	case "BASIC WL":
		return FacturXBasicWLRoot, true
	case "BASIC":
		return FacturXBasicRoot, true
	case "EN 16931":
		return FacturXEN16931Root, true
	case "EXTENDED":
		return FacturXExtendedRoot, true
	}
	return "", false
}
