package compliance

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// deviceNComponentRule returns the rule for DeviceN component count limit.
func deviceNComponentRule(part int) Rule {
	// PDF/A-1 §6.1.12 t9; PDF/A-2/3 §6.1.13 t9. ISO 19005-4 states no
	// DeviceN colourant limit at all, so part 4 has no rule to report:
	// emitting the part-1 id there flagged a construct that part permits.
	switch part {
	case 2:
		return Rule_19005_2_6_1_13_t09
	case 3:
		return Rule_19005_3_6_1_13_t09
	case 4:
		return Rule{}
	default:
		return Rule_19005_1_6_1_12_t09
	}
}

// separationConsistencyRule selects §6.2.4.4 t2: all Separation arrays with the
// same colorant name share alternateSpace and tintTransform.
//
// # Corpus evidence
//
//   - All corpora: emitting t1 here was a 36-cell litho-only mislabel.
func separationConsistencyRule(part int) Rule {
	// §6.2.4.4 t2: all Separation arrays with the same colorant name shall
	// share alternateSpace and tintTransform. t1 is the separate
	// Colorants-dictionary presence rule, a distinct sub-test of the same
	// clause, so this rule must not emit t1.
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_4_4_t02, Rule_19005_3_6_2_4_4_t02, Rule_19005_4_6_2_4_4_t02)
}

// separationColorantsRule selects §6.2.4.4 t1: any spot colour used in a
// DeviceN or NChannel colour space needs an entry in the Colorants dict.
func separationColorantsRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_4_4_t01, Rule_19005_3_6_2_4_4_t01, Rule_19005_4_6_2_4_4_t01)
}

// checkSeparationDeviceN validates the Separation and DeviceN rules on the
// colour-space links veraPDF makes (see [colorLinkWalk]): the DeviceN
// colourant limit, ISO 19005-1 §6.1.12 t9 and ISO 19005-2/3 §6.1.13 t9, ISO
// 19005-2 §6.2.4.4 t1 and the name length limit of the colorant names every
// link reads again, ISO 19005-1 §6.1.12 t4 and ISO 19005-2/3 §6.1.13 t4 (see
// [checkDeviceNLinks]), and ISO 19005-2 §6.2.4.4 t2 (see
// [checkSeparationLinks]).
func checkSeparationDeviceN(ctx context.Context, doc *document.Document, pages []pageCtx, profile string, results *[]Violation) {
	part := PDFAPart(profile)
	checkDeviceNLinks(ctx, doc, pages, profile, results)
	checkSeparationLinks(ctx, doc, pages, part, results)
}

// deviceNColorantLimit returns the most colourants a DeviceN colour space may
// name under a PDF/A part: 8 for part 1 (ISO 19005-1 §6.1.12, from PDF
// Reference 1.4 Table C.1) and 32 for parts 2 and 3 (ISO 19005-2/3 §6.1.13,
// from ISO 32000-1 Annex C, Table C.1).
func deviceNColorantLimit(part int) int {
	if part == 1 {
		return 8
	}
	return 32
}

// ISO 32000-1 §8.7.3.1, Table 75: /PatternType 1 is a tiling pattern. Per
// §8.7.3.3 its /PaintType 2 is the uncoloured form, whose cell specifies no
// colour of its own.
const (
	tilingPatternType   = 1
	uncolouredPaintType = 2
)

// separationDef is the definition a Separation colour space gives its
// colourant: the family of its alternate space and its tint transform. An
// alternate that names a /ColorSpace entry is the family of that entry (see
// [resolveColorSpaceName]).
type separationDef struct {
	alternate string
	tint      ast.PdfObject
}

// separationDefOf returns the colourant a Separation array arr names and the
// definition it gives it, or false when arr names no colourant. colorSpaces is
// the /ColorSpace dictionary a name the array holds resolves against.
func separationDefOf(ctx context.Context, doc *document.Document, arr ast.PdfArray, colorSpaces ast.PdfDict) (string, separationDef, bool) {
	if len(arr) < 4 {
		return "", separationDef{}, false
	}
	name, err := document.ResolveName(ctx, doc, arr[1])
	if err != nil || name == "" {
		return "", separationDef{}, false
	}
	alternate := resolveCSName(ctx, doc, resolveColorSpaceName(ctx, doc, arr[2], colorSpaces))
	return string(name), separationDef{alternate: alternate, tint: arr[3]}, true
}

