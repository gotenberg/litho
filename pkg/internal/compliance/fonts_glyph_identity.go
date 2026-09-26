package compliance

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// glyphRef names the glyph a glyph-level violation is about: the character
// code of a simple font, or the CID of a composite font. The zero value names
// no glyph.
type glyphRef struct {
	code  uint32
	named bool
}

// glyphFailf is [failf] for a rule veraPDF states on a Glyph object. It records
// code, so [fontJudgements.glyphs] can state the rule once per glyph.
func glyphFailf(rule Rule, code uint32, format string, args ...any) []Violation {
	return []Violation{{Rule: rule, Context: fmt.Sprintf(format, args...), glyph: glyphRef{code: code, named: true}}}
}

// glyphFaults gathers, for one rule, the first failure a glyph check finds for
// each code a content stream shows, so the check reports every failing glyph
// instead of the first. veraPDF states a Glyph rule on every glyph that fails
// it.
type glyphFaults struct {
	rule   Rule
	byCode map[uint32]Violation
}

// newGlyphFaults returns an empty collector for rule.
func newGlyphFaults(rule Rule) *glyphFaults {
	return &glyphFaults{rule: rule}
}

// add records a failure for code unless one is already recorded.
func (g *glyphFaults) add(code uint32, format string, args ...any) {
	if g.byCode == nil {
		g.byCode = make(map[uint32]Violation)
	}
	if _, ok := g.byCode[code]; ok {
		return
	}
	g.byCode[code] = glyphFailf(g.rule, code, format, args...)[0]
}

// violations returns the recorded failures in ascending code order.
func (g *glyphFaults) violations() []Violation {
	if len(g.byCode) == 0 {
		return nil
	}
	codes := make([]uint32, 0, len(g.byCode))
	for code := range g.byCode {
		codes = append(codes, code)
	}
	slices.Sort(codes)
	out := make([]Violation, 0, len(codes))
	for _, code := range codes {
		out = append(out, g.byCode[code])
	}
	return out
}

// first returns the failure for the lowest code alone, naming no glyph. A
// check that could not learn which codes a stream shows judges every code the
// font declares, and stating each of those would report glyphs nothing
// renders; it keeps to one violation, as it always did.
func (g *glyphFaults) first() []Violation {
	all := g.violations()
	if len(all) == 0 {
		return nil
	}
	v := all[0]
	v.glyph = glyphRef{}
	return []Violation{v}
}

// glyphContext is what veraPDF tells two glyphs of one font and one code apart
// by: the text rendering mode in force, the marked-content sequence around the
// show, and the structure key of the stream it is shown in ([structureKey]).
// mc is empty outside marked content.
type glyphContext struct {
	mode  int
	mc    string
	owner int32
}

