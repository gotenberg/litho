package cii_schematron

import (
	"bytes"
	"context"
	"embed"
	"encoding/xml"
	"fmt"
	"io/fs"
	"maps"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/schematron"
)

// Bundled Schematron roots. Each constant names the root file inside
// [FS] for one rule set so callers can pick between EN 16931
// (cross-EU baseline), XRechnung (German CIUS), and the
// Factur-X-profile-specific rule sets (MINIMUM / BASIC WL / BASIC /
// EN 16931 / EXTENDED) that the Factur-X 1.0.07 publication
// references.
const (
	EN16931Root   = "en16931/EN16931-CII-validation.sch"
	XRechnungRoot = "xrechnung/XRechnung-CII-validation.sch"

	FacturXMinimumRoot  = "facturx_profiles/FACTUR-X_MINIMUM.sch"
	FacturXBasicWLRoot  = "facturx_profiles/FACTUR-X_BASIC-WL.sch"
	FacturXBasicRoot    = "facturx_profiles/FACTUR-X_BASIC.sch"
	FacturXEN16931Root  = "facturx_profiles/FACTUR-X_EN16931.sch"
	FacturXExtendedRoot = "facturx_profiles/FACTUR-X_EXTENDED.sch"

	// ZUGFeRD1p0Root names the bundled root for the legacy ZUGFeRD
	// 1.0 schematron. ZUGFeRD 1.0 uses the
	// urn:ferd:CrossIndustryDocument:invoice:1p0 root namespace and
	// the ram/qdt v12/v15 child types, none of which the v100 CII
	// rule sets cover. The bundle source is the ZUGFeRD project's
	// ZUGFeRD_1p0.sch published under the FeRD-AWV
	// non-discriminatory licence.
	ZUGFeRD1p0Root = "zugferd1p0/ZUGFeRD_1p0.sch"

	// PeppolRoot names the bundled root of the PEPPOL BIS 3.0
	// EN 16931 CII schematron sourced from OpenPEPPOL's
	// peppol-bis-invoice-3 repository. The rule set fires on
	// documents whose CII profile is plain
	// "urn:cen.eu:en16931:2017" with no CIUS suffix.
	PeppolRoot = "peppol/PEPPOL-EN16931-CII.sch"
)

//go:embed en16931/*.sch xrechnung/*.sch facturx_profiles/*.sch zugferd1p0/*.sch peppol/*.sch
var bundledFS embed.FS

// FS exposes the bundled rule set as an [io/fs.FS] for callers that
// want to introspect the bundle or pass it through to
// pdf.WithSchematron explicitly. The returned filesystem is
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

	zugferd1p0Once   sync.Once
	zugferd1p0Schema *schematron.Schema
	//nolint:errname // memoised load result, not a sentinel error.
	zugferd1p0Fault error

	peppolOnce   sync.Once
	peppolSchema *schematron.Schema
	//nolint:errname // memoised load result, not a sentinel error.
	peppolFault error
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

// LoadZUGFeRD1p0 returns the bundled legacy ZUGFeRD 1.0 CII
// Schematron, loaded lazily and memoised across the process.
// Returns nil and the load error when the bundle fails to parse;
// the legacy variant then runs without a Schematron pass and falls
// back to the structural CII spine and XSD layers.
//
// Callers route documents to this rule set when the embedded
// invoice declares the legacy ZUGFeRD 1.0 namespace
// (urn:ferd:CrossIndustryDocument:invoice:1p0); the EN 16931 family
// schematrons assume the v100 CII namespace and do not match v1.
func LoadZUGFeRD1p0(ctx context.Context) (*schematron.Schema, error) {
	zugferd1p0Once.Do(func() {
		s, err := schematron.Load(ctx, bundledFS, ZUGFeRD1p0Root)
		if err != nil {
			zugferd1p0Fault = fmt.Errorf("loading bundled ZUGFeRD 1.0 Schematron: %w", err)
			return
		}
		zugferd1p0Schema = s
	})
	return zugferd1p0Schema, zugferd1p0Fault
}

// LoadPeppol returns the bundled PEPPOL BIS 3.0 EN 16931 CII
// Schematron. Same lazy-load semantics as [LoadDefault].
//
// Callers route documents to this rule set when the CII
// GuidelineSpecifiedDocumentContextParameter/ID names the plain
// EN 16931 profile (urn:cen.eu:en16931:2017) with no CIUS suffix;
// Factur-X and XRechnung documents have their own per-profile
// rules and skip this pass.
func LoadPeppol(ctx context.Context) (*schematron.Schema, error) {
	peppolOnce.Do(func() {
		s, err := schematron.Load(ctx, bundledFS, PeppolRoot)
		if err != nil {
			peppolFault = fmt.Errorf("loading bundled PEPPOL EN 16931 Schematron: %w", err)
			return
		}
		peppolSchema = s
	})
	return peppolSchema, peppolFault
}