// checkSeparationLinks validates §6.2.4.4 t2: every Separation colour space
// naming a colourant shall give it the same alternate space and tint
// transform.
//
// veraPDF records a Separation when it builds it and judges it where a link
// reaches it (see [colorLinkWalk]): a direct one at every link, an indirect one
// once. A Separation fails as soon as one recorded for its colourant defines it
// differently, and every later one naming that colourant fails with it,
// whichever definition it gives. veraPDF builds the colour space of every
// operator of a content stream when it parses the stream, before judging any,
// and a Separation an /Indexed base, a DeviceN or an image holds when it
// reaches that space (see [colorLinkWalk.declare]). Measured with veraPDF 1.30
// at PDF/A-2b: two direct Separations naming one colourant over different
// alternate spaces, each selected by cs, set by scn and filled by f, fail the
// test six times, both of them from their first link.
//
// Alternate spaces compare by family and tint transforms by content (see
// [tintTransformEqual]). veraPDF compares the alternate spaces it reads, so an
// alternate naming a /ColorSpace entry compares as that entry: measured with
// veraPDF 1.30 at PDF/A-2b and PDF/A-4, a Separation over /CS0, where /CS0 is
// /DeviceCMYK, and one over /DeviceCMYK, naming one colourant through one tint
// transform, pass the test.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-2-4-4-t03-fail-a.pdf: two direct
//     Separations naming /Red over different tint transforms, that seven
//     operators link to, fail the test seven times, where litho failed it
//     once, for the second.
func checkSeparationLinks(ctx context.Context, doc *document.Document, pages []pageCtx, part int, results *[]Violation) {
	// §6.2.4.4 exists for parts 2+ only; a zero Rule must never be emitted
	// (it serialized as a clause-less "-t00" residual).
	rule := separationConsistencyRule(part)
	if rule.Specification == "" {
		return
	}
	defs := make(map[string][]separationDef)
	inconsistent := make(map[string]bool)
	same := func(a, b separationDef) bool {
		return a.alternate == b.alternate && tintTransformEqual(ctx, doc, a.tint, b.tint)
	}
	record := func(arr ast.PdfArray, colorSpaces ast.PdfDict) (string, separationDef, bool) {
		name, def, ok := separationDefOf(ctx, doc, arr, colorSpaces)
		if !ok {
			return "", separationDef{}, false
		}
		for _, known := range defs[name] {
			if same(known, def) {
				return name, def, true
			}
		}
		defs[name] = append(defs[name], def)
		return name, def, true
	}
	walkColorLinksDeclaring(ctx, doc, pages, part, func(n colorNode) {
		if n.kind != "Separation" {
			return
		}
		name, def, ok := record(n.arr, n.colorSpaces)
		if !ok {
			return
		}
		for _, known := range defs[name] {
			if !same(known, def) {
				inconsistent[name] = true
				if known.alternate != def.alternate {
					*results = append(*results, failf(rule,
						"page %d: Separation colorant %q uses alternate %q but previously used %q",
						n.page+1, name, def.alternate, known.alternate)...)
				} else {
					*results = append(*results, failf(rule,
						"page %d: Separation colorant %q has different tintTransform", n.page+1, name)...)
				}
				return
			}
		}
		if inconsistent[name] {
			*results = append(*results, failf(rule,
				"page %d: Separation colorant %q has different tintTransform or alternateSpace than another", n.page+1, name)...)
		}
	}, func(arr ast.PdfArray, colorSpaces ast.PdfDict) {
		record(arr, colorSpaces)
	})
}

// tintTransformEqual compares two tint transform function objects for
// equivalence. Per the spec, the comparison is structural (same dict keys/values
// and stream content), ignoring compression and indirect/direct status.
func tintTransformEqual(ctx context.Context, doc *document.Document, a, b ast.PdfObject) bool {
	// Try as streams (Type 4 PostScript calculator functions).
	sa, errA := document.ResolveStream(ctx, doc, a)
	sb, errB := document.ResolveStream(ctx, doc, b)
	if errA == nil && errB == nil {
		return functionStreamEqual(ctx, doc, sa, sb)
	}

	// Try as dicts (Type 0/2/3 functions).
	da, errA := document.ResolveDict(ctx, doc, a)
	db, errB := document.ResolveDict(ctx, doc, b)
	if errA == nil && errB == nil && da != nil && db != nil {
		return functionDictEqual(ctx, doc, da, db)
	}

	return false
}