// describe renders where a glyph was shown, for a violation stated for more
// than one context of the same stream.
func (g glyphContext) describe() string {
	var parts []string
	if g.mode != 0 {
		parts = append(parts, "rendering mode "+strconv.Itoa(g.mode))
	}
	switch {
	case g.mc == "":
	case g.mc == mcUnnamed:
		parts = append(parts, "in marked content")
	case strings.Contains(g.mc, "#"):
		parts = append(parts, "in the marked-content sequence at operator "+g.mc[strings.LastIndex(g.mc, "#")+1:])
	case strings.HasPrefix(g.mc, "object "):
		parts = append(parts, "in marked content whose property list is "+g.mc)
	default:
		parts = append(parts, "in marked content "+g.mc)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// glyphShow is one string a text-showing operator paints through a font, with
// the context it is painted in.
type glyphShow struct {
	str ast.PdfString
	at  glyphContext
}

// mcUnnamed identifies a marked-content sequence that carries no property
// list: a BMC, or a BDC whose operand is neither a dictionary nor a name.
// veraPDF hashes the missing list to 0, so all of them are one sequence to it.
const mcUnnamed = "none"

// showSite identifies the content stream a usage walk reads, so the
// marked-content sequences it opens can be told apart from another stream's.
// key is unique to the stream within the document; props is the /Properties
// dictionary of the stream's resources, which a BDC may name its property list
// from; owner is the stream's [structureKey]; fonts names the /Font dictionary
// of those resources ([fontDictKey]).
type showSite struct {
	key   string
	props ast.PdfDict
	owner int32
	fonts string
}

// structureKey returns the value veraPDF tells the glyphs of one content stream
// apart from another's by: the /StructParent and /StructParents entries of the
// dictionary that owns the stream, which is the page for a page's contents,
// the stream itself for a Form XObject, a tiling pattern or an annotation
// appearance, and the font for a Type 3 glyph procedure (ISO 32000-1, 14.7.4.4,
// Table 326 gives pages and content-bearing streams these entries). veraPDF
// keys a Glyph by StructureElementAccessObject.hashCode, 31 times the Java
// hash of /StructParent plus that of /StructParents, so an entry that is
// absent, zero or not an integer contributes nothing, and a page with
// /StructParents 0 shares its glyphs with a page carrying none.
//
// Measured with veraPDF 1.30 at PDF/A-2u, PDF/A-3u and PDF/UA-1 on one glyph
// with no Unicode value: shown on two pages whose /StructParents are 1 and 2,
// or 0 and 1, it fails twice, through a Type 3, a Type 1 and a composite font
// alike; on two pages with 1 and 1, 0 and none, or none and none, once. Which
// rules the key splits is [fontJudgements.glyphs]' concern.
//
// # Corpus evidence
//
//   - All corpora, at PDF/A-2u: keying the ToUnicode rules by it moved the
//     counts of ISO 19005-2, 6.2.11.7.2 t1 toward veraPDF's in 2 files and
//     those of t2 in 1, and none away; each now equals veraPDF's, a summed
//     distance of 368 and 298 before.
//   - govdocs thread_000_000028.pdf: a tagged document showing its glyphs
//     without Unicode values on pages with distinct /StructParents; litho
//     states 5450 of them, as veraPDF does, where it stated 5083.
//   - pdfcpu pkg_testdata__read.go.pdf: veraPDF states the value rule 324
//     times for the glyphs of its Type 3 fonts, page by page; litho states
//     324 where it stated 26.
func structureKey(ctx context.Context, doc *document.Document, owner ast.PdfDict) int32 {
	if owner == nil {
		return 0
	}
	entry := func(key ast.PdfName) int32 {
		obj, err := document.ResolveObject(ctx, doc, owner[key])
		if err != nil {
			return 0
		}
		n, ok := obj.(ast.PdfInt)
		if !ok {
			return 0
		}
		// Java's Long.hashCode folds the high half into the low one and keeps
		// 32 bits by definition.
		return int32(int64(n) ^ int64(uint64(n)>>32)) //nolint:gosec // the truncation is the hash being modelled.
	}
	// Signed overflow wraps in Go as it does in Java's int arithmetic.
	return 31*entry("StructParent") + entry("StructParents")
}

// markedContentKey identifies the property list of the BDC or BMC operator op,
// the index-th operator of the stream at site.
//
// veraPDF tells two glyphs apart by the property list of the sequence that
// encloses them, compared as an object rather than by value (GFOpMarkedContent
// hashes the dictionary's identity). A list written inline in the stream is a
// new object at every operator, so two sequences each carrying <</MCID 0>>
// hold two glyphs. A list named from /Properties is one object wherever it is
// named. A BMC has no list. The innermost sequence decides, and a Form
// XObject's glyphs belong to the sequences of its own stream, not to the one
// around the Do that draws it. Measured with veraPDF 1.30 at PDF/A-1b and
// PDF/A-2b: one missing glyph shown in two BDC sequences with inline lists
// fails twice, in two BMC sequences or two BDC naming one /Properties entry
// once, in two inline-list BDC nested in one BDC twice, in a BMC nested in
// each of two inline-list BDC once, and on the page and in a form drawn inside
// a BDC once.
func markedContentKey(op content.Operation, index int, site showSite) string {
	if op.Operator != "BDC" || len(op.Operands) < 2 {
		return mcUnnamed
	}
	switch v := op.Operands[1].(type) {
	case ast.PdfDict:
		return site.key + "#" + strconv.Itoa(index)
	case ast.PdfName:
		if ref, ok := site.props[v].(ast.PdfIndirectRef); ok {
			return "object " + strconv.Itoa(ref.ObjectNum)
		}
		return "/" + string(v)
	}
	return mcUnnamed
}

// fontIdentity names the font object veraPDF holds a font resource to be: the
// object a reference points at, or, for a font dictionary written directly in
// a /Font dictionary, that entry of that dictionary.
//
// veraPDF tells fonts apart by their dictionary as a Java object
// (GFIDGenerator hashes the COSDictionary, which keeps Object.hashCode), and
// judges a font object it has met before only once. A direct font is one
// object for every page or stream whose resources reach the same /Font
// dictionary, and a new one wherever a dictionary writes its own copy.
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on an unembedded
// Helvetica written directly in the resources: two pages each writing the
// resources inline fail ISO 19005-1, 6.3.4 t1 and ISO 19005-2, 6.2.11.4.1 t1
// twice, and one page naming two copies twice; two pages sharing one indirect
// /Resources, one indirect /Font dictionary or the resources of the /Pages
// node fail them once, as do two forms sharing one /Resources and one form
// drawn on two pages; a page naming an indirect font /F1 and another writing
// a copy under the same name fail them twice.
//
// # Corpus evidence
//
//   - All corpora: at PDF/A-1b and PDF/A-2b, judging each copy of a font
//     written directly in a /Font dictionary, rather than the first page that
//     names it, moves no count.
func fontIdentity(fontRef ast.PdfObject, name ast.PdfName, site showSite) string {
	if ref, ok := fontRef.(ast.PdfIndirectRef); ok {
		return "obj " + strconv.Itoa(ref.ObjectNum)
	}
	fonts := site.fonts
	if fonts == "" {
		fonts = site.key + "/Resources/Font"
	}
	return fonts + "/" + string(name)
}

// fontDictKey names the /Font dictionary of the resources res, which the
// object holder names writes as rawRes: the object a reference points at, or
// the path to the dictionary from the nearest object that writes it directly.
// Two streams whose resources reach one dictionary get one key, which is what
// makes a font written directly in it one font; see [fontIdentity].
func fontDictKey(holder string, rawRes ast.PdfObject, res ast.PdfDict) string {
	if ref, ok := res["Font"].(ast.PdfIndirectRef); ok {
		return "obj " + strconv.Itoa(ref.ObjectNum)
	}
	if ref, ok := rawRes.(ast.PdfIndirectRef); ok {
		return "obj " + strconv.Itoa(ref.ObjectNum) + "/Font"
	}
	return holder + "/Resources/Font"
}

// descendantIdentity names the descendant CIDFont descendant, the first entry
// of the /DescendantFonts array of the composite font id names: the object a
// reference points at, or that entry of the composite font. veraPDF states the
// rules of a CIDFont on that object, once however many composite fonts share
// it.
func descendantIdentity(id string, descendant ast.PdfObject) string {
	if ref, ok := descendant.(ast.PdfIndirectRef); ok {
		return "obj " + strconv.Itoa(ref.ObjectNum)
	}
	return id + "/DescendantFonts"
}

// glyphKey identifies one glyph-level statement: a rule, a font, a character
// code and the context it is shown in.
type glyphKey struct {
	rule Rule
	font string
	code uint32
	at   glyphContext
}

// shownGlyph is one character code a stream shows through a font, with the
// glyph the checks judge it by (the code itself for a simple font, its CID for
// a composite one) and the context it is shown in.
type shownGlyph struct {
	code  uint32
	glyph uint32
	at    glyphContext
}

// noContext stands for a glyph a check named but the stream's shows do not
// decode to, so it is stated once per font and code.
var noContext = glyphContext{mode: -1}

// glyphs states the violations the glyph checks raised against one content
// stream's use of a font, each once per glyph veraPDF tells apart. A violation
// that names no glyph passes through unchanged.
//
// veraPDF builds a Glyph object for each code a text-showing operator paints,
// identified by the font object, the code, the text rendering mode and the
// marked-content sequence around the show (GFIDGenerator), and judges an
// object it meets again only once. The font rules veraPDF states on a Glyph
// (ISO 19005-1, 6.3.5 t1, 6.3.6 t1 and 6.3.8 t1, ISO 19005-2 and ISO 19005-3,
// 6.2.11.4.1 t2, 6.2.11.5 t1, 6.2.11.7.2 and 6.2.11.8 t1, and their ISO
// 19005-4 and ISO 14289 counterparts) are therefore stated once per such glyph
// across the whole document, for every check that names the glyph it fails
// with [glyphFailf]: once for a code two pages show through one font, once per
// rendering mode it is shown in, once per marked-content sequence ([markedContentKey])
// and once per font object that shows it. The rules that exempt text
// rendering mode 3 (ISO 19005-2, 6.2.11.4.1 t2 and 6.2.11.5 t1 begin
// "renderingMode == 3 ||") state nothing for a glyph shown only in that mode.
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on one missing glyph:
// shown on two pages, or twice on one, it fails once; in rendering modes 0, 1,
// 2 and 3 three times; through two font objects that share one program twice,
// and through one object named twice once; on the page and in a Form XObject,
// or in one form drawn on two pages, once.
//
// veraPDF's identity also carries the structure key of the stream a glyph is
// shown in ([structureKey]), and the ToUnicode rules (ISO 19005-2 and ISO
// 19005-3, 6.2.11.7.2, ISO 19005-1, 6.3.8, ISO 14289-1, 7.21.7, ISO 14289-2,
// 8.4.5.8 and ISO 19005-4, 6.2.10.7) state a glyph once per key. The presence
// and width rules do not: measured with veraPDF 1.30 at PDF/A-2b, a CID an
// embedded composite program lacks and a Type 1 width that disagrees with its
// program each fail once on two pages whose /StructParents are 1 and 2, so the
// key is left out of theirs.
//
// TODO: the .notdef rule (ISO 19005-2, 6.2.11.8) splits by the key too: the
// same composite fixture fails it twice at PDF/A-2u. Key it once its counts
// are measured over the corpora.
//
// A composite font's checks judge CIDs, while veraPDF names a glyph by its
// character code, so a CID is stated once for every code that selects it:
// codes an embedded CMap leaves unmapped all select CID 0 and are as many
// glyphs.
//
// A font written directly in a /Font dictionary is identified by that entry of
// that dictionary ([fontIdentity]), as veraPDF identifies it by the dictionary
// object. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on a Type1
// subset whose /Widths disagree with its program for two codes: two pages
// each writing the font inline fail the width rule four times, and two pages
// or two forms sharing one /Resources twice.
//
// # Corpus evidence
//
//   - All corpora: at PDF/A-1b and PDF/A-2b, and at their own profile for the
//     veraPDF files, the counts of ISO 19005-1, 6.3.5 t1 and ISO 19005-2,
//     6.2.11.4.1 t2 moved toward veraPDF's for 15 pairs of file and profile
//     and away for none; their summed distance from veraPDF fell from 382 to
//     236.
//   - pdfjs bug920426.pdf: shows 13 codes its embedded CMap leaves unmapped,
//     and veraPDF fails ISO 19005-1, 6.3.5 t1, ISO 19005-2, 6.2.11.4.1 t2 and
//     6.2.11.8 t1 13 times each, where litho failed each once, for CID 0.
//   - All corpora: the counts of ISO 19005-1, 6.3.6 t1, ISO 19005-2, 6.2.11.5
//     t1 and ISO 19005-4, 6.2.10.5 t1 moved toward veraPDF's for 68 pairs of
//     file and profile and away for 12, six files at each of PDF/A-1b and
//     PDF/A-2b; their summed distance fell from 3309 to 1090.
//   - govdocs thread_300_300035.pdf, thread_300_300083.pdf and
//     thread_300_300084.pdf: the width counts that moved away. Each embeds
//     Type 1 programs whose /FontMatrix makes the advances fractional, and
//     veraPDF truncates them: litho states 24, 30 and 37 glyphs against
//     veraPDF's 1, 12 and 12, and truncating litho's own program widths
//     leaves exactly veraPDF's 1, 12 and 12.
//   - govdocs thread_200_200155.pdf: the same truncation accounts for 146 of
//     the 217 width glyphs litho states, against veraPDF's 69.
//   - pdfjs issue19835.pdf: 28 of the 278 width glyphs litho states, against
//     veraPDF's 204, come from a CIDFontType2 program with no cmap table, on
//     page 8, where veraPDF reports none, and 44 more pass once truncated.
//   - pdfjs issue7074_reduced.pdf: embeds a program whose post table is cut
//     short, which veraPDF does not judge widths against; litho reads its
//     hmtx and states 14 glyphs where it stated 1.
//   - All corpora: the counts of ISO 19005-2, 6.2.11.8 t1 moved toward
//     veraPDF's in 34 files and away in none; their summed distance fell from
//     2371 to 28.
//   - All corpora but verapdf, at PDF/A-2u: the counts of ISO 19005-2,
//     6.2.11.7.2 t1 moved toward veraPDF's in 273 files and away in 3; their
//     summed distance fell from 23493 to 2634. Those of 6.2.11.7.2 t2 moved
//     toward in 8 files and away in 1; their distance fell from 1493 to 1094.
//   - pdfcpu pkg_testdata__read.go.pdf: the t2 count that moved away, from 164
//     to 26 against veraPDF's 324, because veraPDF tells the glyphs of its
//     Type 3 fonts apart by the /StructParents of each page that shows them;
//     see [structureKey].
//   - verapdf: the ToUnicode presence counts at PDF/A-1a, PDF/A-2u, PDF/UA-1
//     and PDF/UA-2, a distance of 27 from veraPDF's, now match them.
//   - govdocs thread_200_200408.pdf: a ToUnicode count that moved away, from
//     26 to 15 against veraPDF's 181, because litho stated nothing for the
//     Type 3 fonts whose /ToUnicode leaves shown codes out; see
//     [checkType3ToUnicode]. For the fonts it did judge the counts moved
//     toward veraPDF's: T13 from 22 to 10 against 8, T18 from 3 to 4 against 4.
//   - pdfjs issue5010.pdf: its /ToUnicode takes most mappings from the
//     Adobe-Korea1-UCS2 base CMap it names with usecmap, which litho did not
//     load, so litho stated 10 unmapped codes where veraPDF states 5; see
//     [resolveUsedCMaps].
func (j *fontJudgements) glyphs(ctx context.Context, doc *document.Document, fontID string, font ast.PdfDict, usage *fontUsageInfo, profile string, vs []Violation) []Violation {
	var out []Violation
	var where map[uint32][]shownGlyph
	for _, v := range vs {
		if !v.glyph.named {
			out = append(out, v)
			continue
		}
		if where == nil {
			where = shownGlyphs(ctx, doc, font, usage)
		}
		judgedGlyph := v.glyph.code
		v.glyph = glyphRef{}
		shown := where[judgedGlyph]
		if len(shown) == 0 {
			shown = []shownGlyph{{code: judgedGlyph, glyph: judgedGlyph, at: noContext}}
		}
		renderedOnly := v.Rule == glyphPresenceRule(profile) || v.Rule == glyphWidthRule(profile)
		byOwner := v.Rule == toUnicodeRule(profile) || v.Rule == toUnicodeCMapValuesRule(profile)
		var fresh []shownGlyph
		for _, g := range shown {
			if renderedOnly && g.at.mode == invisibleRenderingMode {
				continue
			}
			at := g.at
			if !byOwner {
				at.owner = 0
			}
			key := glyphKey{rule: v.Rule, font: fontID, code: g.code, at: at}
			if j.glyph[key] {
				continue
			}
			j.glyph[key] = true
			fresh = append(fresh, g)
		}
		for _, g := range fresh {
			w := v
			if len(fresh) > 1 {
				w.Context += g.describe()
			}
			out = append(out, w)
		}
	}
	return out
}

// describe renders where a glyph stated for more than one code or context of
// the same stream was shown.
func (g shownGlyph) describe() string {
	at := g.at.describe()
	if g.code == g.glyph {
		return at
	}
	code := " (code <" + strings.ToUpper(strconv.FormatUint(uint64(g.code), 16)) + ">)"
	if at == "" {
		return code
	}
	return code + at
}

// invisibleRenderingMode is text rendering mode 3, neither fill nor stroke
// (ISO 32000-1, 9.3.6, Table 106).
const invisibleRenderingMode = 3

// shownGlyphs maps each glyph the checks judge, the character code of a
// simple font or the CID of a composite one, to the distinct character codes
// and contexts the shows of usage select it with. It is nil when the codes
// cannot be read.
func shownGlyphs(ctx context.Context, doc *document.Document, font ast.PdfDict, usage *fontUsageInfo) map[uint32][]shownGlyph {
	if usage == nil || len(usage.shows) == 0 {
		return nil
	}
	split := func(s ast.PdfString) []shownGlyph {
		codes := singleByteCodes([]byte(s))
		out := make([]shownGlyph, len(codes))
		for i, c := range codes {
			out[i] = shownGlyph{code: c, glyph: c}
		}
		return out
	}
	if fontSubtype(ctx, doc, font) == "Type0" {
		if split = type0ShownGlyphs(ctx, doc, font); split == nil {
			return nil
		}
	}
	seen := make(map[shownGlyph]bool)
	out := make(map[uint32][]shownGlyph)
	for _, show := range usage.shows {
		for _, g := range split(show.str) {
			g.at = show.at
			if seen[g] {
				continue
			}
			seen[g] = true
			out[g.glyph] = append(out[g.glyph], g)
		}
	}
	return out
}

// type0ShownGlyphs returns how a composite font's /Encoding turns a shown
// string into its character codes and the CIDs they select, decoding as
// [collectType0CIDs] does: two bytes per code, each its own CID, under
// Identity-H or Identity-V, through the CMap stream an embedded encoding holds,
// and through the bundled predefined CMap otherwise. A code that selects no
// CID litho can name is left out. It returns nil when the encoding cannot be
// read.
func type0ShownGlyphs(ctx context.Context, doc *document.Document, font ast.PdfDict) func(ast.PdfString) []shownGlyph {
	encName, _ := document.ResolveName(ctx, doc, font["Encoding"])
	switch encName {
	case "Identity-H", "Identity-V":
		return func(s ast.PdfString) []shownGlyph {
			data := []byte(s)
			out := make([]shownGlyph, 0, len(data)/2)
			for i := 0; i+1 < len(data); i += 2 {
				cid := uint32(data[i])<<8 | uint32(data[i+1])
				out = append(out, shownGlyph{code: cid, glyph: cid})
			}
			return out
		}
	case "":
		stream, err := document.ResolveStream(ctx, doc, font["Encoding"])
		if err != nil {
			return nil
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			doc.Logger.Debug("compliance: fonts: cannot decode Type0 encoding CMap", "error", err)
			return nil
		}
		embedded := cmap.ParseEmbeddedCIDMap(ctx, data)
		if embedded == nil {
			return nil
		}
		return func(s ast.PdfString) []shownGlyph {
			return zipCodesAndCIDs(embedded, []byte(s), embedded.UnmappedNotdef())
		}
	default:
		predefined := cmap.LoadPredefinedCIDMap(string(encName))
		if predefined == nil {
			return nil
		}
		return func(s ast.PdfString) []shownGlyph {
			return zipCodesAndCIDs(predefined, []byte(s), false)
		}
	}
}

// zipCodesAndCIDs pairs each character code m reads from data with the CID it
// selects. [cmap.CIDMap.Codes] and [cmap.CIDMap.Decode] split data the same
// way, one entry per code. An unmapped code selects CID 0 when unmappedNotdef
// holds and is left out otherwise.
func zipCodesAndCIDs(m *cmap.CIDMap, data []byte, unmappedNotdef bool) []shownGlyph {
	codes, cids := m.Codes(data), m.Decode(data)
	out := make([]shownGlyph, 0, len(cids))
	for i, c := range cids {
		if i >= len(codes) {
			break
		}
		switch {
		case !c.Mapped:
			if unmappedNotdef {
				out = append(out, shownGlyph{code: codes[i], glyph: 0})
			}
		case c.CID >= 0 && c.CID <= maxCID:
			out = append(out, shownGlyph{code: codes[i], glyph: uint32(c.CID)})
		}
	}
	return out
}