// LoadXRechnung returns the bundled XRechnung CII Schematron, the
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
	facturxProfileOnce sync.Map // bundle root path -> *sync.Once
	facturxProfileMu   sync.Mutex
	facturxProfileMem  = map[string]*schematron.Schema{}
	facturxProfileErr  = map[string]error{}
)

// LoadFacturXProfile returns the bundled Factur-X profile Schematron
// matching the supplied conformance level. The five profiles ship
// alongside the EN 16931 baseline and XRechnung CIUS bundles; each
// rolls CEN EN 16931 + PEPPOL-EN16931 + Factur-X-specific
// BR-FX-EN-* rules into one rule set per profile. Callers pick this
// in place of [LoadDefault] when the document's fx:ConformanceLevel
// selects one of MINIMUM, BASIC WL, BASIC, EN 16931, or EXTENDED.
//
// level must equal one of the canonical Factur-X 1.0.07 spellings
// exactly, case and spacing included: "MINIMUM", "BASIC WL", "BASIC",
// "EN 16931", or "EXTENDED". Returns (nil, nil) when level is empty or
// any other string, including a differently cased canonical name, so
// callers can fall back to the generic EN 16931 schematron without
// branching on the absence.
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
		// Augment only the EN 16931 profile with EN 16931 baseline
		// rules whose canonical id (the bracketed prefix in profile
		// assertion messages) isn't already carried by the profile.
		// The upstream per-profile sources rebadge most baseline
		// rules under FX-SCH-A-* ids, but ~600 entries (the
		// CII-DT-* "should not be present" data-type guards and the
		// CII-SR-* warning-class schema refinements) drop off the
		// per-profile cut. Importing them restores the spec
		// coverage without touching the BR-CO-* arithmetic rules
		// whose profile equivalents already fire (the baseline
		// xs:decimal forms trip the XPath engine and would produce
		// spurious firings on otherwise-conformant documents).
		//
		// The augmentation only fires for level=="EN 16931":
		// MINIMUM / BASIC WL / BASIC / EXTENDED loosen or tighten
		// the baseline requirements in ways the baseline schematron
		// can't represent (BR-12 / BR-16 mandate line items even
		// though MINIMUM legitimately omits them; CII-DT-031
		// forbids @currencyID where the BASIC profile allows it),
		// so mixing in baseline rules produces spurious firings on
		// otherwise-conformant profile documents.
		if level == "EN 16931" {
			if baseline, baseErr := LoadDefault(ctx); baseErr == nil && baseline != nil {
				s = augmentProfileWithBaseline(s, baseline)
			}
		}
		facturxProfileMem[key] = s
	})
	facturxProfileMu.Lock()
	defer facturxProfileMu.Unlock()
	return facturxProfileMem[key], facturxProfileErr[key]
}

// augmentProfileWithBaseline appends EN 16931 baseline rules to the
// profile whose canonical rule id is not yet referenced in the
// profile's assertion messages. The merge skips arithmetic-class
// baseline rules (BR-CO-* / BR-IT-* / BR-S-* / etc.) because the
// profile already carries their FX-SCH-A-* renamings and adding the
// baseline xs:decimal forms creates spurious firings under the
// antchfx XPath engine.
//
// The returned schema shares the profile's namespaces and metadata,
// then exposes one additional synthetic pattern collecting the
// imported rules. Callers treat it as a regular schema.
func augmentProfileWithBaseline(profile, baseline *schematron.Schema) *schematron.Schema {
	if profile == nil || baseline == nil {
		return profile
	}
	covered := profileRuleCoverage(profile)
	importPattern := &schematron.Pattern{ID: "__en16931_baseline_imports"}
	for _, patID := range baseline.PatternOrder {
		pat, ok := baseline.Patterns[patID]
		if !ok {
			continue
		}
		for _, rule := range pat.Rules {
			imported := importableRule(rule, covered)
			if imported != nil {
				importPattern.Rules = append(importPattern.Rules, imported)
			}
		}
	}
	if len(importPattern.Rules) == 0 {
		return profile
	}
	// Construct a new schema sharing metadata + patterns by reference,
	// then add the import pattern. The schematron loader marks Schema
	// fields as immutable after Load returns; building a fresh
	// container respects that contract while letting the new pattern
	// participate in evaluation.
	out := &schematron.Schema{
		Title:        profile.Title,
		QueryBinding: profile.QueryBinding,
		Namespaces:   profile.Namespaces,
		Patterns:     make(map[string]*schematron.Pattern, len(profile.Patterns)+1),
		PatternOrder: make([]string, 0, len(profile.PatternOrder)+1),
		Phases:       profile.Phases,
		Lets:         profile.Lets,
	}
	maps.Copy(out.Patterns, profile.Patterns)
	out.PatternOrder = append(out.PatternOrder, profile.PatternOrder...)
	out.Patterns[importPattern.ID] = importPattern
	out.PatternOrder = append(out.PatternOrder, importPattern.ID)
	return out
}