// functionStreamEqual compares two function streams (Type 4 PostScript).
func functionStreamEqual(ctx context.Context, doc *document.Document, a, b ast.PdfStream) bool {
	// Compare decoded stream bytes.
	dataA, errA := doc.DecodeStream(ctx, a)
	dataB, errB := doc.DecodeStream(ctx, b)
	if errA != nil || errB != nil {
		return false
	}
	if !bytes.Equal(dataA, dataB) {
		return false
	}
	// Compare function dict keys (FunctionType, Domain, Range, etc.)
	// ignoring stream-specific keys (Length, Filter, DecodeParms).
	return functionDictEqual(ctx, doc, a.Dict, b.Dict)
}

// functionDictEqual compares two function dictionaries, ignoring
// stream-encoding keys (Length, Filter, DecodeParms, DL).
func functionDictEqual(ctx context.Context, doc *document.Document, a, b ast.PdfDict) bool {
	skip := map[ast.PdfName]bool{"Length": true, "Filter": true, "DecodeParms": true, "DL": true}
	keysA := 0
	for k, vA := range a {
		if skip[k] {
			continue
		}
		keysA++
		vB, ok := b[k]
		if !ok {
			return false
		}
		if !pdfObjectEqual(ctx, doc, vA, vB) {
			return false
		}
	}
	keysB := 0
	for k := range b {
		if !skip[k] {
			keysB++
		}
	}
	return keysA == keysB
}

// deviceNProcessColorants are the colorant names a DeviceN array may carry
// without a /Colorants entry because they name process colorants rather than
// spot colours. ISO 32000-1 §8.6.6.4 defines the DeviceCMYK process colorant
// names as Cyan, Magenta, Yellow and Black; §8.6.6.5 lets an NChannel
// /Process /Components array name further process colorants, collected
// separately in [deviceNAllowedColorants]. veraPDF's PDDeviceN model exempts
// this exact four-name set, so the additive names Red, Green and Blue are spot
// colours here and do require a /Colorants entry.
//
//nolint:gochecknoglobals // immutable lookup set.
var deviceNProcessColorants = map[string]bool{
	"Cyan":    true,
	"Magenta": true,
	"Yellow":  true,
	"Black":   true,
}

