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
	"bytes"
	"context"
	"embed"
	"encoding/xml"
	"fmt"
	"io/fs"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/schematron"
)

// Bundled Schematron roots. Each constant names the root file inside
// [FS] for one rule set so callers can pick between EN 16931
// (cross-EU baseline), XRechnung (German CIUS), and the
// Factur-X-profile-specific rule sets (MINIMUM / BASIC WL / BASIC /
// EN 16931 / EXTENDED) that mirror what Mustang's reference
// validator applies.
const (
	EN16931Root   = "en16931/EN16931-CII-validation.sch"
	XRechnungRoot = "xrechnung/XRechnung-CII-validation.sch"

	FacturXMinimumRoot  = "facturx_profiles/FACTUR-X_MINIMUM.sch"
	FacturXBasicWLRoot  = "facturx_profiles/FACTUR-X_BASIC-WL.sch"
	FacturXBasicRoot    = "facturx_profiles/FACTUR-X_BASIC.sch"
	FacturXEN16931Root  = "facturx_profiles/FACTUR-X_EN16931.sch"
	FacturXExtendedRoot = "facturx_profiles/FACTUR-X_EXTENDED.sch"
)

//go:embed en16931/*.sch xrechnung/*.sch facturx_profiles/*.sch
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

//nolint:gochecknoglobals // memoised load results, not sentinel errors.
var (
	facturxProfileOnce sync.Map // string conformance level -> *sync.Once
	facturxProfileMu   sync.Mutex
	facturxProfileMem  = map[string]*schematron.Schema{}
	facturxProfileErr  = map[string]error{}
)

// LoadFacturXProfile returns the bundled Factur-X profile Schematron
// matching the supplied conformance level. The five profiles ship
// alongside the EN 16931 baseline and XRechnung CIUS bundles; they
// mirror the rule set Mustang's reference validator applies, which
// is CEN EN 16931 + PEPPOL-EN16931 + Factur-X-specific BR-FX-EN-*
// rules rolled into one. Callers pick this in place of [LoadDefault]
// when the document's fx:ConformanceLevel selects one of MINIMUM,
// BASIC WL, BASIC, EN 16931, or EXTENDED.
//
// level is matched case-insensitively against the canonical Factur-X
// 1.0.07 spelling. Returns (nil, nil) when level is empty or
// unrecognised so callers can fall back to the generic EN 16931
// schematron without branching on the absence.
//
// Concurrency: results memoise per level behind a [sync.Mutex];
// concurrent callers see one parse cost per level per process.
func LoadFacturXProfile(ctx context.Context, level string) (*schematron.Schema, error) {
	root, ok := facturxProfileRoot(level)
	if !ok {
		return nil, nil
	}
	key := root
	onceObj, _ := facturxProfileOnce.LoadOrStore(key, &sync.Once{})
	once := onceObj.(*sync.Once)
	once.Do(func() {
		s, err := schematron.Load(ctx, bundledFS, root)
		facturxProfileMu.Lock()
		defer facturxProfileMu.Unlock()
		if err != nil {
			facturxProfileErr[key] = fmt.Errorf("loading bundled Factur-X %s Schematron: %w", level, err)
			return
		}
		facturxProfileMem[key] = s
	})
	facturxProfileMu.Lock()
	defer facturxProfileMu.Unlock()
	return facturxProfileMem[key], facturxProfileErr[key]
}

// facturxProfileRoot maps a conformance-level string to the
// corresponding bundle root path. Returns ("", false) for unknown
// or unsupported levels (XRECHNUNG routes through LoadXRechnung;
// every other free-form level falls back to LoadDefault).
// baselineFlagsOnce + baselineFlags carry the lazy-loaded
// {EN 16931 rule id -> flag attribute} map used by
// [BaselineFlagForRule]. The bundled FACTUR-X profile schematrons
// rebadge CEN EN 16931 rules under fresh FX-SCH-A-* ids but lose the
// flag attribute the baseline carries, so rules that should fire
// non-fatal (e.g. CII-SR-465's "[CII-SR-465]-Only one BT-41
// element is allowed" warning) silently flip the validity bit in
// per-profile mode. The lookup mirrors the baseline flag so the
// severity mapper in pkg/internal/compliance honours it.
var (
	baselineFlagsOnce sync.Once
	baselineFlags     map[string]string
)

// BaselineFlagForRule returns the EN 16931 baseline schematron's
// flag attribute for the supplied rule id (the bracketed prefix that
// appears in [CII-SR-465] / [BR-CO-10] / etc. message text). The
// lookup is case-sensitive and the empty string maps to "no flag /
// implicit fatal" - distinct from the absent-key signal returned
// via ok=false.
//
// The compliance severity mapper consults this so per-profile rules
// inherit the baseline's flag classification without forcing a
// regeneration of the bundled Factur-X profile schematrons. Returns
// "", false when id is unknown to the baseline (BR-FX-EN-*,
// PEPPOL-EN16931-*, etc. that exist only in extensions).
func BaselineFlagForRule(id string) (string, bool) {
	baselineFlagsOnce.Do(loadBaselineFlags)
	flag, ok := baselineFlags[id]
	return flag, ok
}

func loadBaselineFlags() {
	data, err := bundledFS.ReadFile(EN16931Root)
	if err != nil {
		return
	}
	// Streaming XML decoder keeps the parse cost low and avoids a
	// second full schematron-tree allocation. We only care about
	// <assert>/<report> id and flag attributes regardless of nesting.
	dec := xml.NewDecoder(bytes.NewReader(data))
	out := make(map[string]string, 1024)
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "assert" && start.Name.Local != "report" {
			continue
		}
		var id, flag string
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "id":
				id = a.Value
			case "flag":
				flag = a.Value
			}
		}
		if id != "" {
			out[id] = flag
		}
	}
	baselineFlags = out
}

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