// profileRuleCoverage returns the set of canonical rule ids the
// profile already mentions in its assertion message text. The
// bundled Factur-X profile schematrons format messages as
// "[CANONICAL-ID]-explanatory text", so the prefix scan recovers
// every baseline / PEPPOL / BR-FX-EN-* rule id the profile carries
// regardless of the FX-SCH-A-* assert id used to host it.
func profileRuleCoverage(profile *schematron.Schema) map[string]struct{} {
	covered := make(map[string]struct{}, 256)
	for _, pat := range profile.Patterns {
		for _, rule := range pat.Rules {
			for _, a := range rule.Asserts {
				if id := bracketedRuleID(a.Message); id != "" {
					covered[id] = struct{}{}
				}
			}
			for _, r := range rule.Reports {
				if id := bracketedRuleID(r.Message); id != "" {
					covered[id] = struct{}{}
				}
			}
		}
	}
	return covered
}

// importableRule returns a Rule containing only the baseline rule's
// asserts and reports whose canonical ids aren't already covered by
// the profile. Arithmetic-class rule families stay out regardless
// of coverage because their xs:decimal-heavy XPath blocks trip the
// antchfx XPath engine; the profile's FX-SCH-A-* renamings of those
// rules already fire under the engine's matching rewriters.
func importableRule(rule *schematron.Rule, covered map[string]struct{}) *schematron.Rule {
	if rule == nil {
		return nil
	}
	out := &schematron.Rule{Context: rule.Context, Lets: rule.Lets}
	for _, a := range rule.Asserts {
		if !canonicalImportable(a.ID, covered) {
			continue
		}
		out.Asserts = append(out.Asserts, a)
	}
	for _, r := range rule.Reports {
		if !canonicalImportable(r.ID, covered) {
			continue
		}
		out.Reports = append(out.Reports, r)
	}
	if len(out.Asserts) == 0 && len(out.Reports) == 0 {
		return nil
	}
	return out
}

// canonicalImportable reports whether a baseline rule should be
// pulled into the augmented profile. Arithmetic-family prefixes
// stay out because the profile already carries their renamed
// equivalents; everything else imports when the id isn't already
// covered by the profile.
func canonicalImportable(id string, covered map[string]struct{}) bool {
	if id == "" {
		return false
	}
	if _, ok := covered[id]; ok {
		return false
	}
	switch {
	case strings.HasPrefix(id, "BR-CO-"),
		strings.HasPrefix(id, "BR-IT-"),
		strings.HasPrefix(id, "BR-S-"),
		strings.HasPrefix(id, "BR-Z-"),
		strings.HasPrefix(id, "BR-E-"),
		strings.HasPrefix(id, "BR-AE-"),
		strings.HasPrefix(id, "BR-AF-"),
		strings.HasPrefix(id, "BR-AG-"),
		strings.HasPrefix(id, "BR-G-"),
		strings.HasPrefix(id, "BR-I-"),
		strings.HasPrefix(id, "BR-O-"),
		strings.HasPrefix(id, "BR-DEC-"):
		return false
	}
	return true
}

// bracketedRuleID lifts the "[ID]" prefix from a profile assertion
// message. Returns the empty string when msg doesn't open with a
// bracketed identifier consisting of upper-case letters, digits, or
// dashes.
func bracketedRuleID(msg string) string {
	s := strings.TrimLeft(msg, " \t\n\r")
	if s == "" || s[0] != '[' {
		return ""
	}
	end := strings.IndexByte(s, ']')
	if end <= 1 {
		return ""
	}
	id := s[1:end]
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c != '-' && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return ""
		}
	}
	return id
}

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
// implicit fatal", distinct from the absent-key signal returned
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
	// second full schematron-tree allocation. Only <assert>/<report>
	// id and flag attributes matter, regardless of nesting depth.
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

// facturxProfileRoot maps a conformance-level string to the
// corresponding bundle root path. Returns ("", false) for unknown
// or unsupported levels (XRECHNUNG routes through [LoadXRechnung];
// every other free-form level falls back to [LoadDefault]).
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