// checkDeviceNLinks validates the rules veraPDF judges on each PDDeviceN it
// reaches: the colourant limit, ISO 19005-1 §6.1.12 t9 and ISO 19005-2/3
// §6.1.13 t9 (see [deviceNColorantLimit]), and §6.2.4.4 t1, every spot
// colourant a DeviceN names needs an entry in the /Colorants dictionary of its
// attributes. Each fails once per link [colorLinkWalk] follows to a DeviceN
// that breaks it, however far it breaks it: a direct DeviceN wherever a link
// reaches it, an indirect one once for the whole document, in the state the
// link walk follows, so a DeviceN an operator selects with its colour
// operators off is never reached, while the image it paints is. The same links
// to a Separation or a DeviceN read its colorant names again, which the name
// length limit, ISO 19005-1 §6.1.12 t4 and ISO 19005-2/3 §6.1.13 t4, judges
// (see [colorantNameReads]). The limit violations come first, then the
// /Colorants ones, then the colorant names.
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b:
//
//   - A direct DeviceN naming 33 spot colourants that cs selects and f fills
//     fails each test twice, where litho failed t1 once per colourant.
//   - A DeviceN image painted in the cell of an uncoloured tiling pattern, in
//     a coloured cell beside an uncoloured one, or in a form such a cell
//     paints, fails the limit once, and one a cs of those streams selects
//     fails nothing; an /Indexed image or /Resources/ColorSpace entry whose
//     base names a DeviceN entry over the limit fails it as often as the
//     /Indexed is reached.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-2-4-4-t02-fail-a.pdf: a direct
//     DeviceN naming three spot colourants and no /Colorants that seven
//     operators link to fails t1 seven times, where litho failed it once per
//     colourant, three times.
//   - verapdf PDF_A-1b__veraPDF_test_suite_6-1-12-t09-fail-a.pdf: a direct
//     12-colourant DeviceN that CS selects, SCN sets and B strokes fails the
//     limit three times.
//   - litho limits_2b.pdf: a direct 33-colourant DeviceN that cs selects and f
//     fills fails the limit twice under PDF/A-1b and PDF/A-2b.
//   - All corpora: judging the limit on the links, where a walk of the
//     resources judged it, moves no count at PDF/A-1b or PDF/A-2b.
func checkDeviceNLinks(ctx context.Context, doc *document.Document, pages []pageCtx, profile string, results *[]Violation) {
	// §6.2.4.4 t1 exists for parts 2+ only, and ISO 19005-4 states no
	// colourant limit nor name length limit; a zero Rule must never be
	// emitted (it serializes as a clause-less "-t00" residual).
	part := PDFAPart(profile)
	limitRule, colorantsRule := deviceNComponentRule(part), separationColorantsRule(part)
	if limitRule.Specification == "" && colorantsRule.Specification == "" && nameLengthRule(profile).Specification == "" {
		return
	}
	limit := deviceNColorantLimit(part)
	names := newColorantNameReads(profile)
	colorants := newDeviceNColorantsReads(colorantsRule)
	var overLimit, missing, longNames []Violation
	walkColorLinks(ctx, doc, pages, part, func(n colorNode) {
		if n.kind == "Separation" || n.kind == "DeviceN" {
			longNames = append(longNames, names.judge(ctx, doc, n)...)
		}
		if n.kind != "DeviceN" {
			return
		}
		if limitRule.Specification != "" {
			if count, ok := deviceNColorantCount(ctx, doc, n.arr); ok && count > limit {
				overLimit = append(overLimit, failf(limitRule,
					"page %d: DeviceN has %d components (max %d)", n.page+1, count, limit)...)
			}
		}
		if colorantsRule.Specification == "" {
			return
		}
		missing = append(missing, colorants.judge(ctx, doc, n)...)
	})
	*results = append(*results, overLimit...)
	*results = append(*results, missing...)
	*results = append(*results, longNames...)
}

// deviceNColorantCount returns how many colourants the DeviceN array arr
// names, the length of its names array (ISO 32000-1, 8.6.6.5), or false when
// it names none that resolve.
func deviceNColorantCount(ctx context.Context, doc *document.Document, arr ast.PdfArray) (int, bool) {
	if len(arr) < 2 {
		return 0, false
	}
	names, err := document.ResolveArray(ctx, doc, arr[1])
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve DeviceN names array", "error", err)
		return 0, false
	}
	return len(names), true
}

// maxMissingColorantsListed caps how many of the spot colorants a DeviceN
// leaves out of its /Colorants dictionary the context of a failure of the
// /Colorants rule names, and [MaxNameBytes] caps how many bytes of each name
// it quotes. Anti-DoS guard, not spec-mandated: the context is built once for
// each page that links to the colour space (see [deviceNColorantsReads]), and
// a crafted DeviceN can name thousands of colorants, each as long as the file
// allows. It equals the most colorants ISO 32000-1, Annex C, Table C.1 lets a
// DeviceN name, [maxDeviceN2], so every DeviceN within that limit is listed
// whole; the rest are counted.
const maxMissingColorantsListed = maxDeviceN2

// deviceNColorantsReads judges the /Colorants rule, ISO 19005-2/3, 6.2.4.4 t1,
// at every link [walkColorLinks] follows to a DeviceN colour space. The rule
// fails once per link, as [checkDeviceNLinks] measured veraPDF doing, but what
// fails depends on the colour space alone: listed remembers, by the colour
// space array, the spot colorants it leaves out, and failures, by the array
// and the page, the failure one link adds. A link then costs no more than the
// failure it adds, where listing the colorants anew at each one made a
// DeviceN naming 1000 of them that 100000 operators reach cost 63 s and 5.5
// GB.
type deviceNColorantsReads struct {
	rule     Rule
	listed   map[*ast.PdfObject]string
	failures map[deviceNPageLink][]Violation
}

// deviceNPageLink is a DeviceN colour space array, by its first element, as a
// link on a page reaches it.
type deviceNPageLink struct {
	arr  *ast.PdfObject
	page int
}

// newDeviceNColorantsReads returns the reads of a validation run whose
// /Colorants rule is rule, the zero Rule for a part that states none.
func newDeviceNColorantsReads(rule Rule) *deviceNColorantsReads {
	return &deviceNColorantsReads{
		rule:     rule,
		listed:   make(map[*ast.PdfObject]string),
		failures: make(map[deviceNPageLink][]Violation),
	}
}

// judge returns the failure of the /Colorants rule the link n, to a DeviceN
// colour space, adds, or nil when its spot colorants all have an entry.
func (r *deviceNColorantsReads) judge(ctx context.Context, doc *document.Document, n colorNode) []Violation {
	if r.rule.Specification == "" || len(n.arr) < 2 {
		return nil
	}
	key := deviceNPageLink{arr: &n.arr[0], page: n.page}
	if failures, ok := r.failures[key]; ok {
		return failures
	}
	listed, ok := r.listed[key.arr]
	if !ok {
		listed = listColorants(deviceNMissingColorants(ctx, doc, n.arr))
		r.listed[key.arr] = listed
	}
	var failures []Violation
	if listed != "" {
		failures = failf(r.rule, "page %d: DeviceN spot colorants %s have no Colorants entry", n.page+1, listed)
	}
	r.failures[key] = failures
	return failures
}

// listColorants quotes names for the context of a failure, the first
// [maxMissingColorantsListed] of them and each cut to [MaxNameBytes] bytes,
// and counts the rest. No name gives "".
func listColorants(names []string) string {
	quoted := make([]string, 0, min(len(names), maxMissingColorantsListed))
	for _, name := range names[:min(len(names), maxMissingColorantsListed)] {
		if len(name) > MaxNameBytes {
			name = name[:MaxNameBytes] + "..."
		}
		quoted = append(quoted, fmt.Sprintf("%q", name))
	}
	listed := strings.Join(quoted, ", ")
	if more := len(names) - len(quoted); more > 0 {
		listed += fmt.Sprintf(" and %d more", more)
	}
	return listed
}

// deviceNMissingColorants returns the spot colorants the DeviceN array arr
// names that the /Colorants dictionary of its attributes does not list.
func deviceNMissingColorants(ctx context.Context, doc *document.Document, arr ast.PdfArray) []string {
	if len(arr) < 2 {
		return nil
	}
	names, err := document.ResolveArray(ctx, doc, arr[1])
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve DeviceN names array", "error", err)
		return nil
	}

	allowed := deviceNAllowedColorants(ctx, doc, arr)
	var missing []string
	for _, nameObj := range names {
		colorantName, err := document.ResolveName(ctx, doc, nameObj)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot resolve colorant name", "error", err)
			continue
		}
		cName := string(colorantName)
		// ISO 32000-1 §8.6.6.5: None marks a component that never paints, so
		// it names no ink and needs no Colorants entry.
		if cName == "None" || deviceNProcessColorants[cName] || allowed[cName] {
			continue
		}
		missing = append(missing, cName)
	}
	return missing
}

// deviceNAllowedColorants returns the colorant names a DeviceN array declares
// through its attributes dictionary: the keys of /Colorants plus the process
// colorants named in /Process /Components (ISO 32000-1 §8.6.6.5, Tables 72-73).
func deviceNAllowedColorants(ctx context.Context, doc *document.Document, arr ast.PdfArray) map[string]bool {
	allowed := make(map[string]bool)
	if len(arr) < 5 {
		return allowed
	}
	attrs, err := document.ResolveDict(ctx, doc, arr[4])
	if err != nil || attrs == nil {
		return allowed
	}

	if colorants, err := document.ResolveDict(ctx, doc, attrs["Colorants"]); err == nil {
		for name := range colorants {
			allowed[string(name)] = true
		}
	}

	process, err := document.ResolveDict(ctx, doc, attrs["Process"])
	if err != nil || process == nil {
		return allowed
	}
	components, err := document.ResolveArray(ctx, doc, process["Components"])
	if err != nil {
		return allowed
	}
	for _, compObj := range components {
		name, err := document.ResolveName(ctx, doc, compObj)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot resolve DeviceN process component", "error", err)
			continue
		}
		allowed[string(name)] = true
	}
	return allowed
}
